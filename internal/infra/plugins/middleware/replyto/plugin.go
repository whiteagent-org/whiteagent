// Package replyto implements a MiddlewarePlugin that parses [[reply_to_current]]
// and [[reply_to:<id>]] tags from outbound assistant messages, sets the
// appropriate TargetID for reply threading, and strips the tag from content.
package replyto

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

var replyTagRe = regexp.MustCompile(`\[\[\s*reply_to(?:_current|:\s*(.+?))\s*\]\]`)

// Plugin implements port.MiddlewarePlugin to extract reply-to tags.
type Plugin struct {
	id string
}

// NewPlugin creates a new replyto middleware plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindMiddleware }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("replyto: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error                             { return nil }
func (p *Plugin) Stop(_ context.Context) error                              { return nil }

// Wrap intercepts outbound assistant messages, extracts reply-to tags,
// sets TargetID accordingly, and strips the tag from content.
func (p *Plugin) Wrap(next port.MessageHandler) port.MessageHandler {
	return func(ctx context.Context, msg entity.Message) error {
		if msg.Role != entity.RoleAssistant || msg.Kind != entity.MessageKindMessage {
			return next(ctx, msg)
		}

		match := replyTagRe.FindStringSubmatchIndex(msg.Content)
		if match == nil {
			return next(ctx, msg)
		}

		// Use the first match to determine TargetID.
		// match[2] and match[3] are the submatch for the ID capture group.
		// If they are -1, the tag was [[reply_to_current]].
		if match[2] < 0 {
			msg.TargetID = msg.CausedByID
		} else {
			id := strings.TrimSpace(msg.Content[match[2]:match[3]])
			msg.TargetID = entity.MessageID(id)
		}

		// Strip all reply-to tags from content.
		msg.Content = strings.TrimSpace(replyTagRe.ReplaceAllString(msg.Content, ""))
		return next(ctx, msg)
	}
}
