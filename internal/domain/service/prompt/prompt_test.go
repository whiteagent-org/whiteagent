package prompt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// mockPathProvider implements PathProvider for testing.
type mockPathProvider struct {
	userHome   string
	tenantHome string
	messages   string
}

func (m *mockPathProvider) UserHomePath(_ entity.UserID) string     { return m.userHome }
func (m *mockPathProvider) TenantHomePath(_ entity.TenantID) string { return m.tenantHome }
func (m *mockPathProvider) MessagesPath() string                    { return m.messages }

// mockConvService returns a fixed history for testing Build() with history.
type mockConvService struct {
	history []entity.Message
}

func (m *mockConvService) ResolveConversation(_ context.Context, _ entity.Message) (entity.ConversationID, error) {
	return "conv-test", nil
}
func (m *mockConvService) Append(context.Context, entity.ConversationID, entity.Message) error {
	return nil
}
func (m *mockConvService) GetHistory(_ context.Context, _ entity.ConversationID, _, _ int, _ *entity.MessageID) ([]entity.Message, error) {
	return m.history, nil
}
func (m *mockConvService) RegisterConversation(entity.TenantID, entity.ConversationID) {}
func (m *mockConvService) ResetConversation(context.Context, entity.ConversationID) error {
	return nil
}
func (m *mockConvService) SwitchConversation(entity.Message, entity.ConversationID) {}

func buildPrompt(t *testing.T, msg entity.Message) string {
	t.Helper()
	pb, err := NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, msg, "", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("Build returned no messages")
	}
	return msgs[0].Content
}

func TestChannelContextRendered(t *testing.T) {
	msg := entity.Message{
		Metadata: map[string]string{
			"sender_name": "Dmitry",
			"chat_type":   "supergroup",
		},
	}
	prompt := buildPrompt(t, msg)

	if !strings.Contains(prompt, "## Message Context") {
		t.Error("missing '## Message Context' heading")
	}
	if !strings.Contains(prompt, "chat_type: supergroup") {
		t.Error("missing 'chat_type: supergroup'")
	}
	if !strings.Contains(prompt, "sender_name: Dmitry") {
		t.Error("missing 'sender_name: Dmitry'")
	}
}

func TestChannelContextEmpty(t *testing.T) {
	msg := entity.Message{}
	prompt := buildPrompt(t, msg)

	if !strings.Contains(prompt, "## Message Context") {
		t.Error("missing '## Message Context' heading")
	}
	if !strings.Contains(prompt, "Message context not available.") {
		t.Error("missing fallback text for empty metadata")
	}
}

func TestChannelTypeInjected(t *testing.T) {
	msg := entity.Message{
		Metadata: map[string]string{"channel_type": "channel.telegram"},
	}
	prompt := buildPrompt(t, msg)

	if !strings.Contains(prompt, "channel_type: channel.telegram") {
		t.Error("missing 'channel_type: channel.telegram'")
	}
}

func TestChannelContextSorted(t *testing.T) {
	msg := entity.Message{
		Metadata: map[string]string{
			"z_key":        "z",
			"a_key":        "a",
			"m_key":        "m",
			"channel_type": "channel.test",
		},
	}
	prompt := buildPrompt(t, msg)

	// Expected order: a_key, channel_type, m_key, z_key
	idxA := strings.Index(prompt, "a_key: a")
	idxC := strings.Index(prompt, "channel_type: channel.test")
	idxM := strings.Index(prompt, "m_key: m")
	idxZ := strings.Index(prompt, "z_key: z")

	if idxA < 0 || idxC < 0 || idxM < 0 || idxZ < 0 {
		t.Fatalf("missing keys in prompt: a=%d c=%d m=%d z=%d", idxA, idxC, idxM, idxZ)
	}
	if !(idxA < idxC && idxC < idxM && idxM < idxZ) {
		t.Errorf("keys not sorted: a=%d c=%d m=%d z=%d", idxA, idxC, idxM, idxZ)
	}
}

func TestSenderNameGroupNameRemoved(t *testing.T) {
	msg := entity.Message{
		ChatID: entity.ChatID("chat-prompt-test"), IsGroup: true,
		Metadata: map[string]string{
			"sender_name": "Dmitry",
			"group_name":  "Test Group",
		},
	}
	prompt := buildPrompt(t, msg)

	// Session heading should be plain "## Session: Group Chat" without group name
	if !strings.Contains(prompt, "## Session: Group Chat") {
		t.Error("missing group chat heading")
	}
	if strings.Contains(prompt, "## Session: Group Chat --") || strings.Contains(prompt, "## Session: Group Chat —") {
		t.Error("group name should not appear in heading")
	}
	// Should not contain "The current message is from"
	if strings.Contains(prompt, "The current message is from") {
		t.Error("sender name reference should be removed from group session text")
	}
}

func TestCronBlockRendered(t *testing.T) {
	msg := entity.Message{Kind: entity.MessageKindCron}
	prompt := buildPrompt(t, msg)
	if !strings.Contains(prompt, "## Cron Task Execution") {
		t.Error("missing cron execution block for cron message")
	}
	if !strings.Contains(prompt, "Do NOT create new scheduled tasks") {
		t.Error("missing cron_create prohibition")
	}
	if !strings.Contains(prompt, "Do NOT use cron_create") {
		t.Error("missing explicit cron_create tool prohibition")
	}
}

