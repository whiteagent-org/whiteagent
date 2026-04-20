package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// TestIsMentionRoundTripTrue verifies that IsMention=true is persisted and
// retrieved correctly via SaveMessage/GetMessages (MSG-01).
func TestIsMentionRoundTripTrue(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	msg := entity.Message{
		ID:             "is-mention-true-msg",
		TenantID:       tid,
		UserID:         uid,
		AgentID:        aid,
		ConversationID: "conv-mention-true",
		ChatID:  entity.ChatID("chat-mention-test"),
		IsGroup: true,
		Kind:      entity.MessageKindMessage,
		Role:      entity.RoleUser,
		Content:   "hey @bot what's up",
		IsMention: true,
		CreatedAt: now,
	}

	if err := p.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{MessageID: "is-mention-true-msg"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !msgs[0].IsMention {
		t.Errorf("IsMention: got false, want true")
	}
}

// TestIsMentionRoundTripFalse verifies that IsMention=false is persisted and
// retrieved correctly via SaveMessage/GetMessages (MSG-01).
func TestIsMentionRoundTripFalse(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")
	aid := entity.AgentID("a1")
	seedTestData(t, p, tid, uid, aid)

	now := time.Now().UTC().Truncate(time.Second)
	msg := entity.Message{
		ID:             "is-mention-false-msg",
		TenantID:       tid,
		UserID:         uid,
		AgentID:        aid,
		ConversationID: "conv-mention-false",
		ChatID:  entity.ChatID("chat-mention-test"),
		IsGroup: true,
		Kind:      entity.MessageKindMessage,
		Role:      entity.RoleUser,
		Content:   "just chatting",
		IsMention: false,
		CreatedAt: now,
	}

	if err := p.SaveMessage(ctx, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	msgs, err := p.GetMessages(ctx, tid, port.MessageFilter{MessageID: "is-mention-false-msg"})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].IsMention {
		t.Errorf("IsMention: got true, want false")
	}
}
