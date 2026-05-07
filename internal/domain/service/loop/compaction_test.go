package loop

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/app/config"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/compaction"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/prompt"
)

type historyConvService struct {
	mu             sync.Mutex
	resolveID      entity.ConversationID
	appended       []entity.Message
	historyUpToIDs []entity.MessageID
}

func (m *historyConvService) ResolveConversation(_ context.Context, _ entity.Message) (entity.ConversationID, error) {
	if m.resolveID == "" {
		return "conv-test", nil
	}
	return m.resolveID, nil
}

func (m *historyConvService) Append(_ context.Context, _ entity.ConversationID, msg entity.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appended = append(m.appended, msg)
	return nil
}

func (m *historyConvService) GetHistory(_ context.Context, _ entity.ConversationID, offset, limit int, upToID *entity.MessageID) ([]entity.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var boundary entity.MessageID
	if upToID != nil {
		boundary = *upToID
	}
	m.historyUpToIDs = append(m.historyUpToIDs, boundary)

	filtered := make([]entity.Message, 0, len(m.appended))
	for _, msg := range m.appended {
		if upToID != nil && !upToID.IsEmpty() && string(msg.ID) > string(*upToID) {
			continue
		}
		filtered = append(filtered, msg)
	}

	if offset >= len(filtered) {
		return nil, nil
	}
	filtered = filtered[offset:]
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}

	out := make([]entity.Message, len(filtered))
	copy(out, filtered)
	return out, nil
}

func (m *historyConvService) RegisterConversation(entity.TenantID, entity.ConversationID) {}
func (m *historyConvService) ResetConversation(context.Context, entity.ConversationID) error {
	return nil
}
func (m *historyConvService) SwitchConversation(entity.Message, entity.ConversationID) {}

func (m *historyConvService) snapshotAppended() []entity.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]entity.Message, len(m.appended))
	copy(out, m.appended)
	return out
}

type compactionStoreSpy struct {
	port.StorePlugin

	mu               sync.Mutex
	latestSummary    *entity.Summary
	summaries        []entity.Summary
	saveCalls        int
	savedTenantID    entity.TenantID
	savedSummary     entity.Summary
	savedSummaryCh   chan struct{}
	listOffset       int
	listLimit        int
	listConversation entity.ConversationID
}

