package prompt

import (
	"context"
	"encoding/json"
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
// Compacted context (current-conversation summary) injection tests
// ---------------------------------------------------------------------------

// mockStoreReader stubs the store dependency that NewPromptBuilder consumes
// via type assertion. Only GetJournal is required by the formal parameter
// type (port.JournalReader); journals are no longer auto-injected and the
// method should never be called from Build().
type mockStoreReader struct {
	latestSummary        *entity.Summary
	journalCalled        bool
	latestCalled         bool
	latestConversationID entity.ConversationID
}

func (m *mockStoreReader) GetJournal(_ context.Context, _ entity.TenantID, _ entity.JournalFilter) ([]entity.JournalEntry, error) {
	m.journalCalled = true
	return nil, nil
}

func (m *mockStoreReader) GetLatestSummary(_ context.Context, _ entity.TenantID, convID entity.ConversationID) (*entity.Summary, error) {
	m.latestCalled = true
	m.latestConversationID = convID
	if m.latestSummary == nil {
		return nil, nil
	}
	summary := *m.latestSummary
	return &summary, nil
}

// TestSummaryInjectedWhenPresent asserts that when GetLatestSummary returns a
// summary for the current convID, it appears in the compacted-context block
// between the system prompt and the active history.
func TestSummaryInjectedWhenPresent(t *testing.T) {
	baseTime := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	store := &mockStoreReader{
		latestSummary: &entity.Summary{
			ID:        "sum-1",
			Content:   "## Goals\nConversation summary",
			MessageID: "msg-1",
			CreatedAt: baseTime.Add(5 * time.Minute),
		},
	}
	history := []entity.Message{
		{ID: "msg-1", Role: entity.RoleUser, Content: "covered hello", CreatedAt: baseTime},
		{ID: "msg-2", Role: entity.RoleAssistant, Content: "covered hi", CreatedAt: baseTime.Add(time.Minute)},
		{ID: "msg-3", Role: entity.RoleUser, Content: "active follow up", CreatedAt: baseTime.Add(10 * time.Minute)},
	}
	mock := &mockConvService{history: history}
	pb, err := NewPromptBuilder(nil, mock, store, nil, 32000, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewPromptBuilder: %v", err)
	}
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-1"}, "conv-123", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	if msgs[1].Role != entity.RoleSystem {
		t.Errorf("msgs[1] role = %q, want system", msgs[1].Role)
	}
	if !strings.Contains(msgs[1].Content, "<wa_compacted_context") {
		t.Errorf("msgs[1] should contain <wa_compacted_context, got: %s", msgs[1].Content[:min(120, len(msgs[1].Content))])
	}
	if !strings.Contains(msgs[1].Content, "<summary") {
		t.Error("summary block missing")
	}
	if !strings.Contains(msgs[1].Content, "Conversation summary") {
		t.Error("summary content missing")
	}
	if strings.Contains(msgs[1].Content, "<journal_entry") {
		t.Error("compacted context should not contain journal entries -- they are no longer auto-injected")
	}
	if store.journalCalled {
		t.Error("Build() must not call GetJournal -- journal entries are not auto-injected")
	}
}

// TestNoCompactedContextWhenSummaryAbsent asserts that a fresh conversation
// (no summary yet) starts clean: only system prompt + history, no compacted block.
func TestNoCompactedContextWhenSummaryAbsent(t *testing.T) {
	history := []entity.Message{
		{Role: entity.RoleUser, Content: "hello", CreatedAt: time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)},
	}
	mock := &mockConvService{history: history}
	store := &mockStoreReader{}
	pb, _ := NewPromptBuilder(nil, mock, store, nil, 32000, nil, nil, nil)
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-1"}, "conv-fresh", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i, m := range msgs {
		if i > 0 && strings.Contains(m.Content, "<wa_compacted_context") {
			t.Errorf("msgs[%d] unexpectedly contains compacted context for a fresh convID", i)
		}
		if i > 0 && m.Role == entity.RoleSystem {
			t.Errorf("msgs[%d] is an unexpected system message; only the initial system prompt should remain", i)
		}
	}
	if !store.latestCalled {
		t.Error("expected GetLatestSummary to be called to check for a current-convID summary")
	}
	if store.latestConversationID != "conv-fresh" {
		t.Errorf("GetLatestSummary called with convID %q, want %q", store.latestConversationID, "conv-fresh")
	}
	if store.journalCalled {
		t.Error("Build() must not call GetJournal -- journals are no longer auto-injected")
	}
}

// TestFullHistoryFitsWhenNoCompactedContext asserts that without a summary
// the entire raw history fits as long as the budget allows.
func TestFullHistoryFitsWhenNoCompactedContext(t *testing.T) {
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
	store := &mockStoreReader{}
	pb, _ := NewPromptBuilder(nil, mockConv, store, nil, 9000, nil, nil, nil)
	msgs, _, _ := pb.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-1"}, "conv-1", port.ChannelCapabilities{}, nil)

	historyCount := 0
	for _, msg := range msgs {
		if msg.Role != entity.RoleSystem {
			historyCount++
		}
	}
	if historyCount != len(history) {
		t.Fatalf("history count = %d, want %d", historyCount, len(history))
	}
}

// TestSummaryScopedToCurrentConvID asserts that the summary lookup uses the
// current convID -- the per-convID scoping that makes new conversations
// start clean.
func TestSummaryScopedToCurrentConvID(t *testing.T) {
	store := &mockStoreReader{}
	mock := &mockConvService{history: nil}
	pb, _ := NewPromptBuilder(nil, mock, store, nil, 32000, nil, nil, nil)
	_, _, _ = pb.Build(context.Background(), nil, nil, nil, entity.Message{TenantID: "tenant-1", ChatID: "chat-1"}, "conv-xyz", port.ChannelCapabilities{}, nil)

	if !store.latestCalled {
		t.Fatal("expected GetLatestSummary to be called")
	}
	if store.latestConversationID != "conv-xyz" {
		t.Errorf("GetLatestSummary convID = %q, want %q", store.latestConversationID, "conv-xyz")
	}
}

// TestSummaryBoundaryTrimsCoveredMessages asserts that messages whose ID is
// at or before the summary's MessageID are removed from active history while
// messages after the boundary remain.
func TestSummaryBoundaryTrimsCoveredMessages(t *testing.T) {
	baseTime := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	store := &mockStoreReader{
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

// TestNilStoreReaderProducesCleanPrompt asserts that a nil store still
// yields a working prompt with no compacted-context system message.
func TestNilStoreReaderProducesCleanPrompt(t *testing.T) {
	history := []entity.Message{
		{Role: entity.RoleUser, Content: "hello", CreatedAt: time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)},
	}
	mock := &mockConvService{history: history}
	pb, _ := NewPromptBuilder(nil, mock, nil, nil, 32000, nil, nil, nil)
	msgs, _, err := pb.Build(context.Background(), nil, nil, nil, entity.Message{}, "conv-nil", port.ChannelCapabilities{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i, m := range msgs {
		if i > 0 && m.Role == entity.RoleSystem {
			t.Error("unexpected system message when store is nil")
		}
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
