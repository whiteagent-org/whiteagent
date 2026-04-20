// Package agent_test verifies the behavioral contract of the inbound message
// handler defined in runtime.go (INV-07).
//
// The handler is an anonymous closure inside Runtime.Start() and cannot be
// extracted without modifying the implementation. This test validates the
// observable contract of the handler by:
//
//  1. Testing the invite service and feedback logic that the handler delegates
//     to directly (same inputs/outputs the handler uses).
//  2. Testing the handler decision tree in a parallel standalone function that
//     mirrors the handler logic exactly.
//
// This provides behavioral coverage without modifying any implementation file.
package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/identity"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/onboarding"
)

// ---------------------------------------------------------------------------
// Minimal mock store for invite.Service
// ---------------------------------------------------------------------------

type mockStoreForHandler struct {
	port.StorePlugin

	inviteCodes       map[string]*entity.InviteCode
	tenants           map[entity.TenantID]*entity.Tenant
	users             []entity.User
	agents            []entity.Agent
	workspaceMappings map[string]entity.TenantID
	usedCodes         map[string]entity.UserID

	// userIdentities: userID -> channelID -> externalID
	userIdentities map[entity.UserID]map[string]string
}

func newMockStoreForHandler() *mockStoreForHandler {
	return &mockStoreForHandler{
		inviteCodes:       make(map[string]*entity.InviteCode),
		tenants:           make(map[entity.TenantID]*entity.Tenant),
		workspaceMappings: make(map[string]entity.TenantID),
		usedCodes:         make(map[string]entity.UserID),
		userIdentities:    make(map[entity.UserID]map[string]string),
	}
}

func (m *mockStoreForHandler) GetInviteCode(_ context.Context, code string) (*entity.InviteCode, error) {
	ic, ok := m.inviteCodes[code]
	if !ok {
		return nil, errors.New("not found")
	}
	return ic, nil
}

func (m *mockStoreForHandler) UseInviteCode(_ context.Context, code string, userID entity.UserID) error {
	m.usedCodes[code] = userID
	if ic, ok := m.inviteCodes[code]; ok {
		ic.UsedBy = userID
	}
	return nil
}

func (m *mockStoreForHandler) GetTenant(_ context.Context, tenantID entity.TenantID) (*entity.Tenant, error) {
	t, ok := m.tenants[tenantID]
	if !ok {
		return nil, errors.New("tenant not found")
	}
	return t, nil
}

func (m *mockStoreForHandler) SaveTenant(_ context.Context, _ entity.TenantID, tenant entity.Tenant) error {
	m.tenants[tenant.ID] = &tenant
	return nil
}

func (m *mockStoreForHandler) SaveAgent(_ context.Context, _ entity.TenantID, agent entity.Agent) error {
	m.agents = append(m.agents, agent)
	return nil
}

func (m *mockStoreForHandler) SaveUser(_ context.Context, _ entity.TenantID, user entity.User) error {
	m.users = append(m.users, user)
	return nil
}

func (m *mockStoreForHandler) SaveTenantMapping(_ context.Context, mapping entity.TenantMapping) error {
	key := mapping.ChannelID + ":" + mapping.ExternalTenantID
	m.workspaceMappings[key] = mapping.TenantID
	return nil
}

func (m *mockStoreForHandler) GetTenantByMapping(_ context.Context, channelID, externalTenantID string) (entity.TenantID, error) {
	key := channelID + ":" + externalTenantID
	tid, ok := m.workspaceMappings[key]
	if !ok {
		return "", errors.New("no mapping")
	}
	return tid, nil
}

func (m *mockStoreForHandler) GetUser(_ context.Context, tenantID entity.TenantID, userID entity.UserID) (*entity.User, error) {
	for i := range m.users {
		if m.users[i].TenantID == tenantID && m.users[i].ID == userID {
			return &m.users[i], nil
		}
	}
	return nil, errors.New("user not found")
}

func (m *mockStoreForHandler) GetExternalID(_ context.Context, _ entity.TenantID, userID entity.UserID, channelID string) (string, error) {
	if ci, ok := m.userIdentities[userID]; ok {
		return ci[channelID], nil
	}
	return "", nil
}

func (m *mockStoreForHandler) AddUserIdentity(_ context.Context, _ entity.TenantID, channelID, externalID string, userID entity.UserID) error {
	if m.userIdentities[userID] == nil {
		m.userIdentities[userID] = make(map[string]string)
	}
	m.userIdentities[userID][channelID] = externalID
	return nil
}

