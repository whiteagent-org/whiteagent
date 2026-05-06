package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/dto"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/llm"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/prompt"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// noopSecretService is a minimal SecretService stub for loop tests.
// Only Redact is called by the loop; it returns text unchanged.
type noopSecretService struct{}

func (n *noopSecretService) Set(context.Context, entity.TenantID, entity.UserID, string, []byte, entity.SecretMode) error {
	panic("not used in loop tests")
}
func (n *noopSecretService) Get(context.Context, entity.TenantID, entity.UserID, string) ([]byte, error) {
	panic("not used in loop tests")
}
func (n *noopSecretService) Exists(context.Context, entity.TenantID, entity.UserID, string) (bool, error) {
	panic("not used in loop tests")
}
func (n *noopSecretService) List(context.Context, entity.TenantID, entity.UserID) ([]secret.SecretEntry, error) {
	panic("not used in loop tests")
}
func (n *noopSecretService) Delete(context.Context, entity.TenantID, entity.UserID, string) error {
	panic("not used in loop tests")
}
func (n *noopSecretService) Redact(_ context.Context, text string, _ entity.TenantID, _ entity.UserID) string {
	return text
}
func (n *noopSecretService) EnvVars(context.Context, entity.TenantID, entity.UserID) ([]secret.SecretEnvEntry, error) {
	panic("not used in loop tests")
}
func (n *noopSecretService) RequestEntry(context.Context, []string, map[string]entity.SecretMode, entity.TenantID, entity.UserID, entity.ConversationID, entity.ChatID) (string, error) {
	panic("not used in loop tests")
}
func (n *noopSecretService) ValidateToken(context.Context, string) (*entity.SecretToken, error) {
	panic("not used in loop tests")
}
func (n *noopSecretService) ConsumeToken(context.Context, string, map[string]secret.SecretSubmission) error {
	panic("not used in loop tests")
}

// mockCompletionService returns a simple text response (no tool calls).
type mockCompletionService struct{}

func (m *mockCompletionService) Complete(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	return &port.CompletionResponse{Content: "hello"}, nil
}

// mockStorePlugin implements port.StorePlugin with minimal stubs.
type mockStorePlugin struct {
	port.StorePlugin // embed to satisfy interface -- panics on unimplemented calls
	errorLogs        []entity.ErrorLogEntry
	appendErrorLogFn func(entity.ErrorLogEntry) error
	capturedAgentID  entity.AgentID // last AgentID passed to GetAgent
}

func (m *mockStorePlugin) ID() string                                          { return "store.mock" }
func (m *mockStorePlugin) Kind() entity.PluginKind                             { return entity.PluginKindStore }
func (m *mockStorePlugin) Init(context.Context, string, json.RawMessage) error { return nil }
func (m *mockStorePlugin) Start(context.Context) error                         { return nil }
func (m *mockStorePlugin) Stop(context.Context) error                          { return nil }
func (m *mockStorePlugin) Status() entity.PluginState                          { return entity.PluginStateHealthy }
func (m *mockStorePlugin) MigrateToLatest(context.Context) error               { return nil }

func (m *mockStorePlugin) GetTenant(_ context.Context, _ entity.TenantID) (*entity.Tenant, error) {
	return &entity.Tenant{
		ID:             "t1",
		DefaultAgentID: "a1",
	}, nil
}

func (m *mockStorePlugin) GetAgent(_ context.Context, _ entity.TenantID, id entity.AgentID) (*entity.Agent, error) {
	m.capturedAgentID = id
	return &entity.Agent{ID: id, Name: "test"}, nil
}

func (m *mockStorePlugin) GetUser(_ context.Context, _ entity.TenantID, _ entity.UserID) (*entity.User, error) {
	return nil, nil
}

func (m *mockStorePlugin) AppendErrorLog(_ context.Context, _ entity.TenantID, entry entity.ErrorLogEntry) error {
	m.errorLogs = append(m.errorLogs, entry)
	if m.appendErrorLogFn != nil {
		return m.appendErrorLogFn(entry)
	}
	return nil
}

func (m *mockStorePlugin) EvictMessages(_ context.Context, _ entity.TenantID, _ entity.ConversationID, _ []entity.MessageID) error {
	return nil
}

// errorCompletionService always returns an error.
type errorCompletionService struct{}

