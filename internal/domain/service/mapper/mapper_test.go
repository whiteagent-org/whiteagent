package mapper

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/identity"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/onboarding"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// stubStore implements the store methods called by the mapper.
type stubStore struct {
	port.StorePlugin // embed to satisfy interface; panics on unimplemented methods

	messages []entity.Message
	chats    []entity.Chat
}

func (s *stubStore) GetMessages(_ context.Context, _ entity.TenantID, filter port.MessageFilter) ([]entity.Message, error) {
	if filter.MessageID != "" {
		for _, m := range s.messages {
			if m.ID == filter.MessageID {
				return []entity.Message{m}, nil
			}
		}
	}
	if filter.ExternalMessageID != "" {
		for _, m := range s.messages {
			if m.MessageContext.ExternalMessageID == filter.ExternalMessageID {
				return []entity.Message{m}, nil
			}
		}
	}
	return nil, nil
}

func (s *stubStore) GetChat(_ context.Context, _ entity.TenantID, chatID entity.ChatID) (*entity.Chat, error) {
	for i := range s.chats {
		if s.chats[i].ID == chatID {
			return &s.chats[i], nil
		}
	}
	return nil, nil
}

func (s *stubStore) SaveChat(_ context.Context, _ entity.TenantID, _ entity.Chat) error {
	return nil
}

func (s *stubStore) GetChatByChannel(_ context.Context, tenantID entity.TenantID, channelID, externalChatID string) (*entity.Chat, error) {
	for i := range s.chats {
		if s.chats[i].TenantID == tenantID && s.chats[i].ChannelID == channelID && s.chats[i].ExternalChatID == externalChatID {
			return &s.chats[i], nil
		}
	}
	return nil, nil
}

// stubIdentityStore implements the store methods used by identity.Resolver.
type stubIdentityStore struct {
	port.StorePlugin

	tenants           []entity.Tenant
	users             map[string]*entity.User    // key: channelID+userExternalID
	workspaceMappings map[string]entity.TenantID // key: channelID+":"+externalTenantID
	chats             []entity.Chat
}

func (s *stubIdentityStore) GetTenantByMapping(_ context.Context, channelID, externalTenantID string) (entity.TenantID, error) {
	if s.workspaceMappings == nil {
		return "", fmt.Errorf("no mapping")
	}
	tid, ok := s.workspaceMappings[channelID+":"+externalTenantID]
	if !ok {
		return "", fmt.Errorf("no mapping")
	}
	return tid, nil
}

func (s *stubIdentityStore) ListTenants(_ context.Context) ([]entity.Tenant, error) {
	return s.tenants, nil
}