func TestCronBlockAbsentForRegularMessage(t *testing.T) {
	msg := entity.Message{Kind: entity.MessageKindMessage}
	prompt := buildPrompt(t, msg)
	if strings.Contains(prompt, "Cron Task Execution") {
		t.Error("cron block should not appear for regular messages")
	}
}

func TestMessageContextHeading(t *testing.T) {
	msg := entity.Message{}
	prompt := buildPrompt(t, msg)
	if !strings.Contains(prompt, "## Message Context") {
		t.Error("missing '## Message Context' heading")
	}
	if strings.Contains(prompt, "## Channel Context") {
		t.Error("should use '## Message Context' not '## Channel Context'")
	}
}

// TestMessageToolDuplicationGuard asserts the prompt explicitly tells the
// model that `message` tool calls and the final reply both reach the user.
// This guards against the duplicate-message bug where the model called
// `message("X")` and then ended its turn with a final reply that also said "X",
// resulting in the user seeing the same content twice.
func TestMessageToolDuplicationGuard(t *testing.T) {
	prompt := buildPrompt(t, entity.Message{})
	if !strings.Contains(prompt, "Both channels are delivered") {
		t.Error("prompt is missing the 'Both channels are delivered' clarification — duplicate-message guard removed")
	}
	if !strings.Contains(prompt, "must not repeat the same content") {
		t.Error("prompt is missing the 'must not repeat the same content' rule — duplicate-message guard weakened")
	}
}

func TestAllMetadataRendered(t *testing.T) {
	msg := entity.Message{
		Metadata: map[string]string{
			"key_a": "alpha",
			"key_b": "beta",
		},
	}
	prompt := buildPrompt(t, msg)

	if !strings.Contains(prompt, "key_a: alpha") {
		t.Error("missing 'key_a: alpha'")
	}
	if !strings.Contains(prompt, "key_b: beta") {
		t.Error("missing 'key_b: beta'")
	}
}

func TestBuildContextBlockSelfClosing(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-123"),
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
	}
	got := buildContextBlock(msg, nil, "/messages")
	if !strings.Contains(got, `<wa_msg_context msg_id="msg-123"`) {
		t.Error("missing msg_id attribute")
	}
	if !strings.Contains(got, `ts="2026-03-14T10:30:00Z"`) {
		t.Error("missing ts attribute")
	}
	if !strings.Contains(got, "/>") {
		t.Error("expected self-closing tag")
	}
	if strings.Contains(got, "</wa_msg_context>") {
		t.Error("should not have closing tag for self-closing element")
	}
}

func TestMessageContextBlocksDocumentToolResults(t *testing.T) {
	prompt := buildPrompt(t, entity.Message{})
	if !strings.Contains(prompt, "Each user, assistant, and tool-result message is prefixed") {
		t.Fatalf("prompt missing tool-result context block documentation: %q", prompt)
	}
}

func TestBuildContextBlockNoID(t *testing.T) {
	msg := entity.Message{
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
	}
	got := buildContextBlock(msg, nil, "/messages")
	if !strings.Contains(got, `<wa_msg_context ts="2026-03-14T10:30:00Z"`) {
		t.Error("missing ts attribute")
	}
	if strings.Contains(got, "msg_id") {
		t.Error("should not contain msg_id when ID is empty")
	}
}

func TestBuildContextBlockEmpty(t *testing.T) {
	msg := entity.Message{}
	got := buildContextBlock(msg, nil, "/messages")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestBuildContextBlockWithAttachments(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-456"),
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
		Attachments: []entity.Attachment{
			{Kind: "photo", Filename: "pic.jpg", MimeType: "image/jpeg", Size: 1048576, Caption: "sunset"},
			{Kind: "document", Filename: "doc.pdf", MimeType: "application/pdf", Size: 2048},
		},
	}
	got := buildContextBlock(msg, nil, "/messages")
	if !strings.Contains(got, `<attachment idx="0"`) {
		t.Error("missing attachment idx 0")
	}
	if !strings.Contains(got, "<kind>photo</kind>") {
		t.Error("missing kind photo")
	}
	if !strings.Contains(got, "<filename>pic.jpg</filename>") {
		t.Error("missing filename")
	}
	if !strings.Contains(got, "<size>1.0 MB</size>") {
		t.Error("missing size")
	}
	if !strings.Contains(got, "<mime_type>image/jpeg</mime_type>") {
		t.Error("missing mime_type")
	}
	if !strings.Contains(got, "<caption>sunset</caption>") {
		t.Error("missing caption")
	}
	if !strings.Contains(got, `<attachment idx="1"`) {
		t.Error("missing attachment idx 1")
	}
	if !strings.Contains(got, "</wa_msg_context>") {
		t.Error("missing closing context tag")
	}
}

func TestAttachmentPathUsesMessagesPathTmpl(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-custom"),
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
		Attachments: []entity.Attachment{
			{Kind: "photo", Filename: "pic.jpg", MimeType: "image/jpeg", Size: 100},
		},
	}
	got := buildContextBlock(msg, nil, "/custom/msgs")
	wantPath := "/custom/msgs/msg-custom/pic.jpg"
	if !strings.Contains(got, wantPath) {
		t.Errorf("expected path %q in output, got: %s", wantPath, got)
	}
}

