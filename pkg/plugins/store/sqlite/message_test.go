package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

func boolPtr(v bool) *bool { return &v }

func makeMessage(id entity.MessageID, aid entity.AgentID, convID entity.ConversationID, role entity.Role, content string, tid entity.TenantID, uid entity.UserID, mc entity.MessageContext, createdAt time.Time) entity.Message {
	return entity.Message{
		ID:             id,
		TenantID:       tid,
		UserID:         uid,
		AgentID:        aid,
		ConversationID: convID,
		MessageContext: mc,
		Kind:           entity.MessageKindMessage,
		Role:           role,
		Content:        content,
		CreatedAt:      createdAt,
	}
}

func TestSaveMessageRoundTrip(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{
		ExternalUserID:    "ext-u1",
		ExternalMessageID: "ext-msg-1",
		ExternalReplyToID: "ext-reply-1",
	}
	msg := entity.Message{
		ID:             "msg-1",
		TenantID:       tid,
		UserID:         uid,
		AgentID:        aid,
		ConversationID: "conv-1",
		MessageContext: mc,
		Kind:           entity.MessageKindMessage,
		RepliedToID:    "replied-1",
		TargetID:       "target-1",
		CausedByID:     "caused-1",
		Role:           entity.RoleUser,
		Content:        "Hello world",
		ToolCalls: []entity.ToolCall{
			{ID: "tc1", Name: "search", Arguments: `{"q":"test"}`},
		},
		ToolCallID:  "tc-resp-1",
		ToolName:    "search",
		Attachments: []entity.Attachment{{ID: "att1", Kind: "photo", Filename: "pic.jpg"}},
		CreatedAt:   now,
	}

	if err := p.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	got := msgs[0]
	if got.ID != "msg-1" {
		t.Errorf("ID: got %q, want %q", got.ID, "msg-1")
	}
	if got.TenantID != tid {
		t.Errorf("TenantID: got %q, want %q", got.TenantID, tid)
	}
	if got.UserID != uid {
		t.Errorf("UserID: got %q, want %q", got.UserID, uid)
	}
	if got.AgentID != aid {
		t.Errorf("AgentID: got %q, want %q", got.AgentID, aid)
	}
	if got.ConversationID != "conv-1" {
		t.Errorf("ConversationID: got %q, want %q", got.ConversationID, "conv-1")
	}
	// MessageContext no longer carries ChannelID or ExternalChatID (moved to Chat entity).
	if got.IsGroup {
		t.Errorf("IsGroup: got true, want false")
	}
	if got.MessageContext.ExternalMessageID != "ext-msg-1" {
		t.Errorf("ExternalMessageID: got %q, want %q", got.MessageContext.ExternalMessageID, "ext-msg-1")
	}
	if got.MessageContext.ExternalReplyToID != "ext-reply-1" {
		t.Errorf("ExternalReplyToID: got %q, want %q", got.MessageContext.ExternalReplyToID, "ext-reply-1")
	}
	if got.Kind != entity.MessageKindMessage {
		t.Errorf("Kind: got %q, want %q", got.Kind, entity.MessageKindMessage)
	}
	if got.RepliedToID != "replied-1" {
		t.Errorf("RepliedToID: got %q, want %q", got.RepliedToID, "replied-1")
	}
	if got.TargetID != "target-1" {
		t.Errorf("TargetID: got %q, want %q", got.TargetID, "target-1")
	}
	if got.CausedByID != "caused-1" {
		t.Errorf("CausedByID: got %q, want %q", got.CausedByID, "caused-1")
	}
	if got.Role != entity.RoleUser {
		t.Errorf("Role: got %q, want %q", got.Role, entity.RoleUser)
	}
	if got.Content != "Hello world" {
		t.Errorf("Content: got %q, want %q", got.Content, "Hello world")
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCalls: got %+v", got.ToolCalls)
	}
	if got.ToolCallID != "tc-resp-1" {
		t.Errorf("ToolCallID: got %q, want %q", got.ToolCallID, "tc-resp-1")
	}
	if got.ToolName != "search" {
		t.Errorf("ToolName: got %q, want %q", got.ToolName, "search")
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Kind != "photo" {
		t.Errorf("Attachments: got %+v", got.Attachments)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, now)
	}
}

