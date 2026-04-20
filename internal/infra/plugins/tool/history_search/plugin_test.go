package history_search

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
	if err := p.Init(context.Background(), "history_search", nil); err != nil {
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

func TestSearchDMScoping(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)
	tc := dmContext()

	args := json.RawMessage(`{"query":"hello"}`)
	p.Execute(context.Background(), tc, args)

	if store.filter.ChatID != "chat-dm-1" {
		t.Errorf("expected ChatID chat-dm-1, got %q", store.filter.ChatID)
	}
	if store.filter.UserID != "u1" {
		t.Errorf("DM should include UserID, got %q", store.filter.UserID)
	}
	if store.filter.Query != "hello" {
		t.Errorf("expected Query=hello, got %q", store.filter.Query)
	}
}

func TestSearchGroupScoping(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)
	tc := groupContext()

	args := json.RawMessage(`{"query":"hello"}`)
	p.Execute(context.Background(), tc, args)

	if store.filter.UserID != "" {
		t.Errorf("group should NOT include UserID, got %q", store.filter.UserID)
	}
	if store.filter.ChatID != "chat-grp-1" {
		t.Errorf("expected ChatID=chat-grp-1 for group, got %q", store.filter.ChatID)
	}
}

func TestSearchDefaultRoles(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"query":"test"}`)
	p.Execute(context.Background(), dmContext(), args)

	if len(store.filter.Roles) != 2 {
		t.Fatalf("expected 2 default roles, got %d", len(store.filter.Roles))
	}
	if store.filter.Roles[0] != entity.RoleUser || store.filter.Roles[1] != entity.RoleAssistant {
		t.Errorf("expected [user, assistant], got %v", store.filter.Roles)
	}
}

func TestSearchRoleOverride(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"query":"test","role":"assistant"}`)
	p.Execute(context.Background(), dmContext(), args)

	if len(store.filter.Roles) != 1 || store.filter.Roles[0] != entity.RoleAssistant {
		t.Errorf("expected [assistant], got %v", store.filter.Roles)
	}
}

func TestSearchOptionalFilters(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"query":"test","conversation_id":"conv99","after":"2026-01-01T00:00:00Z","before":"2026-12-31T00:00:00Z"}`)
	p.Execute(context.Background(), dmContext(), args)

	if store.filter.ConversationID != "conv99" {
		t.Errorf("expected ConversationID conv99, got %q", store.filter.ConversationID)
	}
	if store.filter.After == nil {
		t.Fatal("expected After to be set")
	}
	if store.filter.Before == nil {
		t.Fatal("expected Before to be set")
	}
}

func TestSearchDefaultLimit(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"query":"test"}`)
	p.Execute(context.Background(), dmContext(), args)

	if store.filter.Limit != 10 {
		t.Errorf("expected default limit 10, got %d", store.filter.Limit)
	}
}

func TestSearchLimitCappedAt50(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"query":"test","limit":100}`)
	p.Execute(context.Background(), dmContext(), args)

	if store.filter.Limit != 50 {
		t.Errorf("expected limit capped at 50, got %d", store.filter.Limit)
	}
}

func TestSearchEmptyResults(t *testing.T) {
	store := &mockStore{messages: nil}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"query":"nonexistent"}`)
	result, err := p.Execute(context.Background(), dmContext(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "No messages found." {
		t.Errorf("expected %q, got %q", "No messages found.", result)
	}
}

func TestSearchResultFormatting(t *testing.T) {
	ts := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	store := &mockStore{
		messages: []entity.Message{
			{
				ID:             "msg1",
				ConversationID: "conv1",
				Role:           entity.RoleUser,
				Content:        "Hello world",
				CreatedAt:      ts,
			},
		},
	}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"query":"hello"}`)
	result, err := p.Execute(context.Background(), dmContext(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(result, "Found 1 message(s):") {
		t.Errorf("expected summary line, got: %q", result)
	}
	if !strings.Contains(result, "[2026-03-16T10:00:00Z] user msg1 (conv1):") {
		t.Errorf("expected formatted header, got: %q", result)
	}
	if !strings.Contains(result, "Hello world") {
		t.Errorf("expected content, got: %q", result)
	}
}

func TestSearchContentTruncation(t *testing.T) {
	// Build content longer than 300 runes
	longContent := strings.Repeat("a", 350)
	store := &mockStore{
		messages: []entity.Message{
			{
				ID:             "msg1",
				ConversationID: "conv1",
				Role:           entity.RoleUser,
				Content:        longContent,
				CreatedAt:      time.Now(),
			},
		},
	}
	p := newPlugin(t, store)

	args := json.RawMessage(`{"query":"test"}`)
	result, err := p.Execute(context.Background(), dmContext(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(result, "...") {
		t.Error("expected truncation suffix '...'")
	}
	// The content portion should be at most 300 runes + "..."
	// Check that the full 350-char content is NOT present
	if strings.Contains(result, longContent) {
		t.Error("expected content to be truncated")
	}
}