func TestAttachmentPathDefaultsToMessages(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-default"),
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
		Attachments: []entity.Attachment{
			{Kind: "document", Filename: "file.txt", MimeType: "text/plain", Size: 50},
		},
	}
	// Empty messagesPath gets defaulted in PromptBuilder; at buildContextBlock level
	// we pass the resolved value directly.
	got := buildContextBlock(msg, nil, "/messages")
	wantPath := "/messages/msg-default/file.txt"
	if !strings.Contains(got, wantPath) {
		t.Errorf("expected path %q in output, got: %s", wantPath, got)
	}
}

func TestBuildContextBlockXMLEscape(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-789"),
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
		Attachments: []entity.Attachment{
			{Kind: "document", Filename: "file<>&.txt", MimeType: "text/plain", Size: 100},
		},
	}
	got := buildContextBlock(msg, nil, "/messages")
	if strings.Contains(got, "file<>&.txt") {
		t.Error("filename should be XML-escaped")
	}
	if !strings.Contains(got, "file&lt;&gt;&amp;.txt") {
		t.Errorf("expected escaped filename, got: %s", got)
	}
}

func TestBuildContextBlockReplyTo(t *testing.T) {
	msg := entity.Message{
		ID:          entity.MessageID("msg-reply"),
		CreatedAt:   time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
		RepliedToID: entity.MessageID("msg-original"),
	}
	got := buildContextBlock(msg, nil, "/messages")
	if !strings.Contains(got, `reply_to="msg-original"`) {
		t.Errorf("missing reply_to attribute, got: %s", got)
	}
	if !strings.Contains(got, "/>") {
		t.Error("expected self-closing tag")
	}
}

func TestBuildContextBlockReplyToWithAttachments(t *testing.T) {
	msg := entity.Message{
		ID:          entity.MessageID("msg-reply2"),
		CreatedAt:   time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
		RepliedToID: entity.MessageID("msg-original2"),
		Attachments: []entity.Attachment{
			{Kind: "photo", Filename: "pic.jpg", MimeType: "image/jpeg", Size: 1024},
		},
	}
	got := buildContextBlock(msg, nil, "/messages")
	if !strings.Contains(got, `reply_to="msg-original2"`) {
		t.Errorf("missing reply_to attribute, got: %s", got)
	}
	if !strings.Contains(got, "</wa_msg_context>") {
		t.Error("expected closing context tag with attachments")
	}
}

func TestBuildContextBlockNoReplyTo(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-no-reply"),
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
	}
	got := buildContextBlock(msg, nil, "/messages")
	if strings.Contains(got, "reply_to") {
		t.Error("should not contain reply_to when RepliedToID is empty")
	}
}

// --- Integration tests for Build() enrichment ---

