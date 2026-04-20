// Package secret_list implements a ToolPlugin that lists available secret keys.
package secret_list

import (
	"context"

	"encoding/json"
	"fmt"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
)

// Plugin implements port.ToolPlugin for listing secret keys.
type Plugin struct {
	id      string
	secrets secret.SecretService
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new secret_list tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("secret_list: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetSecretService injects the secret service dependency.
func (p *Plugin) SetSecretService(svc secret.SecretService) { p.secrets = svc }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "secret_list" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Lists available secret key names with their scope (user or tenant)."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (p *Plugin) Instructions() string { return "" }

type listEntry struct {
	Key   string `json:"key"`
	Scope string `json:"scope"`
	Mode  string `json:"mode"`
}

type listResult struct {
	Secrets []listEntry `json:"secrets"`
	Count   int         `json:"count"`
}

// Execute lists all effective secret keys for the current user.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, _ json.RawMessage) (string, error) {
	entries, err := p.secrets.List(ctx, tc.TenantID, tc.UserID)
	if err != nil {
		return "", fmt.Errorf("secret_list: %w", err)
	}

	secrets := make([]listEntry, len(entries))
	for i, e := range entries {
		secrets[i] = listEntry{Key: e.Key, Scope: string(e.Scope), Mode: string(e.Mode)}
	}

	result, _ := json.Marshal(listResult{Secrets: secrets, Count: len(secrets)})
	return string(result), nil
}
