// Package command implements a middleware plugin that intercepts slash commands
// before the agent loop. It is loaded as a standard .so plugin via config.
package command

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
)

const pluginID = "middleware.command"

// Plugin implements port.MiddlewarePlugin, port.ConversationAware,
// port.TransportAware, and port.StoreAware.
type Plugin struct {
	id           string
	log          *slog.Logger
	convResetter port.ConversationResetter
	transport    port.TransportPlugin
	store        port.StorePlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindMiddleware}
}

// NewPlugin creates a new command middleware plugin instance.
func NewPlugin() port.Plugin { return &Plugin{id: pluginID} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind     { return entity.PluginKindMiddleware }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

// Init creates the structured logger for the command middleware.
func (p *Plugin) Init(ctx context.Context, id string, _ json.RawMessage) error {
	if id != "" {
		p.id = id
	}
	p.log = logger.FromCtx(ctx).With("component", "command", "id", p.id)
	return nil
}

// Start validates that required dependencies have been injected.
// Store is NOT validated here -- graceful degradation per design decision.
func (p *Plugin) Start(_ context.Context) error {
	if p.convResetter == nil {
		return fmt.Errorf("%s: ConversationResetter not injected (runtime wiring bug)", pluginID)
	}
	if p.transport == nil {
		return fmt.Errorf("%s: TransportPlugin not injected (runtime wiring bug)", pluginID)
	}
	return nil
}

// Stop is a no-op.
func (p *Plugin) Stop(_ context.Context) error { return nil }

// SetConversationResetter injects the ConversationResetter dependency.
func (p *Plugin) SetConversationResetter(cr port.ConversationResetter) { p.convResetter = cr }

// SetTransport injects the TransportPlugin dependency.
func (p *Plugin) SetTransport(t port.TransportPlugin) { p.transport = t }

// SetStore injects the StorePlugin dependency (implements port.StoreAware).
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// publishReply builds an assistant response and publishes it to TopicOutbound.
func (p *Plugin) publishReply(ctx context.Context, msg entity.Message, content string) error {
	reply := entity.Message{
		TenantID:       msg.TenantID,
		ChatID:         msg.ChatID,
		MessageContext: msg.MessageContext,
		Role:           entity.RoleAssistant,
		Content:        content,
		Kind:           entity.MessageKindMessage,
	}
	return p.transport.Publish(ctx, entity.TopicOutbound, reply)
}

// Wrap returns a MessageHandler that intercepts slash commands before passing
// non-command messages to the next handler. Unknown commands pass through.
func (p *Plugin) Wrap(next port.MessageHandler) port.MessageHandler {
	return func(ctx context.Context, msg entity.Message) error {
		// Only intercept user messages starting with "/".
		if msg.Role != entity.RoleUser || !strings.HasPrefix(msg.Content, "/") {
			return next(ctx, msg)
		}

		// Parse command name from first token.
		fields := strings.Fields(msg.Content)
		if len(fields) == 0 {
			return next(ctx, msg)
		}
		cmd := strings.TrimPrefix(fields[0], "/")

		switch cmd {
		case "new":
			convID, err := p.convResetter.ResolveConversation(ctx, msg)
			if err != nil {
				return fmt.Errorf("%s: resolve conversation: %w", pluginID, err)
			}
			if err := p.convResetter.ResetConversation(ctx, convID); err != nil {
				return fmt.Errorf("%s: reset conversation: %w", pluginID, err)
			}
			return p.publishReply(ctx, msg, "Session reset. Starting fresh.")

		case "logs":
			if msg.IsGroup || msg.UserID.IsEmpty() {
				return p.publishReply(ctx, msg, "Error log is only available in direct messages.")
			}
			if p.store == nil {
				return p.publishReply(ctx, msg, "Error log not available.")
			}
			limit := parseLogsLimit(fields)
			entries, err := p.store.GetErrorLog(ctx, msg.TenantID, entity.ErrorLogFilter{
				UserID: msg.UserID,
				Limit:  limit,
			})
			if err != nil {
				p.log.Error("middleware.command.logs", "err", err)
				return p.publishReply(ctx, msg, "Failed to retrieve error log.")
			}
			if len(entries) == 0 {
				return p.publishReply(ctx, msg, "No recent errors.")
			}
			return p.publishReply(ctx, msg, formatErrorLog(entries))

		default:
			// Unknown command -- pass through to the agent loop.
			return next(ctx, msg)
		}
	}
}

// parseLogsLimit extracts the optional limit argument from /logs N.
// Defaults to 10, caps at 100, falls back to 10 on invalid input.
func parseLogsLimit(fields []string) int {
	const defaultLimit = 10
	const maxLimit = 100
	if len(fields) < 2 {
		return defaultLimit
	}
	n, err := strconv.Atoi(fields[1])
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

// formatErrorLog formats error log entries as one line per entry.
// Format: [Jan 02 15:04] message (reftype: refid)
func formatErrorLog(entries []entity.ErrorLogEntry) string {
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[%s] %s", e.CreatedAt.UTC().Format("Jan 02 15:04"), e.Content)
		if e.RefType != "" {
			fmt.Fprintf(&b, " (%s: %s)", e.RefType, e.RefID)
		}
	}
	return b.String()
}
