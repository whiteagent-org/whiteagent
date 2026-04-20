// Package journal_search implements a ToolPlugin that searches journal entries
// by text query.
package journal_search

import (
	"context"

	"encoding/json"
	"fmt"
	"strings"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
)

// Plugin implements port.ToolPlugin for searching journal entries.
type Plugin struct {
	id    string
	store port.StorePlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new journal_search tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("journal_search: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }
func (p *Plugin) IsEphemeral() bool             { return false }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "journal_search" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Searches journal entries by text query across all users in the channel. Optionally filter by category."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Text search query"},"category":{"type":"string","description":"Filter by journal category","enum":["Key Events","Decisions","TODOs","Preferences","Open Questions","Plans"]}},"required":[]}`)
}

func (p *Plugin) Instructions() string { return "" }

type searchArgs struct {
	Query    string `json:"query"`
	Category string `json:"category"`
}

// Execute searches journal entries matching the query and returns formatted results.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	log := logger.FromCtx(ctx)

	var a searchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("journal_search: parse args: %w", err)
	}

	query := strings.TrimSpace(a.Query)
	log.Debug("journal_search: parsed args",
		"raw_query", a.Query,
		"trimmed_query", query,
		"category", a.Category,
		"chat_id", tc.ChatID,
		"is_group", tc.IsGroup,
	)

	filter := entity.JournalFilter{
		Query: query,
	}
	// In group chats, scope results to that chat. In DMs, scope to the DM chat.
	if !tc.ChatID.IsEmpty() {
		filter.ChatID = tc.ChatID
	}
	if a.Category != "" {
		filter.Categories = []string{a.Category}
	}

	entries, err := p.store.GetJournal(ctx, tc.TenantID, filter)
	if err != nil {
		log.Debug("journal_search: store error", "error", err)
		return "", err
	}

	log.Debug("journal_search: query returned", "count", len(entries))

	if len(entries) == 0 {
		return "No journal entries found.", nil
	}

	// Collect unique user IDs and resolve names.
	userIDs := make(map[entity.UserID]struct{})
	for _, e := range entries {
		userIDs[e.UserID] = struct{}{}
	}
	userNames := make(map[entity.UserID]string, len(userIDs))
	for uid := range userIDs {
		u, err := p.store.GetUser(ctx, tc.TenantID, uid)
		if err == nil && u != nil {
			userNames[uid] = u.Name
		}
	}

	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString("\n\n")
		}
		authorName := userNames[e.UserID]
		if authorName == "" {
			authorName = string(e.UserID)
		}
		fmt.Fprintf(&b, "[%s] %s (%s): %s: %s", e.CreatedAt.Format("2006-01-02"), authorName, e.UserID, e.Category, e.Content)
	}

	return b.String(), nil
}
