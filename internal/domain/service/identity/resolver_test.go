package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/onboarding"
)

// ---------------------------------------------------------------------------
// Mock store
// ---------------------------------------------------------------------------

type mockStore struct {
	port.StorePlugin

	tenants           map[entity.TenantID]*entity.Tenant
	users             []entity.User
	chats             []entity.Chat
	workspaceMappings map[string]entity.TenantID // "channelID:extTenantID"
	savedChats        []entity.Chat

	// userIdentities: userID -> channelID -> externalID
	userIdentities map[entity.UserID]map[string]string
}

func newMockStore() *mockStore {
	return &mockStore{
		tenants:           make(map[entity.TenantID]*entity.Tenant),
		workspaceMappings: make(map[string]entity.TenantID),
		userIdentities:    make(map[entity.UserID]map[string]string),
	}
}

func (m *mockStore) GetTenantByMapping(_ context.Context, channelID, externalTenantID string) (entity.TenantID, error) {
	key := channelID + ":" + externalTenantID
	tid, ok := m.workspaceMappings[key]
	if !ok {
		return "", errors.New("no mapping")
	}
	return tid, nil
}

func (m *mockStore) GetTenant(_ context.Context, tenantID entity.TenantID) (*entity.Tenant, error) {
	t, ok := m.tenants[tenantID]
	if !ok {
		return nil, errors.New("tenant not found")
	}
	return t, nil
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

func (m *mockStore) GetChatByChannel(_ context.Context, tenantID entity.TenantID, channelID, externalChatID string) (*entity.Chat, error) {
	for i := range m.chats {
		if m.chats[i].TenantID == tenantID && m.chats[i].ChannelID == channelID && m.chats[i].ExternalChatID == externalChatID {
			return &m.chats[i], nil
		}
	}
	return nil, nil
}

func (m *mockStore) SaveChat(_ context.Context, _ entity.TenantID, chat entity.Chat) error {
	m.savedChats = append(m.savedChats, chat)
	m.chats = append(m.chats, chat)
	return nil
}

func (m *mockStore) GetChat(_ context.Context, _ entity.TenantID, chatID entity.ChatID) (*entity.Chat, error) {
	for i := range m.chats {
		if m.chats[i].ID == chatID {
			return &m.chats[i], nil
		}
	}
	return nil, nil
}

func (m *mockStore) SaveUser(_ context.Context, _ entity.TenantID, user entity.User) error {
	for i := range m.users {
		if m.users[i].ID == user.ID {
			m.users[i] = user
			return nil
		}
	}
	m.users = append(m.users, user)
	return nil
}

func (m *mockStore) ListTenants(_ context.Context) ([]entity.Tenant, error) {
	return nil, errors.New("ListTenants should not be called")
}

// ---------------------------------------------------------------------------
// Tests: DM resolution
// ---------------------------------------------------------------------------

func TestResolve_DM_TenantMapping(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("t1")
	agentID := entity.AgentID("a1")
	userID := entity.UserID("u1")

	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, DefaultAgentID: agentID}
	store.workspaceMappings["ch1:bot123"] = tenantID
	store.users = append(store.users, entity.User{
		ID:       userID,
		TenantID: tenantID,
	})
	store.userIdentities[userID] = map[string]string{"ch1": "ext-user-1"}

	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-1",
		TenantID: "bot123",
		ChatID:   "ext-user-1", // DM chat uses user's external ID
	}

	ri, err := r.Resolve(context.Background(), "ch1", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ri.TenantID != tenantID {
		t.Errorf("TenantID = %q, want %q", ri.TenantID, tenantID)
	}
	if ri.UserID != userID {
		t.Errorf("UserID = %q, want %q", ri.UserID, userID)
	}
	if ri.AgentID != agentID {
		t.Errorf("AgentID = %q, want %q", ri.AgentID, agentID)
	}
}

func TestResolve_DM_CacheHit(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("t1")
	agentID := entity.AgentID("a1")
	userID := entity.UserID("u1")

	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, DefaultAgentID: agentID}
	store.workspaceMappings["ch1:bot123"] = tenantID
	store.users = append(store.users, entity.User{
		ID:       userID,
		TenantID: tenantID,
	})
	store.userIdentities[userID] = map[string]string{"ch1": "ext-user-1"}

	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-1",
		TenantID: "bot123",
		ChatID:   "ext-user-1",
	}

	// First call populates cache
	_, err := r.Resolve(context.Background(), "ch1", msg)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	// Remove from store to prove cache is used
	store.users = nil
	ri, err := r.Resolve(context.Background(), "ch1", msg)
	if err != nil {
		t.Fatalf("second resolve (cache): %v", err)
	}
	if ri.UserID != userID {
		t.Errorf("cached UserID = %q, want %q", ri.UserID, userID)
	}
}

