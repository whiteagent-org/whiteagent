package entity

import "time"

// Summary stores persisted conversation compaction output bounded by the latest
// message ID it covers.
type Summary struct {
	ID             string
	TenantID       TenantID
	ConversationID ConversationID
	Content        string
	MessageID      MessageID
	CreatedAt      time.Time
}