func buildWithHistory(t *testing.T, history []entity.Message) []entity.Message {
	t.Helper()
	mock := &mockConvService{history: history}
	pb, err := NewPromptBuilder(nil, mock, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{}, "conv-test", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return msgs
}

func TestBuildEnrichesUserMessage(t *testing.T) {
	history := []entity.Message{
		{
			ID:        entity.MessageID("msg-100"),
			Role:      entity.RoleUser,
			Content:   "hello",
			CreatedAt: time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
			Attachments: []entity.Attachment{
				{Kind: "photo", Filename: "pic.jpg", MimeType: "image/jpeg", Size: 2048},
			},
		},
	}
	msgs := buildWithHistory(t, history)
	if len(msgs) < 2 {
		t.Fatal("expected at least 2 messages")
	}
	userContent := msgs[1].Content
	if !strings.HasPrefix(userContent, `<wa_msg_context msg_id="msg-100"`) {
		t.Errorf("user message should start with context block, got: %s", userContent[:min(80, len(userContent))])
	}
	if !strings.Contains(userContent, `<attachment idx="0"`) {
		t.Error("missing attachment in enriched user message")
	}
	if !strings.Contains(userContent, "hello") {
		t.Error("original content should be preserved")
	}
}

func TestBuildEnrichesAssistantMessage(t *testing.T) {
	history := []entity.Message{
		{
			Role:      entity.RoleAssistant,
			Content:   "hi there",
			CreatedAt: time.Date(2026, 3, 14, 10, 5, 0, 0, time.UTC),
		},
	}
	msgs := buildWithHistory(t, history)
	if len(msgs) < 2 {
		t.Fatal("expected at least 2 messages")
	}
	content := msgs[1].Content
	if !strings.HasPrefix(content, `<wa_msg_context ts="2026-03-14T10:05:00Z"`) {
		t.Errorf("assistant message should start with context block, got: %s", content[:min(80, len(content))])
	}
	if strings.Contains(content, "msg_id") {
		t.Error("assistant message with no ID should not have msg_id")
	}
}

func TestBuildSkipsToolMessage(t *testing.T) {
	history := []entity.Message{
		{
			Role:       entity.RoleTool,
			Content:    "tool result",
			ToolCallID: "call-1",
			ToolName:   "test_tool",
			CreatedAt:  time.Date(2026, 3, 14, 10, 5, 0, 0, time.UTC),
		},
	}
	msgs := buildWithHistory(t, history)
	if len(msgs) < 2 {
		t.Fatal("expected at least 2 messages")
	}
	if strings.Contains(msgs[1].Content, "<context") {
		t.Error("tool message should not have context block")
	}
}

func TestBuildNoAttachmentsInSystemPrompt(t *testing.T) {
	history := []entity.Message{
		{
			ID:        entity.MessageID("msg-200"),
			Role:      entity.RoleUser,
			Content:   "check this",
			CreatedAt: time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
			Attachments: []entity.Attachment{
				{Kind: "photo", Filename: "pic.jpg", MimeType: "image/jpeg", Size: 1024},
			},
		},
	}
	msgs := buildWithHistory(t, history)
	sysPrompt := msgs[0].Content
	if strings.Contains(sysPrompt, "<attachments>") {
		t.Error("system prompt should not contain <attachments> block")
	}
	// The template guidance mentions <attachment> as documentation, but the
	// old rendered attachment list (<attachment>\n<kind>...) should be gone.
	if strings.Contains(sysPrompt, "<kind>photo</kind>") {
		t.Error("system prompt should not contain rendered attachment data")
	}
}

func TestBuildContextGuidanceInSystemPrompt(t *testing.T) {
	msgs := buildWithHistory(t, nil)
	sysPrompt := msgs[0].Content
	if !strings.Contains(sysPrompt, "## Message Context Blocks") {
		t.Error("system prompt should contain context blocks guidance")
	}
}

func TestBuildDoesNotMutateHistory(t *testing.T) {
	history := []entity.Message{
		{
			ID:        entity.MessageID("msg-300"),
			Role:      entity.RoleUser,
			Content:   "original text",
			CreatedAt: time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
		},
	}
	mock := &mockConvService{history: history}
	pb, err := NewPromptBuilder(nil, mock, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	_, _, err = pb.Build(context.Background(), nil, nil, nil, entity.Message{}, "conv-test", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if history[0].Content != "original text" {
		t.Errorf("history was mutated: %q", history[0].Content)
	}
}

// ---------------------------------------------------------------------------
// Channel capabilities tests
// ---------------------------------------------------------------------------

func TestReactionsSectionRenderedWhenCapable(t *testing.T) {
	pb, err := NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	caps := port.ChannelCapabilities{Reactions: true}
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{}, "", caps, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(msgs[0].Content, "## Reactions") {
		t.Error("expected Reactions section when caps.Reactions=true")
	}
}

func TestReactionsSectionHiddenWhenNotCapable(t *testing.T) {
	pb, err := NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	caps := port.ChannelCapabilities{Reactions: false}
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{}, "", caps, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(msgs[0].Content, "## Reactions") {
		t.Error("Reactions section should be hidden when caps.Reactions=false")
	}
}

// stubTool is a minimal ToolPlugin for testing tool filtering.
type stubTool struct {
	name string
}

func (s *stubTool) ID() string                                          { return "tool." + s.name }
func (s *stubTool) Kind() entity.PluginKind                             { return entity.PluginKindTool }
func (s *stubTool) Init(context.Context, string, json.RawMessage) error { return nil }
func (s *stubTool) Start(context.Context) error                         { return nil }
func (s *stubTool) Stop(context.Context) error                          { return nil }
func (s *stubTool) Status() entity.PluginState                          { return entity.PluginStateHealthy }
func (s *stubTool) Name() string                                        { return s.name }
func (s *stubTool) Description() string                                 { return s.name + " tool" }
func (s *stubTool) Parameters() json.RawMessage                         { return json.RawMessage("{}") }
func (s *stubTool) Instructions() string                                { return "" }
func (s *stubTool) Execute(context.Context, port.ToolContext, json.RawMessage) (string, error) {
	return "ok", nil
}

func TestReactionToolFilteredByCapabilities(t *testing.T) {
	tools := map[string]port.ToolPlugin{
		"reaction":   &stubTool{name: "reaction"},
		"other_tool": &stubTool{name: "other_tool"},
	}
	pb, err := NewPromptBuilder(tools, nil, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}

	// Without reactions capability: reaction tool should be absent.
	defs := pb.FilteredToolDefs(nil, port.ChannelCapabilities{Reactions: false})
	for _, d := range defs {
		if d.Name == "reaction" {
			t.Error("reaction tool should be filtered out when Reactions=false")
		}
	}
	found := false
	for _, d := range defs {
		if d.Name == "other_tool" {
			found = true
		}
	}
	if !found {
		t.Error("other_tool should be present when Reactions=false")
	}

	// With reactions capability: both tools should be present.
	defs = pb.FilteredToolDefs(nil, port.ChannelCapabilities{Reactions: true})
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["reaction"] {
		t.Error("reaction tool should be present when Reactions=true")
	}
	if !names["other_tool"] {
		t.Error("other_tool should be present when Reactions=true")
	}
}

// ---------------------------------------------------------------------------
// Journal injection tests
// ---------------------------------------------------------------------------

type mockJournalReader struct {
	entries              []entity.JournalEntry
	latestSummary        *entity.Summary
	filter               entity.JournalFilter // captured for assertions
	called               bool
	latestCalled         bool
	latestConversationID entity.ConversationID
}

func (m *mockJournalReader) GetJournal(_ context.Context, _ entity.TenantID, f entity.JournalFilter) ([]entity.JournalEntry, error) {
	m.called = true
	m.filter = f
	return m.entries, nil
}

func (m *mockJournalReader) GetLatestSummary(_ context.Context, _ entity.TenantID, convID entity.ConversationID) (*entity.Summary, error) {
	m.latestCalled = true
	m.latestConversationID = convID
	if m.latestSummary == nil {
		return nil, nil
	}
	summary := *m.latestSummary
	return &summary, nil
}

func TestJournalInjection(t *testing.T) {
	baseTime := time.Now().UTC().Add(-30 * time.Minute)
	journals := &mockJournalReader{
		entries: []entity.JournalEntry{
			{Category: "Decisions", Content: "User prefers dark mode", CreatedAt: baseTime.Add(15 * time.Minute)},
			{Category: "Key Events", Content: "Deployed v2", CreatedAt: baseTime.Add(20 * time.Minute)},
		},
		latestSummary: &entity.Summary{ID: "sum-1", Content: "## Goals\nConversation summary", MessageID: "msg-1", CreatedAt: baseTime.Add(5 * time.Minute)},
	}
	history := []entity.Message{
		{ID: "msg-1", Role: entity.RoleUser, Content: "hello", CreatedAt: baseTime},
		{ID: "msg-2", Role: entity.RoleAssistant, Content: "hi there", CreatedAt: baseTime.Add(time.Minute)},
		{ID: "msg-3", Role: entity.RoleUser, Content: "follow up", CreatedAt: baseTime.Add(2 * time.Minute)},
	}
	mock := &mockConvService{history: history}
	pb, err := NewPromptBuilder(nil, mock, journals, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-1"}, "conv-123", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Expect: [system, compacted, assistant, user]
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(msgs))
	}
	if msgs[1].Role != entity.RoleSystem {
		t.Errorf("msgs[1] role = %q, want system", msgs[1].Role)
	}
	if !strings.Contains(msgs[1].Content, "<wa_compacted_context") {
		t.Errorf("msgs[1] should contain <wa_compacted_context, got: %s", msgs[1].Content[:min(100, len(msgs[1].Content))])
	}
	if !strings.Contains(msgs[1].Content, "User prefers dark mode") {
		t.Error("journal content missing")
	}
	if !strings.Contains(msgs[1].Content, "Deployed v2") {
		t.Error("second journal entry content missing")
	}
	if !strings.Contains(msgs[1].Content, "<summary") {
		t.Error("summary block missing")
	}
	if strings.Index(msgs[1].Content, "<summary") > strings.Index(msgs[1].Content, `<journal_entry category="Decisions"`) {
		t.Error("summary should render before journal entries")
	}
	if strings.Contains(msgs[1].Content, "hello") {
		t.Error("messages covered by summary should not remain in active history")
	}
	if !strings.Contains(msgs[1].Content, "Conversation summary") {
		t.Error("summary content missing")
	}
}

func TestCompactedContextDoesNotTrimHistory(t *testing.T) {
	// With no compacted context present, the full raw history should fit when the
	// remaining shared budget is large enough.
	filler := strings.Repeat("x", 400)
	baseTime := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	var history []entity.Message
	for i := 0; i < 20; i++ {
		role := entity.RoleUser
		if i%2 == 1 {
			role = entity.RoleAssistant
		}
		history = append(history, entity.Message{
			Role:      role,
			Content:   filler,
			CreatedAt: baseTime.Add(time.Duration(i) * time.Minute),
		})
	}

	mockConv := &mockConvService{history: history}
	journals := &mockJournalReader{entries: nil}
	pbSplit, _ := NewPromptBuilder(nil, mockConv, journals, nil, 9000, nil, nil, nil)
	msgsSplit, _, _ := pbSplit.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-1"}, "conv-1", port.ChannelCapabilities{}, nil)

	historyCount := 0
	for _, msg := range msgsSplit {
		if msg.Role != entity.RoleSystem {
			historyCount++
		}
	}
	if historyCount != len(history) {
		t.Fatalf("history count = %d, want %d", historyCount, len(history))
	}
}

func TestJournalFiltering(t *testing.T) {
	baseTime := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	journals := &mockJournalReader{entries: nil}
	history := []entity.Message{
		{Role: entity.RoleUser, Content: "old message", CreatedAt: baseTime},
		{Role: entity.RoleAssistant, Content: "reply", CreatedAt: baseTime.Add(time.Minute)},
		{Role: entity.RoleUser, Content: "new message", CreatedAt: baseTime.Add(2 * time.Minute)},
	}
	mock := &mockConvService{history: history}
	pb, _ := NewPromptBuilder(nil, mock, journals, nil, 32000, nil, nil, nil)
	_, _, _ = pb.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-filter"}, "conv-filter", port.ChannelCapabilities{}, nil)

	if !journals.called {
		t.Fatal("journal reader was not called")
	}
	if journals.filter.ChatID != "chat-filter" {
		t.Errorf("filter.ChatID = %q, want chat-filter", journals.filter.ChatID)
	}
	// BeforeTime should be the current conversation start time.
	if journals.filter.BeforeTime != baseTime {
		t.Errorf("filter.BeforeTime = %v, want %v", journals.filter.BeforeTime, baseTime)
	}
}

func TestSummaryScopedJournalsKeepNewestTenFromLastWeek(t *testing.T) {
	baseTime := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)
	entries := make([]entity.JournalEntry, 0, 14)
	for i := 0; i < 12; i++ {
		entries = append(entries, entity.JournalEntry{
			Category:  "Decisions",
			Content:   fmt.Sprintf("recent-%02d", i),
			CreatedAt: baseTime.Add(-time.Duration(12-i) * time.Hour),
		})
	}
	entries = append(entries,
		entity.JournalEntry{Category: "Decisions", Content: "too-old", CreatedAt: baseTime.AddDate(0, 0, -8)},
		entity.JournalEntry{Category: "Decisions", Content: "covered-by-summary", CreatedAt: baseTime.Add(-5 * 24 * time.Hour)},
	)

	journals := &mockJournalReader{
		entries:       entries,
		latestSummary: &entity.Summary{ID: "sum-1", MessageID: "msg-2", Content: "latest summary", CreatedAt: baseTime.Add(-4 * 24 * time.Hour)},
	}
	history := []entity.Message{
		{ID: "msg-1", Role: entity.RoleUser, Content: "covered user", CreatedAt: baseTime.Add(-5 * 24 * time.Hour)},
		{ID: "msg-2", Role: entity.RoleAssistant, Content: "covered assistant", CreatedAt: baseTime.Add(-4 * 24 * time.Hour)},
		{ID: "msg-3", Role: entity.RoleUser, Content: "active user", CreatedAt: baseTime.Add(-2 * time.Hour)},
	}

	pb, _ := NewPromptBuilder(nil, &mockConvService{history: history}, journals, nil, 32000, nil, nil, nil)
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-1"}, "conv-summary", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	rendered := msgs[1].Content
	if strings.Contains(rendered, "too-old") || strings.Contains(rendered, "covered-by-summary") {
		t.Fatal("summary-scoped compacted context should exclude journals outside the 7-day window or at/before the summary timestamp")
	}
	if strings.Contains(rendered, "recent-00") || strings.Contains(rendered, "recent-01") {
		t.Fatal("summary-scoped compacted context should keep only the newest 10 journal entries")
	}
	for i := 2; i < 12; i++ {
		want := fmt.Sprintf("recent-%02d", i)
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected compacted context to retain %q", want)
		}
	}
}

