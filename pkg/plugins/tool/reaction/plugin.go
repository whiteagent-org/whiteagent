// Package reaction implements a ToolPlugin that adds an emoji reaction to the
// user's last message via the outbound transport bus.
package reaction

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

//go:embed instructions.tmpl
var instructionsText string

// Plugin implements port.ToolPlugin for adding emoji reactions.
type Plugin struct {
	id        string
	transport port.TransportPlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new reaction tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("reaction: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetTransport injects the transport dependency.
func (p *Plugin) SetTransport(t port.TransportPlugin) { p.transport = t }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "reaction" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Adds an emoji reaction to the user's last message."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"emoji":{"type":"string","description":"One of the allowed reaction emoji (e.g. 👍, ❤, 🔥)"},"target_msg_id":{"type":"string","description":"msg_id of the message to react to. When the user's context block has a reply_to attribute, use that value. Omit to react to the user's own message."}},"required":["emoji"]}`)
}

// Instructions returns embedded instructions template text for the system prompt.
func (p *Plugin) Instructions() string { return instructionsText }

type reactionArgs struct {
	Emoji       string `json:"emoji"`
	TargetMsgID string `json:"target_msg_id,omitempty"`
}

// Execute adds an emoji reaction to the user's message via the outbound bus.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a reactionArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("reaction: parse args: %w", err)
	}

	targetID := tc.MessageID
	if a.TargetMsgID != "" {
		targetID = entity.MessageID(a.TargetMsgID)
	}

	msg := entity.Message{
		TenantID: tc.TenantID,
		UserID:   tc.UserID,
		ChatID:   tc.ChatID,
		Kind:     entity.MessageKindReaction,
		TargetID: targetID,
		Content:  a.Emoji,
	}

	if err := p.transport.Publish(ctx, entity.TopicOutbound, msg); err != nil {
		return "", err
	}

	return "Reaction added.", nil
}
