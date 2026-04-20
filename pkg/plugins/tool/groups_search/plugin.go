// Package groups_search implements a ToolPlugin that searches for group chats
// the current user has participated in.
package groups_search

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

//go:embed instructions.tmpl
var instructionsText string

const pluginID = "tool.groups_search"

// Plugin implements port.ToolPlugin for searching group chats by name.
type Plugin struct {
	store port.StorePlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new groups_search tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return pluginID }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, _ string, _ json.RawMessage) error { return nil }
func (p *Plugin) Start(_ context.Context) error                             { return nil }
func (p *Plugin) Stop(_ context.Context) error                              { return nil }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "groups_search" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Searches for group chats by name that the current user participates in."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Partial group name to search for"}},"required":["name"]}`)
}

// Instructions returns embedded instructions template text for the system prompt.
func (p *Plugin) Instructions() string { return instructionsText }

type searchArgs struct {
	Name string `json:"name"`
}

// Execute searches for group chats matching the name query and returns formatted results.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a searchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("groups_search: parse args: %w", err)
	}

	chats, err := p.store.SearchChats(ctx, tc.TenantID, tc.UserID, a.Name)
	if err != nil {
		return "", err
	}

	if len(chats) == 0 {
		return "No groups found.", nil
	}

	var b strings.Builder
	for i, c := range chats {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "- %s (ID: %s)", c.Name, c.ID)
	}

	return b.String(), nil
}
