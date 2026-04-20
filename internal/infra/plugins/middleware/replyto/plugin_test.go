package replyto

import (
	"context"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

func TestWrap(t *testing.T) {
	p := &Plugin{}

	tests := []struct {
		name      string
		msg       entity.Message
		wantID    entity.MessageID
		wantBody  string
		wantCalls int
	}{
		{
			name: "reply_to_current sets TargetID from CausedByID",
			msg: entity.Message{
				Role:       entity.RoleAssistant,
				Kind:       entity.MessageKindMessage,
				CausedByID: "user-msg-1",
				Content:    "[[reply_to_current]] hello",
			},
			wantID:    "user-msg-1",
			wantBody:  "hello",
			wantCalls: 1,
		},
		{
			name: "reply_to_current with whitespace",
			msg: entity.Message{
				Role:       entity.RoleAssistant,
				Kind:       entity.MessageKindMessage,
				CausedByID: "user-msg-2",
				Content:    "[[ reply_to_current ]] hi",
			},
			wantID:    "user-msg-2",
			wantBody:  "hi",
			wantCalls: 1,
		},
		{
			name: "reply_to with explicit ID",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: "[[reply_to:abc123]] answer",
			},
			wantID:    "abc123",
			wantBody:  "answer",
			wantCalls: 1,
		},
		{
			name: "reply_to with whitespace around ID",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: "[[ reply_to: abc123 ]] answer",
			},
			wantID:    "abc123",
			wantBody:  "answer",
			wantCalls: 1,
		},
		{
			name: "tag at end of content",
			msg: entity.Message{
				Role:       entity.RoleAssistant,
				Kind:       entity.MessageKindMessage,
				CausedByID: "user-msg-3",
				Content:    "hello [[reply_to_current]]",
			},
			wantID:    "user-msg-3",
			wantBody:  "hello",
			wantCalls: 1,
		},
		{
			name: "tag in middle of content",
			msg: entity.Message{
				Role:       entity.RoleAssistant,
				Kind:       entity.MessageKindMessage,
				CausedByID: "user-msg-4",
				Content:    "hello [[reply_to_current]] world",
			},
			wantID:    "user-msg-4",
			wantBody:  "hello  world",
			wantCalls: 1,
		},
		{
			name: "multiple reply_to_current tags are all stripped",
			msg: entity.Message{
				Role:       entity.RoleAssistant,
				Kind:       entity.MessageKindMessage,
				CausedByID: "user-msg-5",
				Content:    "[[reply_to_current]] hello [[reply_to_current]] world [[reply_to_current]]",
			},
			wantID:    "user-msg-5",
			wantBody:  "hello  world",
			wantCalls: 1,
		},
		{
			name: "mixed reply_to tags are all stripped and first sets TargetID",
			msg: entity.Message{
				Role:       entity.RoleAssistant,
				Kind:       entity.MessageKindMessage,
				CausedByID: "user-msg-6",
				Content:    "[[reply_to_current]] hello [[reply_to:other-id]] world",
			},
			wantID:    "user-msg-6",
			wantBody:  "hello  world",
			wantCalls: 1,
		},
		{
			name: "no tag passes through unchanged",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: "just a message",
			},
			wantID:    "",
			wantBody:  "just a message",
			wantCalls: 1,
		},
		{
			name: "non-assistant role passes through",
			msg: entity.Message{
				Role:    entity.RoleUser,
				Kind:    entity.MessageKindMessage,
				Content: "[[reply_to_current]] should not be parsed",
			},
			wantID:    "",
			wantBody:  "[[reply_to_current]] should not be parsed",
			wantCalls: 1,
		},
		{
			name: "reaction kind passes through",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindReaction,
				Content: "[[reply_to_current]]",
			},
			wantID:    "",
			wantBody:  "[[reply_to_current]]",
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called int
			var got entity.Message
			handler := p.Wrap(func(_ context.Context, m entity.Message) error {
				called++
				got = m
				return nil
			})

			if err := handler(context.Background(), tt.msg); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if called != tt.wantCalls {
				t.Fatalf("next called %d times, want %d", called, tt.wantCalls)
			}
			if got.TargetID != tt.wantID {
				t.Errorf("TargetID = %q, want %q", got.TargetID, tt.wantID)
			}
			if got.Content != tt.wantBody {
				t.Errorf("Content = %q, want %q", got.Content, tt.wantBody)
			}
		})
	}
}
