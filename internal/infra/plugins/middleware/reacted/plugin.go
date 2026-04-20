// Package reacted implements a MiddlewarePlugin that suppresses outbound
// messages containing only the "[[reacted]]" marker. When the LLM uses the
// reaction tool and responds with this marker, the middleware drops the message
// so no text is delivered to the user.
package reacted

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Plugin implements port.MiddlewarePlugin to suppress [[reacted]] markers.
type Plugin struct {
	id string
}

// NewPlugin creates a new reacted middleware plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindMiddleware }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("reacted: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error                             { return nil }
func (p *Plugin) Stop(_ context.Context) error                              { return nil }

// Wrap intercepts outbound assistant messages and suppresses those that contain
// only the [[reacted]] marker, since the reaction was already sent via the
// reaction tool.
func (p *Plugin) Wrap(next port.MessageHandler) port.MessageHandler {
	return func(ctx context.Context, msg entity.Message) error {
		if msg.Role == entity.RoleAssistant &&
			msg.Kind == entity.MessageKindMessage &&
			strings.TrimSpace(msg.Content) == "[[reacted]]" {
			return nil
		}
		return next(ctx, msg)
	}
}