func TestSummaryBoundaryTrimsCoveredMessages(t *testing.T) {
	baseTime := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	store := &mockJournalReader{
		latestSummary: &entity.Summary{ID: "sum-2", Content: "latest summary", MessageID: "msg-2", CreatedAt: baseTime.Add(10 * time.Minute)},
	}
	history := []entity.Message{
		{ID: "msg-1", Role: entity.RoleUser, Content: "covered user", CreatedAt: baseTime},
		{ID: "msg-2", Role: entity.RoleAssistant, Content: "covered assistant", CreatedAt: baseTime.Add(time.Minute)},
		{ID: "msg-3", Role: entity.RoleUser, Content: "active user", CreatedAt: baseTime.Add(2 * time.Minute)},
	}
	pb, _ := NewPromptBuilder(nil, &mockConvService{history: history}, store, nil, 32000, nil, nil, nil)

	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-1"}, "conv-summary", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !store.latestCalled {
		t.Fatal("expected latest summary to be loaded")
	}
	if store.latestConversationID != "conv-summary" {
		t.Fatalf("GetLatestSummary conversation = %q, want %q", store.latestConversationID, "conv-summary")
	}
	rendered := msgs[1].Content
	if !strings.Contains(rendered, "latest summary") {
		t.Fatal("expected latest summary in compacted context")
	}
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "covered user") || strings.Contains(msg.Content, "covered assistant") {
			t.Fatal("messages covered by summary should not remain in active prompt history")
		}
	}
	foundActive := false
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "active user") {
			foundActive = true
		}
	}
	if !foundActive {
		t.Fatal("expected unsummarized history to remain in prompt")
	}
}

