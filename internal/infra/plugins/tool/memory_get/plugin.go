// Package memory_get implements a ToolPlugin that returns persistent memory
// for the current context (user memory in DMs, group memory in groups).
package memory_get

import (
	"context"

	"encoding/json"
	"fmt"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Plugin implements port.ToolPlugin for reading persistent memory.
type Plugin struct {
	id    string
	store port.StorePlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new memory_get tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("memory_get: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }
func (p *Plugin) IsEphemeral() bool             { return false }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "memory_get" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Returns persistent memory notes for the current context."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (p *Plugin) Instructions() string { return "" }

// Execute returns the memory for the current context (user memory in DMs, group memory in groups).
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, _ json.RawMessage) (string, error) {
	var ownerType, ownerID string
	if tc.IsGroup {
		ownerType = "chat"
		ownerID = tc.ChatID.String()
	} else {
		ownerType = "user"
		ownerID = tc.UserID.String()
	}

	mem, err := p.store.GetMemory(ctx, tc.TenantID, ownerType, ownerID)
	if err != nil {
		return "", err
	}
	if mem == nil || mem.Content == "" {
		return "No memory stored.", nil
	}

	return mem.Content, nil
}
