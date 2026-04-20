// Package set_model_profile implements a ToolPlugin that allows the LLM to set
// a model profile optimized for the current task.
package set_model_profile

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

//go:embed instructions.tmpl
var instructionsTmpl string

// modelProfile maps an alias to its description and endpoint chain.
type modelProfile struct {
	Alias       string   `json:"alias"`
	Description string   `json:"description"`
	Endpoints   []string `json:"endpoints"`
}

type pluginConfig struct {
	Models []modelProfile `json:"models"`
}

// Plugin implements port.ToolPlugin for model profile selection.
type Plugin struct {
	id       string
	profiles map[string]modelProfile // alias -> profile
	aliases  []string                // ordered list for enum/instructions
	params   json.RawMessage
	instText string
	override *port.ModelOverride
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new set_model_profile tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, raw json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("set_model_profile: plugin ID is required")
	}
	p.id = id

	var cfg pluginConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("set_model_profile: parse config: %w", err)
	}
	if len(cfg.Models) == 0 {
		return fmt.Errorf("set_model_profile: config must have at least one model profile")
	}

	p.profiles = make(map[string]modelProfile, len(cfg.Models))
	p.aliases = make([]string, 0, len(cfg.Models))
	for i, m := range cfg.Models {
		if m.Alias == "" {
			return fmt.Errorf("set_model_profile: models[%d]: alias is required", i)
		}
		if m.Description == "" {
			return fmt.Errorf("set_model_profile: models[%d]: description is required", i)
		}
		if len(m.Endpoints) == 0 {
			return fmt.Errorf("set_model_profile: models[%d]: at least one endpoint is required", i)
		}
		if _, exists := p.profiles[m.Alias]; exists {
			return fmt.Errorf("set_model_profile: duplicate alias %q", m.Alias)
		}
		p.profiles[m.Alias] = m
		p.aliases = append(p.aliases, m.Alias)
	}

	// Build parameters JSON schema with enum from aliases.
	p.params = buildParams(p.aliases)

	// Render instructions template.
	tmpl, err := template.New("instructions").Parse(instructionsTmpl)
	if err != nil {
		return fmt.Errorf("set_model_profile: parse instructions template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, cfg.Models); err != nil {
		return fmt.Errorf("set_model_profile: render instructions: %w", err)
	}
	p.instText = buf.String()

	return nil
}

func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetModelOverride injects the per-message model override holder.
func (p *Plugin) SetModelOverride(override *port.ModelOverride) {
	p.override = override
}

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "set_model_profile" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Sets a model profile optimized for the current task."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage { return p.params }

// Instructions returns rendered instructions listing available profiles.
func (p *Plugin) Instructions() string { return p.instText }

type profileArgs struct {
	Alias string `json:"alias"`
}

// Execute sets the model override to the requested profile's endpoint chain.
func (p *Plugin) Execute(_ context.Context, _ port.ToolContext, args json.RawMessage) (string, error) {
	var a profileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("set_model_profile: parse args: %w", err)
	}

	profile, ok := p.profiles[a.Alias]
	if !ok {
		return "", fmt.Errorf("set_model_profile: unknown profile %q", a.Alias)
	}

	if p.override == nil {
		return "", fmt.Errorf("set_model_profile: model override not available")
	}

	p.override.Set(profile.Endpoints)
	return fmt.Sprintf("Model profile set to %s.", profile.Alias), nil
}

// buildParams creates a JSON Schema with alias as an enum.
func buildParams(aliases []string) json.RawMessage {
	enumItems := make([]string, len(aliases))
	for i, a := range aliases {
		enumItems[i] = fmt.Sprintf("%q", a)
	}
	schema := fmt.Sprintf(
		`{"type":"object","properties":{"alias":{"type":"string","enum":[%s],"description":"The profile alias to set"}},"required":["alias"]}`,
		strings.Join(enumItems, ","),
	)
	return json.RawMessage(schema)
}
