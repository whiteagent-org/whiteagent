// Package secret_set implements a ToolPlugin that stores a secret value directly.
package secret_set

import (
	"context"

	"encoding/json"
	"fmt"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
)

// Plugin implements port.ToolPlugin for setting a secret value directly.
type Plugin struct {
	id      string
	secrets secret.SecretService
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new secret_set tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("secret_set: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetSecretService injects the secret service dependency.
func (p *Plugin) SetSecretService(svc secret.SecretService) { p.secrets = svc }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "secret_set" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Stores a secret value directly. The value will appear in conversation history."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","description":"Secret key name"},"value":{"type":"string","description":"Secret value"}},"required":["key","value"]}`)
}

func (p *Plugin) Instructions() string { return "" }

type setArgs struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type setResult struct {
	Stored bool   `json:"stored"`
	Key    string `json:"key"`
}

// Execute stores the secret value for the current user.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a setArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("secret_set: parse args: %w", err)
	}
	if a.Key == "" {
		return "", fmt.Errorf("secret_set: key is required")
	}

	if err := p.secrets.Set(ctx, tc.TenantID, tc.UserID, a.Key, []byte(a.Value), entity.SecretModeValue); err != nil {
		return "", fmt.Errorf("secret_set: %w", err)
	}

	result, _ := json.Marshal(setResult{Stored: true, Key: a.Key})
	return string(result), nil
}
