// Package secret defines the secret service interface.
package secret

import (
	"context"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// SecretEntry represents a secret key with its effective scope and mode after merge.
type SecretEntry struct {
	Key   string
	Scope entity.SecretScope
	Mode  entity.SecretMode
}

// SecretEnvEntry represents a decrypted secret with its mode for sandbox injection.
type SecretEnvEntry struct {
	Key   string
	Value string
	Mode  entity.SecretMode
}

// SecretSubmission represents a secret value with its mode as submitted by the web form.
type SecretSubmission struct {
	Value []byte
	Mode  entity.SecretMode
}

// SecretService manages encrypted secrets scoped to tenants and users.
type SecretService interface {
	// Set encrypts and stores a plaintext secret value for the given tenant/user and key.
	Set(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string, value []byte, mode entity.SecretMode) error

	// Get retrieves and decrypts the secret value for the given key.
	Get(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) ([]byte, error)

	// Exists checks whether a secret with the given key exists.
	Exists(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) (bool, error)

	// List returns effective merged view: user keys shadow tenant keys on collision.
	List(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) ([]SecretEntry, error)

	// Delete removes a user-scoped secret by key.
	Delete(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) error

	// Redact replaces any decrypted secret values found in text with "[REDACTED]".
	// Best-effort: decrypt failures are logged and skipped.
	Redact(ctx context.Context, text string, tenantID entity.TenantID, userID entity.UserID) string

	// EnvVars returns decrypted secrets with mode information for sandbox injection.
	EnvVars(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) ([]SecretEnvEntry, error)

	// RequestEntry creates a one-time token for entering secret values via URL.
	// ConversationID and ChatID are stored on the token so that the web form
	// submission can route a notification back to the originating conversation.
	// Modes is an optional map of key->mode hints for the web form.
	RequestEntry(ctx context.Context, keys []string, modes map[string]entity.SecretMode, tenantID entity.TenantID, userID entity.UserID, convID entity.ConversationID, chatID entity.ChatID) (string, error)

	// ValidateToken checks whether the given token is valid and unused.
	ValidateToken(ctx context.Context, tokenID string) (*entity.SecretToken, error)

	// ConsumeToken marks the token as used and stores the provided secret values with modes.
	ConsumeToken(ctx context.Context, tokenID string, values map[string]SecretSubmission) error
}
