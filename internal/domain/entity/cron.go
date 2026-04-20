package entity

import "time"

// CronEntry represents a scheduled task owned by a user within a tenant.
type CronEntry struct {
	ID             CronEntryID
	TenantID       TenantID
	AgentID        AgentID
	UserID         UserID
	ChatID         ChatID // Chat where the cron was created
	IsGroup        bool   // Whether the originating chat is a group
	Name           string // agent-generated label
	Instructions   string // task instructions
	Type           string // "recurring" | "once"
	CronExpr       string // populated for recurring
	NextRunAt      *time.Time
	Status         string // "active" | "paused" | "completed"
	CreatedAt      time.Time
	Metadata       map[string]string // Channel/sender context, JSON in SQLite
	ConversationID ConversationID    // Conversation active when cron was created
	MessageID      MessageID         // Message that triggered creation of this cron entry
}

// MessageMetadata returns stored metadata merged with cron-specific fields
// derived from entry columns. This avoids storing redundant data.
func (e CronEntry) MessageMetadata() map[string]string {
	meta := make(map[string]string, len(e.Metadata)+3)
	for k, v := range e.Metadata {
		meta[k] = v
	}
	meta["cron_name"] = e.Name
	meta["cron_type"] = e.Type
	meta["cron_created_at"] = e.CreatedAt.Format(time.RFC3339)
	return meta
}

// CronRun represents a single execution of a cron entry.
type CronRun struct {
	ID          CronRunID
	CronEntryID CronEntryID
	TenantID    TenantID
	Status      string // "running" | "success" | "failed"
	Error       string // inline error message, empty on success
	StartedAt   time.Time
	FinishedAt  *time.Time
}
