package entity

import "time"

// ErrorLogEntry represents an error event logged for observability.
type ErrorLogEntry struct {
	ID        ErrorLogEntryID
	TenantID  TenantID
	UserID    UserID
	RefType   string // "cron" | "message" | ""
	RefID     string // CronEntryID or MessageID as string
	Content   string
	CreatedAt time.Time
}

// ErrorLogFilter specifies criteria for querying error log entries.
type ErrorLogFilter struct {
	UserID  UserID
	RefType string // optional, empty = all
	Limit   int
}