func TestJournalNilReader(t *testing.T) {
	history := []entity.Message{
		{Role: entity.RoleUser, Content: "hello", CreatedAt: time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)},
	}
	mock := &mockConvService{history: history}
	pb, _ := NewPromptBuilder(nil, mock, nil, nil, 32000, nil, nil, nil)
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{}, "conv-nil", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Should be [system, user] -- no journal message.
	for i, m := range msgs {
		if i > 0 && m.Role == entity.RoleSystem {
			t.Error("unexpected system message (journal) when journals is nil")
		}
	}
}

func TestJournalEmptyHistory(t *testing.T) {
	journals := &mockJournalReader{entries: []entity.JournalEntry{
		{Category: "Test", Content: "should not appear"},
	}}
	mock := &mockConvService{history: nil}
	pb, _ := NewPromptBuilder(nil, mock, journals, nil, 32000, nil, nil, nil)
	msgs, _, _ := pb.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-empty"}, "conv-empty", port.ChannelCapabilities{}, nil)
	if !journals.called {
		t.Error("journal reader should still be called so journals can form compacted context without raw history")
	}
	if len(msgs) != 2 {
		t.Errorf("expected system prompt plus compacted context, got %d messages", len(msgs))
	}
	if len(msgs) > 1 && !strings.Contains(msgs[1].Content, "should not appear") {
		t.Error("expected journal-only compacted context when history is empty")
	}
}

