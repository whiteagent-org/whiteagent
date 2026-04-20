// Package time implements a ToolPlugin that returns the current date and time
// with optional timezone support.
package time

import (
	"context"

	"encoding/json"
	"fmt"
	gotime "time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Plugin implements port.ToolPlugin for returning the current time.
type Plugin struct {
	id        string
	defaultTZ string
}

// pluginConfig is the optional config passed to Init.
type pluginConfig struct {
	Timezone string `json:"timezone"`
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new time tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, raw json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("time: plugin ID is required")
	}
	p.id = id
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var cfg pluginConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("time: parse config: %w", err)
	}
	p.defaultTZ = cfg.Timezone
	return nil
}

func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "time" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	desc := "Returns the current date and time. Optionally specify a timezone (e.g., 'America/New_York', 'UTC')."
	if p.defaultTZ != "" {
		desc += " Default timezone: " + p.defaultTZ + "."
	}
	return desc
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	defaultNote := "Defaults to UTC."
	if p.defaultTZ != "" {
		defaultNote = "Defaults to " + p.defaultTZ + "."
	}
	return json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string","description":"IANA timezone name (e.g., America/New_York). ` + defaultNote + `"}}}`)
}

func (p *Plugin) Instructions() string { return "" }

// timeArgs is the expected JSON input for the time tool.
type timeArgs struct {
	Timezone string `json:"timezone"`
}

// Execute returns the current time formatted as RFC3339 in the given timezone.
// ToolContext is available for tools that need session context; the time tool
// ignores it.
func (p *Plugin) Execute(_ context.Context, _ port.ToolContext, args json.RawMessage) (string, error) {
	var a timeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("time: parse args: %w", err)
	}

	tz := a.Timezone
	if tz == "" {
		tz = p.defaultTZ
	}
	if tz == "" {
		tz = "UTC"
	}

	loc, err := gotime.LoadLocation(tz)
	if err != nil {
		return "", fmt.Errorf("time: invalid timezone %q: %w", tz, err)
	}

	return gotime.Now().In(loc).Format(gotime.RFC3339), nil
}
