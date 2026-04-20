package journal_search

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockStore returns canned journal entries and user lookups.
type mockStore struct {
	port.StorePlugin // embed for unsatisfied interface methods
	entries          []entity.JournalEntry
	users            map[entity.UserID]*entity.User
}

func (m *mockStore) GetJournal(_ context.Context, _ entity.TenantID, _ entity.JournalFilter) ([]entity.JournalEntry, error) {
	return m.entries, nil
}

func (m *mockStore) GetUser(_ context.Context, _ entity.TenantID, uid entity.UserID) (*entity.User, error) {
	if m.users != nil {
		if u, ok := m.users[uid]; ok {
			return u, nil
		}
	}
	return nil, nil
}

func newPlugin(t *testing.T, store port.StorePlugin) *Plugin {
	t.Helper()
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "journal_search", nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	p.SetStore(store)
	return p
}

func TestJournalSearchResultsUseContentField(t *testing.T) {
	ts := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store := &mockStore{
		entries: []entity.JournalEntry{
			{
				UserID:    "u1",
				Category:  "Key Events",
				Content:   "the user prefers concise answers",
				CreatedAt: ts,
			},
		},
		users: map[entity.UserID]*entity.User{
			"u1": {ID: "u1", Name: "Alice"},
		},
	}
	p := newPlugin(t, store)

	tc := port.ToolContext{
		TenantID: "t1",
		UserID:   "u1",
		ChatID: "chat-1",
	}
	args := json.RawMessage(`{"query":"concise"}`)

	result, err := p.Execute(context.Background(), tc, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(result, "the user prefers concise answers") {
		t.Errorf("expected result to contain entry Content, got: %q", result)
	}
	if !strings.Contains(result, "Key Events") {
		t.Errorf("expected result to contain Category, got: %q", result)
	}
}

func TestJournalSearchReturnsNoEntriesMessage(t *testing.T) {
	store := &mockStore{entries: nil}
	p := newPlugin(t, store)

	tc := port.ToolContext{
		TenantID: "t1",
		UserID:   "u1",
		ChatID: "chat-1",
	}
	args := json.RawMessage(`{}`)

	result, err := p.Execute(context.Background(), tc, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result != "No journal entries found." {
		t.Errorf("expected %q, got %q", "No journal entries found.", result)
	}
}
