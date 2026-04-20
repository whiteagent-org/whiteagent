package onboarding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mock store
// ---------------------------------------------------------------------------

type mockStore struct {
	port.StorePlugin // embed to satisfy interface; panics on unimplemented calls

	inviteCodes       map[string]*entity.InviteCode
	tenants           map[entity.TenantID]*entity.Tenant
	users             []entity.User
	agents            []entity.Agent
	chats             []entity.Chat
	workspaceMappings map[string]entity.TenantID // "channelID:extTenantID" -> tenantID
	usedCodes         map[string]entity.UserID

	// userIdentities: userID -> channelID -> externalID
	userIdentities map[entity.UserID]map[string]string

	// Track calls
	saveTenantCalls        int
	saveAgentCalls         int
	saveUserCalls          int
	saveGroupCalls         int
	saveTenantMappingCalls int
	useInviteCodeCalls     int
	mergeUserCalls         int
}

func newMockStore() *mockStore {
	return &mockStore{
		inviteCodes:       make(map[string]*entity.InviteCode),
		tenants:           make(map[entity.TenantID]*entity.Tenant),
		workspaceMappings: make(map[string]entity.TenantID),
		usedCodes:         make(map[string]entity.UserID),
		userIdentities:    make(map[entity.UserID]map[string]string),
	}
}

func (m *mockStore) GetInviteCode(_ context.Context, code string) (*entity.InviteCode, error) {
	ic, ok := m.inviteCodes[code]
	if !ok {
		return nil, errors.New("not found")
	}
	return ic, nil
}

func (m *mockStore) UseInviteCode(_ context.Context, code string, userID entity.UserID) error {
	m.useInviteCodeCalls++
	m.usedCodes[code] = userID
	if ic, ok := m.inviteCodes[code]; ok {
		ic.UsedBy = userID
	}
	return nil
}

func (m *mockStore) GetTenant(_ context.Context, tenantID entity.TenantID) (*entity.Tenant, error) {
	t, ok := m.tenants[tenantID]
	if !ok {
		return nil, errors.New("tenant not found")
	}
	return t, nil
}

func (m *mockStore) SaveTenant(_ context.Context, _ entity.TenantID, tenant entity.Tenant) error {
	m.saveTenantCalls++
	m.tenants[tenant.ID] = &tenant
	return nil
}

func (m *mockStore) SaveAgent(_ context.Context, _ entity.TenantID, agent entity.Agent) error {
	m.saveAgentCalls++
	m.agents = append(m.agents, agent)
	return nil
}

func (m *mockStore) SaveUser(_ context.Context, _ entity.TenantID, user entity.User) error {
	m.saveUserCalls++
	m.users = append(m.users, user)
	return nil
}

func (m *mockStore) SaveChat(_ context.Context, _ entity.TenantID, chat entity.Chat) error {
	m.saveGroupCalls++
	m.chats = append(m.chats, chat)
	return nil
}

func (m *mockStore) SaveTenantMapping(_ context.Context, mapping entity.TenantMapping) error {
	m.saveTenantMappingCalls++
	key := mapping.ChannelID + ":" + mapping.ExternalTenantID
	m.workspaceMappings[key] = mapping.TenantID
	return nil
}

func (m *mockStore) GetTenantByMapping(_ context.Context, channelID, externalTenantID string) (entity.TenantID, error) {
	key := channelID + ":" + externalTenantID
	tid, ok := m.workspaceMappings[key]
	if !ok {
		return "", errors.New("no mapping")
	}
	return tid, nil
}

func (m *mockStore) GetUser(_ context.Context, tenantID entity.TenantID, userID entity.UserID) (*entity.User, error) {
	for i := range m.users {
		if m.users[i].TenantID == tenantID && m.users[i].ID == userID {
			return &m.users[i], nil
		}
	}
	return nil, errors.New("user not found")
}

func (m *mockStore) MergeUser(_ context.Context, _ entity.TenantID, fromID, toID entity.UserID) error {
	m.mergeUserCalls++
	// Simulate merge: remove the source user from the users slice
	for i := range m.users {
		if m.users[i].ID == fromID {
			m.users = append(m.users[:i], m.users[i+1:]...)
			break
		}
	}
	return nil
}