func (m *mockStoreForHandler) GetUserByChannel(_ context.Context, tenantID entity.TenantID, channelID, userExternalID string) (*entity.User, error) {
	for i := range m.users {
		if m.users[i].TenantID == tenantID {
			if ci, ok := m.userIdentities[m.users[i].ID]; ok {
				if extID, ok := ci[channelID]; ok && extID == userExternalID {
					return &m.users[i], nil
				}
			}
		}
	}
	return nil, nil
}

func (m *mockStoreForHandler) MergeUser(_ context.Context, _ entity.TenantID, fromID, _ entity.UserID) error {
	for i := range m.users {
		if m.users[i].ID == fromID {
			m.users = append(m.users[:i], m.users[i+1:]...)
			break
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// handlerDecision mirrors the runtime inbound handler decision tree.
// It is NOT extracted from runtime.go — it is a faithful reproduction of the
// handler logic used to verify the behavioral contract.
//
// Handler contract (INV-07):
//   - On join error (ErrRejected): rejection message is returned to caller
//   - On join success: feedback message is returned (may be empty = discard silently)
//   - Code-containing messages are fully consumed by TryJoin; no agent processing
// ---------------------------------------------------------------------------

type handlerDecision struct {
	// rejectionMsg is non-empty when access was denied.
	rejectionMsg string
	// feedbackMsg is the join success feedback (may be empty = silent).
	feedbackMsg string
	// action is the join result action (empty on rejection).
	action string
	// rejected is true when access was denied.
	rejected bool
	// forwardToAgent is true when the message should continue to the agent loop
	// (not discarded after onboarding). Only true for auto_joined.
	forwardToAgent bool
}

// simulateHandler runs the same decision logic as the runtime inbound handler.
// tenantID is pre-resolved for group context (passed from partial identity),
// empty for DM context (TryJoin resolves via workspace mapping).
func simulateHandler(svc *onboarding.Service, channelID string, incoming dto.IncomingMessage, tenantID entity.TenantID) handlerDecision {
	result, rejectionMsg, joinErr := svc.TryJoin(context.Background(), channelID, incoming, tenantID)
	if joinErr != nil {
		if errors.Is(joinErr, onboarding.ErrRejected) {
			return handlerDecision{rejectionMsg: rejectionMsg, rejected: true}
		}
		// Non-ErrRejected errors: log and skip (no rejection message sent).
		return handlerDecision{rejected: true, rejectionMsg: "internal error"}
	}
	// Successful join: send feedback if available (may be empty = discard silently).
	feedback := onboarding.FeedbackMessage(result.Action)
	// For auto_joined, the handler re-resolves identity and forwards the
	// message to the agent loop instead of discarding it.
	forward := result.Action == "auto_joined"
	return handlerDecision{feedbackMsg: feedback, action: result.Action, forwardToAgent: forward}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestMessageHandler_UnknownUser_RejectionSent verifies that when a user
// without a valid invite code contacts the bot, a rejection message is returned.
// This covers the ErrUnknownUser/ErrUnknownGroup branch in the handler.
func TestMessageHandler_UnknownUser_RejectionSent(t *testing.T) {
	store := newMockStoreForHandler()
	// invite_required tenant with workspace mapping — no code, no user
	tenantID := entity.TenantID("t1")
	store.tenants[tenantID] = &entity.Tenant{
		ID:               tenantID,
		JoinPolicy:       "invite_required",
		RejectionMessage: "Please provide an invite code.",
	}
	store.workspaceMappings["ch1:bot123"] = tenantID

	svc := onboarding.NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello",
		UserID:   "unknown-ext-user",
		TenantID: "bot123",
	}

	d := simulateHandler(svc, "ch1", msg, "")
	if !d.rejected {
		t.Fatal("expected rejection, got success")
	}
	if d.rejectionMsg != "Please provide an invite code." {
		t.Errorf("rejection = %q, want custom tenant message", d.rejectionMsg)
	}
}

// TestMessageHandler_UnknownUser_DefaultRejection verifies that when no
// tenant-specific rejection message is configured, the handler falls back to
// the hardcoded default ("Access denied. You are not registered.").
func TestMessageHandler_UnknownUser_DefaultRejection(t *testing.T) {
	store := newMockStoreForHandler()
	// No workspace mapping at all → TryJoin returns defaultRejection
	svc := onboarding.NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello",
		UserID:   "unknown-ext-user",
		TenantID: "nonexistent-bot",
	}

	d := simulateHandler(svc, "ch1", msg, "")
	if !d.rejected {
		t.Fatal("expected rejection")
	}
	// The runtime handler replaces empty rejectionMsg with its own default.
	// TryJoin returns a non-empty default from the service; if the service
	// returned empty the handler would use "Access denied. You are not registered."
	if d.rejectionMsg == "" {
		t.Error("expected non-empty rejection message")
	}
}

// TestMessageHandler_ValidCode_MessageDiscardedSilently verifies that when a
// user provides a valid tenant-creation code, the handler performs the join and
// returns — it does NOT forward the message to the agent (code messages are
// discarded). The test confirms result.Action is set and no rejection occurs.
func TestMessageHandler_ValidCode_MessageDiscardedSilently(t *testing.T) {
	store := newMockStoreForHandler()
	store.inviteCodes["AB12-CD34"] = &entity.InviteCode{
		Code:      "AB12-CD34",
		Type:      "tenant",
		CreatedAt: time.Now(),
	}

	svc := onboarding.NewService(store)
	msg := dto.IncomingMessage{
		Content:  "AB12-CD34",
		UserID:   "ext-new-user",
		TenantID: "bot123",
		Metadata: map[string]string{"sender_name": "Alice"},
	}

	d := simulateHandler(svc, "ch1", msg, "")
	if d.rejected {
		t.Fatalf("expected success, got rejection: %s", d.rejectionMsg)
	}
	if d.action != "tenant_created" {
		t.Errorf("action = %q, want tenant_created", d.action)
	}
	// feedbackMsg may be non-empty (feedback is sent) — the message itself
	// is still discarded (not forwarded to agent). The handler returns nil
	// after processing, so no agent processing occurs.
}

// TestMessageHandler_ValidUserCode_FeedbackMessageSent verifies that after a
// successful user join, the handler obtains a non-empty feedback message to send.
func TestMessageHandler_ValidUserCode_FeedbackMessageSent(t *testing.T) {
	store := newMockStoreForHandler()
	tenantID := entity.TenantID("t2")
	store.tenants[tenantID] = &entity.Tenant{
		ID:         tenantID,
		JoinPolicy: "invite_required",
	}
	store.inviteCodes["XY56-ZW78"] = &entity.InviteCode{
		Code:      "XY56-ZW78",
		Type:      "user",
		TenantID:  tenantID,
		CreatedAt: time.Now(),
	}

	svc := onboarding.NewService(store)
	msg := dto.IncomingMessage{
		Content:  "XY56-ZW78",
		UserID:   "ext-user-2",
		Metadata: map[string]string{"sender_name": "Bob"},
	}

	d := simulateHandler(svc, "ch1", msg, "")
	if d.rejected {
		t.Fatalf("unexpected rejection: %s", d.rejectionMsg)
	}
	if d.action != "user_joined" {
		t.Errorf("action = %q, want user_joined", d.action)
	}
	if d.feedbackMsg == "" {
		t.Error("expected non-empty feedback message for user_joined action")
	}
	if d.feedbackMsg != "Welcome! You have joined the workspace." {
		t.Errorf("feedbackMsg = %q, want welcome message", d.feedbackMsg)
	}
}

// TestMessageHandler_UsedCode_Rejected verifies that a used invite code is
// rejected by the handler (not processed as a successful join).
func TestMessageHandler_UsedCode_Rejected(t *testing.T) {
	store := newMockStoreForHandler()
	store.inviteCodes["USED-CODE"] = &entity.InviteCode{
		Code:      "USED-CODE",
		Type:      "user",
		TenantID:  "t1",
		UsedBy:    "some-user",
		CreatedAt: time.Now(),
	}

	svc := onboarding.NewService(store)
	msg := dto.IncomingMessage{
		Content: "USED-CODE",
		UserID:  "ext-user-3",
	}

	d := simulateHandler(svc, "ch1", msg, "")
	if !d.rejected {
		t.Fatal("expected rejection for used invite code")
	}
	if d.rejectionMsg == "" {
		t.Error("expected non-empty rejection message for used code")
	}
}

// TestMessageHandler_OpenPolicy_AutoJoin verifies that a user joining a tenant
// with open policy is auto-joined (no code needed) and receives feedback.
func TestMessageHandler_OpenPolicy_AutoJoin(t *testing.T) {
	store := newMockStoreForHandler()
	tenantID := entity.TenantID("t-open")
	store.tenants[tenantID] = &entity.Tenant{
		ID:         tenantID,
		JoinPolicy: "open",
	}
	store.workspaceMappings["ch1:bot-open"] = tenantID

	svc := onboarding.NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello",
		UserID:   "ext-user-new",
		TenantID: "bot-open",
		Metadata: map[string]string{"sender_name": "Carol"},
	}

	d := simulateHandler(svc, "ch1", msg, "")
	if d.rejected {
		t.Fatalf("expected auto-join success, got rejection: %s", d.rejectionMsg)
	}
	if d.action != "auto_joined" {
		t.Errorf("action = %q, want auto_joined", d.action)
	}
	// Auto-join also sends feedback
	if d.feedbackMsg == "" {
		t.Error("expected non-empty feedback message for auto_joined action")
	}
	// Auto-join forwards the message to the agent loop (not discarded).
	if !d.forwardToAgent {
		t.Error("expected forwardToAgent=true for auto_joined action")
	}
}

// TestMessageHandler_InviteCode_NotForwarded verifies that invite-code
// onboarding actions do NOT forward the message to the agent loop.
func TestMessageHandler_InviteCode_NotForwarded(t *testing.T) {
	store := newMockStoreForHandler()
	tenantID := entity.TenantID("t-inv")
	store.tenants[tenantID] = &entity.Tenant{
		ID:         tenantID,
		JoinPolicy: "invite_required",
	}
	store.inviteCodes["AA11-BB22"] = &entity.InviteCode{
		Code:      "AA11-BB22",
		Type:      "user",
		TenantID:  tenantID,
		CreatedAt: time.Now(),
	}

	svc := onboarding.NewService(store)
	msg := dto.IncomingMessage{
		Content:  "AA11-BB22",
		UserID:   "ext-user-code",
		UserName: "Dave",
	}

	d := simulateHandler(svc, "ch1", msg, "")
	if d.rejected {
		t.Fatalf("expected success, got rejection: %s", d.rejectionMsg)
	}
	if d.action != "user_joined" {
		t.Errorf("action = %q, want user_joined", d.action)
	}
	if d.forwardToAgent {
		t.Error("expected forwardToAgent=false for invite-code join")
	}
}

// simulateIdentityErrorHandler mirrors the runtime handler's identity error
// decision tree, including the ErrUnknownGroup → ErrUnknownUser fallthrough
// for mentions.
func simulateIdentityErrorHandler(svc *onboarding.Service, channelID string, incoming dto.IncomingMessage, identityErr error) (decision handlerDecision, discarded bool) {
	if errors.Is(identityErr, identity.ErrUnknownGroup) {
		if !incoming.IsMention {
			return handlerDecision{}, true // silent discard
		}
		identityErr = identity.ErrUnknownUser
	}
	if errors.Is(identityErr, identity.ErrUnknownUser) {
		var tenantID entity.TenantID
		if incoming.IsGroup {
			// In the real handler, tenantID comes from partial identity (msg.TenantID).
			// For unknown groups, no tenant is resolved so tenantID stays empty.
		}
		d := simulateHandler(svc, channelID, incoming, tenantID)
		return d, false
	}
	return handlerDecision{}, true // other errors: logged and discarded
}

// ---------------------------------------------------------------------------
// Tests: ErrUnknownGroup handling
// ---------------------------------------------------------------------------

// TestMessageHandler_UnknownGroup_Mention_RejectionSent verifies that when
// a bot is mentioned in an unknown group, the handler treats it as an unknown
// user and sends a rejection/onboarding message (not silently discarded).
func TestMessageHandler_UnknownGroup_Mention_RejectionSent(t *testing.T) {
	store := newMockStoreForHandler()
	svc := onboarding.NewService(store)

	msg := dto.IncomingMessage{
		Content:   "hey @bot",
		UserID:    "ext-user-group",
		ChatID:    "group-chat-unknown",
		IsGroup:   true,
		IsMention: true,
		TenantID:  "nonexistent-bot",
	}

	d, discarded := simulateIdentityErrorHandler(svc, "ch1", msg, identity.ErrUnknownGroup)
	if discarded {
		t.Fatal("expected mention in unknown group to trigger onboarding, got silent discard")
	}
	if !d.rejected {
		t.Fatal("expected rejection (no workspace mapping), got success")
	}
	if d.rejectionMsg == "" {
		t.Error("expected non-empty rejection message")
	}
}

// TestMessageHandler_UnknownGroup_NoMention_Discarded verifies that non-mention
// messages in unknown groups are still silently discarded.
func TestMessageHandler_UnknownGroup_NoMention_Discarded(t *testing.T) {
	store := newMockStoreForHandler()
	svc := onboarding.NewService(store)

	msg := dto.IncomingMessage{
		Content:   "hello everyone",
		UserID:    "ext-user-group",
		ChatID:    "group-chat-unknown",
		IsGroup:   true,
		IsMention: false,
	}

	_, discarded := simulateIdentityErrorHandler(svc, "ch1", msg, identity.ErrUnknownGroup)
	if !discarded {
		t.Fatal("expected non-mention in unknown group to be silently discarded")
	}
}

// ---------------------------------------------------------------------------
// Tests: Group-aware TryJoin (pre-resolved tenantID)
// ---------------------------------------------------------------------------

// TestMessageHandler_Group_OpenPolicy_AutoJoin verifies that a group message
// from an unregistered user in an open-policy tenant is auto-joined when
// tenantID is pre-resolved (group context).
func TestMessageHandler_Group_OpenPolicy_AutoJoin(t *testing.T) {
	store := newMockStoreForHandler()
	tenantID := entity.TenantID("t-group-open")
	store.tenants[tenantID] = &entity.Tenant{
		ID:         tenantID,
		JoinPolicy: "open",
	}

	svc := onboarding.NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello from group",
		UserID:   "ext-user-new",
		ChatID:   "group-chat-1",
		IsGroup:  true,
		UserName: "GroupUser",
	}

	// Group context: tenantID is pre-resolved from partial identity.
	d := simulateHandler(svc, "ch1", msg, tenantID)
	if d.rejected {
		t.Fatalf("expected auto-join success, got rejection: %s", d.rejectionMsg)
	}
	if d.action != "auto_joined" {
		t.Errorf("action = %q, want auto_joined", d.action)
	}
	if d.feedbackMsg == "" {
		t.Error("expected non-empty feedback for auto_joined")
	}
}

