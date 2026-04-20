package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// SaveInviteCode inserts a new invite code record.
func (p *Plugin) SaveInviteCode(ctx context.Context, code entity.InviteCode) error {
	const q = `
        INSERT INTO invite_codes (code, type, tenant_id, target_id, used_by, created_at, revoked_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)`

	var revokedAt *string
	if code.RevokedAt != nil {
		s := code.RevokedAt.UTC().Format(time.RFC3339)
		revokedAt = &s
	}

	_, err := p.db.ExecContext(ctx, q,
		code.Code,
		code.Type,
		string(code.TenantID),
		code.TargetID,
		string(code.UsedBy),
		code.CreatedAt.UTC().Format(time.RFC3339),
		revokedAt,
	)
	if err != nil {
		return fmt.Errorf("SaveInviteCode: %w", err)
	}
	return nil
}

// GetInviteCode retrieves an invite code by its code string (cross-tenant lookup).
// Returns (nil, nil) if the code does not exist.
func (p *Plugin) GetInviteCode(ctx context.Context, code string) (*entity.InviteCode, error) {
	const q = `
        SELECT code, type, tenant_id, target_id, used_by, created_at, revoked_at
        FROM invite_codes
        WHERE code = ?`
	row := p.db.QueryRowContext(ctx, q, code)
	return scanInviteCode(row)
}

// ListInviteCodes returns invite codes matching the filter, ordered by creation time descending.
// Zero-value filter fields are ignored.
func (p *Plugin) ListInviteCodes(ctx context.Context, filter entity.InviteCodeFilter) ([]entity.InviteCode, error) {
	q := `SELECT code, type, tenant_id, target_id, used_by, created_at, revoked_at FROM invite_codes WHERE 1=1`
	var args []any

	if filter.Type != "" {
		q += ` AND type = ?`
		args = append(args, filter.Type)
	}
	if filter.TenantID != "" {
		q += ` AND tenant_id = ?`
		args = append(args, string(filter.TenantID))
	}
	q += ` ORDER BY created_at DESC`

	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ListInviteCodes query: %w", err)
	}
	defer rows.Close()

	codes := make([]entity.InviteCode, 0)
	for rows.Next() {
		ic, err := scanInviteRow(rows)
		if err != nil {
			return nil, fmt.Errorf("ListInviteCodes: %w", err)
		}
		codes = append(codes, ic)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListInviteCodes rows: %w", err)
	}
	return codes, nil
}

// RevokeInviteCode marks an invite code as revoked by code (cross-tenant).
func (p *Plugin) RevokeInviteCode(ctx context.Context, code string) error {
	const q = `UPDATE invite_codes SET revoked_at = datetime('now')
        WHERE code = ? AND revoked_at IS NULL`
	_, err := p.db.ExecContext(ctx, q, code)
	if err != nil {
		return fmt.Errorf("RevokeInviteCode: %w", err)
	}
	return nil
}

// UseInviteCode atomically marks an invite code as used by a specific user.
// The single UPDATE with WHERE guards prevents race conditions:
// code must exist, not be used, and not be revoked.
// Returns an error if the code is invalid, already used, or revoked.
func (p *Plugin) UseInviteCode(ctx context.Context, code string, userID entity.UserID) error {
	const q = `UPDATE invite_codes SET used_by = ?
        WHERE code = ? AND used_by = '' AND revoked_at IS NULL`
	res, err := p.db.ExecContext(ctx, q, string(userID), code)
	if err != nil {
		return fmt.Errorf("UseInviteCode: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("UseInviteCode rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("UseInviteCode: code %q is invalid, already used, or revoked", code)
	}
	return nil
}

// scanInviteCode scans a single sql.Row into an entity.InviteCode.
// Returns (nil, nil) on sql.ErrNoRows.
func scanInviteCode(row *sql.Row) (*entity.InviteCode, error) {
	var ic entity.InviteCode
	var typ, tenantID, targetID, usedBy, createdAt string
	var revokedAt *string
	err := row.Scan(&ic.Code, &typ, &tenantID, &targetID, &usedBy, &createdAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanInviteCode: %w", err)
	}
	ic.Type = typ
	ic.TenantID = entity.TenantID(tenantID)
	ic.TargetID = targetID
	ic.UsedBy = entity.UserID(usedBy)

	ic.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("scanInviteCode parse created_at: %w", err)
	}
	if revokedAt != nil {
		t, err := parseTime(*revokedAt)
		if err != nil {
			return nil, fmt.Errorf("scanInviteCode parse revoked_at: %w", err)
		}
		ic.RevokedAt = &t
	}
	return &ic, nil
}

// scanInviteRow scans a single row from sql.Rows into an entity.InviteCode.
func scanInviteRow(rows *sql.Rows) (entity.InviteCode, error) {
	var ic entity.InviteCode
	var typ, tenantID, targetID, usedBy, createdAt string
	var revokedAt *string
	if err := rows.Scan(&ic.Code, &typ, &tenantID, &targetID, &usedBy, &createdAt, &revokedAt); err != nil {
		return ic, fmt.Errorf("scan: %w", err)
	}
	ic.Type = typ
	ic.TenantID = entity.TenantID(tenantID)
	ic.TargetID = targetID
	ic.UsedBy = entity.UserID(usedBy)

	var err error
	ic.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return ic, fmt.Errorf("parse created_at: %w", err)
	}
	if revokedAt != nil {
		t, err := parseTime(*revokedAt)
		if err != nil {
			return ic, fmt.Errorf("parse revoked_at: %w", err)
		}
		ic.RevokedAt = &t
	}
	return ic, nil
}

// parseTime tries RFC3339 first, then SQLite datetime format.
func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", s)
	}
	return t, err
}
