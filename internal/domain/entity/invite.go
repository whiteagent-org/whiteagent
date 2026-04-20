package entity

import "time"

// ---------------------------------------------------------------------------
// InviteCode -- invitation codes (INV-01, INV-02)
// ---------------------------------------------------------------------------

// InviteCode represents a single-use invitation code.
// Type "tenant" creates a new tenant; type "user" joins an existing tenant.
// Codes are generated in XXXX-XXXX format (uppercase alphanumeric).
type InviteCode struct {
	Code      string     // XXXX-XXXX format
	Type      string     // "tenant" or "user"
	TenantID  TenantID   // Empty for tenant-creation codes
	TargetID  string     // Target entity ID (UserID or TenantID depending on Type); empty = standard behavior
	UsedBy    UserID     // Empty until redeemed
	CreatedAt time.Time
	RevokedAt *time.Time // nil = active
}

// InviteCodeFilter specifies retrieval criteria for ListInviteCodes.
// Zero values mean no filter on that field.
type InviteCodeFilter struct {
	Type     string   // "tenant", "user", or "" (no filter)
	TenantID TenantID // Empty = no filter
}
