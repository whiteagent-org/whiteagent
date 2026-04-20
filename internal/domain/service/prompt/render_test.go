package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// ---------------------------------------------------------------------------
// groupMessages tests
// ---------------------------------------------------------------------------

func TestGroupMessagesEmpty(t *testing.T) {
	groups := groupMessages(nil)
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestGroupMessagesSingleUser(t *testing.T) {
	msgs := []entity.Message{{Role: entity.RoleUser, Content: "hello"}}
	groups := groupMessages(msgs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].messages) != 1 {
		t.Errorf("expected 1 message in group, got %d", len(groups[0].messages))
	}
}

func TestGroupMessagesSingleAssistantNoTools(t *testing.T) {
	msgs := []entity.Message{{Role: entity.RoleAssistant, Content: "hi there"}}
	groups := groupMessages(msgs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].messages) != 1 {
		t.Errorf("expected 1 message in group, got %d", len(groups[0].messages))
	}
}

func TestGroupMessagesAssistantWithToolCalls(t *testing.T) {
	msgs := []entity.Message{
		{
			Role:    entity.RoleAssistant,
			Content: "",
			ToolCalls: []entity.ToolCall{
				{ID: "tc-1", Name: "search"},
				{ID: "tc-2", Name: "calc"},
			},
		},
		{Role: entity.RoleTool, ToolCallID: "tc-1", Content: "result1"},
		{Role: entity.RoleTool, ToolCallID: "tc-2", Content: "result2"},
	}
	groups := groupMessages(msgs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].messages) != 3 {
		t.Errorf("expected 3 messages in group, got %d", len(groups[0].messages))
	}
}

func TestGroupMessagesMultipleGroups(t *testing.T) {
	msgs := []entity.Message{
		{Role: entity.RoleUser, Content: "hi"},
		{
			Role:      entity.RoleAssistant,
			ToolCalls: []entity.ToolCall{{ID: "tc-1", Name: "search"}},
		},
		{Role: entity.RoleTool, ToolCallID: "tc-1", Content: "found it"},
		{Role: entity.RoleUser, Content: "thanks"},
	}
	groups := groupMessages(msgs)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	// Group 1: user
	if len(groups[0].messages) != 1 || groups[0].messages[0].Role != entity.RoleUser {
		t.Errorf("group 0: expected single user message")
	}
	// Group 2: assistant + tool
	if len(groups[1].messages) != 2 {
		t.Errorf("group 1: expected 2 messages (assistant+tool), got %d", len(groups[1].messages))
	}
	// Group 3: user
	if len(groups[2].messages) != 1 || groups[2].messages[0].Role != entity.RoleUser {
		t.Errorf("group 2: expected single user message")
	}
}

func TestGroupMessagesOrphanTool(t *testing.T) {
	msgs := []entity.Message{
		{Role: entity.RoleTool, ToolCallID: "tc-orphan", Content: "stale result"},
	}
	groups := groupMessages(msgs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].messages) != 1 {
		t.Errorf("expected 1 message in group, got %d", len(groups[0].messages))
	}
}

// ---------------------------------------------------------------------------
// enrichMessages tests
// ---------------------------------------------------------------------------

func TestEnrichMessagesUserGetsContextBlock(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:      entity.MessageID("msg-1"),
			Role:    entity.RoleUser,
			Content: "hello",
		},
	}
	enriched := enrichMessages(msgs, nil, "/messages")
	if len(enriched) != 1 {
		t.Fatalf("expected 1 message, got %d", len(enriched))
	}
	if enriched[0].Content == "hello" {
		t.Error("user message should have context block prepended")
	}
}

func TestEnrichMessagesAssistantGetsContextBlock(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:      entity.MessageID("msg-2"),
			Role:    entity.RoleAssistant,
			Content: "hi there",
		},
	}
	enriched := enrichMessages(msgs, nil, "/messages")
	if enriched[0].Content == "hi there" {
		t.Error("assistant message should have context block prepended")
	}
}

func TestEnrichMessagesAssistantEmptyContentSkipped(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:      entity.MessageID("msg-tc"),
			Role:    entity.RoleAssistant,
			Content: "",
			ToolCalls: []entity.ToolCall{
				{ID: "tc-1", Name: "search", Arguments: `{"q":"test"}`},
			},
		},
	}
	enriched := enrichMessages(msgs, nil, "/messages")
	if enriched[0].Content != "" {
		t.Errorf("empty assistant message should not be enriched, got %q", enriched[0].Content)
	}
}