func TestJournalBudgetTrimming(t *testing.T) {
	baseTime := time.Now().UTC().Add(-30 * time.Minute)
	// Create large journal entries that cannot all fit beside the system prompt.
	bigContent := strings.Repeat("y", 1200)
	journals := &mockJournalReader{
		entries: []entity.JournalEntry{
			{Category: "Old", Content: bigContent, CreatedAt: baseTime.Add(-30 * time.Minute)},
			{Category: "Mid", Content: bigContent, CreatedAt: baseTime.Add(-20 * time.Minute)},
			{Category: "New", Content: bigContent, CreatedAt: baseTime.Add(-10 * time.Minute)},
		},
	}
	history := []entity.Message{
		{Role: entity.RoleUser, Content: "hi", CreatedAt: baseTime},
	}
	mock := &mockConvService{history: history}
	// With shared budgeting, the newest journals should survive while the oldest is dropped.
	// Budget must be tight enough that 3 large journals + system prompt cannot fit, but 2 can.
	// System prompt ~3950 tokens; each journal entry ~350 tokens; we need room for 2 but not 3.
	pb, _ := NewPromptBuilder(nil, mock, journals, nil, 4700, nil, nil, nil)
	msgs, _, _ := pb.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-trim"}, "conv-trim", port.ChannelCapabilities{}, nil)

	// Find the journal message (skip system prompt at index 0).
	var journalContent string
	for i, m := range msgs {
		if i > 0 && strings.Contains(m.Content, "<wa_compacted_context") {
			journalContent = m.Content
			break
		}
	}
	if journalContent == "" {
		t.Fatal("no journal message found")
	}
	// Newest entry should be present, oldest should be trimmed.
	if !strings.Contains(journalContent, `category="New"`) {
		t.Error("newest journal entry should be kept")
	}
	if strings.Contains(journalContent, `category="Old"`) {
		t.Error("oldest journal entry should be trimmed")
	}
}

// ---------------------------------------------------------------------------
// User ID / User Name in context blocks
// ---------------------------------------------------------------------------

func TestBuildContextBlockUserIDAndName(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-user"),
		Role:      entity.RoleUser,
		UserID:    entity.UserID("u-42"),
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
	}
	userNames := map[entity.UserID]string{
		entity.UserID("u-42"): "Alice",
	}
	got := buildContextBlock(msg, userNames, "/messages")
	if !strings.Contains(got, `user_id="u-42"`) {
		t.Errorf("missing user_id attribute, got: %s", got)
	}
	if !strings.Contains(got, `user_name="Alice"`) {
		t.Errorf("missing user_name attribute, got: %s", got)
	}
}

func TestBuildContextBlockUserIDNoName(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-user2"),
		Role:      entity.RoleUser,
		UserID:    entity.UserID("u-99"),
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
	}
	got := buildContextBlock(msg, nil, "/messages")
	if !strings.Contains(got, `user_id="u-99"`) {
		t.Errorf("missing user_id attribute, got: %s", got)
	}
	if strings.Contains(got, "user_name") {
		t.Error("user_name should not appear when name map is nil")
	}
}

func TestBuildContextBlockAssistantNoUserID(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-asst"),
		Role:      entity.RoleAssistant,
		UserID:    entity.UserID("u-42"),
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
	}
	got := buildContextBlock(msg, map[entity.UserID]string{"u-42": "Alice"}, "/messages")
	if strings.Contains(got, "user_id") {
		t.Error("assistant messages should not have user_id")
	}
}

// ---------------------------------------------------------------------------
// Scheduling section tests
// ---------------------------------------------------------------------------

func TestSchedulingSectionContainsTimeProtocol(t *testing.T) {
	prompt := buildPrompt(t, entity.Message{})
	if !strings.Contains(prompt, "Call `time` before any scheduling action") {
		t.Error("scheduling section should require calling time tool first")
	}
}

func TestSchedulingSectionContainsIntentFraming(t *testing.T) {
	prompt := buildPrompt(t, entity.Message{})
	if !strings.Contains(prompt, "Reminder intent") {
		t.Error("scheduling section should describe reminder intent")
	}
	if !strings.Contains(prompt, "Task intent") {
		t.Error("scheduling section should describe task intent")
	}
}

// ---------------------------------------------------------------------------
// Default agent instructions tests
// ---------------------------------------------------------------------------

func TestDefaultAgentInstructions(t *testing.T) {
	got := entity.DefaultAgentInstructions()
	if got == "" {
		t.Fatal("DefaultAgentInstructions() returned empty string")
	}
	if !strings.Contains(got, "genuinely helpful") {
		t.Error("default instructions should contain 'genuinely helpful'")
	}
	if !strings.Contains(got, "Have opinions") {
		t.Error("default instructions should contain 'Have opinions'")
	}
}

// ---------------------------------------------------------------------------
// Skills integration tests
// ---------------------------------------------------------------------------

// mockSkillLister is a test double for SkillLister.
type mockSkillLister struct {
	skills []entity.Skill
	err    error
}

func (m *mockSkillLister) List(_ entity.TenantID, _ entity.UserID) ([]entity.Skill, error) {
	return m.skills, m.err
}

