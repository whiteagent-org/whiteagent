package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// SaveSecret upserts a secret (INSERT OR REPLACE on tenant_id+user_id+key unique constraint).
func (p *Plugin) SaveSecret(ctx context.Context, tenantID entity.TenantID, s entity.Secret) error {
	now := time.Now().UTC().Format(time.RFC3339)
	createdAt := now
	if !s.CreatedAt.IsZero() {
		createdAt = s.CreatedAt.UTC().Format(time.RFC3339)
	}

	mode := string(s.Mode)
	if mode == "" {
		mode = string(entity.SecretModeValue)
	}

	const q = `
		INSERT INTO secrets (id, tenant_id, user_id, key, encrypted_value, scope, mode, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, user_id, key) DO UPDATE SET
			id = excluded.id,
			encrypted_value = excluded.encrypted_value,
			scope = excluded.scope,
			mode = excluded.mode,
			updated_at = excluded.updated_at`

	_, err := p.db.ExecContext(ctx, q,
		string(s.ID),
		string(tenantID),
		string(s.UserID),
		s.Key,
		s.EncryptedValue,
		string(s.Scope),
		mode,
		createdAt,
		now,
	)
	if err != nil {
		return fmt.Errorf("SaveSecret: %w", err)
	}
	return nil
}

// GetSecret retrieves a single secret by tenant, user, and key.
func (p *Plugin) GetSecret(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) (*entity.Secret, error) {
	const q = `SELECT id, tenant_id, user_id, key, encrypted_value, scope, mode, created_at, updated_at
		FROM secrets WHERE tenant_id = ? AND user_id = ? AND key = ?`

	var s entity.Secret
	var tid, uid, scope, mode, createdAt, updatedAt string
	err := p.db.QueryRowContext(ctx, q, string(tenantID), string(userID), key).Scan(
		&s.ID, &tid, &uid, &s.Key, &s.EncryptedValue, &scope, &mode, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetSecret: %w", err)
	}
	s.TenantID = entity.TenantID(tid)
	s.UserID = entity.UserID(uid)
	s.Scope = entity.SecretScope(scope)
	s.Mode = entity.SecretMode(mode)
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &s, nil
}

// ListSecrets returns all secrets for a tenant that are either tenant-scoped (user_id=”)
// or owned by the given user.
func (p *Plugin) ListSecrets(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) ([]entity.Secret, error) {
	const q = `SELECT id, tenant_id, user_id, key, encrypted_value, scope, mode, created_at, updated_at
		FROM secrets WHERE tenant_id = ? AND (user_id = '' OR user_id = ?) ORDER BY key`

	rows, err := p.db.QueryContext(ctx, q, string(tenantID), string(userID))
	if err != nil {
		return nil, fmt.Errorf("ListSecrets: %w", err)
	}
	defer rows.Close()

	var secrets []entity.Secret
	for rows.Next() {
		var s entity.Secret
		var tid, uid, scope, mode, createdAt, updatedAt string
		if err := rows.Scan(&s.ID, &tid, &uid, &s.Key, &s.EncryptedValue, &scope, &mode, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("ListSecrets scan: %w", err)
		}
		s.TenantID = entity.TenantID(tid)
		s.UserID = entity.UserID(uid)
		s.Scope = entity.SecretScope(scope)
		s.Mode = entity.SecretMode(mode)
		s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		secrets = append(secrets, s)
	}
	return secrets, rows.Err()
}

// DeleteSecret removes a secret by tenant, user, and key. Idempotent (no error if not found).
func (p *Plugin) DeleteSecret(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, key string) error {
	const q = `DELETE FROM secrets WHERE tenant_id = ? AND user_id = ? AND key = ?`
	_, err := p.db.ExecContext(ctx, q, string(tenantID), string(userID), key)
	if err != nil {
		return fmt.Errorf("DeleteSecret: %w", err)
	}
	return nil
}

// SaveSecretToken persists a new secret token with keys and modes as JSON.
func (p *Plugin) SaveSecretToken(ctx context.Context, token entity.SecretToken) error {
	keysJSON, err := json.Marshal(token.Keys)
	if err != nil {
		return fmt.Errorf("SaveSecretToken marshal keys: %w", err)
	}

	modesJSON := []byte("{}")
	if len(token.Modes) > 0 {
		modesJSON, err = json.Marshal(token.Modes)
		if err != nil {
			return fmt.Errorf("SaveSecretToken marshal modes: %w", err)
		}
	}

	const q = `INSERT INTO secret_tokens (token_id, keys, modes, tenant_id, user_id, conversation_id, chat_id, expires_at, used)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`

	_, err = p.db.ExecContext(ctx, q,
		token.TokenID,
		string(keysJSON),
		string(modesJSON),
		string(token.TenantID),
		string(token.UserID),
		string(token.ConversationID),
		token.ChatID.String(),
		token.ExpiresAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("SaveSecretToken: %w", err)
	}
	return nil
}

// GetSecretToken retrieves a secret token by ID with JSON deserialization of keys and modes.
func (p *Plugin) GetSecretToken(ctx context.Context, tokenID string) (*entity.SecretToken, error) {
	const q = `SELECT token_id, keys, modes, tenant_id, user_id, conversation_id, chat_id, expires_at, used
		FROM secret_tokens WHERE token_id = ?`

	var t entity.SecretToken
	var keysJSON, modesJSON, tid, uid, convID, chatID, expiresAt string
	var used int
	err := p.db.QueryRowContext(ctx, q, tokenID).Scan(
		&t.TokenID, &keysJSON, &modesJSON, &tid, &uid, &convID, &chatID, &expiresAt, &used,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetSecretToken: %w", err)
	}

	t.TenantID = entity.TenantID(tid)
	t.UserID = entity.UserID(uid)
	t.ConversationID = entity.ConversationID(convID)
	t.ChatID = entity.ChatID(chatID)
	t.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	t.Used = used != 0

	if err := json.Unmarshal([]byte(keysJSON), &t.Keys); err != nil {
		return nil, fmt.Errorf("GetSecretToken unmarshal keys: %w", err)
	}
	if modesJSON != "" && modesJSON != "{}" {
		if err := json.Unmarshal([]byte(modesJSON), &t.Modes); err != nil {
			return nil, fmt.Errorf("GetSecretToken unmarshal modes: %w", err)
		}
	}
	return &t, nil
}

// ConsumeSecretToken atomically marks a token as used. Returns error if already consumed or not found.
func (p *Plugin) ConsumeSecretToken(ctx context.Context, tokenID string) error {
	const q = `UPDATE secret_tokens SET used = 1 WHERE token_id = ? AND used = 0`
	res, err := p.db.ExecContext(ctx, q, tokenID)
	if err != nil {
		return fmt.Errorf("ConsumeSecretToken: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("ConsumeSecretToken rows: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("ConsumeSecretToken: token %q not found or already consumed", tokenID)
	}
	return nil
}
