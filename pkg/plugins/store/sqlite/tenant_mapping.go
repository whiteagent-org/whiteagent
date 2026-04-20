package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// SaveTenantMapping upserts a tenant mapping.
func (p *Plugin) SaveTenantMapping(ctx context.Context, mapping entity.TenantMapping) error {
	const q = `INSERT OR REPLACE INTO tenant_mappings (channel_id, external_tenant_id, tenant_id)
        VALUES (?, ?, ?)`
	_, err := p.db.ExecContext(ctx, q, mapping.ChannelID, mapping.ExternalTenantID, string(mapping.TenantID))
	if err != nil {
		return fmt.Errorf("SaveTenantMapping: %w", err)
	}
	return nil
}

// GetTenantByMapping returns the tenant ID for a channel+external workspace pair.
// Returns empty TenantID (not an error) if no mapping exists.
func (p *Plugin) GetTenantByMapping(ctx context.Context, channelID, externalTenantID string) (entity.TenantID, error) {
	const q = `SELECT tenant_id FROM tenant_mappings
        WHERE channel_id = ? AND external_tenant_id = ?`
	var tid string
	err := p.db.QueryRowContext(ctx, q, channelID, externalTenantID).Scan(&tid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("GetTenantByMapping: %w", err)
	}
	return entity.TenantID(tid), nil
}

// DeleteTenantMapping removes a tenant mapping by channel+external workspace pair.
func (p *Plugin) DeleteTenantMapping(ctx context.Context, channelID, externalTenantID string) error {
	const q = `DELETE FROM tenant_mappings WHERE channel_id = ? AND external_tenant_id = ?`
	_, err := p.db.ExecContext(ctx, q, channelID, externalTenantID)
	if err != nil {
		return fmt.Errorf("DeleteTenantMapping: %w", err)
	}
	return nil
}

// ListTenantMappings returns tenant mappings, optionally filtered by tenant ID.
// If tenantID is empty, all mappings are returned.
func (p *Plugin) ListTenantMappings(ctx context.Context, tenantID entity.TenantID) ([]entity.TenantMapping, error) {
	var q string
	var args []any
	if tenantID == "" {
		q = `SELECT channel_id, external_tenant_id, tenant_id FROM tenant_mappings ORDER BY channel_id`
	} else {
		q = `SELECT channel_id, external_tenant_id, tenant_id FROM tenant_mappings WHERE tenant_id = ? ORDER BY channel_id`
		args = append(args, string(tenantID))
	}

	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ListTenantMappings query: %w", err)
	}
	defer rows.Close()

	mappings := make([]entity.TenantMapping, 0)
	for rows.Next() {
		var m entity.TenantMapping
		var tid string
		if err := rows.Scan(&m.ChannelID, &m.ExternalTenantID, &tid); err != nil {
			return nil, fmt.Errorf("ListTenantMappings scan: %w", err)
		}
		m.TenantID = entity.TenantID(tid)
		mappings = append(mappings, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListTenantMappings rows: %w", err)
	}
	return mappings, nil
}

// MergeUser atomically reassigns all data from one user (fromID) to another (toID)
// within the same tenant, then deletes the source user. Both users must exist.
// Updates: user_identities, messages, journal, cron_entries, error_log.
func (p *Plugin) MergeUser(ctx context.Context, tenantID entity.TenantID, fromID, toID entity.UserID) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("MergeUser begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	tid := string(tenantID)

	// Verify both users exist.
	var existsID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM users WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`,
		tid, string(fromID)).Scan(&existsID)
	if err != nil {
		return fmt.Errorf("MergeUser: source user %q not found: %w", fromID, err)
	}
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM users WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`,
		tid, string(toID)).Scan(&existsID)
	if err != nil {
		return fmt.Errorf("MergeUser: target user %q not found: %w", toID, err)
	}

	// Reassign user_identities from source to target.
	// UPDATE OR IGNORE skips rows that would violate uniqueness (target already has that channel).
	if _, err := tx.ExecContext(ctx,
		`UPDATE OR IGNORE user_identities SET user_id = ? WHERE tenant_id = ? AND user_id = ?`,
		string(toID), tid, string(fromID)); err != nil {
		return fmt.Errorf("MergeUser update user_identities: %w", err)
	}
	// Clean up any remaining source rows that conflicted (target already had that channel).
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_identities WHERE tenant_id = ? AND user_id = ?`,
		tid, string(fromID)); err != nil {
		return fmt.Errorf("MergeUser delete source user_identities: %w", err)
	}

	// Reassign messages.
	if _, err := tx.ExecContext(ctx,
		`UPDATE messages SET user_id = ? WHERE tenant_id = ? AND user_id = ?`,
		string(toID), tid, string(fromID)); err != nil {
		return fmt.Errorf("MergeUser update messages: %w", err)
	}

	// Reassign journal entries.
	if _, err := tx.ExecContext(ctx,
		`UPDATE journal SET user_id = ? WHERE tenant_id = ? AND user_id = ?`,
		string(toID), tid, string(fromID)); err != nil {
		return fmt.Errorf("MergeUser update journal: %w", err)
	}

	// Reassign cron entries.
	if _, err := tx.ExecContext(ctx,
		`UPDATE cron_entries SET user_id = ? WHERE tenant_id = ? AND user_id = ?`,
		string(toID), tid, string(fromID)); err != nil {
		return fmt.Errorf("MergeUser update cron_entries: %w", err)
	}

	// Reassign error log entries.
	if _, err := tx.ExecContext(ctx,
		`UPDATE error_log SET user_id = ? WHERE tenant_id = ? AND user_id = ?`,
		string(toID), tid, string(fromID)); err != nil {
		return fmt.Errorf("MergeUser update error_log: %w", err)
	}

	// Hard-delete source user (not soft-delete -- merge means source is fully consumed).
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM users WHERE tenant_id = ? AND id = ?`,
		tid, string(fromID)); err != nil {
		return fmt.Errorf("MergeUser delete source user: %w", err)
	}

	return tx.Commit()
}
