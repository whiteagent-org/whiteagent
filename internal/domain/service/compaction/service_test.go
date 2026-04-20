package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/app/config"
	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/llm"
)

type mockCompletionService struct {
	response *port.CompletionResponse
	err      error
	requests []port.CompletionRequest
}

func (m *mockCompletionService) Complete(_ context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
	m.requests = append(m.requests, req)
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

type mockConversationService struct {
	port.ConversationService
	history      []entity.Message
	captured     entity.ConversationID
	capturedUpTo *entity.MessageID
	registered   entity.ConversationID
	tenantID     entity.TenantID
}

func (m *mockConversationService) RegisterConversation(tenantID entity.TenantID, convID entity.ConversationID) {
	m.tenantID = tenantID
	m.registered = convID
}

func (m *mockConversationService) GetHistory(_ context.Context, convID entity.ConversationID, _, _ int, upToID *entity.MessageID) ([]entity.Message, error) {
	m.captured = convID
	if upToID != nil {
		id := *upToID
		m.capturedUpTo = &id
	}
	return append([]entity.Message(nil), m.history...), nil
}

type mockStore struct {
	port.StorePlugin
	latestSummary  *entity.Summary
	summaries      []entity.Summary
	saved          []entity.Summary
	getLatestCalls int
	listCalls      int
	listOffset     int
	listLimit      int
}

func (m *mockStore) GetLatestSummary(_ context.Context, _ entity.TenantID, _ entity.ConversationID) (*entity.Summary, error) {
	m.getLatestCalls++
	return m.latestSummary, nil
}

func (m *mockStore) ListSummaries(_ context.Context, _ entity.TenantID, _ entity.ConversationID, offset, limit int) ([]entity.Summary, error) {
	m.listCalls++
	m.listOffset = offset
	m.listLimit = limit
	if offset >= len(m.summaries) {
		return nil, nil
	}
	end := len(m.summaries)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return append([]entity.Summary(nil), m.summaries[offset:end]...), nil
}

func (m *mockStore) SaveSummary(_ context.Context, _ entity.TenantID, summary entity.Summary) error {
	m.saved = append(m.saved, summary)
	return nil
}

func compactedPrompt(req port.CompletionRequest) string {
	parts := make([]string, 0, len(req.Messages))
	for _, msg := range req.Messages {
		parts = append(parts, msg.Content)
	}
	return strings.Join(parts, "\n")
}

func TestCompactLoadsLatestSummaryOnlyAndPreservesRecentTail(t *testing.T) {
	store := &mockStore{
		latestSummary: &entity.Summary{
			ID:        "sum-latest",
			MessageID: "msg-2",
			Content:   "latest summary content",
		},
		summaries: []entity.Summary{
			{ID: "sum-older", MessageID: "msg-1", Content: "older summary content"},
			{ID: "sum-latest", MessageID: "msg-2", Content: "latest summary content"},
		},
	}
	conv := &mockConversationService{
		history: []entity.Message{
			{ID: "msg-1", Role: entity.RoleUser, Content: "old user"},
			{ID: "msg-2", Role: entity.RoleAssistant, Content: "old assistant"},
			{ID: "msg-3", Role: entity.RoleUser, Content: "new user"},
			{ID: "msg-4", Role: entity.RoleAssistant, Content: "new assistant"},
			{ID: "msg-5", Role: entity.RoleUser, Content: "preserved user"},
			{ID: "msg-6", Role: entity.RoleAssistant, Content: "preserved assistant"},
		},
	}
	completion := &mockCompletionService{
		response: &port.CompletionResponse{
			Content: "## Goals\nShip compaction.\n## State\nRunning.\n## Decisions\nKeep message boundaries.",
		},
	}

	svc, err := New(completion, conv, store, &config.CompactionConfig{
		Model:                  "compaction-model",
		PreserveRecentMessages: 2,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := Request{
		TenantID:        "tenant-1",
		ConversationID:  "conv-1",
		LatestMessageID: "msg-6",
	}
	if err := svc.Compact(context.Background(), req); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if conv.captured != req.ConversationID {
		t.Fatalf("GetHistory conversation = %q, want %q", conv.captured, req.ConversationID)
	}
	if conv.capturedUpTo == nil || *conv.capturedUpTo != req.LatestMessageID {
		t.Fatalf("GetHistory upToID = %v, want %q", conv.capturedUpTo, req.LatestMessageID)
	}

	promptText := compactedPrompt(completion.requests[0])
	for _, want := range []string{"latest summary content", "new user", "new assistant"} {
		if !strings.Contains(promptText, want) {
			t.Fatalf("prompt missing %q:\n%s", want, promptText)
		}
	}
	for _, unwanted := range []string{"older summary content", "old user", "old assistant", "preserved user", "preserved assistant"} {
		if strings.Contains(promptText, unwanted) {
			t.Fatalf("prompt should exclude %q:\n%s", unwanted, promptText)
		}
	}
	if store.listCalls != 0 {
		t.Fatalf("ListSummaries called %d times, want 0", store.listCalls)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved summaries = %d, want 1", len(store.saved))
	}
	if store.saved[0].MessageID != "msg-4" {
		t.Fatalf("saved MessageID = %q, want %q", store.saved[0].MessageID, "msg-4")
	}
}

func TestCompactUsesConfiguredModel(t *testing.T) {
	store := &mockStore{}
	conv := &mockConversationService{
		history: []entity.Message{
			{ID: "msg-1", Role: entity.RoleUser, Content: "older content"},
			{ID: "msg-2", Role: entity.RoleAssistant, Content: "newest content"},
		},
	}
	completion := &mockCompletionService{
		response: &port.CompletionResponse{
			Content: "## Goals\nShip compaction.\n## State\nRunning.\n## Decisions\nKeep message boundaries.",
		},
	}

	svc, err := New(completion, conv, store, &config.CompactionConfig{
		Model:                  "compaction-model",
		PreserveRecentMessages: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := svc.Compact(context.Background(), Request{
		TenantID:        "tenant-1",
		ConversationID:  "conv-1",
		LatestMessageID: "msg-1",
	}); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if len(completion.requests) != 1 {
		t.Fatalf("completion requests = %d, want 1", len(completion.requests))
	}
	if completion.requests[0].Model != "compaction-model" {
		t.Fatalf("completion model = %q, want %q", completion.requests[0].Model, "compaction-model")
	}
}

func TestCompactPersistsNormalizedSummaryWithFilesSection(t *testing.T) {
	store := &mockStore{}
	conv := &mockConversationService{
		history: []entity.Message{
			{
				ID:      "msg-1",
				Role:    entity.RoleUser,
				Content: "See attached file.",
				Attachments: []entity.Attachment{
					{Filename: "notes.md", Caption: "Sprint plan"},
				},
			},
			{ID: "msg-2", Role: entity.RoleAssistant, Content: "Most recent response."},
		},
	}
	completion := &mockCompletionService{
		response: &port.CompletionResponse{
			Content: "## Goals\nShip compaction.\n## State\nRunning.\n## Decisions\nKeep message boundaries.",
		},
	}

	svc, err := New(completion, conv, store, &config.CompactionConfig{
		Model:                  "compaction-model",
		PreserveRecentMessages: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := svc.Compact(context.Background(), Request{
		TenantID:        "tenant-1",
		ConversationID:  "conv-1",
		LatestMessageID: "msg-1",
	}); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if len(store.saved) != 1 {
		t.Fatalf("saved summaries = %d, want 1", len(store.saved))
	}
	saved := store.saved[0]
	if saved.MessageID != "msg-1" {
		t.Fatalf("saved MessageID = %q, want %q", saved.MessageID, "msg-1")
	}
	for _, want := range []string{
		"## Goals",
		"## Files",
		"## State",
		"## Decisions",
		"- notes.md: Sprint plan",
	} {
		if !strings.Contains(saved.Content, want) {
			t.Fatalf("saved summary missing %q:\n%s", want, saved.Content)
		}
	}
	if saved.CreatedAt.IsZero() {
		t.Fatal("saved CreatedAt should be set")
	}
}

func TestCompactNormalizesMissingSectionsAndEmptyFiles(t *testing.T) {
	store := &mockStore{}
	conv := &mockConversationService{
		history: []entity.Message{
			{ID: "msg-1", Role: entity.RoleUser, Content: "No files here."},
			{ID: "msg-2", Role: entity.RoleAssistant, Content: "Keep this raw."},
		},
	}
	completion := &mockCompletionService{
		response: &port.CompletionResponse{
			Content: "## Goals\nTrack progress.",
		},
	}

	svc, err := New(completion, conv, store, &config.CompactionConfig{
		Model:                  "compaction-model",
		PreserveRecentMessages: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := svc.Compact(context.Background(), Request{
		TenantID:        "tenant-1",
		ConversationID:  "conv-1",
		LatestMessageID: "msg-1",
	}); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if len(store.saved) != 1 {
		t.Fatalf("saved summaries = %d, want 1", len(store.saved))
	}
	saved := store.saved[0].Content
	for _, want := range []string{
		"## Goals\nTrack progress.",
		"## Files\nNone.",
		"## State\nNone.",
		"## Decisions\nNone.",
	} {
		if !strings.Contains(saved, want) {
			t.Fatalf("saved summary missing %q:\n%s", want, saved)
		}
	}
}

func TestCompactSkipsPersistenceWhenCompletionFails(t *testing.T) {
	store := &mockStore{}
	conv := &mockConversationService{
		history: []entity.Message{
			{ID: "msg-1", Role: entity.RoleUser, Content: "hello"},
			{ID: "msg-2", Role: entity.RoleAssistant, Content: "world"},
		},
	}
	completion := &mockCompletionService{err: errors.New("model unavailable")}

	svc, err := New(completion, conv, store, &config.CompactionConfig{
		Model:                  "compaction-model",
		PreserveRecentMessages: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = svc.Compact(context.Background(), Request{
		TenantID:        "tenant-1",
		ConversationID:  "conv-1",
		LatestMessageID: "msg-1",
	})
	if err == nil {
		t.Fatal("expected Compact to fail")
	}
	if len(store.saved) != 0 {
		t.Fatalf("saved summaries = %d, want 0", len(store.saved))
	}
}

func TestCompactNoOpsWhenOnlyPreservedTailRemains(t *testing.T) {
	store := &mockStore{
		latestSummary: &entity.Summary{MessageID: "msg-2"},
	}
	conv := &mockConversationService{
		history: []entity.Message{
			{ID: "msg-1", Role: entity.RoleUser, Content: "old user"},
			{ID: "msg-2", Role: entity.RoleAssistant, Content: "old assistant"},
			{ID: "msg-3", Role: entity.RoleUser, Content: "tail user"},
			{ID: "msg-4", Role: entity.RoleAssistant, Content: "tail assistant"},
		},
	}
	completion := &mockCompletionService{
		response: &port.CompletionResponse{Content: "## Goals\nUnused."},
	}

	svc, err := New(completion, conv, store, &config.CompactionConfig{
		Model:                  "compaction-model",
		PreserveRecentMessages: 2,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := svc.Compact(context.Background(), Request{
		TenantID:        "tenant-1",
		ConversationID:  "conv-1",
		LatestMessageID: "msg-4",
	}); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if len(completion.requests) != 0 {
		t.Fatalf("completion requests = %d, want 0", len(completion.requests))
	}
	if len(store.saved) != 0 {
		t.Fatalf("saved summaries = %d, want 0", len(store.saved))
	}
}

var _ llm.CompletionService = (*mockCompletionService)(nil)

func TestRequestJSONShapeStaysMinimal(t *testing.T) {
	data, err := json.Marshal(Request{
		TenantID:        "tenant-1",
		ConversationID:  "conv-1",
		LatestMessageID: "msg-7",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{"tenant-1", "conv-1", "msg-7"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("request JSON missing %q: %s", want, string(data))
		}
	}
}

func makeSummary(id string, messageID entity.MessageID, content string) entity.Summary {
	return entity.Summary{
		ID:             id,
		TenantID:       "tenant-1",
		ConversationID: "conv-1",
		Content:        content,
		MessageID:      messageID,
		CreatedAt:      time.Now().UTC(),
	}
}
