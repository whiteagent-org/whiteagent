package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// cronEntryCols lists all cron_entries columns in SELECT order.
const cronEntryCols = `id, tenant_id, agent_id, user_id, chat_id, is_group, name, instructions, type, cron_expr, next_run_at, status, created_at, metadata, conversation_id, message_id`

// SaveCronEntry persists a cron entry using the caller-provided ID.
func (p *Plugin) SaveCronEntry(ctx context.Context, tenantID entity.TenantID, entry entity.CronEntry) error {
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	var nextRunAt *string
	if entry.NextRunAt != nil {
		s := entry.NextRunAt.UTC().Format(time.RFC3339)
		nextRunAt = &s
	}

	meta := entry.Metadata
	if meta == nil {
		meta = map[string]string{}
	}
	metadataJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("SaveCronEntry marshal metadata: %w", err)
	}

	isGroup := 0
	if entry.IsGroup {
		isGroup = 1
	}

	const q = `
		INSERT INTO cron_entries (id, tenant_id, agent_id, user_id, chat_id, is_group, name, instructions, type, cron_expr, next_run_at, status, created_at, metadata, conversation_id, message_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = p.db.ExecContext(ctx, q,
		string(entry.ID),
		string(tenantID),
		string(entry.AgentID),
		string(entry.UserID),
		entry.ChatID.String(),
		isGroup,
		entry.Name,
		entry.Instructions,
		entry.Type,
		entry.CronExpr,
		nextRunAt,
		entry.Status,
		createdAt.Format(time.RFC3339),
		string(metadataJSON),
		string(entry.ConversationID),
		string(entry.MessageID),
	)
	if err != nil {
		return fmt.Errorf("SaveCronEntry insert: %w", err)
	}
	return nil
}

// GetCronEntry retrieves a cron entry by tenant and ID. Returns (nil, nil) if not found.
func (p *Plugin) GetCronEntry(ctx context.Context, tenantID entity.TenantID, id entity.CronEntryID) (*entity.CronEntry, error) {
	const q = `SELECT ` + cronEntryCols + ` FROM cron_entries WHERE id = ? AND tenant_id = ?`
	row := p.db.QueryRowContext(ctx, q, string(id), string(tenantID))
	e, err := scanCronEntry(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetCronEntry: %w", err)
	}
	return &e, nil
}

// ListCronEntries returns all cron entries for a tenant and user, ordered by created_at DESC.
func (p *Plugin) ListCronEntries(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) ([]entity.CronEntry, error) {
	const q = `SELECT ` + cronEntryCols + `
		FROM cron_entries
		WHERE tenant_id = ? AND user_id = ?
		ORDER BY created_at DESC`

	rows, err := p.db.QueryContext(ctx, q, string(tenantID), string(userID))
	if err != nil {
		return nil, fmt.Errorf("ListCronEntries query: %w", err)
	}
	defer rows.Close()

	var entries []entity.CronEntry
	for rows.Next() {
		e, err := scanCronEntryRows(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ListActiveCronEntries returns all active cron entries across all tenants (cross-tenant).
func (p *Plugin) ListActiveCronEntries(ctx context.Context) ([]entity.CronEntry, error) {
	const q = `SELECT ` + cronEntryCols + `
		FROM cron_entries
		WHERE status = 'active'
		ORDER BY CASE WHEN next_run_at IS NULL THEN 1 ELSE 0 END, next_run_at ASC`

	rows, err := p.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ListActiveCronEntries query: %w", err)
	}
	defer rows.Close()

	var entries []entity.CronEntry
	for rows.Next() {
		e, err := scanCronEntryRows(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// DeleteCronEntry removes a cron entry by tenant and ID.
func (p *Plugin) DeleteCronEntry(ctx context.Context, tenantID entity.TenantID, id entity.CronEntryID) error {
	const q = `DELETE FROM cron_entries WHERE id = ? AND tenant_id = ?`
	_, err := p.db.ExecContext(ctx, q, string(id), string(tenantID))
	if err != nil {
		return fmt.Errorf("DeleteCronEntry: %w", err)
	}
	return nil
}

// UpdateCronStatus changes the status field of a cron entry.
func (p *Plugin) UpdateCronStatus(ctx context.Context, tenantID entity.TenantID, id entity.CronEntryID, status string) error {
	const q = `UPDATE cron_entries SET status = ? WHERE id = ? AND tenant_id = ?`
	_, err := p.db.ExecContext(ctx, q, status, string(id), string(tenantID))
	if err != nil {
		return fmt.Errorf("UpdateCronStatus: %w", err)
	}
	return nil
}

// UpdateCronNextRun updates the next_run_at field. Passing nil clears it.
func (p *Plugin) UpdateCronNextRun(ctx context.Context, tenantID entity.TenantID, id entity.CronEntryID, nextRunAt *time.Time) error {
	var val *string
	if nextRunAt != nil {
		s := nextRunAt.UTC().Format(time.RFC3339)
		val = &s
	}
	const q = `UPDATE cron_entries SET next_run_at = ? WHERE id = ? AND tenant_id = ?`
	_, err := p.db.ExecContext(ctx, q, val, string(id), string(tenantID))
	if err != nil {
		return fmt.Errorf("UpdateCronNextRun: %w", err)
	}
	return nil
}

// InsertCronRun creates a cron run record with a generated ID.
func (p *Plugin) InsertCronRun(ctx context.Context, tenantID entity.TenantID, run entity.CronRun) error {
	var finishedAt *string
	if run.FinishedAt != nil {
		s := run.FinishedAt.UTC().Format(time.RFC3339)
		finishedAt = &s
	}

	const q = `
		INSERT INTO cron_runs (id, cron_entry_id, tenant_id, status, error, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := p.db.ExecContext(ctx, q,
		string(run.ID),
		string(run.CronEntryID),
		string(tenantID),
		run.Status,
		run.Error,
		run.StartedAt.UTC().Format(time.RFC3339),
		finishedAt,
	)
	if err != nil {
		return fmt.Errorf("InsertCronRun insert: %w", err)
	}
	return nil
}

