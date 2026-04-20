package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// SaveMessage inserts a message row. Messages are append-only (no upsert).
func (p *Plugin) SaveMessage(ctx context.Context, msg entity.Message) error {
	isMention := 0
	if msg.IsMention {
		isMention = 1
	}

	// Nullable string fields: empty string -> SQL NULL.
	toolCallID := nilIfEmpty(msg.ToolCallID)
	toolName := nilIfEmpty(msg.ToolName)
	targetID := nilIfEmpty(string(msg.TargetID))
	causedByID := nilIfEmpty(string(msg.CausedByID))
	repliedToID := nilIfEmpty(string(msg.RepliedToID))

	// JSON columns: nil slice -> SQL NULL.
	var toolCalls *string
	if msg.ToolCalls != nil {
		b, err := json.Marshal(msg.ToolCalls)
		if err != nil {
			return fmt.Errorf("SaveMessage marshal tool_calls: %w", err)
		}
		s := string(b)
		toolCalls = &s
	}
	var attachments *string
	if msg.Attachments != nil {
		b, err := json.Marshal(msg.Attachments)
		if err != nil {
			return fmt.Errorf("SaveMessage marshal attachments: %w", err)
		}
		s := string(b)
		attachments = &s
	}

	const q = `
		INSERT OR IGNORE INTO messages (
			id, tenant_id, user_id, agent_id, conversation_id, chat_id,
			kind, replied_to_id, target_id, caused_by_id, role, content,
			tool_calls, tool_call_id, tool_name, attachments,
			is_mention, metadata,
			external_user_id, external_message_id, external_reply_to_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := p.db.ExecContext(ctx, q,
		string(msg.ID),
		string(msg.TenantID),
		string(msg.UserID),
		string(msg.AgentID),
		string(msg.ConversationID),
		msg.ChatID.String(),
		string(msg.Kind),
		repliedToID,
		targetID,
		causedByID,
		string(msg.Role),
		msg.Content,
		toolCalls,
		toolCallID,
		toolName,
		attachments,
		isMention,
		nil, // metadata: not used yet
		msg.MessageContext.ExternalUserID,
		msg.MessageContext.ExternalMessageID,
		msg.MessageContext.ExternalReplyToID,
		msg.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("SaveMessage insert (tenant=%s agent=%s conv=%s role=%s): %w",
			msg.TenantID, msg.AgentID, msg.ConversationID, msg.Role, err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		slog.Warn("store: message dedup", "id", string(msg.ID), "external_id", msg.MessageContext.ExternalMessageID)
	}
	return nil
}

// UpdateExternalMessageID sets the external_message_id on an existing message row.
// Used by the outbound handler to persist the platform message ID after successful Send.
func (p *Plugin) UpdateExternalMessageID(ctx context.Context, msgID entity.MessageID, externalMsgID string) error {
	_, err := p.db.ExecContext(ctx,
		"UPDATE messages SET external_message_id = ? WHERE id = ?",
		externalMsgID, string(msgID))
	return err
}

// GetMessages retrieves messages matching the filter, scoped by tenantID.
func (p *Plugin) GetMessages(ctx context.Context, tenantID entity.TenantID, filter port.MessageFilter) ([]entity.Message, error) {
	q, args := buildMessageQuery(string(tenantID), filter)

	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetMessages query: %w", err)
	}
	defer rows.Close()

	var msgs []entity.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// GetLastConversationID returns the most recent conversation_id for a given chat.
// Returns ("", nil) when no messages exist.
func (p *Plugin) GetLastConversationID(ctx context.Context, msg entity.Message) (entity.ConversationID, error) {
	const q = `
		SELECT conversation_id FROM messages
		WHERE tenant_id = ? AND chat_id = ?
		ORDER BY created_at DESC, id DESC LIMIT 1`

	var convID string
	err := p.db.QueryRowContext(ctx, q,
		string(msg.TenantID),
		msg.ChatID.String(),
	).Scan(&convID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("GetLastConversationID: %w", err)
	}
	return entity.ConversationID(convID), nil
}

// messageCols lists all 23 message columns in SELECT order (bare names, no table prefix).
const messageCols = `id, tenant_id, user_id, agent_id, conversation_id, chat_id,
	kind, replied_to_id, target_id, caused_by_id, role, content,
	tool_calls, tool_call_id, tool_name, attachments,
	is_mention, metadata,
	external_user_id, external_message_id, external_reply_to_id, created_at, evicted`

// buildMessageQuery constructs the dynamic SELECT + WHERE for GetMessages.
func buildMessageQuery(tenantID string, filter port.MessageFilter) (string, []any) {
	var b strings.Builder
	args := make([]any, 0, 12)
	useFTS := strings.TrimSpace(filter.Query) != ""

	if useFTS {
		b.WriteString(`SELECT m.id, m.tenant_id, m.user_id, m.agent_id, m.conversation_id, m.chat_id,
			m.kind, m.replied_to_id, m.target_id, m.caused_by_id, m.role, m.content,
			m.tool_calls, m.tool_call_id, m.tool_name, m.attachments,
			m.is_mention, m.metadata,
			m.external_user_id, m.external_message_id, m.external_reply_to_id, m.created_at, m.evicted
			FROM messages m
			JOIN messages_fts f ON f.rowid = m.rowid
			WHERE m.tenant_id = ?
			  AND messages_fts MATCH ?`)
		args = append(args, tenantID, filter.Query)
	} else {
		b.WriteString(`SELECT ` + messageCols + `
			FROM messages
			WHERE tenant_id = ?`)
		args = append(args, tenantID)
	}

	// Helper for column prefix.
	col := func(name string) string {
		if useFTS {
			return "m." + name
		}
		return name
	}

	if !filter.ConversationID.IsEmpty() {
		b.WriteString(` AND ` + col("conversation_id") + ` = ?`)
		args = append(args, string(filter.ConversationID))
	}
	if !filter.ChatID.IsEmpty() {
		b.WriteString(` AND ` + col("chat_id") + ` = ?`)
		args = append(args, filter.ChatID.String())
	}
	if filter.ExternalMessageID != "" {
		b.WriteString(` AND ` + col("external_message_id") + ` = ?`)
		args = append(args, filter.ExternalMessageID)
	}
	if filter.UserID != "" {
		b.WriteString(` AND ` + col("user_id") + ` = ?`)
		args = append(args, string(filter.UserID))
	}
	if filter.MessageID != "" {
		b.WriteString(` AND ` + col("id") + ` = ?`)
		args = append(args, string(filter.MessageID))
	}
	if filter.UpToID != nil {
		b.WriteString(` AND ` + col("id") + ` <= ?`)
		args = append(args, string(*filter.UpToID))
	}
	if filter.Before != nil {
		b.WriteString(` AND ` + col("created_at") + ` < ?`)
		args = append(args, filter.Before.UTC().Format(time.RFC3339))
	}
	if filter.After != nil {
		b.WriteString(` AND ` + col("created_at") + ` > ?`)
		args = append(args, filter.After.UTC().Format(time.RFC3339))
	}
	if len(filter.Roles) > 0 {
		placeholders := make([]string, len(filter.Roles))
		for i, r := range filter.Roles {
			placeholders[i] = "?"
			args = append(args, string(r))
		}
		b.WriteString(` AND ` + col("role") + ` IN (` + strings.Join(placeholders, ",") + `)`)
	}
	if filter.Evicted != nil {
		if *filter.Evicted {
			b.WriteString(` AND ` + col("evicted") + ` = 1`)
		} else {
			b.WriteString(` AND ` + col("evicted") + ` = 0`)
		}
	}

	if filter.Tail && filter.Limit > 0 {
		b.WriteString(` ORDER BY ` + col("created_at") + ` DESC, ` + col("id") + ` DESC LIMIT ?`)
		args = append(args, filter.Limit)
		if filter.Offset > 0 {
			b.WriteString(` OFFSET ?`)
			args = append(args, filter.Offset)
		}
		inner := b.String()
		b.Reset()
		b.WriteString(`SELECT * FROM (` + inner + `) ORDER BY created_at ASC, id ASC`)
	} else if useFTS {
		b.WriteString(` ORDER BY rank`)
		if filter.Limit > 0 {
			b.WriteString(` LIMIT ?`)
			args = append(args, filter.Limit)
			if filter.Offset > 0 {
				b.WriteString(` OFFSET ?`)
				args = append(args, filter.Offset)
			}
		}
	} else {
		b.WriteString(` ORDER BY ` + col("created_at") + ` ASC, ` + col("id") + ` ASC`)
		if filter.Limit > 0 {
			b.WriteString(` LIMIT ?`)
			args = append(args, filter.Limit)
			if filter.Offset > 0 {
				b.WriteString(` OFFSET ?`)
				args = append(args, filter.Offset)
			}
		}
	}

	return b.String(), args
}

// scanMessage scans a single row from a messages query into entity.Message.
func scanMessage(rows *sql.Rows) (entity.Message, error) {
	var m entity.Message
	var (
		id, tenantID, userID, agentID, convID string
		chatID                                string
		kind                                  string
		repliedToID, targetID, causedByID     *string
		role, content                         string
		toolCallsJSON, attachmentsJSON        *string
		toolCallID, toolName                  *string
		isMention                             int
		metadataJSON                          *string
		extUserID, extMsgID, extReplyToID     string
		createdAt                             string
		evicted                               int
	)

	err := rows.Scan(
		&id, &tenantID, &userID, &agentID, &convID, &chatID,
		&kind, &repliedToID, &targetID, &causedByID, &role, &content,
		&toolCallsJSON, &toolCallID, &toolName, &attachmentsJSON,
		&isMention, &metadataJSON,
		&extUserID, &extMsgID, &extReplyToID, &createdAt, &evicted,
	)
	if err != nil {
		return m, fmt.Errorf("scanMessage: %w", err)
	}

	m.ID = entity.MessageID(id)
	m.TenantID = entity.TenantID(tenantID)
	m.UserID = entity.UserID(userID)
	m.AgentID = entity.AgentID(agentID)
	m.ConversationID = entity.ConversationID(convID)
	m.ChatID = entity.ChatID(chatID)
	m.Kind = entity.MessageKind(kind)
	m.Role = entity.Role(role)
	m.Content = content
	m.IsMention = isMention != 0
	m.Evicted = evicted != 0
	m.MessageContext = entity.MessageContext{
		ExternalUserID:    extUserID,
		ExternalMessageID: extMsgID,
		ExternalReplyToID: extReplyToID,
	}

	if repliedToID != nil {
		m.RepliedToID = entity.MessageID(*repliedToID)
	}
	if targetID != nil {
		m.TargetID = entity.MessageID(*targetID)
	}
	if causedByID != nil {
		m.CausedByID = entity.MessageID(*causedByID)
	}
	if toolCallID != nil {
		m.ToolCallID = *toolCallID
	}
	if toolName != nil {
		m.ToolName = *toolName
	}

	// JSON columns.
	if toolCallsJSON != nil {
		if err := json.Unmarshal([]byte(*toolCallsJSON), &m.ToolCalls); err != nil {
			return m, fmt.Errorf("scanMessage unmarshal tool_calls: %w", err)
		}
	}
	if attachmentsJSON != nil {
		if err := json.Unmarshal([]byte(*attachmentsJSON), &m.Attachments); err != nil {
			return m, fmt.Errorf("scanMessage unmarshal attachments: %w", err)
		}
	}

	// Parse created_at with RFC3339 fallback.
	m.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		m.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		if err != nil {
			return m, fmt.Errorf("scanMessage parse created_at: %w", err)
		}
	}

	return m, nil
}

// EvictMessages marks the specified messages as evicted within a single transaction.
// Scoped by tenantID and conversationID. Idempotent: already-evicted or non-existent IDs are silently skipped.
func (p *Plugin) EvictMessages(ctx context.Context, tenantID entity.TenantID, convID entity.ConversationID, msgIDs []entity.MessageID) error {
	if len(msgIDs) == 0 {
		return nil
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("EvictMessages begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	placeholders := make([]string, len(msgIDs))
	args := make([]any, 0, len(msgIDs)+2)
	args = append(args, string(tenantID), string(convID))
	for i, id := range msgIDs {
		placeholders[i] = "?"
		args = append(args, string(id))
	}

	q := `UPDATE messages SET evicted = 1 WHERE tenant_id = ? AND conversation_id = ? AND id IN (` + strings.Join(placeholders, ",") + `)`
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("EvictMessages update: %w", err)
	}
	return tx.Commit()
}

// nilIfEmpty returns nil for empty strings, pointer to s otherwise.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
