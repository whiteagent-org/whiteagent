// Package cron_list implements a ToolPlugin that lists a user's scheduled cron entries.
package cron_list

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Plugin implements port.ToolPlugin for listing cron entries.
type Plugin struct {
	id       string
	store    port.StorePlugin
	serverTZ string
}

// pluginConfig is the optional config passed to Init.
type pluginConfig struct {
	Timezone string `json:"timezone"`
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new cron_list tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, raw json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("cron_list: plugin ID is required")
	}
	p.id = id
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var cfg pluginConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("cron_list: parse config: %w", err)
	}
	p.serverTZ = cfg.Timezone
	return nil
}

func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "cron_list" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Lists the user's scheduled tasks with status, schedule, next run time, and last run info."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string","description":"IANA timezone name for displaying dates (e.g., America/New_York). Defaults to server timezone."}}}`)
}

// Instructions returns empty string (no instructions.tmpl).
func (p *Plugin) Instructions() string { return "" }

type listArgs struct {
	Timezone string `json:"timezone"`
}

// Execute lists the user's cron entries with optional timezone conversion.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	if p.store == nil {
		return "", fmt.Errorf("cron_list: store not available")
	}

	var a listArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("cron_list: parse args: %w", err)
	}

	entries, err := p.store.ListCronEntries(ctx, tc.TenantID, tc.UserID)
	if err != nil {
		return "", fmt.Errorf("cron_list: query: %w", err)
	}

	// Filter out soft-deleted entries.
	filtered := entries[:0]
	for _, e := range entries {
		if e.Status != "deleted" {
			filtered = append(filtered, e)
		}
	}
	entries = filtered

	if len(entries) == 0 {
		return "No scheduled tasks found.", nil
	}

	// Determine display timezone.
	displayLoc := time.UTC
	if a.Timezone != "" {
		if loc, err := time.LoadLocation(a.Timezone); err == nil {
			displayLoc = loc
		}
	} else if p.serverTZ != "" {
		if loc, err := time.LoadLocation(p.serverTZ); err == nil {
			displayLoc = loc
		}
	}

	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString("\n\n")
		}

		fmt.Fprintf(&b, "ID: %s\nName: %s\nType: %s\nStatus: %s", e.ID, e.Name, e.Type, e.Status)

		if e.CronExpr != "" {
			fmt.Fprintf(&b, "\nSchedule: %s", e.CronExpr)
		}
		if e.NextRunAt != nil {
			fmt.Fprintf(&b, "\nNext run: %s", e.NextRunAt.In(displayLoc).Format(time.RFC3339))
		}
		fmt.Fprintf(&b, "\nCreated: %s", e.CreatedAt.In(displayLoc).Format(time.RFC3339))

		// Query last run.
		runs, runErr := p.store.ListCronRuns(ctx, tc.TenantID, e.ID, 1)
		if runErr == nil && len(runs) > 0 {
			last := runs[0]
			runTime := last.StartedAt.In(displayLoc).Format(time.RFC3339)
			if last.FinishedAt != nil {
				runTime = last.FinishedAt.In(displayLoc).Format(time.RFC3339)
			}
			fmt.Fprintf(&b, "\nLast run: %s (%s)", last.Status, runTime)
		}
	}

	return b.String(), nil
}