func TestEnrichMessagesToolGetsContextBlock(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:         entity.MessageID("msg-tool-1"),
			Role:       entity.RoleTool,
			ToolCallID: "tc-1",
			Content:    "tool result",
		},
	}
	enriched := enrichMessages(msgs, nil, "/messages")
	if !strings.Contains(enriched[0].Content, `<wa_msg_context msg_id="msg-tool-1"`) {
		t.Fatalf("tool message missing msg context block: %q", enriched[0].Content)
	}
	if !strings.HasSuffix(enriched[0].Content, "tool result") {
		t.Fatalf("tool message should preserve content, got %q", enriched[0].Content)
	}
}

func TestEnrichMessagesToolEmptyContentStillGetsContextBlock(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:         entity.MessageID("msg-tool-2"),
			Role:       entity.RoleTool,
			ToolCallID: "tc-2",
			Content:    "",
		},
	}
	enriched := enrichMessages(msgs, nil, "/messages")
	if !strings.Contains(enriched[0].Content, `<wa_msg_context msg_id="msg-tool-2"`) {
		t.Fatalf("tool message missing msg context block: %q", enriched[0].Content)
	}
	if strings.Contains(enriched[0].Content, "tool result") {
		t.Fatalf("unexpected content leakage: %q", enriched[0].Content)
	}
}

func TestEnrichMessagesDoesNotMutateOriginal(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:      entity.MessageID("msg-4"),
			Role:    entity.RoleUser,
			Content: "original",
		},
	}
	enrichMessages(msgs, nil, "/messages")
	if msgs[0].Content != "original" {
		t.Errorf("original slice was mutated: %q", msgs[0].Content)
	}
}

// ---------------------------------------------------------------------------
// countGroupTokens tests
// ---------------------------------------------------------------------------

func TestCountGroupTokensSingleMessage(t *testing.T) {
	g := messageGroup{
		messages: []entity.Message{
			{Role: entity.RoleUser, Content: "hello world"},
		},
	}
	tokens := countGroupTokens(g)
	if tokens <= 0 {
		t.Errorf("expected positive token count, got %d", tokens)
	}
}

func TestCountGroupTokensMultipleMessages(t *testing.T) {
	g := messageGroup{
		messages: []entity.Message{
			{Role: entity.RoleAssistant, ToolCalls: []entity.ToolCall{{ID: "tc-1", Name: "search", Arguments: `{"q":"test"}`}}},
			{Role: entity.RoleTool, ToolCallID: "tc-1", Content: "result"},
		},
	}
	tokens := countGroupTokens(g)
	if tokens <= 0 {
		t.Errorf("expected positive token count, got %d", tokens)
	}
}

// ---------------------------------------------------------------------------
// windowMessages tests
// ---------------------------------------------------------------------------

func TestWindowMessagesAllFit(t *testing.T) {
	groups := []messageGroup{
		{messages: []entity.Message{{Role: entity.RoleUser, Content: "a"}}, tokens: 10},
		{messages: []entity.Message{{Role: entity.RoleAssistant, Content: "b"}}, tokens: 10},
		{messages: []entity.Message{{Role: entity.RoleUser, Content: "c"}}, tokens: 10},
	}
	result := windowMessages(groups, 1000)
	if len(result) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result))
	}
}

func TestWindowMessagesPartialFit(t *testing.T) {
	groups := []messageGroup{
		{messages: []entity.Message{{Role: entity.RoleUser, Content: "oldest"}}, tokens: 200},
		{messages: []entity.Message{{Role: entity.RoleAssistant, Content: "middle"}}, tokens: 200},
		{messages: []entity.Message{{Role: entity.RoleUser, Content: "newest"}}, tokens: 200},
	}
	// Budget only fits the newest group.
	result := windowMessages(groups, 200)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Content != "newest" {
		t.Errorf("expected newest message, got %q", result[0].Content)
	}
}

func TestWindowMessagesNothingFits(t *testing.T) {
	groups := []messageGroup{
		{messages: []entity.Message{{Role: entity.RoleUser, Content: "huge"}}, tokens: 500},
	}
	result := windowMessages(groups, 10)
	if len(result) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result))
	}
}

func TestWindowMessagesExactBudget(t *testing.T) {
	groups := []messageGroup{
		{messages: []entity.Message{{Role: entity.RoleUser, Content: "a"}}, tokens: 50},
		{messages: []entity.Message{{Role: entity.RoleAssistant, Content: "b"}}, tokens: 50},
	}
	result := windowMessages(groups, 100)
	if len(result) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result))
	}
}

func TestWindowMessagesChronologicalOrder(t *testing.T) {
	groups := []messageGroup{
		{messages: []entity.Message{{Role: entity.RoleUser, Content: "first"}}, tokens: 10},
		{messages: []entity.Message{{Role: entity.RoleAssistant, Content: "second"}}, tokens: 10},
		{messages: []entity.Message{{Role: entity.RoleUser, Content: "third"}}, tokens: 10},
	}
	result := windowMessages(groups, 1000)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[0].Content != "first" {
		t.Errorf("expected 'first', got %q", result[0].Content)
	}
	if result[1].Content != "second" {
		t.Errorf("expected 'second', got %q", result[1].Content)
	}
	if result[2].Content != "third" {
		t.Errorf("expected 'third', got %q", result[2].Content)
	}
}

