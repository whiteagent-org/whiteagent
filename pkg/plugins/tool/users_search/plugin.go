// Package users_search implements a ToolPlugin that searches for users by name
// within the current tenant.
package users_search

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

const pluginID = "tool.users_search"

// Plugin implements port.ToolPlugin for searching users by name.
type Plugin struct {
	store port.StorePlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new users_search tool plugin instance.
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
func (p *Plugin) Name() string { return "users_search" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Searches for users by name within the current tenant."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Partial name to search for"}},"required":["name"]}`)
}

// Instructions returns embedded instructions template text for the system prompt.
func (p *Plugin) Instructions() string { return instructionsText }

type searchArgs struct {
	Name string `json:"name"`
}

// Execute searches for users matching the name query and returns formatted results.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a searchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("users_search: parse args: %w", err)
	}

	users, err := p.store.SearchUsers(ctx, tc.TenantID, a.Name)
	if err != nil {
		return "", err
	}

	if len(users) == 0 {
		return "No users found.", nil
	}

	var b strings.Builder
	for i, u := range users {
		if i > 0 {
			b.WriteString("\n")
		}
		chat, _ := p.store.GetDMChat(ctx, tc.TenantID, u.ID)
		if chat != nil {
			fmt.Fprintf(&b, "- %s (ID: %s, Chat: %s)", u.Name, u.ID, chat.ID)
		} else {
			fmt.Fprintf(&b, "- %s (ID: %s)", u.Name, u.ID)
		}
	}

	return b.String(), nil
}
