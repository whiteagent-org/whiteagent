package secret

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// secretStore is the subset of port.StorePlugin used by the secret service.
// Avoids importing the full StorePlugin interface (and its non-secret methods).
type secretStore interface {
	SaveSecret(ctx context.Context, tenantID entity.TenantID, s entity.Secret) error
	GetSecret(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) (*entity.Secret, error)
	ListSecrets(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) ([]entity.Secret, error)
	DeleteSecret(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) error
	SaveSecretToken(ctx context.Context, token entity.SecretToken) error
	GetSecretToken(ctx context.Context, tokenID string) (*entity.SecretToken, error)
	ConsumeSecretToken(ctx context.Context, tokenID string) error
}

// serviceImpl implements SecretService with AES-256-GCM encryption.
type serviceImpl struct {
	store     secretStore
	aead      cipher.AEAD
	publicURL string
	redact    bool
}

// NewService creates a new SecretService. The encryptionKey must be exactly 32 bytes.
// When redact is true, the Redact method replaces decrypted secret values in text;
// when false, Redact returns text unchanged.
func NewService(store secretStore, encryptionKey []byte, publicURL string, redact bool) (SecretService, error) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("secret: encryption key must be 32 bytes, got %d", len(encryptionKey))
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("secret: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: new GCM: %w", err)
	}

	return &serviceImpl{
		store:     store,
		aead:      gcm,
		publicURL: strings.TrimRight(publicURL, "/"),
		redact:    redact,
	}, nil
}

// encrypt produces nonce || ciphertext using AES-256-GCM with a random 12-byte nonce.
func (s *serviceImpl) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize()) // 12 bytes for GCM
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secret: generate nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt splits nonce from ciphertext and decrypts.
func (s *serviceImpl) decrypt(data []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(data) < ns {
		return nil, fmt.Errorf("secret: ciphertext too short")
	}
	return s.aead.Open(nil, data[:ns], data[ns:], nil)
}

// Set encrypts the plaintext value and stores it with the given mode.
func (s *serviceImpl) Set(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string, value []byte, mode entity.SecretMode) error {
	encrypted, err := s.encrypt(value)
	if err != nil {
		return err
	}

	scope := entity.SecretScopeUser
	if userID == "" {
		scope = entity.SecretScopeTenant
	}

	if mode == "" {
		mode = entity.SecretModeValue
	}

	now := time.Now().UTC()
	secret := entity.Secret{
		ID:             entity.SecretID(util.NewID()),
		Key:            key,
		EncryptedValue: encrypted,
		Scope:          scope,
		Mode:           mode,
		TenantID:       tenantID,
		UserID:         userID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return s.store.SaveSecret(ctx, tenantID, secret)
}

// Get retrieves and decrypts a secret value.
func (s *serviceImpl) Get(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) ([]byte, error) {
	secret, err := s.store.GetSecret(ctx, tenantID, userID, key)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, fmt.Errorf("secret %q not found", key)
	}
	return s.decrypt(secret.EncryptedValue)
}

// Exists checks whether a secret with the given key exists.
func (s *serviceImpl) Exists(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) (bool, error) {
	secret, err := s.store.GetSecret(ctx, tenantID, userID, key)
	if err != nil {
		return false, err
	}
	return secret != nil, nil
}

