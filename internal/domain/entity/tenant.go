package entity

import "time"

// Tenant is the top-level isolation boundary. Every agent and user belongs
// to exactly one tenant.
type Tenant struct {
	ID               TenantID
	Name             string
	Instructions     string     // Tenant-level instructions injected into every agent prompt; empty means none
	DefaultAgentID   AgentID    // Agent used when inbound message doesn't specify one; empty means none set
	JoinPolicy       string     // "invite_required" (default), "open", or "closed"
	RejectionMessage string     // Message shown when join is rejected; has a sensible default
	GroupMode        string     // "all" or "mention_only" (default)
	AllowedSkills    []string   // Whitelist of skill names synced from global; empty = all
	AllowedTools     []string   // Whitelist of tool plugin names; empty = all tools allowed
	DeletedAt        *time.Time // Soft-delete timestamp; nil = active
	CreatedAt        time.Time
}

const (
	JoinPolicyOpen           = "open"
	JoinPolicyInviteRequired = "invite_required"
	JoinPolicyClosed         = "closed"

	GroupModeAll         = "all"
	GroupModeMentionOnly = "mention_only"
)
