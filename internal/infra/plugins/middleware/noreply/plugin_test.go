package noreply

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
		wantBody  string
		wantCalls int
	}{
		{
			name: "exact tag drops message",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: "[[no_reply]]",
			},
			wantCalls: 0,
		},
		{
			name: "tag with inner spaces drops message",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: "[[ no_reply ]]",
			},
			wantCalls: 0,
		},
		{
			name: "tag with trailing whitespace drops message",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: "[[no_reply ]] ",
			},
			wantCalls: 0,
		},
		{
			name: "tag with leading whitespace drops message",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: " [[ no_reply]]",
			},
			wantCalls: 0,
		},
		{
			name: "tag at start strips and forwards remaining text",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: "[[no_reply]] some text",
			},
			wantBody:  "some text",
			wantCalls: 1,
		},
		{
			name: "tag at end strips and forwards remaining text",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: "some text [[no_reply]]",
			},
			wantBody:  "some text",
			wantCalls: 1,
		},
		{
			name: "tag with spaces strips and forwards remaining text",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: "[[ no_reply ]] and more",
			},
			wantBody:  "and more",
			wantCalls: 1,
		},
		{
			name: "non-assistant role passes through unchanged",
			msg: entity.Message{
				Role:    entity.RoleUser,
				Kind:    entity.MessageKindMessage,
				Content: "[[no_reply]]",
			},
			wantBody:  "[[no_reply]]",
			wantCalls: 1,
		},
		{
			name: "non-message kind passes through unchanged",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindReaction,
				Content: "[[no_reply]]",
			},
			wantBody:  "[[no_reply]]",
			wantCalls: 1,
		},
		{
			name: "no tag passes through unchanged",
			msg: entity.Message{
				Role:    entity.RoleAssistant,
				Kind:    entity.MessageKindMessage,
				Content: "just a message",
			},
			wantBody:  "just a message",
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
			if tt.wantCalls > 0 && got.Content != tt.wantBody {
				t.Errorf("Content = %q, want %q", got.Content, tt.wantBody)
			}
		})
	}
}