func TestMessageJSONColumns(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}
	msg := makeMessage("m-json", aid, "conv-json", entity.RoleAssistant, "tool call", tid, uid, mc, now)
	msg.ToolCalls = []entity.ToolCall{
		{ID: "tc1", Name: "weather", Arguments: `{"city":"NYC"}`},
		{ID: "tc2", Name: "time", Arguments: `{}`},
	}
	msg.Attachments = []entity.Attachment{
		{ID: "a1", Kind: "document", Filename: "doc.pdf", MimeType: "application/pdf", Size: 1024},
	}

	if err := p.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{MessageID: "m-json"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	got := msgs[0]
	if len(got.ToolCalls) != 2 {
		t.Fatalf("ToolCalls: expected 2, got %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Name != "weather" || got.ToolCalls[1].Name != "time" {
		t.Errorf("ToolCalls names wrong: %+v", got.ToolCalls)
	}
	if got.ToolCalls[0].Arguments != `{"city":"NYC"}` {
		t.Errorf("ToolCalls[0].Arguments: got %q", got.ToolCalls[0].Arguments)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].MimeType != "application/pdf" {
		t.Errorf("Attachments: got %+v", got.Attachments)
	}
}

func TestMessageNullJSON(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}
	msg := makeMessage("m-null", aid, "conv-null", entity.RoleUser, "plain text", tid, uid, mc, now)
	// ToolCalls and Attachments are nil

	if err := p.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{MessageID: "m-null"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	got := msgs[0]
	if got.ToolCalls != nil {
		t.Errorf("ToolCalls: expected nil, got %+v", got.ToolCalls)
	}
	if got.Attachments != nil {
		t.Errorf("Attachments: expected nil, got %+v", got.Attachments)
	}
}

func TestGetMessagesByConversation(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}

	for i, convID := range []entity.ConversationID{"conv-a", "conv-a", "conv-b"} {
		msg := makeMessage(entity.MessageID("m-conv-"+string(rune('0'+i))), aid, convID, entity.RoleUser, "msg", tid, uid, mc, now.Add(time.Duration(i)*time.Millisecond))
		if err := p.SaveMessage(ctx, msg); err != nil {
			t.Fatalf("SaveMessage[%d]: %v", i, err)
		}
	}

	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-a"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for conv-a, got %d", len(msgs))
	}

	msgs, err = p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-b"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for conv-b, got %d", len(msgs))
	}
}

func TestGetMessagesFTS(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}

	msgs := []entity.Message{
		makeMessage("m-fts-1", aid, "conv-fts", entity.RoleUser, "the quick brown fox", tid, uid, mc, now),
		makeMessage("m-fts-2", aid, "conv-fts", entity.RoleAssistant, "lazy dog sleeps", tid, uid, mc, now.Add(time.Millisecond)),
		makeMessage("m-fts-3", aid, "conv-fts", entity.RoleUser, "fox jumps over", tid, uid, mc, now.Add(2*time.Millisecond)),
	}
	for i, m := range msgs {
		if err := p.SaveMessage(ctx, m); err != nil {
			t.Fatalf("SaveMessage[%d]: %v", i, err)
		}
	}

	results, err := p.GetMessages(ctx, tid, port.MessageFilter{Query: "fox"})
	if err != nil {
		t.Fatalf("GetMessages FTS: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 FTS results for 'fox', got %d", len(results))
	}
	// FTS5 results are ordered by rank (relevance), not chronologically.
	// Verify both expected messages are present.
	ids := map[entity.MessageID]bool{results[0].ID: true, results[1].ID: true}
	if !ids["m-fts-1"] || !ids["m-fts-3"] {
		t.Errorf("FTS results should contain m-fts-1 and m-fts-3, got: %v, %v", results[0].ID, results[1].ID)
	}
}

