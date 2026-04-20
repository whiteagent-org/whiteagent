package entity

import "time"

// User is a human user belonging to a tenant.
type User struct {
	ID               UserID
	TenantID         TenantID
	Name             string     // Display name (e.g., "Alice")
	PreferredChannel string     // Channel plugin ID for outgoing responses; empty means reply on same channel
	DeletedAt        *time.Time // Soft-delete timestamp; nil = active
	CreatedAt        time.Time
}
