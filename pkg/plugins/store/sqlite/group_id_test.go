package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// TestChatSaveAndGetByIDRoundTrip verifies that SaveChat assigns a UUID primary key
// and GetChat retrieves the chat by that ID.
func TestChatSaveAndGetByIDRoundTrip(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t-chat-1")
	seedTestData(t, p, tid, entity.UserID("u1"), entity.AgentID("a1"))

	chatID := entity.ChatID("chat-uuid-abc-123")
	chat := entity.Chat{
		ID:             chatID,
		TenantID:       tid,
		ChannelID:      "channel.telegram",
		ExternalChatID: "tg-chat-9001",
		IsGroup:        true,
		Name:           "Test Group",
		CreatedAt:      time.Now().UTC().Truncate(time.Second),
	}

	if err := p.SaveChat(ctx, tid, chat); err != nil {
		t.Fatalf("SaveChat: %v", err)
	}

	got, err := p.GetChat(ctx, tid, chatID)
	if err != nil {
		t.Fatalf("GetChat: %v", err)
	}
	if got == nil {
		t.Fatal("GetChat: expected non-nil chat, got nil")
	}
	if got.ID != chatID {
		t.Errorf("ID: got %q, want %q", got.ID, chatID)
	}
	if got.TenantID != tid {
		t.Errorf("TenantID: got %q, want %q", got.TenantID, tid)
	}
	if got.ChannelID != "channel.telegram" {
		t.Errorf("ChannelID: got %q, want %q", got.ChannelID, "channel.telegram")
	}
	if got.ExternalChatID != "tg-chat-9001" {
		t.Errorf("ExternalChatID: got %q, want %q", got.ExternalChatID, "tg-chat-9001")
	}
	if !got.IsGroup {
		t.Error("expected IsGroup to be true")
	}
}

// TestGetChatByChannelRoundTrip verifies GetChatByChannel returns the chat
// with its persisted fields intact.
func TestGetChatByChannelRoundTrip(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t-chat-2")
	seedTestData(t, p, tid, entity.UserID("u1"), entity.AgentID("a1"))

	chatID := entity.ChatID("chat-channel-lookup-id")
	chat := entity.Chat{
		ID:             chatID,
		TenantID:       tid,
		ChannelID:      "channel.telegram",
		ExternalChatID: "tg-chat-channel-lookup",
		CreatedAt:      time.Now().UTC(),
	}

	if err := p.SaveChat(ctx, tid, chat); err != nil {
		t.Fatalf("SaveChat: %v", err)
	}

	got, err := p.GetChatByChannel(ctx, tid, "channel.telegram", "tg-chat-channel-lookup")
	if err != nil {
		t.Fatalf("GetChatByChannel: %v", err)
	}
	if got == nil {
		t.Fatal("GetChatByChannel: expected non-nil chat, got nil")
	}
	if got.ID != chatID {
		t.Errorf("GetChatByChannel preserves ID: got %q, want %q", got.ID, chatID)
	}
}

// TestMessagesSavedWithChatIDCanBeFilteredByChatID verifies that messages saved
// with a ChatID can be retrieved by filtering on MessageFilter.ChatID.
func TestMessagesSavedWithChatIDCanBeFilteredByChatID(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t-cmsg-1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	chatID := entity.ChatID("chat-filter-test")
	now := time.Now().UTC().Truncate(time.Second)

	// Save a chat message.
	chatMsg := entity.Message{
		ID:             "cmsg-1",
		TenantID:       tid,
		UserID:         uid,
		AgentID:        aid,
		ConversationID: "conv-chat",
		ChatID:         chatID,
		IsGroup:        true,
		Kind:           entity.MessageKindMessage,
		Role:           entity.RoleUser,
		Content:        "Hello chat!",
		CreatedAt:      now,
	}
	if err := p.SaveMessage(ctx, chatMsg); err != nil {
		t.Fatalf("SaveMessage (chat): %v", err)
	}

	// Save a different chat message.
	otherMsg := entity.Message{
		ID:             "cmsg-2",
		TenantID:       tid,
		UserID:         uid,
		AgentID:        aid,
		ConversationID: "conv-other",
		ChatID:         "chat-other",
		Kind:           entity.MessageKindMessage,
		Role:           entity.RoleUser,
		Content:        "Hello other!",
		CreatedAt:      now.Add(time.Second),
	}
	if err := p.SaveMessage(ctx, otherMsg); err != nil {
		t.Fatalf("SaveMessage (other): %v", err)
	}

	// Filter by ChatID: should only return the matching message.
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ChatID: chatID})
	if err != nil {
		t.Fatalf("GetMessages with ChatID filter: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message with ChatID filter, got %d", len(msgs))
	}
	if msgs[0].ID != "cmsg-1" {
		t.Errorf("expected cmsg-1, got %q", msgs[0].ID)
	}
	if msgs[0].ChatID != chatID {
		t.Errorf("ChatID: got %q, want %q", msgs[0].ChatID, chatID)
	}
}
