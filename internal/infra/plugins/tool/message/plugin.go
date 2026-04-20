// Package message implements a ToolPlugin that sends a message to a chat
// (DM or group) via the outbound transport bus.
package message

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

// Plugin implements port.ToolPlugin for sending messages to chats.
type Plugin struct {
	id        string
	store     port.StorePlugin
	transport port.TransportPlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new message tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("message: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// SetTransport injects the transport dependency.
func (p *Plugin) SetTransport(t port.TransportPlugin) { p.transport = t }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "message" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Sends a message to a chat (DM or group)."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"chat_id":{"type":"string","description":"The target chat ID (DM or group). If omitted, sends to the current conversation."},"content":{"type":"string","description":"Message content to send"}},"required":["content"]}`)
}

// Instructions returns embedded instructions template text for the system prompt.
func (p *Plugin) Instructions() string { return instructionsText }

type messageArgs struct {
	ChatID  string `json:"chat_id"`
	Content string `json:"content"`
}

// Execute sends a message to the specified chat via the outbound bus.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a messageArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("message: parse args: %w", err)
	}

	chatID := entity.ChatID(a.ChatID)
	if chatID == "" {
		chatID = tc.ChatID
	}

	chat, err := p.store.GetChat(ctx, tc.TenantID, chatID)
	if err != nil {
		return "", fmt.Errorf("message: get chat: %w", err)
	}
	if chat == nil {
		return "Chat not found.", nil
	}

	msg := entity.Message{
		TenantID: tc.TenantID,
		ChatID:   chat.ID,
		IsGroup:  chat.IsGroup,
		Kind:     entity.MessageKindMessage,
		Content:  a.Content,
	}

	if err := p.transport.Publish(ctx, entity.TopicOutbound, msg); err != nil {
		return "", err
	}

	return "Message sent.", nil
}
