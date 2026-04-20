package command

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// mockTransport captures published messages.
type mockTransport struct {
	published []entity.Message
}

func (m *mockTransport) ID() string                                          { return "transport.mock" }
func (m *mockTransport) Kind() entity.PluginKind                             { return entity.PluginKindTransport }
func (m *mockTransport) Init(context.Context, string, json.RawMessage) error { return nil }
func (m *mockTransport) Start(context.Context) error                         { return nil }
func (m *mockTransport) Stop(context.Context) error                          { return nil }
func (m *mockTransport) Status() entity.PluginState                          { return entity.PluginStateHealthy }

func (m *mockTransport) Publish(_ context.Context, _ string, msg entity.Message) error {
	m.published = append(m.published, msg)
	return nil
}
func (m *mockTransport) Subscribe(string, port.MessageHandler) error   { return nil }
func (m *mockTransport) Unsubscribe(string, port.MessageHandler) error { return nil }

// mockConvResetter is a minimal ConversationResetter.
type mockConvResetter struct {
	resetCalled bool
}

func (m *mockConvResetter) ResolveConversation(_ context.Context, _ entity.Message) (entity.ConversationID, error) {
	return "conv-test", nil
}
func (m *mockConvResetter) ResetConversation(_ context.Context, _ entity.ConversationID) error {
	m.resetCalled = true
	return nil
}

// mockStorePlugin implements the parts of StorePlugin we need.
type mockStorePlugin struct {
	port.StorePlugin // embed to satisfy interface
	entries          []entity.ErrorLogEntry
	err              error
	capturedFilter   entity.ErrorLogFilter
	capturedTenantID entity.TenantID
}