func TestResolve_DM_NoTenantMapping(t *testing.T) {
	store := newMockStore()
	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-1",
		TenantID: "unknown-bot",
		ChatID:   "ext-user-1",
	}

	_, err := r.Resolve(context.Background(), "ch1", msg)
	if !errors.Is(err, ErrUnknownUser) {
		t.Fatalf("expected ErrUnknownUser, got %v", err)
	}
}

func TestResolve_DM_UserNotFound(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("t1")
	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, DefaultAgentID: "a1"}
	store.workspaceMappings["ch1:bot123"] = tenantID
	// No users in store

	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-unknown",
		TenantID: "bot123",
		ChatID:   "ext-user-unknown",
	}

	_, err := r.Resolve(context.Background(), "ch1", msg)
	if !errors.Is(err, ErrUnknownUser) {
		t.Fatalf("expected ErrUnknownUser, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Group resolution
// ---------------------------------------------------------------------------

func TestResolve_Group_ExistingChat(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("t1")
	agentID := entity.AgentID("a1")

	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, DefaultAgentID: agentID}
	store.workspaceMappings["ch1:bot123"] = tenantID
	store.chats = append(store.chats, entity.Chat{
		ID:             entity.ChatID("chat-existing-42"),
		TenantID:       tenantID,
		ChannelID:      "ch1",
		ExternalChatID: "group-42",
		IsGroup:        true,
		CreatedAt:      time.Now(),
	})
	store.users = append(store.users, entity.User{
		ID:       "u1",
		TenantID: tenantID,
	})
	store.userIdentities["u1"] = map[string]string{"ch1": "ext-user-1"}

	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-1",
		ChatID:   "group-42",
		TenantID: "bot123",
		IsGroup:  true,
	}

	ri, err := r.Resolve(context.Background(), "ch1", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ri.TenantID != tenantID {
		t.Errorf("TenantID = %q, want %q", ri.TenantID, tenantID)
	}
	if ri.AgentID != agentID {
		t.Errorf("AgentID = %q, want %q", ri.AgentID, agentID)
	}
	if ri.UserID != "u1" {
		t.Errorf("UserID = %q, want u1", ri.UserID)
	}
	if ri.ChatID != "chat-existing-42" {
		t.Errorf("ChatID = %q, want chat-existing-42", ri.ChatID)
	}
}

func TestResolve_Group_AutoCreate_RegisteredUser(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("t1")
	agentID := entity.AgentID("a1")

	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, DefaultAgentID: agentID}
	store.workspaceMappings["ch1:bot123"] = tenantID
	// No chat in store, but user is registered
	store.users = append(store.users, entity.User{
		ID:       "u1",
		TenantID: tenantID,
	})
	store.userIdentities["u1"] = map[string]string{"ch1": "ext-user-1"}

	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-1",
		ChatID:   "new-group",
		TenantID: "bot123",
		IsGroup:  true,
		Metadata: map[string]string{"chat_title": "My Group"},
	}

	ri, err := r.Resolve(context.Background(), "ch1", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ri.TenantID != tenantID {
		t.Errorf("TenantID = %q, want %q", ri.TenantID, tenantID)
	}
	// Chat should have been auto-created
	if len(store.savedChats) == 0 {
		t.Fatal("expected saved chat, got none")
	}
	if store.savedChats[0].ExternalChatID == "" {
		t.Error("expected ExternalChatID to be set on auto-created chat")
	}
	if ri.ChatID.IsEmpty() {
		t.Error("expected ChatID to be set after auto-create")
	}
}

func TestResolve_Group_UnregisteredUser_NoChat(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("t1")
	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, DefaultAgentID: "a1"}
	store.workspaceMappings["ch1:bot123"] = tenantID
	// No chat, no user

	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-unknown",
		ChatID:   "new-group",
		TenantID: "bot123",
		IsGroup:  true,
	}

	ri, err := r.Resolve(context.Background(), "ch1", msg)
	if !errors.Is(err, ErrUnknownUser) {
		t.Fatalf("expected ErrUnknownUser, got %v", err)
	}
	// Chat should have been created regardless of user registration.
	if len(store.savedChats) == 0 {
		t.Fatal("expected saved chat (chat created before user check)")
	}
	// Partial identity should have TenantID, AgentID, ChatID filled.
	if ri.TenantID != tenantID {
		t.Errorf("TenantID = %q, want %q", ri.TenantID, tenantID)
	}
	if ri.ChatID.IsEmpty() {
		t.Error("ChatID should be set in partial identity")
	}
	if !ri.UserID.IsEmpty() {
		t.Errorf("UserID = %q, want empty", ri.UserID)
	}
}