func (m *errorCompletionService) Complete(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	return nil, fmt.Errorf("model unavailable")
}

// toolLoopCompletionService always returns a tool call (never a final response).
type toolLoopCompletionService struct{}

func (m *toolLoopCompletionService) Complete(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	return &port.CompletionResponse{
		Content: "thinking...",
		ToolCalls: []entity.ToolCall{
			{ID: "tc-1", Name: "fake_tool", Arguments: "{}"},
		},
	}, nil
}

// slowCompletionService blocks until context is cancelled.
type slowCompletionService struct{}

func (m *slowCompletionService) Complete(ctx context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// mockConvService is a minimal ConversationService.
type mockConvService struct {
	appended []entity.Message
}

func (m *mockConvService) ResolveConversation(_ context.Context, _ entity.Message) (entity.ConversationID, error) {
	return "conv-test", nil
}
func (m *mockConvService) Append(_ context.Context, _ entity.ConversationID, msg entity.Message) error {
	m.appended = append(m.appended, msg)
	return nil
}
func (m *mockConvService) GetHistory(context.Context, entity.ConversationID, int, int, *entity.MessageID) ([]entity.Message, error) {
	return nil, nil
}
func (m *mockConvService) RegisterConversation(entity.TenantID, entity.ConversationID)    {}
func (m *mockConvService) ResetConversation(context.Context, entity.ConversationID) error { return nil }
func (m *mockConvService) SwitchConversation(entity.Message, entity.ConversationID)       {}

// mockTransport captures published messages.
type mockTransport struct {
	published       []entity.Message
	publishedTopics []string
	publishErr      error
}

func (m *mockTransport) ID() string                                          { return "transport.mock" }
func (m *mockTransport) Kind() entity.PluginKind                             { return entity.PluginKindTransport }
func (m *mockTransport) Init(context.Context, string, json.RawMessage) error { return nil }
func (m *mockTransport) Start(context.Context) error                         { return nil }
func (m *mockTransport) Stop(context.Context) error                          { return nil }
func (m *mockTransport) Status() entity.PluginState                          { return entity.PluginStateHealthy }

func (m *mockTransport) Publish(_ context.Context, topic string, msg entity.Message) error {
	m.publishedTopics = append(m.publishedTopics, topic)
	m.published = append(m.published, msg)
	return m.publishErr
}
func (m *mockTransport) Subscribe(string, port.MessageHandler) error   { return nil }
func (m *mockTransport) Unsubscribe(string, port.MessageHandler) error { return nil }

// mockChannel implements port.ChannelPlugin (no IndicatorAware).
type mockChannel struct{}

func (m *mockChannel) ID() string                                          { return "channel.plain" }
func (m *mockChannel) Kind() entity.PluginKind                             { return entity.PluginKindChannel }
func (m *mockChannel) Init(context.Context, string, json.RawMessage) error { return nil }
func (m *mockChannel) Start(context.Context) error                         { return nil }
func (m *mockChannel) Stop(context.Context) error                          { return nil }
func (m *mockChannel) Status() entity.PluginState                          { return entity.PluginStateHealthy }
func (m *mockChannel) SetMessageHandler(port.IncomingMessageHandler)       {}
func (m *mockChannel) Send(_ context.Context, _ dto.OutgoingMessage) (port.SendResult, error) {
	return port.SendResult{}, nil
}
func (m *mockChannel) RegisterRoutes(*http.ServeMux) {}

// mockIndicatorChannel implements both ChannelPlugin and IndicatorAware.
type mockIndicatorChannel struct {
	mockChannel
	indicateCalled     atomic.Bool
	stopCalled         atomic.Bool
	capturedIndication map[string]string
}

func (m *mockIndicatorChannel) ID() string { return "channel.indicator" }

func (m *mockIndicatorChannel) Indicate(_ context.Context, indication map[string]string) (stop func()) {
	m.indicateCalled.Store(true)
	m.capturedIndication = indication
	return func() {
		m.stopCalled.Store(true)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testMsg(channelID string) entity.Message {
	return entity.Message{
		ID:       "msg-1",
		TenantID: "t1",
		UserID:   "u1",
		ChatID:   "chat-42",
		Role:     entity.RoleUser,
		Content:  "hi",
		Kind:     entity.MessageKindMessage,
	}
}

func (m *mockStorePlugin) GetChat(_ context.Context, _ entity.TenantID, chatID entity.ChatID) (*entity.Chat, error) {
	return &entity.Chat{
		ID:             chatID,
		ChannelID:      "channel.indicator",
		ExternalChatID: "chat-42",
		Indication:     map[string]string{"chat_id": "chat-42"},
	}, nil
}

func testLoop(channels map[string]port.ChannelEntry) *Loop {
	cfg := LoopConfig{MaxIterations: 5, TurnTimeout: 5 * time.Second, MaxWorkers: 2}
	pb, _ := prompt.NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	return NewLoop(
		cfg,
		&mockCompletionService{},
		&mockStorePlugin{},
		&mockConvService{},
		&mockTransport{},
		nil, // no tools
		pb,
		nil,
		channels,
		&noopSecretService{},
		nil, nil, // sandbox, scopedFS
	)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestIndicationCalledAndStopped(t *testing.T) {
	ch := &mockIndicatorChannel{}
	channels := map[string]port.ChannelEntry{
		"channel.indicator": {Plugin: ch, Capabilities: port.ChannelCapabilities{Indication: true}},
	}
	l := testLoop(channels)

	err := l.Handle(context.Background(), testMsg("channel.indicator"))
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if !ch.indicateCalled.Load() {
		t.Error("expected Indicate to be called")
	}
	if !ch.stopCalled.Load() {
		t.Error("expected stop func to be called")
	}
	if ch.capturedIndication["chat_id"] != "chat-42" {
		t.Errorf("expected indication chat_id %q, got %v", "chat-42", ch.capturedIndication)
	}
}

func TestIndicationSkippedForNonAwareChannel(t *testing.T) {
	ch := &mockChannel{}
	channels := map[string]port.ChannelEntry{
		"channel.plain": {Plugin: ch, Capabilities: port.ChannelCapabilities{}},
	}
	l := testLoop(channels)

	err := l.Handle(context.Background(), testMsg("channel.plain"))
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	// No panic, no error -- pass.
}

func TestDeliveryPropagatedToResponse(t *testing.T) {
	transport := &mockTransport{}
	ch := &mockChannel{}
	channels := map[string]port.ChannelEntry{
		"channel.plain": {Plugin: ch, Capabilities: port.ChannelCapabilities{}},
	}

	cfg := LoopConfig{MaxIterations: 5, TurnTimeout: 5 * time.Second, MaxWorkers: 2}
	pb, _ := prompt.NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	l := NewLoop(cfg, &mockCompletionService{}, &mockStorePlugin{}, &mockConvService{}, transport, nil, pb, nil, channels, &noopSecretService{}, nil, nil)

	msg := testMsg("channel.plain")

	err := l.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(transport.published) == 0 {
		t.Fatal("expected at least one published message")
	}
	resp := transport.published[0]
	if resp.ChatID != msg.ChatID {
		t.Errorf("expected ChatID %q, got %q", msg.ChatID, resp.ChatID)
	}
	if resp.CausedByID != msg.ID {
		t.Errorf("expected CausedByID %q (original message ID), got %q", msg.ID, resp.CausedByID)
	}
}

func TestIndicationSkippedForUnknownChannel(t *testing.T) {
	l := testLoop(map[string]port.ChannelEntry{})

	err := l.Handle(context.Background(), testMsg("channel.missing"))
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	// No panic, no error -- pass.
}

// ---------------------------------------------------------------------------
// Error log tests
// ---------------------------------------------------------------------------

func newErrorLogLoop(completion llm.CompletionService, store *mockStorePlugin, tools map[string]port.ToolPlugin) *Loop {
	cfg := LoopConfig{MaxIterations: 3, TurnTimeout: 5 * time.Second, MaxWorkers: 2}
	pb, _ := prompt.NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	return NewLoop(cfg, completion, store, &mockConvService{}, &mockTransport{}, tools, pb, nil, nil, &noopSecretService{}, nil, nil)
}

func TestErrorLogOnLLMError(t *testing.T) {
	store := &mockStorePlugin{}
	l := newErrorLogLoop(&errorCompletionService{}, store, nil)

	err := l.Handle(context.Background(), testMsg("ch1"))
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(store.errorLogs) != 1 {
		t.Fatalf("expected 1 error log entry, got %d", len(store.errorLogs))
	}
	entry := store.errorLogs[0]
	if !strings.HasPrefix(entry.Content, "LLM error: ") {
		t.Errorf("expected message starting with 'LLM error: ', got %q", entry.Content)
	}
	if entry.RefType != "message" {
		t.Errorf("expected RefType 'message', got %q", entry.RefType)
	}
	if entry.RefID != "msg-1" {
		t.Errorf("expected RefID 'msg-1', got %q", entry.RefID)
	}
	if entry.UserID != "u1" {
		t.Errorf("expected UserID 'u1', got %q", entry.UserID)
	}
}

func TestErrorLogOnMaxIterations(t *testing.T) {
	store := &mockStorePlugin{}
	// Use a fake tool so tool calls don't panic.
	tools := map[string]port.ToolPlugin{"fake_tool": &mockTool{}}
	l := newErrorLogLoop(&toolLoopCompletionService{}, store, tools)

	err := l.Handle(context.Background(), testMsg("ch1"))
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(store.errorLogs) != 1 {
		t.Fatalf("expected 1 error log entry, got %d", len(store.errorLogs))
	}
	if store.errorLogs[0].Content != "Max iterations reached" {
		t.Errorf("expected 'Max iterations reached', got %q", store.errorLogs[0].Content)
	}
}

func TestErrorLogOnTimeout(t *testing.T) {
	store := &mockStorePlugin{}
	cfg := LoopConfig{MaxIterations: 3, TurnTimeout: 50 * time.Millisecond, MaxWorkers: 2}
	pb, _ := prompt.NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	l := NewLoop(cfg, &slowCompletionService{}, store, &mockConvService{}, &mockTransport{}, nil, pb, nil, nil, &noopSecretService{}, nil, nil)

	err := l.Handle(context.Background(), testMsg("ch1"))
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(store.errorLogs) != 1 {
		t.Fatalf("expected 1 error log entry, got %d", len(store.errorLogs))
	}
	if store.errorLogs[0].Content != "Turn timed out" {
		t.Errorf("expected 'Turn timed out', got %q", store.errorLogs[0].Content)
	}
}

func TestErrorLogFireAndForget(t *testing.T) {
	store := &mockStorePlugin{
		appendErrorLogFn: func(_ entity.ErrorLogEntry) error {
			return fmt.Errorf("db write failed")
		},
	}
	l := newErrorLogLoop(&errorCompletionService{}, store, nil)

	// Handle should still succeed (error log failure is silently logged).
	err := l.Handle(context.Background(), testMsg("ch1"))
	if err != nil {
		t.Fatalf("Handle returned error: %v (expected nil -- fire-and-forget)", err)
	}
}

func TestErrorLogCorrectTenantAndUser(t *testing.T) {
	store := &mockStorePlugin{}
	l := newErrorLogLoop(&errorCompletionService{}, store, nil)

	msg := testMsg("ch1")
	msg.TenantID = "tenant-99"
	msg.UserID = "user-77"

	_ = l.Handle(context.Background(), msg)

	if len(store.errorLogs) != 1 {
		t.Fatalf("expected 1 error log entry, got %d", len(store.errorLogs))
	}
	if store.errorLogs[0].UserID != "user-77" {
		t.Errorf("expected UserID 'user-77', got %q", store.errorLogs[0].UserID)
	}
}

// capturingTool captures the ToolContext passed to Execute.
type capturingTool struct {
	mockTool
	capturedTC port.ToolContext
}

func (m *capturingTool) Execute(_ context.Context, tc port.ToolContext, _ json.RawMessage) (string, error) {
	m.capturedTC = tc
	return "ok", nil
}

// mockTool is a no-op tool for testing.
type mockTool struct{}

func (m *mockTool) ID() string                                          { return "fake_tool" }
func (m *mockTool) Kind() entity.PluginKind                             { return entity.PluginKindTool }
func (m *mockTool) Init(context.Context, string, json.RawMessage) error { return nil }
func (m *mockTool) Start(context.Context) error                         { return nil }
func (m *mockTool) Stop(context.Context) error                          { return nil }
func (m *mockTool) Status() entity.PluginState                          { return entity.PluginStateHealthy }
func (m *mockTool) Name() string                                        { return "fake_tool" }
func (m *mockTool) Description() string                                 { return "fake tool" }
func (m *mockTool) Parameters() json.RawMessage                         { return json.RawMessage("{}") }
func (m *mockTool) Instructions() string                                { return "" }
func (m *mockTool) Execute(context.Context, port.ToolContext, json.RawMessage) (string, error) {
	return "ok", nil
}

// ---------------------------------------------------------------------------
// Agent ID preference tests
// ---------------------------------------------------------------------------

func TestAgentIDPrefersMsgAgentID(t *testing.T) {
	store := &mockStorePlugin{}
	cfg := LoopConfig{MaxIterations: 5, TurnTimeout: 5 * time.Second, MaxWorkers: 2}
	pb, _ := prompt.NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	l := NewLoop(cfg, &mockCompletionService{}, store, &mockConvService{}, &mockTransport{}, nil, pb, nil, map[string]port.ChannelEntry{}, &noopSecretService{}, nil, nil)

	msg := testMsg("ch1")
	msg.AgentID = "agent-override"

	err := l.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if store.capturedAgentID != "agent-override" {
		t.Errorf("expected GetAgent called with %q, got %q", "agent-override", store.capturedAgentID)
	}
}

func TestAgentIDFallsBackToDefault(t *testing.T) {
	store := &mockStorePlugin{}
	cfg := LoopConfig{MaxIterations: 5, TurnTimeout: 5 * time.Second, MaxWorkers: 2}
	pb, _ := prompt.NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	l := NewLoop(cfg, &mockCompletionService{}, store, &mockConvService{}, &mockTransport{}, nil, pb, nil, map[string]port.ChannelEntry{}, &noopSecretService{}, nil, nil)

	msg := testMsg("ch1")
	// msg.AgentID is empty -- should fall back to tenant.DefaultAgentID ("a1")

	err := l.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if store.capturedAgentID != "a1" {
		t.Errorf("expected GetAgent called with default %q, got %q", "a1", store.capturedAgentID)
	}
}

// ---------------------------------------------------------------------------
// ToolContext conversation propagation tests
// ---------------------------------------------------------------------------

// toolCallCompletionService returns one tool call then a final response.
type toolCallCompletionService struct {
	calls atomic.Int32
}

func (m *toolCallCompletionService) Complete(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	if m.calls.Add(1) == 1 {
		return &port.CompletionResponse{
			Content: "calling tool",
			ToolCalls: []entity.ToolCall{
				{ID: "tc-1", Name: "capturing_tool", Arguments: "{}"},
			},
		}, nil
	}
	return &port.CompletionResponse{Content: "done"}, nil
}

type toolThenFinalCompletionService struct {
	calls     atomic.Int32
	toolCalls []entity.ToolCall
}

func (m *toolThenFinalCompletionService) Complete(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	if m.calls.Add(1) == 1 {
		return &port.CompletionResponse{
			Content:   "calling tools",
			ToolCalls: m.toolCalls,
		}, nil
	}
	return &port.CompletionResponse{Content: "done"}, nil
}

func TestToolContextConversation(t *testing.T) {
	ct := &capturingTool{}
	tools := map[string]port.ToolPlugin{"capturing_tool": ct}

	store := &mockStorePlugin{}
	transport := &mockTransport{}
	completion := &toolCallCompletionService{}

	cfg := LoopConfig{MaxIterations: 5, TurnTimeout: 5 * time.Second, MaxWorkers: 2}
	pb, _ := prompt.NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	l := NewLoop(cfg, completion, store, &mockConvService{}, transport, tools, pb, nil, map[string]port.ChannelEntry{}, &noopSecretService{}, nil, nil)

	msg := testMsg("ch1")
	err := l.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// mockConvService.ResolveConversation returns "conv-test"
	if ct.capturedTC.ConversationID != entity.ConversationID("conv-test") {
		t.Errorf("expected ConversationID %q, got %q", "conv-test", ct.capturedTC.ConversationID)
	}
	// ToolContext must carry TenantID and UserID from the originating message.
	if ct.capturedTC.TenantID != msg.TenantID {
		t.Errorf("expected ToolContext.TenantID %q, got %q", msg.TenantID, ct.capturedTC.TenantID)
	}
	if ct.capturedTC.UserID != msg.UserID {
		t.Errorf("expected ToolContext.UserID %q, got %q", msg.UserID, ct.capturedTC.UserID)
	}
}