func (s *compactionStoreSpy) SaveSummary(_ context.Context, tenantID entity.TenantID, summary entity.Summary) error {
	s.mu.Lock()
	s.saveCalls++
	s.savedTenantID = tenantID
	s.savedSummary = summary
	s.mu.Unlock()

	if s.savedSummaryCh != nil {
		select {
		case s.savedSummaryCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func (s *compactionStoreSpy) GetLatestSummary(context.Context, entity.TenantID, entity.ConversationID) (*entity.Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latestSummary == nil {
		return nil, nil
	}
	summary := *s.latestSummary
	return &summary, nil
}

func (s *compactionStoreSpy) ListSummaries(_ context.Context, _ entity.TenantID, convID entity.ConversationID, offset, limit int) ([]entity.Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listConversation = convID
	s.listOffset = offset
	s.listLimit = limit
	out := make([]entity.Summary, len(s.summaries))
	copy(out, s.summaries)
	return out, nil
}

func (s *compactionStoreSpy) snapshot() (int, entity.TenantID, entity.Summary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveCalls, s.savedTenantID, s.savedSummary
}

type blockingCompactionCompletion struct {
	calls   atomic.Int32
	started chan struct{}
	release <-chan struct{}
	content string
}

func (m *blockingCompactionCompletion) Complete(ctx context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	m.calls.Add(1)
	if m.started != nil {
		select {
		case m.started <- struct{}{}:
		default:
		}
	}
	if m.release != nil {
		select {
		case <-m.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &port.CompletionResponse{Content: m.content}, nil
}

type largeTool struct {
	content string
}

func (m *largeTool) ID() string                                          { return "large_tool" }
func (m *largeTool) Kind() entity.PluginKind                             { return entity.PluginKindTool }
func (m *largeTool) Init(context.Context, string, json.RawMessage) error { return nil }
func (m *largeTool) Start(context.Context) error                         { return nil }
func (m *largeTool) Stop(context.Context) error                          { return nil }
func (m *largeTool) Status() entity.PluginState                          { return entity.PluginStateHealthy }
func (m *largeTool) Name() string                                        { return "large_tool" }
func (m *largeTool) Description() string                                 { return "large tool" }
func (m *largeTool) Parameters() json.RawMessage                         { return json.RawMessage("{}") }
func (m *largeTool) Instructions() string                                { return "" }
func (m *largeTool) Execute(context.Context, port.ToolContext, json.RawMessage) (string, error) {
	return m.content, nil
}

func newCompactionPromptBuilder(t *testing.T, conv port.ConversationService, tokenBudget int, threshold float64) *prompt.PromptBuilder {
	t.Helper()
	pb, err := prompt.NewPromptBuilder(nil, conv, nil, nil, tokenBudget, &config.CompactionConfig{
		Model:     "compact-model",
		Threshold: threshold,
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	return pb
}

func newCompactionService(t *testing.T, completion *blockingCompactionCompletion, conv port.ConversationService, store *compactionStoreSpy) *compaction.Service {
	t.Helper()
	svc, err := compaction.New(completion, conv, store, &config.CompactionConfig{
		Model:                  "compact-model",
		PreserveRecentMessages: 1,
	})
	if err != nil {
		t.Fatalf("compaction.New: %v", err)
	}
	return svc
}

func waitForSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitForCondition(t *testing.T, label string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func latestToolResultID(t *testing.T, conv *historyConvService) entity.MessageID {
	t.Helper()
	appended := conv.snapshotAppended()
	for i := len(appended) - 1; i >= 0; i-- {
		if appended[i].Role == entity.RoleTool {
			return appended[i].ID
		}
	}
	t.Fatal("missing tool result")
	return ""
}

func messageIDBefore(t *testing.T, conv *historyConvService, target entity.MessageID) entity.MessageID {
	t.Helper()
	appended := conv.snapshotAppended()
	for i, msg := range appended {
		if msg.ID == target {
			if i == 0 {
				t.Fatal("target is first message; no prior boundary exists")
			}
			return appended[i-1].ID
		}
	}
	t.Fatalf("missing message %q", target)
	return ""
}

func TestCompactionSignalIgnoredWithoutService(t *testing.T) {
	conv := &historyConvService{resolveID: "conv-test"}
	pb := newCompactionPromptBuilder(t, conv, 1000, 0.5)
	transport := &mockTransport{}
	l := NewLoop(
		LoopConfig{MaxIterations: 2, TurnTimeout: 5 * time.Second, MaxWorkers: 1},
		&mockCompletionService{},
		&mockStorePlugin{},
		conv,
		transport,
		nil,
		pb,
		nil,
		map[string]port.ChannelEntry{},
		&noopSecretService{},
		nil,
		nil,
	)

	msg := testMsg("ch1")
	msg.Content = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

	if err := l.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(transport.published) != 1 {
		t.Fatalf("published = %d, want 1 final response", len(transport.published))
	}
	if _, ok := l.compactions.Load("conv-test"); ok {
		t.Fatal("compaction in-flight marker should not exist when compaction service is disabled")
	}
}

func TestCompactionLaunchesAsyncAndUsesSignalBoundary(t *testing.T) {
	conv := &historyConvService{resolveID: "conv-test"}
	pb := newCompactionPromptBuilder(t, conv, 1000, 0.5)
	transport := &mockTransport{}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	store := &compactionStoreSpy{savedSummaryCh: make(chan struct{}, 1)}
	compactionCompletion := &blockingCompactionCompletion{
		started: started,
		release: release,
		content: "## Goals\nDone.\n\n## Files\nNone.\n\n## State\nDone.\n\n## Decisions\nDone.",
	}
	compactionSvc := newCompactionService(t, compactionCompletion, conv, store)

	l := NewLoop(
		LoopConfig{MaxIterations: 2, TurnTimeout: 5 * time.Second, MaxWorkers: 1},
		&mockCompletionService{},
		&mockStorePlugin{},
		conv,
		transport,
		nil,
		pb,
		compactionSvc,
		map[string]port.ChannelEntry{},
		&noopSecretService{},
		nil,
		nil,
	)

	msg := testMsg("ch1")
	msg.Content = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

	if err := l.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	waitForSignal(t, started, "compaction launch")
	close(release)
	waitForSignal(t, store.savedSummaryCh, "compaction save")

	if compactionCompletion.calls.Load() != 1 {
		t.Fatalf("compaction calls = %d, want 1", compactionCompletion.calls.Load())
	}

	saveCalls, savedTenantID, savedSummary := store.snapshot()
	if saveCalls != 1 {
		t.Fatalf("saveCalls = %d, want 1", saveCalls)
	}
	if savedTenantID != msg.TenantID {
		t.Fatalf("saved tenant = %q, want %q", savedTenantID, msg.TenantID)
	}
	if savedSummary.ConversationID != "conv-test" {
		t.Fatalf("saved conversation = %q, want %q", savedSummary.ConversationID, "conv-test")
	}
	if savedSummary.MessageID != msg.ID {
		t.Fatalf("saved message boundary = %q, want %q", savedSummary.MessageID, msg.ID)
	}
}

func TestCompactionSuppressesDuplicateLaunchesAndClearsInFlight(t *testing.T) {
	conv := &historyConvService{resolveID: "conv-test"}
	pb := newCompactionPromptBuilder(t, conv, 1000, 0.5)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	store := &compactionStoreSpy{savedSummaryCh: make(chan struct{}, 2)}
	compactionCompletion := &blockingCompactionCompletion{
		started: started,
		release: release,
		content: "## Goals\nDone.\n\n## Files\nNone.\n\n## State\nDone.\n\n## Decisions\nDone.",
	}
	compactionSvc := newCompactionService(t, compactionCompletion, conv, store)

	l := NewLoop(
		LoopConfig{MaxIterations: 2, TurnTimeout: 5 * time.Second, MaxWorkers: 1},
		&mockCompletionService{},
		&mockStorePlugin{},
		conv,
		&mockTransport{},
		nil,
		pb,
		compactionSvc,
		map[string]port.ChannelEntry{},
		&noopSecretService{},
		nil,
		nil,
	)

	first := testMsg("ch1")
	first.Content = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	if err := l.Handle(context.Background(), first); err != nil {
		t.Fatalf("first Handle returned error: %v", err)
	}
	waitForSignal(t, started, "first compaction launch")

	second := testMsg("ch1")
	second.ID = "msg-2"
	second.Content = first.Content
	if err := l.Handle(context.Background(), second); err != nil {
		t.Fatalf("second Handle returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if compactionCompletion.calls.Load() != 1 {
		t.Fatalf("compaction calls while in flight = %d, want 1", compactionCompletion.calls.Load())
	}

	close(release)
	waitForSignal(t, store.savedSummaryCh, "first compaction save")
	waitForCondition(t, "compaction marker cleanup", func() bool {
		_, ok := l.compactions.Load("conv-test")
		return !ok
	})

	third := testMsg("ch1")
	third.ID = "msg-3"
	third.Content = first.Content
	if err := l.Handle(context.Background(), third); err != nil {
		t.Fatalf("third Handle returned error: %v", err)
	}

	waitForCondition(t, "second compaction launch after cleanup", func() bool {
		return compactionCompletion.calls.Load() == 2
	})
}

func TestCompactionTriggersAfterLaterToolResults(t *testing.T) {
	conv := &historyConvService{resolveID: "conv-test"}
	pb := newCompactionPromptBuilder(t, conv, 18000, 0.26)
	store := &compactionStoreSpy{savedSummaryCh: make(chan struct{}, 1)}
	compactionCompletion := &blockingCompactionCompletion{
		content: "## Goals\nDone.\n\n## Files\nNone.\n\n## State\nDone.\n\n## Decisions\nDone.",
	}
	compactionSvc := newCompactionService(t, compactionCompletion, conv, store)
	completion := &toolThenFinalCompletionService{
		toolCalls: []entity.ToolCall{
			{ID: "tc-1", Name: "large_tool", Arguments: "{}"},
		},
	}
	tools := map[string]port.ToolPlugin{
		"large_tool": &largeTool{content: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
	}

	l := NewLoop(
		LoopConfig{MaxIterations: 3, TurnTimeout: 5 * time.Second, MaxWorkers: 1},
		completion,
		&mockStorePlugin{},
		conv,
		&mockTransport{},
		tools,
		pb,
		compactionSvc,
		map[string]port.ChannelEntry{},
		&noopSecretService{},
		nil,
		nil,
	)

	if err := l.Handle(context.Background(), testMsg("ch1")); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	waitForSignal(t, store.savedSummaryCh, "compaction save after tool result")

	_, _, savedSummary := store.snapshot()
	toolResultID := latestToolResultID(t, conv)
	wantBoundary := messageIDBefore(t, conv, toolResultID)
	if savedSummary.MessageID != wantBoundary {
		t.Fatalf("saved message boundary = %q, want last message before preserved tool result %q", savedSummary.MessageID, wantBoundary)
	}
}