// List returns the effective merged view of secrets. User keys shadow tenant keys on collision.
func (s *serviceImpl) List(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) ([]SecretEntry, error) {
	secrets, err := s.store.ListSecrets(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	// Deduplicate: user-scoped overrides tenant-scoped on same key.
	type entry struct {
		scope entity.SecretScope
		mode  entity.SecretMode
	}
	effective := make(map[string]entry)
	for _, sec := range secrets {
		existing, ok := effective[sec.Key]
		if !ok || (existing.scope == entity.SecretScopeTenant && sec.Scope == entity.SecretScopeUser) {
			m := sec.Mode
			if m == "" {
				m = entity.SecretModeValue
			}
			effective[sec.Key] = entry{scope: sec.Scope, mode: m}
		}
	}

	entries := make([]SecretEntry, 0, len(effective))
	for key, e := range effective {
		entries = append(entries, SecretEntry{Key: key, Scope: e.scope, Mode: e.mode})
	}
	return entries, nil
}

// Delete removes a user-scoped secret by key. Idempotent.
func (s *serviceImpl) Delete(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) error {
	return s.store.DeleteSecret(ctx, tenantID, userID, key)
}

// Redact replaces decrypted secret values in text with [REDACTED].
// Best-effort: decrypt failures are logged and skipped.
// Values shorter than 4 chars or empty are skipped.
// Returns text unchanged when redaction is disabled.
func (s *serviceImpl) Redact(ctx context.Context, text string, tenantID entity.TenantID, userID entity.UserID) string {
	if !s.redact {
		return text
	}
	secrets, err := s.store.ListSecrets(ctx, tenantID, userID)
	if err != nil {
		slog.Warn("secret redact: failed to list secrets", "err", err)
		return text
	}

	// Deduplicate: keep user-scoped value over tenant on same key.
	type decrypted struct {
		value string
		scope entity.SecretScope
	}
	effective := make(map[string]decrypted)
	for _, sec := range secrets {
		val, err := s.decrypt(sec.EncryptedValue)
		if err != nil {
			slog.Warn("secret redact: decrypt failed, skipping", "key", sec.Key, "err", err)
			continue
		}
		existing, ok := effective[sec.Key]
		if !ok || (existing.scope == entity.SecretScopeTenant && sec.Scope == entity.SecretScopeUser) {
			effective[sec.Key] = decrypted{value: string(val), scope: sec.Scope}
		}
	}

	replaced := 0
	for _, d := range effective {
		if len(d.value) < 4 || d.value == "" {
			continue
		}
		if strings.Contains(text, d.value) {
			text = strings.ReplaceAll(text, d.value, "[REDACTED]")
			replaced++
		}
	}

	slog.Debug("secret redact", "keys_checked", len(effective), "replacements", replaced)
	return text
}

// EnvVars returns decrypted secrets with mode information. User overrides tenant.
func (s *serviceImpl) EnvVars(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) ([]SecretEnvEntry, error) {
	secrets, err := s.store.ListSecrets(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	// Deduplicate, user overrides tenant.
	type entry struct {
		encrypted []byte
		scope     entity.SecretScope
		mode      entity.SecretMode
	}
	effective := make(map[string]entry)
	for _, sec := range secrets {
		existing, ok := effective[sec.Key]
		if !ok || (existing.scope == entity.SecretScopeTenant && sec.Scope == entity.SecretScopeUser) {
			m := sec.Mode
			if m == "" {
				m = entity.SecretModeValue
			}
			effective[sec.Key] = entry{encrypted: sec.EncryptedValue, scope: sec.Scope, mode: m}
		}
	}

	result := make([]SecretEnvEntry, 0, len(effective))
	for key, e := range effective {
		val, err := s.decrypt(e.encrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt secret %q: %w", key, err)
		}
		result = append(result, SecretEnvEntry{Key: key, Value: string(val), Mode: e.mode})
	}
	return result, nil
}

// RequestEntry creates a one-time token for secret web form entry.
func (s *serviceImpl) RequestEntry(ctx context.Context, keys []string, modes map[string]entity.SecretMode, tenantID entity.TenantID, userID entity.UserID, convID entity.ConversationID, chatID entity.ChatID) (string, error) {
	tokenID := util.NewRandomID()
	token := entity.SecretToken{
		TokenID:        tokenID,
		Keys:           keys,
		Modes:          modes,
		TenantID:       tenantID,
		UserID:         userID,
		ConversationID: convID,
		ChatID:         chatID,
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}
	if err := s.store.SaveSecretToken(ctx, token); err != nil {
		return "", err
	}
	return s.publicURL + "/secrets/" + tokenID, nil
}

// ValidateToken checks whether the token is valid, unused, and unexpired.
func (s *serviceImpl) ValidateToken(ctx context.Context, tokenID string) (*entity.SecretToken, error) {
	token, err := s.store.GetSecretToken(ctx, tokenID)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, fmt.Errorf("secret token %q not found", tokenID)
	}
	if token.Used {
		return nil, fmt.Errorf("secret token %q already consumed", tokenID)
	}
	if time.Now().After(token.ExpiresAt) {
		return nil, fmt.Errorf("secret token %q expired", tokenID)
	}
	return token, nil
}

// ConsumeToken encrypts and stores each value with its mode, then marks the token as used.
func (s *serviceImpl) ConsumeToken(ctx context.Context, tokenID string, values map[string]SecretSubmission) error {
	token, err := s.ValidateToken(ctx, tokenID)
	if err != nil {
		return err
	}

	for key, sub := range values {
		mode := sub.Mode
		if mode == "" {
			mode = entity.SecretModeValue
		}
		if err := s.Set(ctx, token.TenantID, token.UserID, key, sub.Value, mode); err != nil {
			return fmt.Errorf("consume token: store secret %q: %w", key, err)
		}
	}

	return s.store.ConsumeSecretToken(ctx, tokenID)
}
