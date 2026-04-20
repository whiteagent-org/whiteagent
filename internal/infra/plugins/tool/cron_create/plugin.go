// Package cron_create implements a ToolPlugin that creates scheduled cron entries.
package cron_create

import (
	"context"

	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/cron"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// Plugin implements port.ToolPlugin for creating scheduled cron entries.
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

// NewPlugin creates a new cron_create tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, raw json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("cron_create: plugin ID is required")
	}
	p.id = id
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var cfg pluginConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("cron_create: parse config: %w", err)
	}
	p.serverTZ = cfg.Timezone
	return nil
}

func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "cron_create" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Creates a scheduled task that runs on a cron schedule (recurring) or at a specific time (once)."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "schedule": {
      "type": "string",
      "description": "Cron expression (5-field: minute hour day-of-month month day-of-week) for recurring, or ISO 8601 / RFC3339 timestamp for one-shot."
    },
    "instructions": {
      "type": "string",
      "description": "What the agent should do when this task fires."
    },
    "name": {
      "type": "string",
      "description": "Short label for this scheduled task."
    },
    "type": {
      "type": "string",
      "enum": ["recurring", "once"],
      "description": "Whether the task repeats on a cron schedule or runs once."
    },
    "timezone": {
      "type": "string",
      "description": "IANA timezone name (e.g., America/New_York). Used for interpreting naive timestamps and computing next run times."
    }
  },
  "required": ["schedule", "instructions", "name", "type"]
}`)
}

func (p *Plugin) Instructions() string { return "" }

type createArgs struct {
	Schedule     string `json:"schedule"`
	Instructions string `json:"instructions"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Timezone     string `json:"timezone"`
}

// Execute creates a cron entry and saves it to the store.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	if p.store == nil {
		return "", fmt.Errorf("cron_create: store not available")
	}

	var a createArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("cron_create: parse args: %w", err)
	}

	// Validate required fields.
	a.Schedule = strings.TrimSpace(a.Schedule)
	a.Instructions = strings.TrimSpace(a.Instructions)
	a.Name = strings.TrimSpace(a.Name)
	a.Type = strings.TrimSpace(a.Type)

	if a.Schedule == "" || a.Instructions == "" || a.Name == "" || a.Type == "" {
		return "Missing required fields: schedule, instructions, name, and type are all required.", nil
	}
	if a.Type != "recurring" && a.Type != "once" {
		return fmt.Sprintf("Invalid type %q. Must be \"recurring\" or \"once\".", a.Type), nil
	}

	// Load server timezone.
	serverTZ := p.serverTZ
	if serverTZ == "" {
		serverTZ = "UTC"
	}
	serverLoc, err := time.LoadLocation(serverTZ)
	if err != nil {
		return "", fmt.Errorf("cron_create: invalid server timezone %q: %w", serverTZ, err)
	}

	// If user provided a timezone, validate it now.
	if a.Timezone != "" {
		if _, err := time.LoadLocation(a.Timezone); err != nil {
			return fmt.Sprintf("Invalid timezone %q. Use an IANA timezone name like \"America/New_York\" or \"Europe/London\".", a.Timezone), nil
		}
	}

	var cronExpr string
	var nextRunAt *time.Time

	switch a.Type {
	case "recurring":
		parsed, parseErr := cron.Parse(a.Schedule)
		if parseErr != nil {
			return fmt.Sprintf(
				"Invalid cron expression: %s. Use 5-field format: minute hour day-of-month month day-of-week (e.g., \"0 9 * * *\" for daily at 9am).",
				parseErr.Error(),
			), nil
		}
		cronExpr = a.Schedule
		next := parsed.NextAfter(time.Now().In(serverLoc))
		if !next.IsZero() {
			next = next.UTC()
			nextRunAt = &next
		}

	case "once":
		ts, parseErr := parseTimestamp(a.Schedule, a.Timezone, serverLoc)
		if parseErr != nil {
			return fmt.Sprintf(
				"Invalid timestamp: %s. Use RFC3339 format, e.g., \"2006-01-02T15:04:05Z\" or \"2006-01-02T15:04:05-07:00\".",
				parseErr.Error(),
			), nil
		}
		nextRunAt = &ts
	}

	now := time.Now().UTC()
	entry := entity.CronEntry{
		ID:             entity.CronEntryID(util.NewID()),
		TenantID:       tc.TenantID,
		AgentID:        tc.AgentID,
		UserID:         tc.UserID,
		ChatID:         tc.ChatID,
		IsGroup:        tc.IsGroup,
		Name:           a.Name,
		Instructions:   a.Instructions,
		Type:           a.Type,
		CronExpr:       cronExpr,
		NextRunAt:      nextRunAt,
		Status:         "active",
		CreatedAt:      now,
		ConversationID: tc.ConversationID,
		MessageID:      tc.MessageID,
	}

	if err := p.store.SaveCronEntry(ctx, tc.TenantID, entry); err != nil {
		return "", fmt.Errorf("cron_create: save: %w", err)
	}

	nextStr := "N/A"
	if nextRunAt != nil {
		nextStr = nextRunAt.Format(time.RFC3339)
	}

	scheduleStr := cronExpr
	if a.Type == "once" {
		scheduleStr = a.Schedule
	}

	return fmt.Sprintf(
		"Scheduled task created.\nID: %s\nName: %s\nType: %s\nStatus: active\nSchedule: %s\nNext run: %s",
		entry.ID, entry.Name, entry.Type, scheduleStr, nextStr,
	), nil
}

// parseTimestamp parses a schedule string as RFC3339 or naive datetime.
// If timezone is provided and the timestamp has no offset, it is interpreted
// in that timezone and converted to serverLoc.
func parseTimestamp(schedule, timezone string, serverLoc *time.Location) (time.Time, error) {
	// Try RFC3339 first (has offset).
	if ts, err := time.Parse(time.RFC3339, schedule); err == nil {
		return ts.UTC(), nil
	}

	// Try naive datetime with user timezone.
	if timezone != "" {
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return time.Time{}, err
		}
		if ts, err := time.ParseInLocation("2006-01-02T15:04:05", schedule, loc); err == nil {
			return ts.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse %q as timestamp", schedule)
}
