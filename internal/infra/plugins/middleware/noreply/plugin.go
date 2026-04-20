// Package noreply implements a MiddlewarePlugin that suppresses outbound
// messages containing only the "[[no_reply]]" marker. When the LLM determines
// that no reply is needed, it responds with this marker, and the middleware
// drops the message so no text is delivered to the user.
package noreply

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

var noReplyRe = regexp.MustCompile(`\[\[\s*no_reply\s*\]\]`)

// Plugin implements port.MiddlewarePlugin to suppress [[no_reply]] markers.
type Plugin struct {
	id string
}

// NewPlugin creates a new noreply middleware plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindMiddleware }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("noreply: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// Wrap intercepts outbound assistant messages containing the [[no_reply]] tag.
// If the tag is the entire content, the message is dropped. If other text is
// present alongside the tag, the tag is stripped and the remaining text forwarded.
func (p *Plugin) Wrap(next port.MessageHandler) port.MessageHandler {
	return func(ctx context.Context, msg entity.Message) error {
		if msg.Role != entity.RoleAssistant || msg.Kind != entity.MessageKindMessage {
			return next(ctx, msg)
		}
		if !noReplyRe.MatchString(msg.Content) {
			return next(ctx, msg)
		}
		cleaned := strings.TrimSpace(noReplyRe.ReplaceAllString(msg.Content, ""))
		if cleaned == "" {
			return nil
		}
		msg.Content = cleaned
		return next(ctx, msg)
	}
}