func (m *mockStore) GetExternalID(_ context.Context, _ entity.TenantID, userID entity.UserID, channelID string) (string, error) {
	if ci, ok := m.userIdentities[userID]; ok {
		return ci[channelID], nil
	}
	return "", nil
}

func (m *mockStore) AddUserIdentity(_ context.Context, _ entity.TenantID, channelID, externalID string, userID entity.UserID) error {
	if m.userIdentities[userID] == nil {
		m.userIdentities[userID] = make(map[string]string)
	}
	m.userIdentities[userID][channelID] = externalID
	return nil
}

func (m *mockStore) GetUserByChannel(_ context.Context, tenantID entity.TenantID, channelID, userExternalID string) (*entity.User, error) {
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

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestTryJoin_TenantCreationCode(t *testing.T) {
	store := newMockStore()
	store.inviteCodes["AB12-CD34"] = &entity.InviteCode{
		Code:      "AB12-CD34",
		Type:      "tenant",
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "Here is my code AB12-CD34",
		UserID:   "ext-user-1",
		TenantID: "bot123",
		UserName: "Alice",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rejection != "" {
		t.Fatalf("unexpected rejection: %s", rejection)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Action != "tenant_created" {
		t.Errorf("action = %q, want tenant_created", result.Action)
	}
	if result.Tenant == nil {
		t.Fatal("expected non-nil tenant")
	}
	if result.User == nil {
		t.Fatal("expected non-nil user")
	}
	// Tenant name should use UserName fallback: "Alice's Workspace"
	if result.Tenant.Name != "Alice's Workspace" {
		t.Errorf("tenant name = %q, want Alice's Workspace", result.Tenant.Name)
	}
	if result.Tenant.JoinPolicy != "invite_required" {
		t.Errorf("join policy = %q, want invite_required", result.Tenant.JoinPolicy)
	}
	// Agent should have been created
	if store.saveAgentCalls != 1 {
		t.Errorf("saveAgent calls = %d, want 1", store.saveAgentCalls)
	}
	// Workspace mapping should have been saved
	if store.saveTenantMappingCalls != 1 {
		t.Errorf("saveWorkspaceMapping calls = %d, want 1", store.saveTenantMappingCalls)
	}
	// Code should be used
	if store.useInviteCodeCalls != 1 {
		t.Errorf("useInviteCode calls = %d, want 1", store.useInviteCodeCalls)
	}
	// DefaultAgentID set
	if result.Tenant.DefaultAgentID.IsEmpty() {
		t.Error("expected non-empty DefaultAgentID")
	}
}

func TestTryJoin_TenantCreationCode_FallbackName(t *testing.T) {
	store := newMockStore()
	store.inviteCodes["AB12-CD34"] = &entity.InviteCode{
		Code:      "AB12-CD34",
		Type:      "tenant",
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "AB12-CD34",
		UserID:   "ext-user-1",
		TenantID: "",
		// No UserName, no TenantName => fallback to "Tenant-AB12"
	}

	result, _, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Tenant.Name != "Tenant-AB12" {
		t.Errorf("tenant name = %q, want Tenant-AB12", result.Tenant.Name)
	}
}

func TestTryJoin_TenantCreationCode_TenantNameFromMsg(t *testing.T) {
	store := newMockStore()
	store.inviteCodes["AB12-CD34"] = &entity.InviteCode{
		Code:      "AB12-CD34",
		Type:      "tenant",
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:    "AB12-CD34",
		UserID:     "ext-user-1",
		TenantID:   "bot123",
		TenantName: "Acme Corp",
		UserName:   "Alice",
		AgentName:  "HelpBot",
	}

	result, _, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// TenantName takes priority
	if result.Tenant.Name != "Acme Corp" {
		t.Errorf("tenant name = %q, want Acme Corp", result.Tenant.Name)
	}
	// Agent name from msg.AgentName
	if len(store.agents) == 0 {
		t.Fatal("expected agent to be created")
	}
	if store.agents[0].Name != "HelpBot" {
		t.Errorf("agent name = %q, want HelpBot", store.agents[0].Name)
	}
}

func TestTryJoin_TenantCreationCode_NoTenantID(t *testing.T) {
	store := newMockStore()
	store.inviteCodes["AB12-CD34"] = &entity.InviteCode{
		Code:      "AB12-CD34",
		Type:      "tenant",
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "AB12-CD34",
		UserID:   "ext-user-1",
		TenantID: "", // No TenantID => no workspace mapping saved
		UserName: "Bob",
	}

	result, _, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Action != "tenant_created" {
		t.Fatalf("expected tenant_created, got %v", result)
	}
	// No workspace mapping because TenantID is empty
	if store.saveTenantMappingCalls != 0 {
		t.Errorf("saveWorkspaceMapping calls = %d, want 0", store.saveTenantMappingCalls)
	}
}

func TestTryJoin_UserJoinCode(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-1")
	store.tenants[tenantID] = &entity.Tenant{
		ID:         tenantID,
		Name:       "Test Tenant",
		JoinPolicy: "invite_required",
	}
	store.inviteCodes["XY56-ZW78"] = &entity.InviteCode{
		Code:      "XY56-ZW78",
		Type:      "user",
		TenantID:  tenantID,
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "my code is XY56-ZW78",
		UserID:   "ext-user-2",
		UserName: "Charlie",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rejection != "" {
		t.Fatalf("unexpected rejection: %s", rejection)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Action != "user_joined" {
		t.Errorf("action = %q, want user_joined", result.Action)
	}
	if result.User == nil {
		t.Fatal("expected non-nil user")
	}
	if result.User.TenantID != tenantID {
		t.Errorf("user tenant = %q, want %q", result.User.TenantID, tenantID)
	}
	if store.useInviteCodeCalls != 1 {
		t.Errorf("useInviteCode calls = %d, want 1", store.useInviteCodeCalls)
	}
}

func TestTryJoin_OpenPolicy_AutoJoin(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-open")
	store.tenants[tenantID] = &entity.Tenant{
		ID:         tenantID,
		Name:       "Open Tenant",
		JoinPolicy: "open",
	}
	store.workspaceMappings["channel.telegram:bot123"] = tenantID

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello", // No code
		UserID:   "ext-user-3",
		TenantID: "bot123",
		UserName: "Diana",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rejection != "" {
		t.Fatalf("unexpected rejection: %s", rejection)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Action != "auto_joined" {
		t.Errorf("action = %q, want auto_joined", result.Action)
	}
	if result.User == nil {
		t.Fatal("expected non-nil user")
	}
}

func TestTryJoin_InviteRequired_NoCode(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-inv")
	store.tenants[tenantID] = &entity.Tenant{
		ID:               tenantID,
		Name:             "Invite Tenant",
		JoinPolicy:       "invite_required",
		RejectionMessage: "Please get an invite code from the admin.",
	}
	store.workspaceMappings["channel.telegram:bot456"] = tenantID

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello",
		UserID:   "ext-user-4",
		TenantID: "bot456",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result")
	}
	if rejection != "Please get an invite code from the admin." {
		t.Errorf("rejection = %q, want custom message", rejection)
	}
}

func TestTryJoin_ClosedPolicy(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-closed")
	store.tenants[tenantID] = &entity.Tenant{
		ID:               tenantID,
		Name:             "Closed Tenant",
		JoinPolicy:       "closed",
		RejectionMessage: "This tenant is closed.",
	}
	store.workspaceMappings["channel.telegram:bot789"] = tenantID

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello",
		UserID:   "ext-user-5",
		TenantID: "bot789",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result")
	}
	if rejection != "This tenant is closed." {
		t.Errorf("rejection = %q, want custom message", rejection)
	}
}

func TestTryJoin_NoCode_NoMapping(t *testing.T) {
	store := newMockStore()
	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello",
		UserID:   "ext-user-6",
		TenantID: "unknown-bot",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result")
	}
	if rejection != defaultRejection {
		t.Errorf("rejection = %q, want default", rejection)
	}
}

func TestTryJoin_NoCode_EmptyTenantID(t *testing.T) {
	store := newMockStore()
	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello",
		UserID:   "ext-user-7",
		TenantID: "", // Empty
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result")
	}
	if rejection != defaultRejection {
		t.Errorf("rejection = %q, want default", rejection)
	}
}

func TestTryJoin_UsedCodeRejected(t *testing.T) {
	store := newMockStore()
	store.inviteCodes["USED-CODE"] = &entity.InviteCode{
		Code:      "USED-CODE",
		Type:      "user",
		TenantID:  "tenant-1",
		UsedBy:    "some-user",
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content: "USED-CODE",
		UserID:  "ext-user-8",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result")
	}
	if rejection == "" {
		t.Error("expected non-empty rejection")
	}
}

func TestTryJoin_RevokedCodeRejected(t *testing.T) {
	now := time.Now()
	store := newMockStore()
	store.inviteCodes["REVK-CODE"] = &entity.InviteCode{
		Code:      "REVK-CODE",
		Type:      "user",
		TenantID:  "tenant-1",
		CreatedAt: time.Now(),
		RevokedAt: &now,
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content: "REVK-CODE",
		UserID:  "ext-user-9",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result")
	}
	if rejection == "" {
		t.Error("expected non-empty rejection")
	}
}

func TestTryJoin_InviteRequiredDefaultRejection(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-no-msg")
	store.tenants[tenantID] = &entity.Tenant{
		ID:               tenantID,
		Name:             "No Msg Tenant",
		JoinPolicy:       "invite_required",
		RejectionMessage: "", // Empty => default
	}
	store.workspaceMappings["channel.telegram:bot000"] = tenantID

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello",
		UserID:   "ext-user-10",
		TenantID: "bot000",
	}

	_, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if rejection != defaultRejection {
		t.Errorf("rejection = %q, want default", rejection)
	}
}

// ---------------------------------------------------------------------------
// TryJoin with pre-resolved tenantID
// ---------------------------------------------------------------------------

func TestTryJoin_WithPreResolvedTenantID(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("pre-resolved-tenant")
	store.tenants[tenantID] = &entity.Tenant{
		ID:         tenantID,
		Name:       "Pre-resolved Tenant",
		JoinPolicy: "open",
	}
	// No workspace mapping needed -- tenantID is pre-resolved

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "hello",
		UserID:   "ext-user-group",
		TenantID: "", // No external tenant ID
		UserName: "Eve",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, tenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rejection != "" {
		t.Fatalf("unexpected rejection: %s", rejection)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Action != "auto_joined" {
		t.Errorf("action = %q, want auto_joined", result.Action)
	}
	if result.User == nil {
		t.Fatal("expected non-nil user")
	}
	if result.User.Name != "Eve" {
		t.Errorf("user name = %q, want Eve", result.User.Name)
	}
}

// ---------------------------------------------------------------------------
// CreateUser tests
// ---------------------------------------------------------------------------

func TestCreateUser_UsesUserName(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-1")

	svc := NewService(store)
	msg := dto.IncomingMessage{
		UserID:   "ext-user-1",
		UserName: "Alice",
		Metadata: map[string]string{"sender_name": "OldAlice"},
	}

	user, err := svc.CreateUser(context.Background(), tenantID, "channel.telegram", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user == nil {
		t.Fatal("expected non-nil user")
	}
	// Must use msg.UserName, not msg.Metadata["sender_name"]
	if user.Name != "Alice" {
		t.Errorf("user name = %q, want Alice", user.Name)
	}
	if user.TenantID != tenantID {
		t.Errorf("tenant = %q, want %q", user.TenantID, tenantID)
	}
	if ci := store.userIdentities[user.ID]; ci["channel.telegram"] != "ext-user-1" {
		t.Errorf("channel identity = %q, want ext-user-1", ci["channel.telegram"])
	}
	if store.saveUserCalls != 1 {
		t.Errorf("saveUser calls = %d, want 1", store.saveUserCalls)
	}
}

func TestCreateUser_EmptyUserName(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-1")

	svc := NewService(store)
	msg := dto.IncomingMessage{
		UserID:   "ext-user-1",
		UserName: "", // Empty
	}

	user, err := svc.CreateUser(context.Background(), tenantID, "channel.telegram", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Name != "" {
		t.Errorf("user name = %q, want empty", user.Name)
	}
}

// ---------------------------------------------------------------------------
// CreateGroup tests
// ---------------------------------------------------------------------------

func TestCreateChat_GroupChat(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-1")

	svc := NewService(store)
	msg := dto.IncomingMessage{
		ChatID:    "ext-chat-123",
		GroupName: "My Group Chat",
		IsGroup:   true,
	}

	agentID := entity.AgentID("agent-1")
	chat, err := svc.CreateChat(context.Background(), tenantID, "channel.telegram", msg, agentID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chat == nil {
		t.Fatal("expected non-nil chat")
	}
	if chat.TenantID != tenantID {
		t.Errorf("tenant = %q, want %q", chat.TenantID, tenantID)
	}
	if chat.ChannelID != "channel.telegram" {
		t.Errorf("channel = %q, want channel.telegram", chat.ChannelID)
	}
	if chat.ExternalChatID != "ext-chat-123" {
		t.Errorf("external chat ID = %q, want ext-chat-123", chat.ExternalChatID)
	}
	if !chat.IsGroup {
		t.Error("expected IsGroup to be true")
	}
	if store.saveGroupCalls != 1 {
		t.Errorf("saveChat calls = %d, want 1", store.saveGroupCalls)
	}
}

// ---------------------------------------------------------------------------
// Linking tests
// ---------------------------------------------------------------------------

func TestLinkUser_Success(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-1")
	targetUserID := entity.UserID("target-user")
	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, Name: "Test"}
	store.users = append(store.users, entity.User{
		ID:       targetUserID,
		TenantID: tenantID,
		Name:     "Target",
	})
	store.userIdentities[targetUserID] = map[string]string{"channel.telegram": "ext-target"}
	store.inviteCodes["LINK-USER"] = &entity.InviteCode{
		Code:      "LINK-USER",
		Type:      "user",
		TenantID:  tenantID,
		TargetID:  string(targetUserID),
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content: "LINK-USER",
		UserID:  "ext-new-user",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.teams", msg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rejection != "" {
		t.Fatalf("unexpected rejection: %s", rejection)
	}
	if result.Action != "user_linked" {
		t.Errorf("action = %q, want user_linked", result.Action)
	}
	if result.User == nil {
		t.Fatal("expected non-nil user")
	}
	if result.User.ID != targetUserID {
		t.Errorf("user ID = %q, want %q", result.User.ID, targetUserID)
	}
	// New channel should be added
	ci := store.userIdentities[targetUserID]
	if ci["channel.teams"] != "ext-new-user" {
		t.Errorf("channel.teams = %q, want ext-new-user", ci["channel.teams"])
	}
	// Original channel preserved
	if ci["channel.telegram"] != "ext-target" {
		t.Errorf("channel.telegram = %q, want ext-target", ci["channel.telegram"])
	}
	if store.useInviteCodeCalls != 1 {
		t.Errorf("useInviteCode calls = %d, want 1", store.useInviteCodeCalls)
	}
	if store.mergeUserCalls != 0 {
		t.Errorf("mergeUser calls = %d, want 0", store.mergeUserCalls)
	}
}

func TestLinkUser_MergeConflict(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-1")
	targetUserID := entity.UserID("target-user")
	existingUserID := entity.UserID("existing-user")
	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, Name: "Test"}
	store.users = append(store.users,
		entity.User{
			ID:       targetUserID,
			TenantID: tenantID,
			Name:     "Target",
		},
		entity.User{
			ID:       existingUserID,
			TenantID: tenantID,
			Name:     "Existing",
		},
	)
	store.userIdentities[targetUserID] = map[string]string{"channel.telegram": "ext-target"}
	store.userIdentities[existingUserID] = map[string]string{"channel.teams": "ext-existing"}
	store.inviteCodes["MERG-USER"] = &entity.InviteCode{
		Code:      "MERG-USER",
		Type:      "user",
		TenantID:  tenantID,
		TargetID:  string(targetUserID),
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content: "MERG-USER",
		UserID:  "ext-existing", // This user is already registered as existingUserID
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.teams", msg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rejection != "" {
		t.Fatalf("unexpected rejection: %s", rejection)
	}
	if result.Action != "user_linked" {
		t.Errorf("action = %q, want user_linked", result.Action)
	}
	if store.mergeUserCalls != 1 {
		t.Errorf("mergeUser calls = %d, want 1", store.mergeUserCalls)
	}
}

func TestLinkUser_TargetNotFound(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("tenant-1")
	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, Name: "Test"}
	store.inviteCodes["NOTR-USER"] = &entity.InviteCode{
		Code:      "NOTR-USER",
		Type:      "user",
		TenantID:  tenantID,
		TargetID:  "nonexistent-user",
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content: "NOTR-USER",
		UserID:  "ext-user",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.telegram", msg, "")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result")
	}
	if rejection == "" {
		t.Error("expected non-empty rejection")
	}
}

func TestLinkTenant_Success(t *testing.T) {
	store := newMockStore()
	targetTenantID := entity.TenantID("target-tenant")
	store.tenants[targetTenantID] = &entity.Tenant{ID: targetTenantID, Name: "Target Tenant"}
	store.inviteCodes["LINK-TNNT"] = &entity.InviteCode{
		Code:      "LINK-TNNT",
		Type:      "tenant",
		TargetID:  string(targetTenantID),
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "LINK-TNNT",
		UserID:   "ext-user",
		TenantID: "workspace-123",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.teams", msg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rejection != "" {
		t.Fatalf("unexpected rejection: %s", rejection)
	}
	if result.Action != "tenant_linked" {
		t.Errorf("action = %q, want tenant_linked", result.Action)
	}
	if result.Tenant == nil {
		t.Fatal("expected non-nil tenant")
	}
	if result.Tenant.ID != targetTenantID {
		t.Errorf("tenant ID = %q, want %q", result.Tenant.ID, targetTenantID)
	}
	if result.User != nil {
		t.Error("expected nil user for tenant_linked")
	}
	if store.saveTenantMappingCalls != 1 {
		t.Errorf("saveWorkspaceMapping calls = %d, want 1", store.saveTenantMappingCalls)
	}
	if store.useInviteCodeCalls != 1 {
		t.Errorf("useInviteCode calls = %d, want 1", store.useInviteCodeCalls)
	}
	// Used by sentinel
	if store.usedCodes["LINK-TNNT"] != entity.UserID("system:tenant-link") {
		t.Errorf("usedBy = %q, want system:tenant-link", store.usedCodes["LINK-TNNT"])
	}
}

func TestLinkTenant_AlreadyMapped(t *testing.T) {
	store := newMockStore()
	targetTenantID := entity.TenantID("target-tenant")
	otherTenantID := entity.TenantID("other-tenant")
	store.tenants[targetTenantID] = &entity.Tenant{ID: targetTenantID, Name: "Target"}
	store.workspaceMappings["channel.teams:workspace-123"] = otherTenantID
	store.inviteCodes["ALRD-TNNT"] = &entity.InviteCode{
		Code:      "ALRD-TNNT",
		Type:      "tenant",
		TargetID:  string(targetTenantID),
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "ALRD-TNNT",
		UserID:   "ext-user",
		TenantID: "workspace-123",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.teams", msg, "")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result")
	}
	if rejection != "This workspace is already linked to a different tenant." {
		t.Errorf("rejection = %q, want specific message", rejection)
	}
}