func TestGetMessagesTimeRange(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	base := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	mc := entity.MessageContext{}

	for i := 0; i < 5; i++ {
		msg := makeMessage(entity.MessageID("m-time-"+string(rune('0'+i))), aid, "conv-time", entity.RoleUser, "msg", tid, uid, mc, base.Add(time.Duration(i)*time.Hour))
		if err := p.SaveMessage(ctx, msg); err != nil {
			t.Fatalf("SaveMessage[%d]: %v", i, err)
		}
	}

	after := base.Add(1 * time.Hour)  // exclude first message
	before := base.Add(3 * time.Hour) // exclude last two
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{After: &after, Before: &before})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	// Messages at +2h only (after > +1h, before < +3h)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in time range, got %d", len(msgs))
	}
	if msgs[0].ID != "m-time-2" {
		t.Errorf("expected m-time-2, got %s", msgs[0].ID)
	}
}

func TestGetMessagesPagination(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}

	for i := 0; i < 5; i++ {
		msg := makeMessage(entity.MessageID("m-page-"+string(rune('0'+i))), aid, "conv-page", entity.RoleUser, "msg", tid, uid, mc, now.Add(time.Duration(i)*time.Millisecond))
		if err := p.SaveMessage(ctx, msg); err != nil {
			t.Fatalf("SaveMessage[%d]: %v", i, err)
		}
	}

	// Page 1: first 2
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-page", Limit: 2})
	if err != nil {
		t.Fatalf("GetMessages page 1: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("page 1: expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].ID != "m-page-0" || msgs[1].ID != "m-page-1" {
		t.Errorf("page 1 IDs: %v, %v", msgs[0].ID, msgs[1].ID)
	}

	// Page 2: next 2
	msgs, err = p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-page", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("GetMessages page 2: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("page 2: expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].ID != "m-page-2" || msgs[1].ID != "m-page-3" {
		t.Errorf("page 2 IDs: %v, %v", msgs[0].ID, msgs[1].ID)
	}
}

func TestGetLastConversationID(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}

	// Insert messages with different conversation IDs, later one should win
	msg1 := makeMessage("m-last-1", aid, "conv-old", entity.RoleUser, "old", tid, uid, mc, now)
	msg2 := makeMessage("m-last-2", aid, "conv-new", entity.RoleUser, "new", tid, uid, mc, now.Add(time.Second))
	if err := p.SaveMessage(ctx, msg1); err != nil {
		t.Fatalf("SaveMessage 1: %v", err)
	}
	if err := p.SaveMessage(ctx, msg2); err != nil {
		t.Fatalf("SaveMessage 2: %v", err)
	}

	lookupMsg := entity.Message{
		TenantID:       tid,
		UserID:         uid,
		MessageContext: mc,
	}
	convID, err := p.GetLastConversationID(ctx, lookupMsg)
	if err != nil {
		t.Fatalf("GetLastConversationID: %v", err)
	}
	if convID != "conv-new" {
		t.Errorf("expected conv-new, got %q", convID)
	}
}

func TestGetLastConversationIDEmpty(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	mc := entity.MessageContext{}
	lookupMsg := entity.Message{
		TenantID:       tid,
		UserID:         uid,
		MessageContext: mc,
	}
	convID, err := p.GetLastConversationID(ctx, lookupMsg)
	if err != nil {
		t.Fatalf("GetLastConversationID: %v", err)
	}
	if convID != "" {
		t.Errorf("expected empty ConversationID, got %q", convID)
	}
}

func TestSaveMessageDedupSilentlySkips(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{
		ExternalMessageID: "ext-msg-dedup-1",
	}

	msg1 := entity.Message{
		ID:             "msg-dedup-1",
		TenantID:       tid,
		UserID:         uid,
		AgentID:        aid,
		ConversationID: "conv-dedup",
		MessageContext: mc,
		Kind:           entity.MessageKindMessage,
		Role:           entity.RoleUser,
		Content:        "first save",
		CreatedAt:      now,
	}

	// First save should succeed.
	if err := p.SaveMessage(ctx, msg1); err != nil {
		t.Fatalf("SaveMessage (first): %v", err)
	}

	// Second save with same channel_id + external_chat_id + external_message_id
	// but different internal ID should be silently skipped.
	msg2 := msg1
	msg2.ID = "msg-dedup-2"
	msg2.Content = "duplicate"
	if err := p.SaveMessage(ctx, msg2); err != nil {
		t.Fatalf("SaveMessage (duplicate) should not error: %v", err)
	}

	// Verify only one row exists.
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-dedup"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after dedup, got %d", len(msgs))
	}
	if msgs[0].ID != "msg-dedup-1" {
		t.Errorf("expected first message to survive dedup, got ID %q", msgs[0].ID)
	}
}

