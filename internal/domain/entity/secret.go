package entity

import "time"

// ---------------------------------------------------------------------------
// Secret entity
// ---------------------------------------------------------------------------

// SecretScope identifies the ownership scope of a secret.
type SecretScope string

const (
	SecretScopeTenant SecretScope = "tenant"
	SecretScopeUser   SecretScope = "user"
)

// SecretMode identifies how a secret value is injected at runtime.
type SecretMode string

const (
	SecretModeValue SecretMode = "value" // env var = decrypted value (default)
	SecretModeFile  SecretMode = "file"  // env var = path to temp file containing decrypted value
)

// Secret represents an encrypted key-value secret owned by a tenant or user.
type Secret struct {
	ID             SecretID
	Key            string
	EncryptedValue []byte
	Scope          SecretScope
	Mode           SecretMode
	TenantID       TenantID
	UserID         UserID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SecretToken represents a one-time token for secret entry via external URL.
type SecretToken struct {
	TokenID        string
	Keys           []string
	Modes          map[string]SecretMode // per-key mode hints (optional)
	TenantID       TenantID
	UserID         UserID
	ConversationID ConversationID
	ChatID         ChatID
	ExpiresAt      time.Time
	Used           bool
}