func TestWindowMessagesStopsAtFirstNonFitting(t *testing.T) {
	// Groups: [10] [500] [10] -- budget=30
	// Walking from end: [10] fits (20 left), [500] doesn't fit -> STOP
	// Only newest group included.
	groups := []messageGroup{
		{messages: []entity.Message{{Role: entity.RoleUser, Content: "small1"}}, tokens: 10},
		{messages: []entity.Message{{Role: entity.RoleAssistant, Content: "huge"}}, tokens: 500},
		{messages: []entity.Message{{Role: entity.RoleUser, Content: "small2"}}, tokens: 10},
	}
	result := windowMessages(groups, 30)
	if len(result) != 1 {
		t.Fatalf("expected 1 message (no skipping), got %d", len(result))
	}
	if result[0].Content != "small2" {
		t.Errorf("expected newest small message, got %q", result[0].Content)
	}
}

func TestWindowMessagesToolGroupAtomic(t *testing.T) {
	// A group with assistant + 2 tools should be kept/evicted as a unit.
	groups := []messageGroup{
		{
			messages: []entity.Message{
				{Role: entity.RoleAssistant, ToolCalls: []entity.ToolCall{{ID: "tc-1"}, {ID: "tc-2"}}},
				{Role: entity.RoleTool, ToolCallID: "tc-1", Content: "r1"},
				{Role: entity.RoleTool, ToolCallID: "tc-2", Content: "r2"},
			},
			tokens: 100,
		},
	}
	result := windowMessages(groups, 100)
	if len(result) != 3 {
		t.Errorf("expected all 3 messages in tool group, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// Evicted message tests
// ---------------------------------------------------------------------------

var evictedTS = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestBuildContextBlockEvicted(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-ev1"),
		Role:      entity.RoleUser,
		Content:   "this should not appear",
		Evicted:   true,
		CreatedAt: evictedTS,
		Attachments: []entity.Attachment{
			{Kind: "photo", Filename: "pic.jpg", Size: 1024, MimeType: "image/jpeg"},
		},
	}
	block := buildContextBlock(msg, nil, "/messages")
	if !strings.Contains(block, `evicted=""/>`) {
		t.Errorf("expected self-closing tag with evicted attribute, got %q", block)
	}
	if strings.Contains(block, "this should not appear") {
		t.Error("evicted block should not contain message content")
	}
	if strings.Contains(block, "attachment") {
		t.Error("evicted block should not contain attachment elements")
	}
	if !strings.HasSuffix(block, "\n\n") {
		t.Error("evicted block should end with two newlines")
	}
}

func TestBuildContextBlockEvictedNoIDNoTS(t *testing.T) {
	msg := entity.Message{
		Role:    entity.RoleUser,
		Content: "no id no ts",
		Evicted: true,
	}
	block := buildContextBlock(msg, nil, "/messages")
	if block != "" {
		t.Errorf("expected empty string for evicted message with no ID/TS, got %q", block)
	}
}

func TestEnrichMessagesEvictedUser(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:        entity.MessageID("msg-eu1"),
			Role:      entity.RoleUser,
			Content:   "original user content",
			Evicted:   true,
			CreatedAt: evictedTS,
		},
	}
	enriched := enrichMessages(msgs, nil, "/messages")
	if len(enriched) != 1 {
		t.Fatalf("expected 1 message, got %d", len(enriched))
	}
	if !strings.Contains(enriched[0].Content, `evicted=""/>`) {
		t.Error("evicted user message should have self-closing context block")
	}
	if strings.Contains(enriched[0].Content, "original user content") {
		t.Error("evicted user message should not contain original content")
	}
}

func TestEnrichMessagesEvictedAssistantEmptyNoTools(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:        entity.MessageID("msg-ea1"),
			Role:      entity.RoleAssistant,
			Content:   "",
			Evicted:   true,
			CreatedAt: evictedTS,
		},
	}
	enriched := enrichMessages(msgs, nil, "/messages")
	if enriched[0].Content != "" {
		t.Errorf("evicted empty assistant message with no tool calls should produce empty content, got %q", enriched[0].Content)
	}
}

func TestEnrichMessagesEvictedAssistantWithContent(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:        entity.MessageID("msg-ea2"),
			Role:      entity.RoleAssistant,
			Content:   "some text",
			Evicted:   true,
			CreatedAt: evictedTS,
		},
	}
	enriched := enrichMessages(msgs, nil, "/messages")
	if !strings.Contains(enriched[0].Content, `evicted=""/>`) {
		t.Error("evicted assistant message with content should have self-closing placeholder")
	}
	if strings.Contains(enriched[0].Content, "some text") {
		t.Error("evicted assistant message should not contain original content")
	}
}