func TestSaveMessageDedupAllowsEmptyExternalID(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{
		ExternalMessageID: "", // empty -- should NOT trigger dedup
	}

	// Two messages with empty ExternalMessageID should both save.
	msg1 := makeMessage("msg-empty-1", aid, "conv-empty", entity.RoleAssistant, "response 1", tid, uid, mc, now)
	msg2 := makeMessage("msg-empty-2", aid, "conv-empty", entity.RoleAssistant, "response 2", tid, uid, mc, now.Add(time.Second))

	if err := p.SaveMessage(ctx, msg1); err != nil {
		t.Fatalf("SaveMessage 1: %v", err)
	}
	if err := p.SaveMessage(ctx, msg2); err != nil {
		t.Fatalf("SaveMessage 2: %v", err)
	}

	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-empty"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages with empty external_message_id, got %d", len(msgs))
	}
}

func TestDedupDoesNotDropAssistantMessages(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)

	// Save user message with ExternalMessageID "ext-123".
	userMsg := entity.Message{
		ID:             "msg-user",
		TenantID:       tid,
		UserID:         uid,
		AgentID:        aid,
		ConversationID: "conv-dedup-assist",
		MessageContext: entity.MessageContext{
			ExternalMessageID: "ext-123",
		},
		Kind:      entity.MessageKindMessage,
		Role:      entity.RoleUser,
		Content:   "user message",
		CreatedAt: now,
	}
	if err := p.SaveMessage(ctx, userMsg); err != nil {
		t.Fatalf("SaveMessage user: %v", err)
	}

	// Save assistant message with empty ExternalMessageID.
	assistMsg := entity.Message{
		ID:             "msg-assist",
		TenantID:       tid,
		UserID:         uid,
		AgentID:        aid,
		ConversationID: "conv-dedup-assist",
		MessageContext: entity.MessageContext{
			// ExternalMessageID intentionally empty
		},
		Kind:      entity.MessageKindMessage,
		Role:      entity.RoleAssistant,
		Content:   "assistant response",
		CreatedAt: now.Add(time.Second),
	}
	if err := p.SaveMessage(ctx, assistMsg); err != nil {
		t.Fatalf("SaveMessage assistant: %v", err)
	}

	// Both messages must exist.
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-dedup-assist"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d", len(msgs))
	}
	if msgs[0].Role != entity.RoleUser {
		t.Errorf("first message should be user, got %q", msgs[0].Role)
	}
	if msgs[1].Role != entity.RoleAssistant {
		t.Errorf("second message should be assistant, got %q", msgs[1].Role)
	}
}

func TestUpdateExternalMessageID(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}
	msg := makeMessage("msg-update-ext", aid, "conv-update", entity.RoleAssistant, "bot reply", tid, uid, mc, now)

	if err := p.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	// Update external message ID.
	if err := p.UpdateExternalMessageID(ctx, "msg-update-ext", "ext-platform-123"); err != nil {
		t.Fatalf("UpdateExternalMessageID: %v", err)
	}

	// Verify update.
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{MessageID: "msg-update-ext"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].MessageContext.ExternalMessageID != "ext-platform-123" {
		t.Errorf("ExternalMessageID: got %q, want %q", msgs[0].MessageContext.ExternalMessageID, "ext-platform-123")
	}
}

