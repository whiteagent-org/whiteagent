// Package cron_set_status implements a ToolPlugin that pauses, resumes, or soft-deletes a cron entry.
package cron_set_status

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/cron"
)

// Plugin implements port.ToolPlugin for changing cron entry status.
type Plugin struct {
	id       string
	store    port.StorePlugin
	serverTZ string
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new cron_set_status tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, cfg json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("cron_set_status: plugin ID is required")
	}
	p.id = id
	if len(cfg) > 0 {
		var c struct {
			Timezone string `json:"timezone"`
		}
		if err := json.Unmarshal(cfg, &c); err != nil {
			return fmt.Errorf("cron_set_status: parse config: %w", err)
		}
		p.serverTZ = c.Timezone
	}
	return nil
}

func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "cron_set_status" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Pauses, resumes, or removes a scheduled task by ID. Use status 'paused' to pause, 'active' to resume, or 'deleted' to remove."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The scheduled task ID."},"status":{"type":"string","enum":["active","paused","deleted"],"description":"New status: active (resume), paused (pause), or deleted (remove)."}},"required":["id","status"]}`)
}

// Instructions returns empty string (no system prompt additions needed).
func (p *Plugin) Instructions() string { return "" }

type setStatusArgs struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Execute changes the status of a cron entry after verifying ownership.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	if p.store == nil {
		return "", fmt.Errorf("cron_set_status: store not set")
	}

	var a setStatusArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("cron_set_status: parse args: %w", err)
	}

	if a.ID == "" {
		return "Missing required parameter: id", nil
	}
	if a.Status != "active" && a.Status != "paused" && a.Status != "deleted" {
		return "Invalid status. Must be one of: active, paused, deleted.", nil
	}

	entryID := entity.CronEntryID(a.ID)

	entry, err := p.store.GetCronEntry(ctx, tc.TenantID, entryID)
	if err != nil {
		return "", err
	}
	if entry == nil {
		return fmt.Sprintf("Scheduled task %q not found.", a.ID), nil
	}

	// Ownership check.
	if entry.UserID != tc.UserID {
		return "Not authorized to modify this scheduled task.", nil
	}

	// Soft delete.
	if a.Status == "deleted" {
		if err := p.store.UpdateCronStatus(ctx, tc.TenantID, entryID, "deleted"); err != nil {
			return "", err
		}
		if err := p.store.UpdateCronNextRun(ctx, tc.TenantID, entryID, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("Removed scheduled task: %s (ID: %s)", entry.Name, a.ID), nil
	}

	// Update status.
	if err := p.store.UpdateCronStatus(ctx, tc.TenantID, entryID, a.Status); err != nil {
		return "", err
	}

	// Recompute NextRunAt for resumed recurring entries.
	if a.Status == "active" && entry.Type == "recurring" && entry.CronExpr != "" {
		loc := time.UTC
		if p.serverTZ != "" {
			if l, err := time.LoadLocation(p.serverTZ); err == nil {
				loc = l
			}
		}
		parsed, err := cron.Parse(entry.CronExpr)
		if err == nil {
			next := parsed.NextAfter(time.Now().In(loc))
			if err := p.store.UpdateCronNextRun(ctx, tc.TenantID, entryID, &next); err != nil {
				return "", err
			}
			return fmt.Sprintf("Scheduled task %s is now active. Next run: %s",
				entry.Name, next.In(loc).Format(time.RFC3339)), nil
		}
	}

	return fmt.Sprintf("Scheduled task %s is now %s.", entry.Name, a.Status), nil
}