func TestEnrichMessagesEvictedTool(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:         entity.MessageID("msg-et1"),
			Role:       entity.RoleTool,
			ToolCallID: "tc-1",
			Content:    "tool result data",
			Evicted:    true,
			CreatedAt:  evictedTS,
		},
	}
	enriched := enrichMessages(msgs, nil, "/messages")
	if enriched[0].Content != "" {
		t.Errorf("evicted tool message should produce empty content, got %q", enriched[0].Content)
	}
}

func TestEnrichMessagesEvictedDoesNotMutateOriginal(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:        entity.MessageID("msg-em1"),
			Role:      entity.RoleUser,
			Content:   "keep me unchanged",
			Evicted:   true,
			CreatedAt: evictedTS,
		},
	}
	enrichMessages(msgs, nil, "/messages")
	if msgs[0].Content != "keep me unchanged" {
		t.Errorf("original slice was mutated: %q", msgs[0].Content)
	}
}

func TestEnrichMessagesMixedEvictedAndNormal(t *testing.T) {
	msgs := []entity.Message{
		{
			ID:        entity.MessageID("msg-mix1"),
			Role:      entity.RoleUser,
			Content:   "evicted content",
			Evicted:   true,
			CreatedAt: evictedTS,
		},
		{
			ID:        entity.MessageID("msg-mix2"),
			Role:      entity.RoleUser,
			Content:   "normal content",
			CreatedAt: evictedTS,
		},
	}
	enriched := enrichMessages(msgs, nil, "/messages")
	// First: evicted -- no original content
	if strings.Contains(enriched[0].Content, "evicted content") {
		t.Error("evicted message should not contain original content")
	}
	if !strings.Contains(enriched[0].Content, `evicted=""/>`) {
		t.Error("evicted message should have evicted attribute")
	}
	// Second: normal -- has original content
	if !strings.Contains(enriched[1].Content, "normal content") {
		t.Error("normal message should retain content")
	}
	if strings.Contains(enriched[1].Content, "evicted") {
		t.Error("normal message should not have evicted attribute")
	}
}

func TestCountGroupTokensEvictedMessage(t *testing.T) {
	largeContent := strings.Repeat("x", 1000)
	evictedMsg := entity.Message{
		ID:        entity.MessageID("msg-ce1"),
		Role:      entity.RoleUser,
		Content:   largeContent,
		Evicted:   true,
		CreatedAt: evictedTS,
	}
	normalMsg := entity.Message{
		ID:        entity.MessageID("msg-ce2"),
		Role:      entity.RoleUser,
		Content:   largeContent,
		CreatedAt: evictedTS,
	}
	evictedTokens := countGroupTokens(messageGroup{messages: []entity.Message{evictedMsg}})
	normalTokens := countGroupTokens(messageGroup{messages: []entity.Message{normalMsg}})
	if evictedTokens >= 30 {
		t.Errorf("evicted message token count should be under 30, got %d", evictedTokens)
	}
	if evictedTokens >= normalTokens {
		t.Errorf("evicted tokens (%d) should be much less than normal tokens (%d)", evictedTokens, normalTokens)
	}
}

func TestCountGroupTokensEvictedToolGroup(t *testing.T) {
	largeContent := strings.Repeat("y", 500)
	makeGroup := func(evicted bool) messageGroup {
		return messageGroup{
			messages: []entity.Message{
				{
					ID:        entity.MessageID("msg-cg1"),
					Role:      entity.RoleAssistant,
					Content:   "",
					ToolCalls: []entity.ToolCall{{ID: "tc-1", Name: "search", Arguments: largeContent}},
					Evicted:   evicted,
					CreatedAt: evictedTS,
				},
				{
					ID:         entity.MessageID("msg-cg2"),
					Role:       entity.RoleTool,
					ToolCallID: "tc-1",
					Content:    largeContent,
					Evicted:    evicted,
					CreatedAt:  evictedTS,
				},
			},
		}
	}
	evictedTokens := countGroupTokens(makeGroup(true))
	normalTokens := countGroupTokens(makeGroup(false))
	// The evicted tool result is omitted (0 tokens). The evicted assistant with
	// tool calls still gets a placeholder, but it is much smaller than the normal
	// group which includes full tool call arguments and tool result content.
	if evictedTokens >= normalTokens/2 {
		t.Errorf("evicted group tokens (%d) should be much less than normal (%d)", evictedTokens, normalTokens)
	}
	// The evicted tool message contributes 0 tokens, so total should be small
	// (just the assistant placeholder).
	if evictedTokens >= 30 {
		t.Errorf("evicted group tokens should be under 30 (just assistant placeholder), got %d", evictedTokens)
	}
}
