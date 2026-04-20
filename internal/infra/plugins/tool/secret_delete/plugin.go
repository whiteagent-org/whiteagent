// Package secret_delete implements a ToolPlugin that deletes a user-scoped secret.
package secret_delete

import (
	"context"

	"encoding/json"
	"fmt"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
)

// Plugin implements port.ToolPlugin for deleting a user-scoped secret.
type Plugin struct {
	id      string
	secrets secret.SecretService
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new secret_delete tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("secret_delete: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetSecretService injects the secret service dependency.
func (p *Plugin) SetSecretService(svc secret.SecretService) { p.secrets = svc }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "secret_delete" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Deletes a user-scoped secret by key. Cannot delete tenant secrets."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","description":"Secret key to delete"}},"required":["key"]}`)
}

func (p *Plugin) Instructions() string { return "" }

type deleteArgs struct {
	Key string `json:"key"`
}

type deleteResult struct {
	Deleted bool   `json:"deleted"`
	Key     string `json:"key"`
}

// Execute deletes a user-scoped secret by key. Idempotent.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a deleteArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("secret_delete: parse args: %w", err)
	}
	if a.Key == "" {
		return "", fmt.Errorf("secret_delete: key is required")
	}

	if err := p.secrets.Delete(ctx, tc.TenantID, tc.UserID, a.Key); err != nil {
		return "", fmt.Errorf("secret_delete: %w", err)
	}

	result, _ := json.Marshal(deleteResult{Deleted: true, Key: a.Key})
	return string(result), nil
}
