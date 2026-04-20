// Package history_search implements a ToolPlugin that searches past messages
// by keyword with FTS5 relevance ordering.
package history_search

import (
	"context"

	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Plugin implements port.ToolPlugin for searching past messages.
type Plugin struct {
	id    string
	store port.StorePlugin
}

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindTool}
}

// NewPlugin creates a new history_search tool plugin instance.
func NewPlugin() port.Plugin { return &Plugin{} }

func (p *Plugin) ID() string                 { return p.id }
func (p *Plugin) Kind() entity.PluginKind    { return entity.PluginKindTool }
func (p *Plugin) Status() entity.PluginState { return entity.PluginStateHealthy }

func (p *Plugin) Init(_ context.Context, id string, _ json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("history_search: plugin ID is required")
	}
	p.id = id
	return nil
}
func (p *Plugin) Start(_ context.Context) error { return nil }
func (p *Plugin) Stop(_ context.Context) error  { return nil }

// SetStore injects the store dependency.
func (p *Plugin) SetStore(s port.StorePlugin) { p.store = s }

// Name returns the tool function name used in tool schemas.
func (p *Plugin) Name() string { return "history_search" }

// Description returns a human-readable description for the LLM.
func (p *Plugin) Description() string {
	return "Searches past messages by keyword across conversations. Results ordered by relevance when a query is provided."
}

// Parameters returns the JSON Schema describing tool parameters.
func (p *Plugin) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"FTS5 keyword search query (optional -- omit to browse recent messages)"},"role":{"type":"string","description":"Filter by message role","enum":["user","assistant"]},"conversation_id":{"type":"string","description":"Filter to a specific conversation"},"after":{"type":"string","description":"Only messages after this time (ISO 8601)"},"before":{"type":"string","description":"Only messages before this time (ISO 8601)"},"offset":{"type":"integer","description":"Pagination offset (default 0)"},"limit":{"type":"integer","description":"Max results (default 10, max 50)"}}}`)
}

func (p *Plugin) Instructions() string { return "" }

type searchArgs struct {
	Query          string `json:"query"`
	Role           string `json:"role"`
	ConversationID string `json:"conversation_id"`
	After          string `json:"after"`
	Before         string `json:"before"`
	Offset         int    `json:"offset"`
	Limit          int    `json:"limit"`
}

// Execute searches past messages matching the query and returns formatted results.
func (p *Plugin) Execute(ctx context.Context, tc port.ToolContext, args json.RawMessage) (string, error) {
	var a searchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("history_search: parse args: %w", err)
	}

	filter := port.MessageFilter{
		ChatID: tc.ChatID,
		Query:  strings.TrimSpace(a.Query),
		Roles:  []entity.Role{entity.RoleUser, entity.RoleAssistant},
	}

	// DM: scope to user; Group: all participants visible
	if !tc.IsGroup {
		filter.UserID = tc.UserID
	}

	// Role override
	if a.Role != "" {
		filter.Roles = []entity.Role{entity.Role(a.Role)}
	}

	// Optional conversation filter
	if a.ConversationID != "" {
		filter.ConversationID = entity.ConversationID(a.ConversationID)
	}

	// Time range filters
	if a.After != "" {
		if t, err := time.Parse(time.RFC3339, a.After); err == nil {
			filter.After = &t
		}
	}
	if a.Before != "" {
		if t, err := time.Parse(time.RFC3339, a.Before); err == nil {
			filter.Before = &t
		}
	}

	// Limit: default 10, cap at 50
	filter.Limit = a.Limit
	if filter.Limit <= 0 {
		filter.Limit = 10
	}
	if filter.Limit > 50 {
		filter.Limit = 50
	}

	filter.Offset = a.Offset

	// Exclude evicted messages from search results (T-46-05).
	notEvicted := false
	filter.Evicted = &notEvicted

	messages, err := p.store.GetMessages(ctx, tc.TenantID, filter)
	if err != nil {
		return "", err
	}

	if len(messages) == 0 {
		return "No messages found.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d message(s):\n\n", len(messages))

	for i, m := range messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		content := truncateRunes(m.Content, 300)
		fmt.Fprintf(&b, "[%s] %s %s (%s):\n%s",
			m.CreatedAt.UTC().Format(time.RFC3339),
			string(m.Role),
			string(m.ID),
			string(m.ConversationID),
			content,
		)
	}

	return b.String(), nil
}

// truncateRunes truncates s to maxRunes runes, appending "..." if truncated.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
