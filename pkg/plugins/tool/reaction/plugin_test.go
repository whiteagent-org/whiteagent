package reaction

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockTransport captures published messages.
type mockTransport struct {
	port.TransportPlugin
	published []entity.Message
}

func (m *mockTransport) ID() string                 { return "mock-transport" }
func (m *mockTransport) Kind() entity.PluginKind    { return entity.PluginKindTransport }
func (m *mockTransport) Status() entity.PluginState { return entity.PluginStateHealthy }

func (m *mockTransport) Init(_ context.Context, _ string, _ json.RawMessage) error { return nil }
func (m *mockTransport) Start(_ context.Context) error                             { return nil }
func (m *mockTransport) Stop(_ context.Context) error                              { return nil }

func (m *mockTransport) Publish(_ context.Context, _ string, msg entity.Message) error {
	m.published = append(m.published, msg)
	return nil
}

func (m *mockTransport) Subscribe(_ string, _ port.MessageHandler) error   { return nil }
func (m *mockTransport) Unsubscribe(_ string, _ port.MessageHandler) error { return nil }

func newTestPlugin() *Plugin {
	p := &Plugin{}
	_ = p.Init(context.Background(), "reaction", nil)
	return p
}

func TestReactionPublishesChatID(t *testing.T) {
	p := newTestPlugin()
	mt := &mockTransport{}
	p.SetTransport(mt)

	args, _ := json.Marshal(reactionArgs{Emoji: "thumbs_up"})
	tc := port.ToolContext{
		TenantID:  "t1",
		UserID:    "u1",
		MessageID: "msg-1",
		ChatID:    "chat-teams-42",
	}

	result, err := p.Execute(context.Background(), tc, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Reaction added." {
		t.Fatalf("unexpected result: %s", result)
	}
	if len(mt.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(mt.published))
	}

	msg := mt.published[0]
	if msg.ChatID != "chat-teams-42" {
		t.Errorf("expected ChatID chat-teams-42, got %q", msg.ChatID)
	}
	if msg.Kind != entity.MessageKindReaction {
		t.Errorf("expected Kind reaction, got %q", msg.Kind)
	}
	if msg.TargetID != "msg-1" {
		t.Errorf("expected TargetID msg-1, got %q", msg.TargetID)
	}
}

func TestReactionWithCustomTarget(t *testing.T) {
	p := newTestPlugin()
	mt := &mockTransport{}
	p.SetTransport(mt)

	args, _ := json.Marshal(reactionArgs{Emoji: "heart", TargetMsgID: "custom-target"})
	tc := port.ToolContext{
		TenantID:  "t1",
		UserID:    "u1",
		MessageID: "msg-2",
		ChatID:    "chat-tg-12345",
	}

	result, err := p.Execute(context.Background(), tc, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Reaction added." {
		t.Fatalf("unexpected result: %s", result)
	}
	if len(mt.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(mt.published))
	}
	if mt.published[0].TargetID != "custom-target" {
		t.Errorf("expected TargetID custom-target, got %q", mt.published[0].TargetID)
	}
}
