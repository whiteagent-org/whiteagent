package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// AppendErrorLog persists an error log entry with a generated ID and timestamp.
func (p *Plugin) AppendErrorLog(ctx context.Context, tenantID entity.TenantID, entry entity.ErrorLogEntry) error {
	id := util.NewID()
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	const q = `
		INSERT INTO error_log (id, tenant_id, user_id, ref_type, ref_id, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := p.db.ExecContext(ctx, q,
		id,
		string(tenantID),
		string(entry.UserID),
		entry.RefType,
		entry.RefID,
		entry.Content,
		createdAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("AppendErrorLog insert: %w", err)
	}
	return nil
}

// GetErrorLog returns error log entries filtered by user, optionally by ref_type, with limit.
// Results are ordered newest-first.
func (p *Plugin) GetErrorLog(ctx context.Context, tenantID entity.TenantID, filter entity.ErrorLogFilter) ([]entity.ErrorLogEntry, error) {
	var b strings.Builder
	args := make([]any, 0, 4)

	b.WriteString(`SELECT id, tenant_id, user_id, ref_type, ref_id, content, created_at FROM error_log WHERE tenant_id = ? AND user_id = ?`)
	args = append(args, string(tenantID), string(filter.UserID))

	if filter.RefType != "" {
		b.WriteString(` AND ref_type = ?`)
		args = append(args, filter.RefType)
	}

	b.WriteString(` ORDER BY created_at DESC`)

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	b.WriteString(` LIMIT ?`)
	args = append(args, limit)

	rows, err := p.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("GetErrorLog query: %w", err)
	}
	defer rows.Close()

	var entries []entity.ErrorLogEntry
	for rows.Next() {
		var e entity.ErrorLogEntry
		var id, tenantIDStr, userID, createdAt string
		err := rows.Scan(&id, &tenantIDStr, &userID, &e.RefType, &e.RefID, &e.Content, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("GetErrorLog scan: %w", err)
		}
		e.ID = entity.ErrorLogEntryID(id)
		e.TenantID = entity.TenantID(tenantIDStr)
		e.UserID = entity.UserID(userID)
		e.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("GetErrorLog parse created_at: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