func TestLinkTenant_SameTenant(t *testing.T) {
	store := newMockStore()
	targetTenantID := entity.TenantID("target-tenant")
	store.tenants[targetTenantID] = &entity.Tenant{ID: targetTenantID, Name: "Target"}
	store.workspaceMappings["channel.teams:workspace-123"] = targetTenantID // same tenant
	store.inviteCodes["SAME-TNNT"] = &entity.InviteCode{
		Code:      "SAME-TNNT",
		Type:      "tenant",
		TargetID:  string(targetTenantID),
		CreatedAt: time.Now(),
	}

	svc := NewService(store)
	msg := dto.IncomingMessage{
		Content:  "SAME-TNNT",
		UserID:   "ext-user",
		TenantID: "workspace-123",
	}

	result, rejection, err := svc.TryJoin(context.Background(), "channel.teams", msg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rejection != "" {
		t.Fatalf("unexpected rejection: %s", rejection)
	}
	if result.Action != "tenant_linked" {
		t.Errorf("action = %q, want tenant_linked", result.Action)
	}
}

func TestFeedbackMessage(t *testing.T) {
	tests := []struct {
		action string
		want   string
	}{
		{"tenant_created", "Welcome! Your workspace has been created."},
		{"user_joined", "Welcome! You have joined the workspace."},
		{"auto_joined", "Welcome! You have been added to the workspace."},
		{"user_linked", "Your account has been linked to this channel."},
		{"tenant_linked", "This workspace has been linked to the tenant."},
		{"unknown", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := FeedbackMessage(tt.action)
		if got != tt.want {
			t.Errorf("FeedbackMessage(%q) = %q, want %q", tt.action, got, tt.want)
		}
	}
}