func TestBuildMessageQueryRolesFilter(t *testing.T) {
	tests := []struct {
		name     string
		roles    []entity.Role
		wantSQL  string // substring that must appear
		wantArgs int    // expected number of role args added
		noClause bool   // expect no role clause
	}{
		{
			name:     "two roles",
			roles:    []entity.Role{entity.RoleUser, entity.RoleAssistant},
			wantSQL:  `role IN (?,?)`,
			wantArgs: 2,
		},
		{
			name:     "single role",
			roles:    []entity.Role{entity.RoleUser},
			wantSQL:  `role IN (?)`,
			wantArgs: 1,
		},
		{
			name:     "empty roles",
			roles:    []entity.Role{},
			noClause: true,
		},
		{
			name:     "nil roles",
			roles:    nil,
			noClause: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := port.MessageFilter{
				ConversationID: "conv-1",
				Roles:          tt.roles,
			}
			q, args := buildMessageQuery("t1", filter)
			if tt.noClause {
				if strings.Contains(q, "role IN") {
					t.Errorf("expected no role clause, got SQL: %s", q)
				}
				return
			}
			if !strings.Contains(q, tt.wantSQL) {
				t.Errorf("expected SQL to contain %q, got: %s", tt.wantSQL, q)
			}
			// Count role args: they come after tenant_id and conversation_id args
			roleArgs := 0
			for _, a := range args {
				if s, ok := a.(string); ok {
					if s == string(entity.RoleUser) || s == string(entity.RoleAssistant) {
						roleArgs++
					}
				}
			}
			if roleArgs != tt.wantArgs {
				t.Errorf("expected %d role args, got %d (all args: %v)", tt.wantArgs, roleArgs, args)
			}
		})
	}
}

func TestBuildMessageQueryFTSRankOrdering(t *testing.T) {
	// FTS query should use ORDER BY rank, not ORDER BY created_at
	filter := port.MessageFilter{
		Query: "fox",
	}
	q, _ := buildMessageQuery("t1", filter)
	if !strings.Contains(q, "ORDER BY rank") {
		t.Errorf("FTS query should ORDER BY rank, got: %s", q)
	}
	if strings.Contains(q, "ORDER BY m.created_at ASC") {
		t.Errorf("FTS query should not ORDER BY created_at ASC, got: %s", q)
	}

	// Non-FTS, non-Tail should use ORDER BY created_at ASC, id ASC
	filterNoFTS := port.MessageFilter{
		ConversationID: "conv-1",
	}
	q2, _ := buildMessageQuery("t1", filterNoFTS)
	if !strings.Contains(q2, "ORDER BY created_at ASC, id ASC") {
		t.Errorf("Non-FTS query should ORDER BY created_at ASC, id ASC, got: %s", q2)
	}

	// Tail mode should still use existing wrapping
	filterTail := port.MessageFilter{
		ConversationID: "conv-1",
		Tail:           true,
		Limit:          5,
	}
	q3, _ := buildMessageQuery("t1", filterTail)
	if !strings.Contains(q3, "ORDER BY created_at ASC, id ASC") {
		t.Errorf("Tail query should wrap with ORDER BY created_at ASC, id ASC, got: %s", q3)
	}
	if !strings.Contains(q3, "DESC LIMIT") {
		t.Errorf("Tail query should have DESC LIMIT in subquery, got: %s", q3)
	}
}

func TestGetMessagesRolesFilterIntegration(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}

	// Insert messages with different roles
	roles := []entity.Role{entity.RoleUser, entity.RoleAssistant, entity.RoleTool, entity.RoleSystem}
	for i, role := range roles {
		msg := makeMessage(
			entity.MessageID("m-role-"+string(rune('0'+i))),
			aid, "conv-roles", role, "content-"+string(role),
			tid, uid, mc, now.Add(time.Duration(i)*time.Millisecond),
		)
		if err := p.SaveMessage(ctx, msg); err != nil {
			t.Fatalf("SaveMessage[%d]: %v", i, err)
		}
	}

	// Filter for user+assistant only
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{
		ConversationID: "conv-roles",
		Roles:          []entity.Role{entity.RoleUser, entity.RoleAssistant},
	})
	if err != nil {
		t.Fatalf("GetMessages with Roles: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user+assistant), got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.Role != entity.RoleUser && m.Role != entity.RoleAssistant {
			t.Errorf("unexpected role %q in filtered results", m.Role)
		}
	}

	// Filter for single role
	msgs, err = p.GetMessages(ctx, tid, port.MessageFilter{
		ConversationID: "conv-roles",
		Roles:          []entity.Role{entity.RoleTool},
	})
	if err != nil {
		t.Fatalf("GetMessages with single Role: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 tool message, got %d", len(msgs))
	}
	if msgs[0].Role != entity.RoleTool {
		t.Errorf("expected tool role, got %q", msgs[0].Role)
	}

	// No Roles filter returns all
	msgs, err = p.GetMessages(ctx, tid, port.MessageFilter{
		ConversationID: "conv-roles",
	})
	if err != nil {
		t.Fatalf("GetMessages without Roles: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (all roles), got %d", len(msgs))
	}
}

