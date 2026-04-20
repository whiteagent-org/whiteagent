// Package secret_request implements a ToolPlugin that generates a one-time URL
// for the user to securely enter secret values via a web form.
package secret_request

import (
	"context"

	"encoding/json"
	"fmt"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/internal/domain/service/secret"
)

// Plugin implements port.ToolPlugin for requesting secret entry via web form.
type Plugin struct {
	id      string
	secrets secret.SecretService
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new secret_request tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("secret_request: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetSecretService injects the secret service dependency.
func (p *Plugin) SetSecretService(svc secret.SecretService) { p.secrets = svc }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "secret_request" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Generates a one-time URL for the user to securely enter secret values via a web form."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"keys":{"type":"array","items":{"type":"string"},"description":"Secret key names to request"},"modes":{"type":"object","additionalProperties":{"type":"string","enum":["value","file"]},"description":"Optional per-key mode hints. Use 'file' for large/multi-line secrets like certificates or JSON keys."}},"required":["keys"]}`)
}

func (p *Plugin) Instructions() string { return "" }

type requestArgs struct {
	Keys  []string          `json:"keys"`
	Modes map[string]string `json:"modes,omitempty"`
}

type requestResult struct {
	URL       string   `json:"url"`
	Keys      []string `json:"keys"`
	ExpiresIn string   `json:"expires_in"`
}

// Execute generates a one-time secret entry URL for the requested keys.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a requestArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("secret_request: parse args: %w", err)
	}
	if len(a.Keys) == 0 {
		return "", fmt.Errorf("secret_request: at least one key is required")
	}

	// Convert string modes to entity.SecretMode.
	var modes map[string]entity.SecretMode
	if len(a.Modes) > 0 {
		modes = make(map[string]entity.SecretMode, len(a.Modes))
		for k, v := range a.Modes {
			modes[k] = entity.SecretMode(v)
		}
	}

	url, err := p.secrets.RequestEntry(ctx, a.Keys, modes, tc.TenantID, tc.UserID, tc.ConversationID, tc.ChatID)
	if err != nil {
		return "", fmt.Errorf("secret_request: %w", err)
	}

	result, _ := json.Marshal(requestResult{
		URL:       url,
		Keys:      a.Keys,
		ExpiresIn: "1 hour",
	})
	return string(result), nil
}
