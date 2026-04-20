package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// SaveSummary inserts a persisted conversation summary scoped by tenant and conversation.
func (p *Plugin) SaveSummary(ctx context.Context, tenantID entity.TenantID, summary entity.Summary) error {
	const q = `
		INSERT INTO summaries (id, tenant_id, conversation_id, content, message_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`

	_, err := p.db.ExecContext(ctx, q,
		summary.ID,
		string(tenantID),
		string(summary.ConversationID),
		summary.Content,
		string(summary.MessageID),
		summary.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("SaveSummary insert: %w", err)
	}
	return nil
}

// GetLatestSummary returns the most recent stored summary for a tenant-scoped conversation.
func (p *Plugin) GetLatestSummary(ctx context.Context, tenantID entity.TenantID, convID entity.ConversationID) (*entity.Summary, error) {
	const q = `
		SELECT id, tenant_id, conversation_id, content, message_id, created_at
		FROM summaries
		WHERE tenant_id = ? AND conversation_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1`

	row := p.db.QueryRowContext(ctx, q, string(tenantID), string(convID))
	summary, err := scanSummaryRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetLatestSummary query: %w", err)
	}
	return summary, nil
}

// ListSummaries returns tenant-scoped summaries for one conversation in chronological order.
func (p *Plugin) ListSummaries(ctx context.Context, tenantID entity.TenantID, convID entity.ConversationID, offset, limit int) ([]entity.Summary, error) {
	q := `
		SELECT id, tenant_id, conversation_id, content, message_id, created_at
		FROM summaries
		WHERE tenant_id = ? AND conversation_id = ?
		ORDER BY created_at ASC, id ASC`
	args := []any{string(tenantID), string(convID)}

	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
		if offset > 0 {
			q += ` OFFSET ?`
			args = append(args, offset)
		}
	} else if offset > 0 {
		q += ` LIMIT -1 OFFSET ?`
		args = append(args, offset)
	}

	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ListSummaries query: %w", err)
	}
	defer rows.Close()

	var summaries []entity.Summary
	for rows.Next() {
		summary, err := scanSummaryRow(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, *summary)
	}
	return summaries, rows.Err()
}

type summaryScanner interface {
	Scan(dest ...any) error
}

func scanSummaryRow(scanner summaryScanner) (*entity.Summary, error) {
	var (
		summary              entity.Summary
		tenantID, convID     string
		messageID, createdAt string
	)

	if err := scanner.Scan(
		&summary.ID,
		&tenantID,
		&convID,
		&summary.Content,
		&messageID,
		&createdAt,
	); err != nil {
		return nil, err
	}

	summary.TenantID = entity.TenantID(tenantID)
	summary.ConversationID = entity.ConversationID(convID)
	summary.MessageID = entity.MessageID(messageID)

	parsedTime, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		parsedTime, err = time.Parse("2006-01-02 15:04:05", createdAt)
		if err != nil {
			return nil, fmt.Errorf("scanSummaryRow parse created_at: %w", err)
		}
	}
	summary.CreatedAt = parsedTime
	return &summary, nil
}
