package conversation

import (
	"context"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockStore implements the subset of port.StorePlugin used by Service.
// Unused methods panic -- tests only call SaveMessage, GetMessages, GetLastConversationID.
type mockStore struct {
	port.StorePlugin // embed to satisfy interface; panics on unused methods
	saveMessageFn    func(ctx context.Context, msg entity.Message) error
	getMessagesFn    func(ctx context.Context, tenantID entity.TenantID, filter port.MessageFilter) ([]entity.Message, error)
	getLastConvIDFn  func(ctx context.Context, msg entity.Message) (entity.ConversationID, error)
}

func (m *mockStore) SaveMessage(ctx context.Context, msg entity.Message) error {
	return m.saveMessageFn(ctx, msg)
}

func (m *mockStore) GetMessages(ctx context.Context, tenantID entity.TenantID, filter port.MessageFilter) ([]entity.Message, error) {
	return m.getMessagesFn(ctx, tenantID, filter)
}

func (m *mockStore) GetLastConversationID(ctx context.Context, msg entity.Message) (entity.ConversationID, error) {
	return m.getLastConvIDFn(ctx, msg)
}

func newMockStore() *mockStore {
	return &mockStore{
		saveMessageFn: func(_ context.Context, _ entity.Message) error {
			return nil
		},
		getMessagesFn: func(_ context.Context, _ entity.TenantID, _ port.MessageFilter) ([]entity.Message, error) {
			return nil, nil
		},
		getLastConvIDFn: func(_ context.Context, _ entity.Message) (entity.ConversationID, error) {
			return "", nil
		},
	}
}

func testMsg() entity.Message {
	return entity.Message{
		TenantID: "t1",
		UserID:   "u1",
		ChatID:   "chat1",
	}
}

func TestResolveConversation_CacheHit(t *testing.T) {
	calls := 0
	store := newMockStore()
	store.getLastConvIDFn = func(_ context.Context, _ entity.Message) (entity.ConversationID, error) {
		calls++
		return "", nil // DB miss -- generates new
	}
	svc := NewService(store)
	ctx := context.Background()
	msg := testMsg()

	id1, err := svc.ResolveConversation(ctx, msg)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	id2, err := svc.ResolveConversation(ctx, msg)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if id1 != id2 {
		t.Errorf("cache miss: got different IDs %q vs %q", id1, id2)
	}
	if calls != 1 {
		t.Errorf("expected 1 DB call, got %d", calls)
	}
}

func TestResolveConversation_DBHit(t *testing.T) {
	store := newMockStore()
	store.getLastConvIDFn = func(_ context.Context, _ entity.Message) (entity.ConversationID, error) {
		return "conv-123", nil
	}
	svc := NewService(store)
	ctx := context.Background()

	id, err := svc.ResolveConversation(ctx, testMsg())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "conv-123" {
		t.Errorf("expected conv-123, got %q", id)
	}
}

func TestResolveConversation_NewConversation(t *testing.T) {
	store := newMockStore()
	// getLastConvIDFn returns empty (default mock) -- triggers new ID generation
	svc := NewService(store)
	ctx := context.Background()

	id, err := svc.ResolveConversation(ctx, testMsg())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id.IsEmpty() {
		t.Error("expected non-empty generated ID")
	}
}

func TestAppend(t *testing.T) {
	var savedMsg entity.Message
	store := newMockStore()
	store.saveMessageFn = func(_ context.Context, msg entity.Message) error {
		savedMsg = msg
		return nil
	}
	svc := NewService(store)
	ctx := context.Background()
	m := testMsg()

	// Resolve first to populate cache.
	convID, _ := svc.ResolveConversation(ctx, m)

	msg := entity.Message{TenantID: "t1", UserID: "u1", MessageContext: m.MessageContext, Content: "hello"}
	if err := svc.Append(ctx, convID, msg); err != nil {
		t.Fatalf("append: %v", err)
	}
	if savedMsg.ConversationID != convID {
		t.Errorf("ConversationID not set: got %q, want %q", savedMsg.ConversationID, convID)
	}
	if savedMsg.TenantID != "t1" {
		t.Errorf("TenantID: got %q, want %q", savedMsg.TenantID, "t1")
	}
}

func TestGetHistory(t *testing.T) {
	var gotFilter port.MessageFilter
	store := newMockStore()
	store.getMessagesFn = func(_ context.Context, _ entity.TenantID, filter port.MessageFilter) ([]entity.Message, error) {
		gotFilter = filter
		return []entity.Message{{Content: "msg1"}}, nil
	}
	svc := NewService(store)
	ctx := context.Background()
	m := testMsg()

	convID, _ := svc.ResolveConversation(ctx, m)
	msgs, err := svc.GetHistory(ctx, convID, 0, 100, nil)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if gotFilter.ConversationID != convID {
		t.Errorf("filter ConversationID: got %q, want %q", gotFilter.ConversationID, convID)
	}
	if !gotFilter.Tail {
		t.Error("expected Tail=true")
	}
	if gotFilter.Limit != 100 {
		t.Errorf("expected Limit=100, got %d", gotFilter.Limit)
	}
}

func TestGetHistory_WithOffset(t *testing.T) {
	var gotFilter port.MessageFilter
	store := newMockStore()
	store.getMessagesFn = func(_ context.Context, _ entity.TenantID, filter port.MessageFilter) ([]entity.Message, error) {
		gotFilter = filter
		return nil, nil
	}
	svc := NewService(store)
	ctx := context.Background()
	m := testMsg()

	convID, _ := svc.ResolveConversation(ctx, m)
	_, err := svc.GetHistory(ctx, convID, 50, 100, nil)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if gotFilter.Offset != 50 {
		t.Errorf("expected Offset=50, got %d", gotFilter.Offset)
	}
	if gotFilter.Limit != 100 {
		t.Errorf("expected Limit=100, got %d", gotFilter.Limit)
	}
	if !gotFilter.Tail {
		t.Error("expected Tail=true")
	}
}

func TestGetHistory_UnknownConvID(t *testing.T) {
	store := newMockStore()
	svc := NewService(store)
	ctx := context.Background()

	msgs, err := svc.GetHistory(ctx, "unknown-conv", 0, 100, nil)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if msgs != nil {
		t.Errorf("expected nil messages, got %v", msgs)
	}
}

func TestResetConversation(t *testing.T) {
	store := newMockStore()
	svc := NewService(store)
	ctx := context.Background()
	m := testMsg()

	id1, _ := svc.ResolveConversation(ctx, m)
	if err := svc.ResetConversation(ctx, id1); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// After reset, resolving the same msg should return a different ID (from cache, not DB).
	id2, _ := svc.ResolveConversation(ctx, m)
	if id1 == id2 {
		t.Errorf("expected different ConversationID after reset, got same %q", id1)
	}
}

func TestSwitchConversation(t *testing.T) {
	store := newMockStore()
	svc := NewService(store)
	ctx := context.Background()
	m := testMsg()

	// Resolve initial conversation.
	origID, _ := svc.ResolveConversation(ctx, m)

	// Switch to a different conversation.
	targetConvID := entity.ConversationID("target-conv-123")
	svc.SwitchConversation(m, targetConvID)

	// Subsequent resolve should return the switched conversation.
	resolvedID, err := svc.ResolveConversation(ctx, m)
	if err != nil {
		t.Fatalf("resolve after switch: %v", err)
	}
	if resolvedID != targetConvID {
		t.Errorf("expected %q after switch, got %q", targetConvID, resolvedID)
	}

	// Old conversation should no longer be in reverse/tenant maps.
	if _, ok := svc.reverse.Load(origID); ok {
		t.Errorf("old conversation %q still in reverse map", origID)
	}
	if _, ok := svc.tenants.Load(origID); ok {
		t.Errorf("old conversation %q still in tenants map", origID)
	}

	// New conversation should be in reverse/tenant maps.
	if _, ok := svc.reverse.Load(targetConvID); !ok {
		t.Errorf("target conversation %q not in reverse map", targetConvID)
	}
	if _, ok := svc.tenants.Load(targetConvID); !ok {
		t.Errorf("target conversation %q not in tenants map", targetConvID)
	}
}

func TestResetConversation_UnknownID(t *testing.T) {
	store := newMockStore()
	svc := NewService(store)
	ctx := context.Background()

	err := svc.ResetConversation(ctx, "unknown-conv-id")
	if err != nil {
		t.Fatalf("expected nil error for unknown ID, got %v", err)
	}
}
