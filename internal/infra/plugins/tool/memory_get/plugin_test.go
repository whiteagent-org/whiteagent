package memory_get

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockStore captures calls to GetMemory.
type mockStore struct {
	port.StorePlugin // embed for unsatisfied interface methods
	memory           *entity.Memory
	capturedType     string
	capturedID       string
}

func (m *mockStore) GetMemory(_ context.Context, _ entity.TenantID, ownerType, ownerID string) (*entity.Memory, error) {
	m.capturedType = ownerType
	m.capturedID = ownerID
	return m.memory, nil
}

func newPlugin(t *testing.T, store port.StorePlugin) *Plugin {
	t.Helper()
	p := NewPlugin().(*Plugin)
	if err := p.Init(context.Background(), "memory_get", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	p.SetStore(store)
	return p
}

// TestMemoryGetInDMContextRoutesToUserMemory verifies that in a DM context
// (GroupID empty), the tool fetches memory for the user owner type.
func TestMemoryGetInDMContextRoutesToUserMemory(t *testing.T) {
	store := &mockStore{
		memory: &entity.Memory{Content: "User likes tea."},
	}
	p := newPlugin(t, store)

	tc := port.ToolContext{
		TenantID: "t1",
		UserID:   "u-alice",
		// GroupID is zero value — DM context
	}

	result, err := p.Execute(context.Background(), tc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if store.capturedType != "user" {
		t.Errorf("ownerType: got %q, want %q", store.capturedType, "user")
	}
	if store.capturedID != "u-alice" {
		t.Errorf("ownerID: got %q, want %q", store.capturedID, "u-alice")
	}
	if result != "User likes tea." {
		t.Errorf("result: got %q, want %q", result, "User likes tea.")
	}
}

// TestMemoryGetInGroupContextRoutesToGroupMemory verifies that in a group context
// (GroupID non-empty), the tool fetches memory for the group owner type.
func TestMemoryGetInGroupContextRoutesToGroupMemory(t *testing.T) {
	store := &mockStore{
		memory: &entity.Memory{Content: "Team ships on Fridays."},
	}
	p := newPlugin(t, store)

	tc := port.ToolContext{
		TenantID: "t1",
		UserID:   "u-alice",
		ChatID:   entity.ChatID("chat-eng"),
		IsGroup:  true,
	}

	result, err := p.Execute(context.Background(), tc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if store.capturedType != "chat" {
		t.Errorf("ownerType: got %q, want %q", store.capturedType, "chat")
	}
	if store.capturedID != "chat-eng" {
		t.Errorf("ownerID: got %q, want %q", store.capturedID, "chat-eng")
	}
	if result != "Team ships on Fridays." {
		t.Errorf("result: got %q, want %q", result, "Team ships on Fridays.")
	}
}

// TestMemoryGetReturnsNoMemoryMessageWhenNil verifies that the tool returns
// a friendly "No memory stored." message when no memory record exists.
func TestMemoryGetReturnsNoMemoryMessageWhenNil(t *testing.T) {
	store := &mockStore{memory: nil}
	p := newPlugin(t, store)

	tc := port.ToolContext{TenantID: "t1", UserID: "u-new"}

	result, err := p.Execute(context.Background(), tc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "No memory stored." {
		t.Errorf("expected 'No memory stored.', got %q", result)
	}
}
