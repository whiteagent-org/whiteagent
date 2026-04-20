package journal_append

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockStore captures the last JournalEntry passed to AppendJournal.
type mockStore struct {
	port.StorePlugin // embed for unsatisfied interface methods
	captured         entity.JournalEntry
	appendErr        error
}

func (m *mockStore) AppendJournal(_ context.Context, _ entity.TenantID, entry entity.JournalEntry) error {
	m.captured = entry
	return m.appendErr
}

func newPlugin(t *testing.T, store port.StorePlugin) *Plugin {
	t.Helper()
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "journal_append", nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	p.SetStore(store)
	return p
}

func TestJournalAppendMapsNoteParamToContentField(t *testing.T) {
	store := &mockStore{}
	p := newPlugin(t, store)

	tc := port.ToolContext{
		TenantID: "t1",
		UserID:   "u1",
		ChatID: "chat-1",
	}
	args := json.RawMessage(`{"category":"Key Events","note":"the agent met the user"}`)

	result, err := p.Execute(context.Background(), tc, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	if store.captured.Content != "the agent met the user" {
		t.Errorf("expected Content %q, got %q", "the agent met the user", store.captured.Content)
	}
	if store.captured.Category != "Key Events" {
		t.Errorf("expected Category %q, got %q", "Key Events", store.captured.Category)
	}
}

func TestJournalAppendWiresMessageIDFromToolContext(t *testing.T) {
	store := &mockStore{}
	p := newPlugin(t, store)

	tc := port.ToolContext{
		TenantID: "t1",
		UserID:   "u1",
		ChatID: "chat-1",
		MessageID: entity.MessageID("msg-xyz-789"),
	}
	args := json.RawMessage(`{"category":"Decisions","note":"decided to go with option A"}`)

	_, err := p.Execute(context.Background(), tc, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if store.captured.MessageID != "msg-xyz-789" {
		t.Errorf("expected MessageID %q, got %q", "msg-xyz-789", store.captured.MessageID)
	}
}

func TestConversationID(t *testing.T) {
	store := &mockStore{}
	p := newPlugin(t, store)

	tc := port.ToolContext{
		TenantID: "t1",
		UserID:   "u1",
		ChatID: "chat-1",
		ConversationID: entity.ConversationID("conv-123"),
	}
	args := json.RawMessage(`{"category":"Key Events","note":"conversation-bound entry"}`)

	_, err := p.Execute(context.Background(), tc, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if store.captured.ConversationID != entity.ConversationID("conv-123") {
		t.Errorf("expected ConversationID %q, got %q", "conv-123", store.captured.ConversationID)
	}
}

func TestJournalAppendLeavesMessageIDEmptyWhenToolContextHasNone(t *testing.T) {
	store := &mockStore{}
	p := newPlugin(t, store)

	tc := port.ToolContext{
		TenantID: "t1",
		UserID:   "u1",
		ChatID: "chat-1",
		// MessageID is zero value (empty)
	}
	args := json.RawMessage(`{"category":"TODOs","note":"follow up tomorrow"}`)

	_, err := p.Execute(context.Background(), tc, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if store.captured.MessageID != "" {
		t.Errorf("expected empty MessageID for unlinked entry, got %q", store.captured.MessageID)
	}
}

// TestGroupContextSetsChatIDOnEntry verifies that in a group context
// (IsGroup=true), the journal entry has ChatID set.
func TestGroupContextSetsChatIDOnEntry(t *testing.T) {
	store := &mockStore{}
	p := newPlugin(t, store)

	tc := port.ToolContext{
		TenantID: "t1",
		UserID:   "u1",
		ChatID:   entity.ChatID("chat-team-123"),
		IsGroup:  true,
	}
	args := json.RawMessage(`{"category":"Key Events","note":"group event happened"}`)

	_, err := p.Execute(context.Background(), tc, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if store.captured.ChatID != entity.ChatID("chat-team-123") {
		t.Errorf("ChatID: got %q, want %q", store.captured.ChatID, "chat-team-123")
	}
}

// TestDMContextSetsChatID verifies that in a DM context (IsGroup=false),
// ChatID is set from the ToolContext.
func TestDMContextSetsChatID(t *testing.T) {
	store := &mockStore{}
	p := newPlugin(t, store)

	tc := port.ToolContext{
		TenantID: "t1",
		UserID:   "u1",
		ChatID:   entity.ChatID("chat-dm-456"),
		IsGroup:  false,
	}
	args := json.RawMessage(`{"category":"Preferences","note":"prefers morning meetings"}`)

	_, err := p.Execute(context.Background(), tc, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if store.captured.ChatID != entity.ChatID("chat-dm-456") {
		t.Errorf("ChatID: got %q, want %q", store.captured.ChatID, "chat-dm-456")
	}
}
