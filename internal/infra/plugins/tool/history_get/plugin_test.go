package history_get

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockStore implements only GetMessages for testing.
type mockStore struct {
	port.StorePlugin
	messages []entity.Message
	filter   port.MessageFilter // captured for assertions
}

func (m *mockStore) GetMessages(_ context.Context, _ entity.TenantID, f port.MessageFilter) ([]entity.Message, error) {
	m.filter = f
	return m.messages, nil
}

func newPlugin(t *testing.T, store port.StorePlugin) *Plugin {
	t.Helper()
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "history_get", nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	p.SetStore(store)
	return p
}

func dmContext() port.ToolContext {
	return port.ToolContext{
		TenantID:       "t1",
		UserID:         "u1",
		ChatID:         "chat-dm-1",
		ConversationID: "conv1",
	}
}

func groupContext() port.ToolContext {
	return port.ToolContext{
		TenantID:       "t1",
		UserID:         "u1",
		ChatID:         "chat-grp-1",
		IsGroup:        true,
		ConversationID: "conv1",
	}
}

func TestGetDMScoping(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"message_id":"msg1"}`)
	p.Execute(context.Background(), dmContext(), args)

	if store.filter.UserID != "u1" {
		t.Errorf("DM should include UserID, got %q", store.filter.UserID)
	}
	if store.filter.MessageID != "msg1" {
		t.Errorf("expected MessageID msg1, got %q", store.filter.MessageID)
	}
	if store.filter.Limit != 1 {
		t.Errorf("expected Limit 1, got %d", store.filter.Limit)
	}
}

func TestGetGroupScoping(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"message_id":"msg1"}`)
	p.Execute(context.Background(), groupContext(), args)

	if store.filter.UserID != "" {
		t.Errorf("group should NOT include UserID, got %q", store.filter.UserID)
	}
}

func TestGetFoundMessage(t *testing.T) {
	ts := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	store := &mockStore{
		messages: []entity.Message{
			{
				ID:             "msg1",
				ConversationID: "conv1",
				Role:           entity.RoleUser,
				Content:        "Full message content here with lots of detail",
				CreatedAt:      ts,
			},
		},
	}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"message_id":"msg1"}`)
	result, err := p.Execute(context.Background(), dmContext(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(result, "ID: msg1") {
		t.Errorf("expected ID line, got: %q", result)
	}
	if !strings.Contains(result, "Conversation: conv1") {
		t.Errorf("expected Conversation line, got: %q", result)
	}
	if !strings.Contains(result, "Time: 2026-03-16T10:00:00Z") {
		t.Errorf("expected Time line, got: %q", result)
	}
	if !strings.Contains(result, "Role: user") {
		t.Errorf("expected Role line, got: %q", result)
	}
	if !strings.Contains(result, "Full message content here with lots of detail") {
		t.Errorf("expected full content (no truncation), got: %q", result)
	}
}

func TestGetNotFound(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"message_id":"nonexistent"}`)
	result, err := p.Execute(context.Background(), dmContext(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "Message not found." {
		t.Errorf("expected %q, got %q", "Message not found.", result)
	}
}

func TestGetMissingMessageID(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)

	args := json.RawMessage(`{}`)
	_, err := p.Execute(context.Background(), dmContext(), args)
	if err == nil {
		t.Fatal("expected error for missing message_id")
	}
}