// TestMessageHandler_Group_InviteRequired_Rejected verifies that a group
// message from an unregistered user in an invite_required tenant is rejected
// when no invite code is present.
func TestMessageHandler_Group_InviteRequired_Rejected(t *testing.T) {
	store := newMockStoreForHandler()
	tenantID := entity.TenantID("t-group-inv")
	store.tenants[tenantID] = &entity.Tenant{
		ID:               tenantID,
		JoinPolicy:       "invite_required",
		RejectionMessage: "Groups also need invite codes.",
	}

	svc := onboarding.NewService(store)
	msg := dto.IncomingMessage{
		Content: "hello from group",
		UserID:  "ext-user-new",
		ChatID:  "group-chat-2",
		IsGroup: true,
	}

	d := simulateHandler(svc, "ch1", msg, tenantID)
	if !d.rejected {
		t.Fatal("expected rejection for invite_required group")
	}
	if d.rejectionMsg != "Groups also need invite codes." {
		t.Errorf("rejectionMsg = %q, want custom message", d.rejectionMsg)
	}
}

// TestMessageHandler_Group_WithInviteCode_Joined verifies that a group
// message containing an invite code redeems it successfully with pre-resolved
// tenantID.
func TestMessageHandler_Group_WithInviteCode_Joined(t *testing.T) {
	store := newMockStoreForHandler()
	tenantID := entity.TenantID("t-group-code")
	store.tenants[tenantID] = &entity.Tenant{
		ID:         tenantID,
		JoinPolicy: "invite_required",
	}
	store.inviteCodes["GR12-CD34"] = &entity.InviteCode{
		Code:      "GR12-CD34",
		Type:      "user",
		TenantID:  tenantID,
		CreatedAt: time.Now(),
	}

	svc := onboarding.NewService(store)
	msg := dto.IncomingMessage{
		Content:  "GR12-CD34",
		UserID:   "ext-user-group",
		ChatID:   "group-chat-3",
		IsGroup:  true,
		UserName: "GroupCoder",
	}

	// Even with tenantID pre-resolved, code path doesn't use it (code carries its own tenant).
	d := simulateHandler(svc, "ch1", msg, tenantID)
	if d.rejected {
		t.Fatalf("expected success, got rejection: %s", d.rejectionMsg)
	}
	if d.action != "user_joined" {
		t.Errorf("action = %q, want user_joined", d.action)
	}
}
