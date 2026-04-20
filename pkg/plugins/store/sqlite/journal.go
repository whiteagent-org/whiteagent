package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/pkg/logger"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// AppendJournal inserts a new journal entry. Journal entries are append-only -- no update or delete.
// The plugin generates entry.ID using crypto/rand; callers should not set it.
func (p *Plugin) AppendJournal(ctx context.Context, tenantID entity.TenantID, entry entity.JournalEntry) error {
	id := util.NewID()

	const q = `
        INSERT INTO journal (id, tenant_id, user_id, chat_id, conversation_id, message_id, category, content, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := p.db.ExecContext(ctx, q,
		id,
		string(tenantID),
		string(entry.UserID),
		entry.ChatID.String(),
		string(entry.ConversationID),
		entry.MessageID,
		entry.Category,
		entry.Content,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("AppendJournal insert: %w", err)
	}
	return nil
}

// GetJournal retrieves journal entries matching the filter.
// Always scoped by tenantID (explicit param). When filter.UserID is set, results are
// further scoped to that user; when empty, entries from all users are returned.
func (p *Plugin) GetJournal(ctx context.Context, tenantID entity.TenantID, filter entity.JournalFilter) ([]entity.JournalEntry, error) {
	log := logger.FromCtx(ctx)

	q, args := buildJournalQuery(string(tenantID), filter)
	log.Debug("GetJournal: executing query",
		"args_count", len(args),
		"filter_query", filter.Query,
		"filter_user_id", filter.UserID,
		"filter_chat_id", filter.ChatID,
		"filter_categories", filter.Categories,
		"uses_fts", strings.TrimSpace(filter.Query) != "",
	)

	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Debug("GetJournal: query failed", "error", err)
		return nil, fmt.Errorf("GetJournal query: %w", err)
	}
	defer rows.Close()

	var entries []entity.JournalEntry
	for rows.Next() {
		e, err := scanJournalEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	log.Debug("GetJournal: returned entries", "count", len(entries))
	return entries, rows.Err()
}

// buildJournalQuery constructs the dynamic WHERE clause for GetJournal.
// Base filter: tenant_id (always required).
// Optional: UserID, ChatID, Categories, Query (FTS5).
// Per-field zero-value semantics: empty/nil field = no filter on that field.
func buildJournalQuery(tenantID string, filter entity.JournalFilter) (string, []any) {
	var b strings.Builder
	args := make([]any, 0, 6)
	useFTS := strings.TrimSpace(filter.Query) != ""

	if useFTS {
		// FTS5 path: join journal_fts for full-text search, then scope by tenant.
		b.WriteString(`
            SELECT j.id, j.tenant_id, j.user_id, j.chat_id, j.conversation_id, j.category, j.content, j.message_id, j.created_at
            FROM journal j
            JOIN journal_fts f ON f.rowid = j.rowid
            WHERE j.tenant_id = ?
              AND journal_fts MATCH ?`)
		args = append(args, tenantID, filter.Query)
	} else {
		b.WriteString(`
            SELECT id, tenant_id, user_id, chat_id, conversation_id, category, content, message_id, created_at
            FROM journal
            WHERE tenant_id = ?`)
		args = append(args, tenantID)
	}

	// Helper for column prefix.
	col := func(name string) string {
		if useFTS {
			return "j." + name
		}
		return name
	}

	// Optional: user_id scoping.
	if filter.UserID != "" {
		b.WriteString(` AND ` + col("user_id") + ` = ?`)
		args = append(args, string(filter.UserID))
	}

	// Optional: chat_id scoping.
	// Returns user-level entries (chat_id = '') PLUS entries for the specific chat.
	// This gives the agent full context in one call.
	if !filter.ChatID.IsEmpty() {
		b.WriteString(` AND (` + col("chat_id") + ` = '' OR ` + col("chat_id") + ` = ?)`)
		args = append(args, filter.ChatID.String())
	}

	// Optional: category filter.
	if len(filter.Categories) > 0 {
		placeholders := strings.Repeat("?,", len(filter.Categories))
		placeholders = placeholders[:len(placeholders)-1] // trim trailing comma
		b.WriteString(` AND ` + col("category") + ` IN (` + placeholders + `)`)
		for _, c := range filter.Categories {
			args = append(args, c)
		}
	}

	// Optional: conversation_id scoping.
	if filter.ConversationID != "" {
		b.WriteString(` AND ` + col("conversation_id") + ` = ?`)
		args = append(args, string(filter.ConversationID))
	}

	// Optional: created_at upper bound.
	if !filter.BeforeTime.IsZero() {
		b.WriteString(` AND ` + col("created_at") + ` < ?`)
		args = append(args, filter.BeforeTime.Format(time.RFC3339))
	}

	b.WriteString(` ORDER BY created_at ASC`)
	return b.String(), args
}

// scanJournalEntry scans a single journal row into entity.JournalEntry.
func scanJournalEntry(rows *sql.Rows) (entity.JournalEntry, error) {
	var e entity.JournalEntry
	var tenantID, userID, chatID, convID, createdAt string
	err := rows.Scan(&e.ID, &tenantID, &userID, &chatID, &convID, &e.Category, &e.Content, &e.MessageID, &createdAt)
	if err != nil {
		return e, fmt.Errorf("scanJournalEntry: %w", err)
	}
	e.TenantID = entity.TenantID(tenantID)
	e.UserID = entity.UserID(userID)
	e.ChatID = entity.ChatID(chatID)
	e.ConversationID = entity.ConversationID(convID)
	e.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		e.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		if err != nil {
			return e, fmt.Errorf("scanJournalEntry parse created_at: %w", err)
		}
	}
	return e, nil
}