func (m *mockStorePlugin) GetErrorLog(_ context.Context, tenantID entity.TenantID, filter entity.ErrorLogFilter) ([]entity.ErrorLogEntry, error) {
	m.capturedTenantID = tenantID
	m.capturedFilter = filter
	return m.entries, m.err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func dmMsg(content string) entity.Message {
	return entity.Message{
		ID:       "msg-1",
		TenantID: "t1",
		UserID:   "u1",
		ChatID:   "chat-1",
		Role:     entity.RoleUser,
		Content:  content,
		Kind:     entity.MessageKindMessage,
	}
}

func groupMsg(content string) entity.Message {
	return entity.Message{
		ID:       "msg-1",
		TenantID: "t1",
		ChatID:   "chat-grp-1",
		IsGroup:  true,
		Role:     entity.RoleUser,
		Content:  content,
		Kind:     entity.MessageKindMessage,
	}
}

// nextCalled tracks whether the next handler was invoked.
func noopNext(_ context.Context, _ entity.Message) error { return nil }

func newTestPlugin(transport *mockTransport, store *mockStorePlugin) *Plugin {
	p := &Plugin{
		id:           pluginID,
		convResetter: &mockConvResetter{},
		transport:    transport,
	}
	if store != nil {
		p.store = store
	}
	return p
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestLogsWithEntries(t *testing.T) {
	tr := &mockTransport{}
	store := &mockStorePlugin{
		entries: []entity.ErrorLogEntry{
			{
				Content:   "LLM error: timeout",
				RefType:   "message",
				RefID:     "msg-abc",
				CreatedAt: time.Date(2026, 3, 12, 14, 30, 0, 0, time.UTC),
			},
			{
				Content:   "Turn timed out",
				RefType:   "message",
				RefID:     "msg-def",
				CreatedAt: time.Date(2026, 3, 12, 15, 0, 0, 0, time.UTC),
			},
		},
	}
	p := newTestPlugin(tr, store)
	handler := p.Wrap(noopNext)

	err := handler(context.Background(), dmMsg("/logs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tr.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(tr.published))
	}

	want := "[Mar 12 14:30] LLM error: timeout (message: msg-abc)\n[Mar 12 15:00] Turn timed out (message: msg-def)"
	if tr.published[0].Content != want {
		t.Errorf("content mismatch\nwant: %s\ngot:  %s", want, tr.published[0].Content)
	}
}

func TestLogsEmpty(t *testing.T) {
	tr := &mockTransport{}
	store := &mockStorePlugin{entries: nil}
	p := newTestPlugin(tr, store)
	handler := p.Wrap(noopNext)

	err := handler(context.Background(), dmMsg("/logs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tr.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(tr.published))
	}
	if tr.published[0].Content != "No recent errors." {
		t.Errorf("expected 'No recent errors.', got %q", tr.published[0].Content)
	}
}

func TestLogsDefaultLimit(t *testing.T) {
	tr := &mockTransport{}
	store := &mockStorePlugin{entries: nil}
	p := newTestPlugin(tr, store)
	handler := p.Wrap(noopNext)

	_ = handler(context.Background(), dmMsg("/logs"))
	if store.capturedFilter.Limit != 10 {
		t.Errorf("expected default limit 10, got %d", store.capturedFilter.Limit)
	}
}

func TestLogsCustomLimit(t *testing.T) {
	tr := &mockTransport{}
	store := &mockStorePlugin{entries: nil}
	p := newTestPlugin(tr, store)
	handler := p.Wrap(noopNext)

	_ = handler(context.Background(), dmMsg("/logs 5"))
	if store.capturedFilter.Limit != 5 {
		t.Errorf("expected limit 5, got %d", store.capturedFilter.Limit)
	}
}

func TestLogsInvalidArg(t *testing.T) {
	tr := &mockTransport{}
	store := &mockStorePlugin{entries: nil}
	p := newTestPlugin(tr, store)
	handler := p.Wrap(noopNext)

	_ = handler(context.Background(), dmMsg("/logs abc"))
	if store.capturedFilter.Limit != 10 {
		t.Errorf("expected fallback limit 10, got %d", store.capturedFilter.Limit)
	}
}

func TestLogsCapsAt100(t *testing.T) {
	tr := &mockTransport{}
	store := &mockStorePlugin{entries: nil}
	p := newTestPlugin(tr, store)
	handler := p.Wrap(noopNext)

	_ = handler(context.Background(), dmMsg("/logs 200"))
	if store.capturedFilter.Limit != 100 {
		t.Errorf("expected capped limit 100, got %d", store.capturedFilter.Limit)
	}
}

func TestLogsUserScoping(t *testing.T) {
	tr := &mockTransport{}
	store := &mockStorePlugin{entries: nil}
	p := newTestPlugin(tr, store)
	handler := p.Wrap(noopNext)

	msg := dmMsg("/logs")
	msg.UserID = "user-42"
	_ = handler(context.Background(), msg)

	if store.capturedFilter.UserID != "user-42" {
		t.Errorf("expected UserID 'user-42', got %q", store.capturedFilter.UserID)
	}
	if store.capturedTenantID != "t1" {
		t.Errorf("expected TenantID 't1', got %q", store.capturedTenantID)
	}
}

func TestLogsGroupChatRejected(t *testing.T) {
	tr := &mockTransport{}
	store := &mockStorePlugin{entries: nil}
	p := newTestPlugin(tr, store)
	handler := p.Wrap(noopNext)

	err := handler(context.Background(), groupMsg("/logs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tr.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(tr.published))
	}
	if tr.published[0].Content != "Error log is only available in direct messages." {
		t.Errorf("expected group rejection message, got %q", tr.published[0].Content)
	}
}

func TestLogsStoreNil(t *testing.T) {
	tr := &mockTransport{}
	p := newTestPlugin(tr, nil) // no store
	handler := p.Wrap(noopNext)

	err := handler(context.Background(), dmMsg("/logs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tr.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(tr.published))
	}
	if tr.published[0].Content != "Error log not available." {
		t.Errorf("expected 'Error log not available.', got %q", tr.published[0].Content)
	}
}

func TestLogsDoesNotCallNext(t *testing.T) {
	tr := &mockTransport{}
	store := &mockStorePlugin{entries: nil}
	p := newTestPlugin(tr, store)

	nextCalled := false
	handler := p.Wrap(func(_ context.Context, _ entity.Message) error {
		nextCalled = true
		return nil
	})

	_ = handler(context.Background(), dmMsg("/logs"))
	if nextCalled {
		t.Error("expected next handler NOT to be called for /logs")
	}
}

// TestNewCommandReplyPropagatesChatID verifies that the /new command's
// confirmation reply carries the ChatID from the incoming message,
// ensuring outbound routing works when the middleware intercepts.
func TestNewCommandReplyPropagatesChatID(t *testing.T) {
	tr := &mockTransport{}
	p := newTestPlugin(tr, nil)

	msg := dmMsg("/new")
	msg.ChatID = "chat-route-test"

	handler := p.Wrap(noopNext)
	err := handler(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tr.published) != 1 {
		t.Fatalf("expected 1 published reply, got %d", len(tr.published))
	}
	reply := tr.published[0]

	if reply.ChatID != "chat-route-test" {
		t.Errorf("expected ChatID chat-route-test, got %q", reply.ChatID)
	}
	if reply.Content != "Session reset. Starting fresh." {
		t.Errorf("unexpected reply content: %q", reply.Content)
	}
}
