// Package history_get implements a ToolPlugin that retrieves a single message
// by ID with full content and metadata.
package history_get

import (
	"context"

	"encoding/json"
	"fmt"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Plugin implements port.ToolPlugin for retrieving a single message by ID.
type Plugin struct {
	id    string
	store port.StorePlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new history_get tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("history_get: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "history_get" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Retrieves a single message by ID with full content and metadata."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"message_id":{"type":"string","description":"The message ID to retrieve"}},"required":["message_id"]}`)
}

func (p *Plugin) Instructions() string { return "" }

type getArgs struct {
	MessageID string `json:"message_id"`
}

// Execute retrieves a single message by ID with full content and metadata.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a getArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("history_get: parse args: %w", err)
	}

	if a.MessageID == "" {
		return "", fmt.Errorf("history_get: message_id is required")
	}

	filter := port.MessageFilter{
		MessageID: entity.MessageID(a.MessageID),
		ChatID:    tc.ChatID,
		Limit:     1,
	}

	// DM: scope to user; Group: all participants visible
	if !tc.IsGroup {
		filter.UserID = tc.UserID
	}

	messages, err := p.store.GetMessages(ctx, tc.TenantID, filter)
	if err != nil {
		return "", err
	}

	if len(messages) == 0 {
		return "Message not found.", nil
	}

	m := messages[0]
	return fmt.Sprintf("ID: %s\nConversation: %s\nTime: %s\nRole: %s\n\n%s",
		string(m.ID),
		string(m.ConversationID),
		m.CreatedAt.UTC().Format(time.RFC3339),
		string(m.Role),
		m.Content,
	), nil
}
