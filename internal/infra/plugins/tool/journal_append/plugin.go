// Package journal_append implements a ToolPlugin that saves journal entries
// scoped to the current conversation.
package journal_append

import (
	"context"

	"encoding/json"
	"fmt"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Plugin implements port.ToolPlugin for appending journal entries.
type Plugin struct {
	id    string
	store port.StorePlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new journal_append tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("journal_append: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "journal_append" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Saves a journal entry scoped to the current conversation."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"category":{"type":"string","enum":["Key Events","Decisions","TODOs","Preferences","Open Questions","Plans"],"description":"Entry category"},"note":{"type":"string","description":"The journal note text"}},"required":["category","note"]}`)
}

func (p *Plugin) Instructions() string { return "" }

type appendArgs struct {
	Category string `json:"category"`
	Note     string `json:"note"`
}

// Execute appends a journal entry for the current session.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a appendArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("journal_append: parse args: %w", err)
	}

	if tc.UserID.IsEmpty() && tc.ChatID.IsEmpty() {
		return "", fmt.Errorf("journal_append: no user or chat context available")
	}

	entry := entity.JournalEntry{
		TenantID:       tc.TenantID,
		UserID:         tc.UserID,
		ChatID:         tc.ChatID,
		ConversationID: tc.ConversationID,
		Category:       a.Category,
		Content:        a.Note,
	}
	if !tc.MessageID.IsEmpty() {
		entry.MessageID = tc.MessageID.String()
	}

	if err := p.store.AppendJournal(ctx, tc.TenantID, entry); err != nil {
		return "", err
	}

	return "Journal entry saved.", nil
}