func TestEvictMessages_Batch(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}

	for i := 0; i < 3; i++ {
		msg := makeMessage(entity.MessageID("m-evict-"+string(rune('0'+i))), aid, "conv-evict", entity.RoleUser, "msg", tid, uid, mc, now.Add(time.Duration(i)*time.Millisecond))
		if err := p.SaveMessage(ctx, msg); err != nil {
			t.Fatalf("SaveMessage[%d]: %v", i, err)
		}
	}

	// Evict 2 of 3
	if err := p.EvictMessages(ctx, tid, "conv-evict", []entity.MessageID{"m-evict-0", "m-evict-1"}); err != nil {
		t.Fatalf("EvictMessages: %v", err)
	}

	// Evicted=false returns 1
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-evict", Evicted: boolPtr(false)})
	if err != nil {
		t.Fatalf("GetMessages Evicted=false: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("Evicted=false: expected 1, got %d", len(msgs))
	}
	if msgs[0].ID != "m-evict-2" {
		t.Errorf("Evicted=false: expected m-evict-2, got %s", msgs[0].ID)
	}

	// Evicted=true returns 2
	msgs, err = p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-evict", Evicted: boolPtr(true)})
	if err != nil {
		t.Fatalf("GetMessages Evicted=true: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("Evicted=true: expected 2, got %d", len(msgs))
	}
	for _, m := range msgs {
		if !m.Evicted {
			t.Errorf("expected Evicted=true for %s", m.ID)
		}
	}

	// Evicted=nil (zero value) returns all 3
	msgs, err = p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-evict"})
	if err != nil {
		t.Fatalf("GetMessages Evicted=nil: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("Evicted=nil: expected 3, got %d", len(msgs))
	}
}

func TestEvictMessages_Idempotent(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}
	msg := makeMessage("m-idemp-0", aid, "conv-idemp", entity.RoleUser, "msg", tid, uid, mc, now)
	if err := p.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	// Evict twice
	for i := 0; i < 2; i++ {
		if err := p.EvictMessages(ctx, tid, "conv-idemp", []entity.MessageID{"m-idemp-0"}); err != nil {
			t.Fatalf("EvictMessages call %d: %v", i+1, err)
		}
	}

	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-idemp", Evicted: boolPtr(true)})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 evicted message, got %d", len(msgs))
	}
}

func TestEvictMessages_EmptySlice(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")

	if err := p.EvictMessages(ctx, tid, "conv-any", []entity.MessageID{}); err != nil {
		t.Fatalf("EvictMessages with empty slice: %v", err)
	}
}

func TestEvictMessages_CrossConversation(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}

	msgA := makeMessage("m-cross-a", aid, "conv-a", entity.RoleUser, "msg-a", tid, uid, mc, now)
	msgB := makeMessage("m-cross-b", aid, "conv-b", entity.RoleUser, "msg-b", tid, uid, mc, now)
	if err := p.SaveMessage(ctx, msgA); err != nil {
		t.Fatalf("SaveMessage A: %v", err)
	}
	if err := p.SaveMessage(ctx, msgB); err != nil {
		t.Fatalf("SaveMessage B: %v", err)
	}

	// Try to evict msg-a using conv-b scope -- should not affect msg-a
	if err := p.EvictMessages(ctx, tid, "conv-b", []entity.MessageID{"m-cross-a"}); err != nil {
		t.Fatalf("EvictMessages: %v", err)
	}

	// msg-a should still be non-evicted
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{MessageID: "m-cross-a"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Evicted {
		t.Error("msg-a should not be evicted when eviction was scoped to conv-b")
	}
}