func (s *stubIdentityStore) GetTenant(_ context.Context, id entity.TenantID) (*entity.Tenant, error) {
	for _, t := range s.tenants {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, nil
}

func (s *stubIdentityStore) GetUserByChannel(_ context.Context, _ entity.TenantID, channelID, userExternalID string) (*entity.User, error) {
	if u, ok := s.users[channelID+userExternalID]; ok {
		return u, nil
	}
	return nil, nil
}

func (s *stubIdentityStore) SaveUser(_ context.Context, _ entity.TenantID, _ entity.User) error {
	return nil
}

func (s *stubIdentityStore) GetChatByChannel(_ context.Context, tenantID entity.TenantID, channelID, externalChatID string) (*entity.Chat, error) {
	for i := range s.chats {
		if s.chats[i].TenantID == tenantID && s.chats[i].ChannelID == channelID && s.chats[i].ExternalChatID == externalChatID {
			return &s.chats[i], nil
		}
	}
	return nil, nil
}

func (s *stubIdentityStore) SaveChat(_ context.Context, _ entity.TenantID, chat entity.Chat) error {
	s.chats = append(s.chats, chat)
	return nil
}

func (s *stubIdentityStore) GetChat(_ context.Context, _ entity.TenantID, chatID entity.ChatID) (*entity.Chat, error) {
	for i := range s.chats {
		if s.chats[i].ID == chatID {
			return &s.chats[i], nil
		}
	}
	return nil, nil
}

func (s *stubIdentityStore) GetMessages(_ context.Context, _ entity.TenantID, filter port.MessageFilter) ([]entity.Message, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestToMessageSetsUserExternalID verifies that ToMessage populates
// UserExternalID on the MessageContext.
func TestToMessageSetsUserExternalID(t *testing.T) {
	idStore := &stubIdentityStore{
		tenants:           []entity.Tenant{{ID: "t1", DefaultAgentID: "a1"}},
		users:             map[string]*entity.User{"channel-1" + "user-ext-1": {ID: "u1", TenantID: "t1"}},
		workspaceMappings: map[string]entity.TenantID{"channel-1:tenant-ext-1": "t1"},
	}
	resolver := identity.NewResolver(idStore, onboarding.NewService(idStore))
	store := &stubStore{}
	m := NewMapper(store, resolver)

	incoming := dto.IncomingMessage{
		ID:         "msg-42",
		TenantID:   "tenant-ext-1",
		UserID:     "user-ext-1",
		ChatID:     "chat-ext-1",
		Content:    "hello mapper",
		IsGroup:    false,
		Delivery:   map[string]string{"service_url": "https://smba.example.com"},
		ReceivedAt: time.Now(),
	}

	ctx := context.Background()
	msg, _, err := m.ToMessage(ctx, incoming, "channel-1")
	if err != nil {
		t.Fatalf("ToMessage returned error: %v", err)
	}

	// UserExternalID must be set on MessageContext.
	if msg.MessageContext.ExternalUserID != "user-ext-1" {
		t.Errorf("MessageContext.ExternalUserID: got %q, want %q", msg.MessageContext.ExternalUserID, "user-ext-1")
	}

	// ExternalMessageID should carry the incoming message ID.
	if msg.MessageContext.ExternalMessageID != "msg-42" {
		t.Errorf("ExternalMessageID: got %q, want %q", msg.MessageContext.ExternalMessageID, "msg-42")
	}

	if msg.ID == "" || msg.ID == "msg-42" {
		t.Errorf("ID: expected a new unique ID, got %q", msg.ID)
	}
	if msg.AgentID != "a1" {
		t.Errorf("AgentID: got %q, want %q", msg.AgentID, "a1")
	}
	if msg.TenantID != "t1" {
		t.Errorf("TenantID: got %q, want %q", msg.TenantID, "t1")
	}
	if msg.UserID != "u1" {
		t.Errorf("UserID: got %q, want %q", msg.UserID, "u1")
	}
	if msg.Content != "hello mapper" {
		t.Errorf("Content: got %q, want %q", msg.Content, "hello mapper")
	}
	if msg.Role != entity.RoleUser {
		t.Errorf("Role: got %q, want %q", msg.Role, entity.RoleUser)
	}
}

// TestToMessageReturnsIdentityError verifies ToMessage propagates identity resolution errors.
func TestToMessageReturnsIdentityError(t *testing.T) {
	idStore := &stubIdentityStore{
		tenants: []entity.Tenant{{ID: "t1", DefaultAgentID: "a1"}},
		users:   map[string]*entity.User{}, // no users registered
	}
	resolver := identity.NewResolver(idStore, onboarding.NewService(idStore))
	store := &stubStore{}
	m := NewMapper(store, resolver)

	incoming := dto.IncomingMessage{
		ID:     "msg-1",
		UserID: "unknown-user",
		ChatID: "chat-1",
	}

	_, _, err := m.ToMessage(context.Background(), incoming, "channel-1")
	if err == nil {
		t.Fatal("expected error from ToMessage for unknown user, got nil")
	}
}

// TestToOutgoingResolvesFromChat verifies that ToOutgoing fetches the chat entity
// to populate delivery, channel, and external chat ID.
func TestToOutgoingResolvesFromChat(t *testing.T) {
	store := &stubStore{
		chats: []entity.Chat{
			{
				ID:             "chat-out-1",
				TenantID:       "t1",
				ChannelID:      "channel-teams",
				ExternalChatID: "ext-chat-1",
				Delivery: map[string]string{
					"service_url":     "https://smba.example.com",
					"conversation_id": "conv-out",
				},
			},
		},
	}
	m := NewMapper(store, nil)

	msg := entity.Message{
		ID:       "msg-out-1",
		TenantID: "t1",
		ChatID:   "chat-out-1",
		Kind:     entity.MessageKindMessage,
		Role:     entity.RoleAssistant,
		Content:  "bot response",
	}

	ctx := context.Background()
	outgoing, err := m.ToOutgoing(ctx, msg)
	if err != nil {
		t.Fatalf("ToOutgoing returned error: %v", err)
	}

	if outgoing.ChannelID != "channel-teams" {
		t.Errorf("ChannelID: got %q, want %q", outgoing.ChannelID, "channel-teams")
	}
	if outgoing.ChatID != "ext-chat-1" {
		t.Errorf("ChatID: got %q, want %q", outgoing.ChatID, "ext-chat-1")
	}
	if outgoing.Delivery["service_url"] != "https://smba.example.com" {
		t.Errorf("Delivery service_url: got %q, want %q", outgoing.Delivery["service_url"], "https://smba.example.com")
	}
	if outgoing.ID != "msg-out-1" {
		t.Errorf("ID: got %q, want %q", outgoing.ID, "msg-out-1")
	}
	if outgoing.Content != "bot response" {
		t.Errorf("Content: got %q, want %q", outgoing.Content, "bot response")
	}
}

// TestToOutgoingResolvesTargetID verifies that when entity.Message has a TargetID,
// the mapper looks up the target message in the store and uses its ExternalMessageID.
func TestToOutgoingResolvesTargetID(t *testing.T) {
	store := &stubStore{
		messages: []entity.Message{
			{
				ID:       "target-internal-id",
				TenantID: "t1",
				MessageContext: entity.MessageContext{
					ExternalMessageID: "ext-target-123",
				},
			},
		},
		chats: []entity.Chat{
			{
				ID:             "chat-1",
				TenantID:       "t1",
				ChannelID:      "channel-1",
				ExternalChatID: "ext-chat-1",
			},
		},
	}
	m := NewMapper(store, nil)

	msg := entity.Message{
		ID:       "msg-reply",
		TenantID: "t1",
		ChatID:   "chat-1",
		TargetID: "target-internal-id",
		Kind:     entity.MessageKindMessage,
		Content:  "reply content",
	}

	ctx := context.Background()
	outgoing, err := m.ToOutgoing(ctx, msg)
	if err != nil {
		t.Fatalf("ToOutgoing returned error: %v", err)
	}

	if outgoing.TargetID != "ext-target-123" {
		t.Errorf("TargetID: got %q, want %q", outgoing.TargetID, "ext-target-123")
	}
}

// TestToOutgoingTargetIDNotFound verifies that when TargetID lookup returns no results,
// outgoing.TargetID is empty (does not block sending).
func TestToOutgoingTargetIDNotFound(t *testing.T) {
	store := &stubStore{
		chats: []entity.Chat{
			{ID: "chat-1", TenantID: "t1", ChannelID: "channel-1", ExternalChatID: "ext-chat-1"},
		},
	}
	m := NewMapper(store, nil)

	msg := entity.Message{
		ID:       "msg-reply",
		TenantID: "t1",
		ChatID:   "chat-1",
		TargetID: "nonexistent-target",
		Kind:     entity.MessageKindMessage,
		Content:  "reply content",
	}

	ctx := context.Background()
	outgoing, err := m.ToOutgoing(ctx, msg)
	if err != nil {
		t.Fatalf("ToOutgoing returned error: %v", err)
	}

	if outgoing.TargetID != "" {
		t.Errorf("TargetID: expected empty when not found, got %q", outgoing.TargetID)
	}
}

// TestToOutgoingNoTargetIDNoStoreLookup verifies that when TargetID is empty,
// no store lookup is performed.
func TestToOutgoingNoTargetIDNoStoreLookup(t *testing.T) {
	store := &stubStore{
		chats: []entity.Chat{
			{ID: "chat-1", TenantID: "t1", ChannelID: "channel-1", ExternalChatID: "ext-chat-1"},
		},
	}
	m := NewMapper(store, nil)

	msg := entity.Message{
		ID:       "msg-no-target",
		TenantID: "t1",
		ChatID:   "chat-1",
		Kind:     entity.MessageKindMessage,
		Content:  "no reply",
	}

	ctx := context.Background()
	outgoing, err := m.ToOutgoing(ctx, msg)
	if err != nil {
		t.Fatalf("ToOutgoing returned error: %v", err)
	}

	if outgoing.TargetID != "" {
		t.Errorf("TargetID: expected empty, got %q", outgoing.TargetID)
	}
}

// TestToMessageReplyFound verifies that when ReplyToID is set and the store has the
// replied-to message, RepliedToID is set and the returned ConversationID matches.
func TestToMessageReplyFound(t *testing.T) {
	idStore := &stubIdentityStore{
		tenants:           []entity.Tenant{{ID: "t1", DefaultAgentID: "a1"}},
		users:             map[string]*entity.User{"ch1" + "u-ext": {ID: "u1", TenantID: "t1"}},
		workspaceMappings: map[string]entity.TenantID{"ch1:bot1": "t1"},
	}
	resolver := identity.NewResolver(idStore, onboarding.NewService(idStore))
	store := &stubStore{
		messages: []entity.Message{
			{
				ID:             "replied-msg-id",
				TenantID:       "t1",
				ConversationID: "conv-old",
				MessageContext: entity.MessageContext{
					ExternalMessageID: "ext-42",
				},
			},
		},
	}
	m := NewMapper(store, resolver)

	incoming := dto.IncomingMessage{
		ID:         "msg-99",
		TenantID:   "bot1",
		UserID:     "u-ext",
		ChatID:     "chat1",
		Content:    "replying",
		ReplyToID:  "ext-42",
		ReceivedAt: time.Now(),
	}

	msg, replyConvID, err := m.ToMessage(context.Background(), incoming, "ch1")
	if err != nil {
		t.Fatalf("ToMessage error: %v", err)
	}
	if msg.RepliedToID != "replied-msg-id" {
		t.Errorf("RepliedToID: got %q, want %q", msg.RepliedToID, "replied-msg-id")
	}
	if replyConvID != "conv-old" {
		t.Errorf("replyConvID: got %q, want %q", replyConvID, "conv-old")
	}
	if msg.MessageContext.ExternalReplyToID != "ext-42" {
		t.Errorf("ExternalReplyToID: got %q, want %q", msg.MessageContext.ExternalReplyToID, "ext-42")
	}
}

// TestToMessageReplyNotFound verifies that when ReplyToID is set but the store has
// no matching message, RepliedToID is empty, ConversationID is empty, and no error.
func TestToMessageReplyNotFound(t *testing.T) {
	idStore := &stubIdentityStore{
		tenants:           []entity.Tenant{{ID: "t1", DefaultAgentID: "a1"}},
		users:             map[string]*entity.User{"ch1" + "u-ext": {ID: "u1", TenantID: "t1"}},
		workspaceMappings: map[string]entity.TenantID{"ch1:bot1": "t1"},
	}
	resolver := identity.NewResolver(idStore, onboarding.NewService(idStore))
	store := &stubStore{} // empty store
	m := NewMapper(store, resolver)

	incoming := dto.IncomingMessage{
		ID:         "msg-100",
		TenantID:   "bot1",
		UserID:     "u-ext",
		ChatID:     "chat1",
		Content:    "reply to unknown",
		ReplyToID:  "nonexistent-ext-id",
		ReceivedAt: time.Now(),
	}

	msg, replyConvID, err := m.ToMessage(context.Background(), incoming, "ch1")
	if err != nil {
		t.Fatalf("ToMessage error: %v", err)
	}
	if msg.RepliedToID != "" {
		t.Errorf("RepliedToID: expected empty, got %q", msg.RepliedToID)
	}
	if replyConvID != "" {
		t.Errorf("replyConvID: expected empty, got %q", replyConvID)
	}
}

// TestToOutgoing_DeliveryPopulated verifies that when chat entity has ExternalChatID
// but no explicit delivery, the outgoing Delivery["chat_id"] is auto-populated.
func TestToOutgoing_DeliveryPopulated(t *testing.T) {
	store := &stubStore{
		chats: []entity.Chat{
			{ID: "chat-42", TenantID: "t1", ChannelID: "channel-1", ExternalChatID: "ext-chat-42"},
		},
	}
	m := NewMapper(store, nil)

	msg := entity.Message{
		ID:       "msg-no-delivery",
		TenantID: "t1",
		ChatID:   "chat-42",
		Kind:     entity.MessageKindMessage,
		Content:  "hello",
	}

	ctx := context.Background()
	outgoing, err := m.ToOutgoing(ctx, msg)
	if err != nil {
		t.Fatalf("ToOutgoing returned error: %v", err)
	}
	if outgoing.Delivery == nil {
		t.Fatal("expected Delivery to be non-nil")
	}
	if outgoing.Delivery["chat_id"] != "ext-chat-42" {
		t.Errorf("Delivery chat_id: got %q, want %q", outgoing.Delivery["chat_id"], "ext-chat-42")
	}
}

// TestToOutgoing_DeliveryPreserved verifies that when chat entity has explicit
// delivery with chat_id, it is preserved.
func TestToOutgoing_DeliveryPreserved(t *testing.T) {
	store := &stubStore{
		chats: []entity.Chat{
			{
				ID:             "chat-explicit",
				TenantID:       "t1",
				ChannelID:      "channel-1",
				ExternalChatID: "ext-should-not-override",
				Delivery:       map[string]string{"chat_id": "explicit-chat", "service_url": "https://example.com"},
			},
		},
	}
	m := NewMapper(store, nil)

	msg := entity.Message{
		ID:       "msg-with-delivery",
		TenantID: "t1",
		ChatID:   "chat-explicit",
		Kind:     entity.MessageKindMessage,
		Content:  "hello",
	}

	ctx := context.Background()
	outgoing, err := m.ToOutgoing(ctx, msg)
	if err != nil {
		t.Fatalf("ToOutgoing returned error: %v", err)
	}
	if outgoing.Delivery["chat_id"] != "explicit-chat" {
		t.Errorf("Delivery chat_id: got %q, want %q (should not be overwritten)", outgoing.Delivery["chat_id"], "explicit-chat")
	}
	if outgoing.Delivery["service_url"] != "https://example.com" {
		t.Errorf("Delivery service_url: got %q, want %q", outgoing.Delivery["service_url"], "https://example.com")
	}
}
