package entity

import "time"

// Memory represents a polymorphic memory entry owned by a user or chat.
// The OwnerType + OwnerID pair identifies the owner (e.g., "user" + UserID or "chat" + ChatID).
type Memory struct {
	TenantID  TenantID
	OwnerType string // "user" or "chat"
	OwnerID   string // UserID or ChatID as string
	Content   string
	UpdatedAt time.Time
}