func TestEvictMessages_NonExistentID(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}
	msg := makeMessage("m-exist", aid, "conv-ne", entity.RoleUser, "msg", tid, uid, mc, now)
	if err := p.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	if err := p.EvictMessages(ctx, tid, "conv-ne", []entity.MessageID{"does-not-exist"}); err != nil {
		t.Fatalf("EvictMessages non-existent ID: %v", err)
	}
}

func TestBuildMessageQueryEvictedFilter(t *testing.T) {
	tests := []struct {
		name    string
		evicted *bool
		query   string // if non-empty, triggers FTS path
		want    string // substring that must appear
		notWant string // substring that must NOT appear
	}{
		{
			name:    "evicted=true non-FTS",
			evicted: boolPtr(true),
			want:    "evicted = 1",
		},
		{
			name:    "evicted=false non-FTS",
			evicted: boolPtr(false),
			want:    "evicted = 0",
		},
		{
			name:    "evicted=nil non-FTS",
			evicted: nil,
			notWant: "evicted = ",
		},
		{
			name:    "evicted=true FTS",
			evicted: boolPtr(true),
			query:   "test",
			want:    "m.evicted = 1",
		},
		{
			name:    "evicted=false FTS",
			evicted: boolPtr(false),
			query:   "test",
			want:    "m.evicted = 0",
		},
		{
			name:    "evicted=nil FTS",
			evicted: nil,
			query:   "test",
			notWant: "evicted = ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := port.MessageFilter{
				ConversationID: "conv-1",
				Evicted:        tt.evicted,
				Query:          tt.query,
			}
			q, _ := buildMessageQuery("t1", filter)
			if tt.want != "" && !strings.Contains(q, tt.want) {
				t.Errorf("expected SQL to contain %q, got: %s", tt.want, q)
			}
			if tt.notWant != "" && strings.Contains(q, tt.notWant) {
				t.Errorf("expected SQL to NOT contain %q, got: %s", tt.notWant, q)
			}
		})
	}
}

func TestGetMessages_EvictedFilterNil(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}

	// Save 2 messages, evict 1
	msg1 := makeMessage("m-nil-0", aid, "conv-nil", entity.RoleUser, "msg0", tid, uid, mc, now)
	msg2 := makeMessage("m-nil-1", aid, "conv-nil", entity.RoleUser, "msg1", tid, uid, mc, now.Add(time.Millisecond))
	if err := p.SaveMessage(ctx, msg1); err != nil {
		t.Fatalf("SaveMessage 0: %v", err)
	}
	if err := p.SaveMessage(ctx, msg2); err != nil {
		t.Fatalf("SaveMessage 1: %v", err)
	}
	if err := p.EvictMessages(ctx, tid, "conv-nil", []entity.MessageID{"m-nil-0"}); err != nil {
		t.Fatalf("EvictMessages: %v", err)
	}

	// Evicted=nil (zero value filter) returns all
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-nil"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("Evicted=nil: expected 2, got %d", len(msgs))
	}
}

func TestGetMessagesFTSEvicted(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}

	// Create 3 messages with "fox" keyword in different content
	msgs := []entity.Message{
		makeMessage("m-fts-ev-1", aid, "conv-fts-evict", entity.RoleUser, "the quick fox", tid, uid, mc, now),
		makeMessage("m-fts-ev-2", aid, "conv-fts-evict", entity.RoleAssistant, "fox jumps", tid, uid, mc, now.Add(time.Millisecond)),
		makeMessage("m-fts-ev-3", aid, "conv-fts-evict", entity.RoleUser, "lazy fox rests", tid, uid, mc, now.Add(2*time.Millisecond)),
	}
	for i, m := range msgs {
		if err := p.SaveMessage(ctx, m); err != nil {
			t.Fatalf("SaveMessage[%d]: %v", i, err)
		}
	}

	// Evict the third message
	if err := p.EvictMessages(ctx, tid, "conv-fts-evict", []entity.MessageID{"m-fts-ev-3"}); err != nil {
		t.Fatalf("EvictMessages: %v", err)
	}

	// FTS query for "fox" with Evicted=false should return only 2 non-evicted matches
	results, err := p.GetMessages(ctx, tid, port.MessageFilter{Query: "fox", Evicted: boolPtr(false)})
	if err != nil {
		t.Fatalf("GetMessages FTS Evicted=false: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 FTS results with Evicted=false, got %d", len(results))
	}

	// Verify the evicted message is not in results
	for _, r := range results {
		if r.ID == "m-fts-ev-3" {
			t.Error("evicted message m-fts-ev-3 should not appear in FTS results with Evicted=false")
		}
	}
}