func TestResolve_Group_NoTenantMapping(t *testing.T) {
	store := newMockStore()
	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-1",
		ChatID:   "group-42",
		TenantID: "unknown-bot",
		IsGroup:  true,
	}

	_, err := r.Resolve(context.Background(), "ch1", msg)
	if !errors.Is(err, ErrUnknownGroup) {
		t.Fatalf("expected ErrUnknownGroup, got %v", err)
	}
}

func TestResolve_Group_ExistingChat_UnregisteredSender(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("t1")
	agentID := entity.AgentID("a1")

	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, DefaultAgentID: agentID}
	store.workspaceMappings["ch1:bot123"] = tenantID
	store.chats = append(store.chats, entity.Chat{
		ID:             entity.ChatID("chat-no-user-42"),
		TenantID:       tenantID,
		ChannelID:      "ch1",
		ExternalChatID: "group-42",
		IsGroup:        true,
	})
	// No user

	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-unknown",
		ChatID:   "group-42",
		TenantID: "bot123",
		IsGroup:  true,
	}

	ri, err := r.Resolve(context.Background(), "ch1", msg)
	if !errors.Is(err, ErrUnknownUser) {
		t.Fatalf("expected ErrUnknownUser, got %v", err)
	}
	// Partial identity should have TenantID, AgentID, ChatID filled.
	if ri.TenantID != tenantID {
		t.Errorf("TenantID = %q, want %q", ri.TenantID, tenantID)
	}
	if ri.AgentID != agentID {
		t.Errorf("AgentID = %q, want %q", ri.AgentID, agentID)
	}
	if ri.ChatID != "chat-no-user-42" {
		t.Errorf("ChatID = %q, want chat-no-user-42", ri.ChatID)
	}
	if !ri.UserID.IsEmpty() {
		t.Errorf("UserID = %q, want empty (unregistered sender)", ri.UserID)
	}
	if len(store.users) != 0 {
		t.Errorf("store.users has %d entries, want 0 (resolver should not create users)", len(store.users))
	}
}

func TestResolve_Group_ExistingChat_RegisteredSender(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("t1")
	agentID := entity.AgentID("a1")

	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, DefaultAgentID: agentID}
	store.workspaceMappings["ch1:bot123"] = tenantID
	store.chats = append(store.chats, entity.Chat{
		ID:             entity.ChatID("chat-42"),
		TenantID:       tenantID,
		ChannelID:      "ch1",
		ExternalChatID: "group-42",
		IsGroup:        true,
	})
	store.users = append(store.users, entity.User{
		ID:       "u1",
		TenantID: tenantID,
	})
	store.userIdentities["u1"] = map[string]string{"ch1": "ext-user-1"}

	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-1",
		ChatID:   "group-42",
		TenantID: "bot123",
		IsGroup:  true,
	}

	ri, err := r.Resolve(context.Background(), "ch1", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ri.TenantID != tenantID {
		t.Errorf("TenantID = %q, want %q", ri.TenantID, tenantID)
	}
	if ri.UserID != "u1" {
		t.Errorf("UserID = %q, want u1", ri.UserID)
	}
	if ri.ChatID != "chat-42" {
		t.Errorf("ChatID = %q, want chat-42", ri.ChatID)
	}
}

// ---------------------------------------------------------------------------
// Tests: No ListTenants
// ---------------------------------------------------------------------------

func TestResolve_NoListTenantsCalled(t *testing.T) {
	store := newMockStore()
	tenantID := entity.TenantID("t1")
	store.tenants[tenantID] = &entity.Tenant{ID: tenantID, DefaultAgentID: "a1"}
	store.workspaceMappings["ch1:bot123"] = tenantID
	store.users = append(store.users, entity.User{
		ID:       "u1",
		TenantID: tenantID,
	})
	store.userIdentities["u1"] = map[string]string{"ch1": "ext-user-1"}

	r := NewResolver(store, onboarding.NewService(store))
	msg := dto.IncomingMessage{
		UserID:   "ext-user-1",
		TenantID: "bot123",
		ChatID:   "ext-user-1",
	}

	_, err := r.Resolve(context.Background(), "ch1", msg)
	if err != nil {
		t.Fatalf("unexpected error (ListTenants may have been called): %v", err)
	}
}