// UpdateCronRun sets status, error, and finished_at on an existing run.
func (p *Plugin) UpdateCronRun(ctx context.Context, tenantID entity.TenantID, runID entity.CronRunID, status string, errMsg string, finishedAt *time.Time) error {
	var fin *string
	if finishedAt != nil {
		s := finishedAt.UTC().Format(time.RFC3339)
		fin = &s
	}
	const q = `UPDATE cron_runs SET status = ?, error = ?, finished_at = ? WHERE id = ? AND tenant_id = ?`
	_, err := p.db.ExecContext(ctx, q, status, errMsg, fin, string(runID), string(tenantID))
	if err != nil {
		return fmt.Errorf("UpdateCronRun: %w", err)
	}
	return nil
}

// ListCronRuns returns runs for a cron entry ordered by started_at DESC, limited.
func (p *Plugin) ListCronRuns(ctx context.Context, tenantID entity.TenantID, cronEntryID entity.CronEntryID, limit int) ([]entity.CronRun, error) {
	const q = `
		SELECT id, cron_entry_id, tenant_id, status, error, started_at, finished_at
		FROM cron_runs
		WHERE cron_entry_id = ? AND tenant_id = ?
		ORDER BY started_at DESC
		LIMIT ?`

	rows, err := p.db.QueryContext(ctx, q, string(cronEntryID), string(tenantID), limit)
	if err != nil {
		return nil, fmt.Errorf("ListCronRuns query: %w", err)
	}
	defer rows.Close()

	var runs []entity.CronRun
	for rows.Next() {
		r, err := scanCronRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// scanCronEntry scans a single cron entry from a *sql.Row.
func scanCronEntry(row *sql.Row) (entity.CronEntry, error) {
	var e entity.CronEntry
	var id, tenantID, agentID, userID, chatID, createdAt string
	var metadataStr string
	var nextRunAt *string
	var conversationID, messageID string
	var isGroup int

	err := row.Scan(&id, &tenantID, &agentID, &userID, &chatID, &isGroup, &e.Name, &e.Instructions, &e.Type, &e.CronExpr, &nextRunAt, &e.Status, &createdAt, &metadataStr, &conversationID, &messageID)
	if err != nil {
		return e, err
	}

	e.ID = entity.CronEntryID(id)
	e.TenantID = entity.TenantID(tenantID)
	e.AgentID = entity.AgentID(agentID)
	e.UserID = entity.UserID(userID)
	e.ChatID = entity.ChatID(chatID)
	e.IsGroup = isGroup != 0
	e.ConversationID = entity.ConversationID(conversationID)
	e.MessageID = entity.MessageID(messageID)

	if metadataStr != "" && metadataStr != "null" {
		if err := json.Unmarshal([]byte(metadataStr), &e.Metadata); err != nil {
			return e, fmt.Errorf("scanCronEntry unmarshal metadata: %w", err)
		}
	}

	e.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return e, fmt.Errorf("scanCronEntry parse created_at: %w", err)
	}

	if nextRunAt != nil {
		t, err := parseTime(*nextRunAt)
		if err != nil {
			return e, fmt.Errorf("scanCronEntry parse next_run_at: %w", err)
		}
		e.NextRunAt = &t
	}

	return e, nil
}

// scanCronEntryRows scans a single cron entry from *sql.Rows.
func scanCronEntryRows(rows *sql.Rows) (entity.CronEntry, error) {
	var e entity.CronEntry
	var id, tenantID, agentID, userID, chatID, createdAt string
	var metadataStr string
	var nextRunAt *string
	var conversationID, messageID string
	var isGroup int

	err := rows.Scan(&id, &tenantID, &agentID, &userID, &chatID, &isGroup, &e.Name, &e.Instructions, &e.Type, &e.CronExpr, &nextRunAt, &e.Status, &createdAt, &metadataStr, &conversationID, &messageID)
	if err != nil {
		return e, fmt.Errorf("scanCronEntryRows: %w", err)
	}

	e.ID = entity.CronEntryID(id)
	e.TenantID = entity.TenantID(tenantID)
	e.AgentID = entity.AgentID(agentID)
	e.UserID = entity.UserID(userID)
	e.ChatID = entity.ChatID(chatID)
	e.IsGroup = isGroup != 0
	e.ConversationID = entity.ConversationID(conversationID)
	e.MessageID = entity.MessageID(messageID)

	if metadataStr != "" && metadataStr != "null" {
		if err := json.Unmarshal([]byte(metadataStr), &e.Metadata); err != nil {
			return e, fmt.Errorf("scanCronEntryRows unmarshal metadata: %w", err)
		}
	}

	e.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return e, fmt.Errorf("scanCronEntryRows parse created_at: %w", err)
	}

	if nextRunAt != nil {
		t, err := parseTime(*nextRunAt)
		if err != nil {
			return e, fmt.Errorf("scanCronEntryRows parse next_run_at: %w", err)
		}
		e.NextRunAt = &t
	}

	return e, nil
}

// scanCronRun scans a single cron run from *sql.Rows.
func scanCronRun(rows *sql.Rows) (entity.CronRun, error) {
	var r entity.CronRun
	var id, cronEntryID, tenantID, startedAt string
	var finishedAt *string

	err := rows.Scan(&id, &cronEntryID, &tenantID, &r.Status, &r.Error, &startedAt, &finishedAt)
	if err != nil {
		return r, fmt.Errorf("scanCronRun: %w", err)
	}

	r.ID = entity.CronRunID(id)
	r.CronEntryID = entity.CronEntryID(cronEntryID)
	r.TenantID = entity.TenantID(tenantID)

	r.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return r, fmt.Errorf("scanCronRun parse started_at: %w", err)
	}

	if finishedAt != nil {
		t, err := parseTime(*finishedAt)
		if err != nil {
			return r, fmt.Errorf("scanCronRun parse finished_at: %w", err)
		}
		r.FinishedAt = &t
	}

	return r, nil
}

// parseTime is defined in invite.go -- reused here.