func TestGetMessagesEvictedFilterNonFTS(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	mc := entity.MessageContext{}

	// Create 3 messages
	for i := 0; i < 3; i++ {
		msg := makeMessage(entity.MessageID("m-evfilt-"+string(rune('0'+i))), aid, "conv-evict-nonfts", entity.RoleUser, "msg", tid, uid, mc, now.Add(time.Duration(i)*time.Millisecond))
		if err := p.SaveMessage(ctx, msg); err != nil {
			t.Fatalf("SaveMessage[%d]: %v", i, err)
		}
	}

	// Evict 1 message
	if err := p.EvictMessages(ctx, tid, "conv-evict-nonfts", []entity.MessageID{"m-evfilt-1"}); err != nil {
		t.Fatalf("EvictMessages: %v", err)
	}

	// Evicted=false should return 2
	results, err := p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-evict-nonfts", Evicted: boolPtr(false)})
	if err != nil {
		t.Fatalf("GetMessages Evicted=false: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Evicted=false: expected 2, got %d", len(results))
	}

	// Evicted=nil should return all 3 (backward compatible)
	results, err = p.GetMessages(ctx, tid, port.MessageFilter{ConversationID: "conv-evict-nonfts"})
	if err != nil {
		t.Fatalf("GetMessages Evicted=nil: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Evicted=nil: expected 3, got %d", len(results))
	}
}

func TestGetMessages_Tail(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	base := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	mc := entity.MessageContext{}

	// Insert 5 messages with sequential times.
	for i := 0; i < 5; i++ {
		msg := makeMessage(
			entity.MessageID("m-tail-"+string(rune('0'+i))),
			aid, "conv-tail", entity.RoleUser, "msg",
			tid, uid, mc, base.Add(time.Duration(i)*time.Second),
		)
		if err := p.SaveMessage(ctx, msg); err != nil {
			t.Fatalf("SaveMessage[%d]: %v", i, err)
		}
	}

	// Tail=true, Limit=3: last 3 messages in ASC order.
	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{
		ConversationID: "conv-tail", Limit: 3, Tail: true,
	})
	if err != nil {
		t.Fatalf("Tail=true Limit=3: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("Tail=true Limit=3: expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].ID != "m-tail-2" || msgs[1].ID != "m-tail-3" || msgs[2].ID != "m-tail-4" {
		t.Errorf("Tail=true Limit=3: got IDs %s, %s, %s", msgs[0].ID, msgs[1].ID, msgs[2].ID)
	}

	// Tail=true, Limit=0: returns all 5 messages.
	msgs, err = p.GetMessages(ctx, tid, port.MessageFilter{
		ConversationID: "conv-tail", Tail: true,
	})
	if err != nil {
		t.Fatalf("Tail=true Limit=0: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("Tail=true Limit=0: expected 5 messages, got %d", len(msgs))
	}

	// Tail=false, Limit=3: first 3 messages (existing behavior).
	msgs, err = p.GetMessages(ctx, tid, port.MessageFilter{
		ConversationID: "conv-tail", Limit: 3,
	})
	if err != nil {
		t.Fatalf("Tail=false Limit=3: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("Tail=false Limit=3: expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].ID != "m-tail-0" || msgs[1].ID != "m-tail-1" || msgs[2].ID != "m-tail-2" {
		t.Errorf("Tail=false Limit=3: got IDs %s, %s, %s", msgs[0].ID, msgs[1].ID, msgs[2].ID)
	}
}