func TestSkillsBlockRendered(t *testing.T) {
	skills := &mockSkillLister{skills: []entity.Skill{
		{Name: "web-search", DisplayName: "Web Search", Description: "Search the web", Level: entity.SkillLevelTenant},
		{Name: "my-skill", DisplayName: "My Skill", Description: "User skill", Level: entity.SkillLevelUser},
	}}
	paths := &mockPathProvider{userHome: "/home/whiteagent", tenantHome: "/tenant", messages: "/messages"}
	pb, err := NewPromptBuilder(nil, nil, nil, nil, 32000, nil, skills, paths)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	tenant := &entity.Tenant{ID: entity.TenantID("t-1"), Name: "Test"}
	user := &entity.User{ID: entity.UserID("u-42"), Name: "Alice"}
	msgs, _, err := pb.Build(context.Background(), tenant, nil, user, entity.Message{}, "", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := msgs[0].Content
	if !strings.Contains(sys, "<skills>") {
		t.Error("system prompt should contain <skills> block")
	}
	if !strings.Contains(sys, "<name>web-search</name>") {
		t.Error("missing web-search skill name")
	}
	if !strings.Contains(sys, "<description>Search the web</description>") {
		t.Error("missing skill description")
	}
	// Tenant skill should use tenant home path
	if !strings.Contains(sys, "<path>/tenant/skills/web-search</path>") {
		t.Error("tenant skill should have container path /tenant/skills/web-search")
	}
	// User skill should use user home path
	if !strings.Contains(sys, "<path>/home/whiteagent/skills/my-skill</path>") {
		t.Error("user skill should have container path /home/whiteagent/skills/my-skill")
	}
	if !strings.Contains(sys, "<name>my-skill</name>") {
		t.Error("missing my-skill skill name")
	}
	// Source element should not be present
	if strings.Contains(sys, "<source>") {
		t.Error("source element should not be present")
	}
	if !strings.Contains(sys, "## Skills") {
		t.Error("missing Skills heading in prompt")
	}
	if !strings.Contains(sys, "BEFORE using any skill, you MUST") {
		t.Error("missing mandatory skill usage instructions")
	}
	if !strings.Contains(sys, "SKILL.md defines how to use the skill") {
		t.Error("missing anti-hallucination guard in skill instructions")
	}
}

func TestSkillsBlockEmpty(t *testing.T) {
	skills := &mockSkillLister{skills: nil}
	paths := &mockPathProvider{userHome: "/home/whiteagent", tenantHome: "/tenant", messages: "/messages"}
	pb, err := NewPromptBuilder(nil, nil, nil, nil, 32000, nil, skills, paths)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	tenant := &entity.Tenant{ID: entity.TenantID("t-1")}
	user := &entity.User{ID: entity.UserID("u-42")}
	msgs, _, err := pb.Build(context.Background(), tenant, nil, user, entity.Message{}, "", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := msgs[0].Content
	if !strings.Contains(sys, "<skills>") {
		t.Error("empty skills should still render <skills> block")
	}
	// user_home and tenant_home should no longer be present
	if strings.Contains(sys, "<user_home>") {
		t.Error("user_home should not be present")
	}
	if strings.Contains(sys, "<tenant_home>") {
		t.Error("tenant_home should not be present")
	}
}

func TestSkillsPathResolution(t *testing.T) {
	skills := &mockSkillLister{skills: []entity.Skill{
		{Name: "test-skill", Description: "Test", Level: entity.SkillLevelUser},
	}}
	paths := &mockPathProvider{userHome: "/home/whiteagent", tenantHome: "/tenant", messages: "/messages"}
	pb, err := NewPromptBuilder(nil, nil, nil, nil, 32000, nil, skills, paths)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	tenant := &entity.Tenant{ID: entity.TenantID("t-99")}
	user := &entity.User{ID: entity.UserID("u-77")}
	msgs, _, err := pb.Build(context.Background(), tenant, nil, user, entity.Message{}, "", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := msgs[0].Content
	// Skill path should use PathProvider home paths
	if !strings.Contains(sys, "<path>/home/whiteagent/skills/test-skill</path>") {
		t.Error("user skill path not rendered correctly")
	}
}

func TestSkillsNilLister(t *testing.T) {
	// nil SkillLister should not break anything (backward compat)
	pb, err := NewPromptBuilder(nil, nil, nil, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{}, "", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := msgs[0].Content
	// Skills section should still appear in template but SkillsBlock should be empty
	if strings.Contains(sys, "<skills>") {
		t.Error("nil lister should not produce <skills> block")
	}
}

func TestSkillsNilListerBackwardCompat(t *testing.T) {
	// Existing buildPrompt helper uses nil for skills -- must still work
	prompt := buildPrompt(t, entity.Message{})
	if prompt == "" {
		t.Error("buildPrompt should produce non-empty output")
	}
}

func TestBuildContextBlockEmptyUserID(t *testing.T) {
	msg := entity.Message{
		ID:        entity.MessageID("msg-nouid"),
		Role:      entity.RoleUser,
		CreatedAt: time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
	}
	got := buildContextBlock(msg, nil, "/messages")
	if strings.Contains(got, "user_id") {
		t.Error("user_id should not appear when UserID is empty")
	}
}
