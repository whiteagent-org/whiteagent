// Package memory_update implements a ToolPlugin that replaces persistent memory
// for the current context (user memory in DMs, group memory in groups).
package memory_update

import (
	"context"

	"encoding/json"
	"fmt"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Plugin implements port.ToolPlugin for updating persistent memory.
type Plugin struct {
	id    string
	store port.StorePlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new memory_update tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("memory_update: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "memory_update" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Replaces persistent memory with new content for the current context."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"content":{"type":"string","description":"The complete new memory content. This replaces all existing memory."}},"required":["content"]}`)
}

func (p *Plugin) Instructions() string { return "" }

type updateArgs struct {
	Content string `json:"content"`
}

// Execute replaces the memory for the current context (user memory in DMs, group memory in groups).
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a updateArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("memory_update: parse args: %w", err)
	}

	var ownerType, ownerID string
	if tc.IsGroup {
		ownerType = "chat"
		ownerID = tc.ChatID.String()
	} else {
		ownerType = "user"
		ownerID = tc.UserID.String()
	}

	mem := entity.Memory{
		TenantID:  tc.TenantID,
		OwnerType: ownerType,
		OwnerID:   ownerID,
		Content:   a.Content,
	}
	if err := p.store.SaveMemory(ctx, tc.TenantID, mem); err != nil {
		return "", err
	}

	return "Memory updated.", nil
}
