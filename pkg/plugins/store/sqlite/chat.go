package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/pkg/util"
)

// SaveChat upserts a chat record. Generates a UUID for ID if empty.
// Uses the unique index on (tenant_id, channel_id, external_chat_id) for conflict resolution.
func (p *Plugin) SaveChat(ctx context.Context, tenantID entity.TenantID, chat entity.Chat) error {
	id := chat.ID.String()
	if id == "" {
		id = util.NewID()
	}

	isGroup := 0
	if chat.IsGroup {
		isGroup = 1
	}

	deliveryJSON, err := json.Marshal(chat.Delivery)
	if err != nil {
		return fmt.Errorf("SaveChat marshal delivery: %w", err)
	}
	indicationJSON, err := json.Marshal(chat.Indication)
	if err != nil {
		return fmt.Errorf("SaveChat marshal indication: %w", err)
	}

	const q = `
		INSERT INTO chats (id, tenant_id, channel_id, external_chat_id, user_id, is_group, name, agent_id, delivery, indication, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, channel_id, external_chat_id) DO UPDATE SET
			delivery   = excluded.delivery,
			indication = excluded.indication,
			user_id    = excluded.user_id,
			name       = excluded.name,
			agent_id   = excluded.agent_id`
	_, err = p.db.ExecContext(ctx, q,
		id,
		string(tenantID),
		chat.ChannelID,
		chat.ExternalChatID,
		string(chat.UserID),
		isGroup,
		chat.Name,
		string(chat.AgentID),
		string(deliveryJSON),
		string(indicationJSON),
		chat.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("SaveChat: %w", err)
	}
	return nil
}

// GetChat retrieves a chat by tenant ID and chat ID.
// Returns (nil, nil) if the chat does not exist.
func (p *Plugin) GetChat(ctx context.Context, tenantID entity.TenantID, chatID entity.ChatID) (*entity.Chat, error) {
	const q = `
		SELECT id, tenant_id, channel_id, external_chat_id, user_id, is_group, name, agent_id, delivery, indication, created_at
		FROM chats
		WHERE id = ? AND tenant_id = ?`
	row := p.db.QueryRowContext(ctx, q, string(chatID), string(tenantID))
	return scanChat(row)
}

// GetChatByChannel looks up a chat by tenant, channel plugin ID, and external chat ID.
// Returns (nil, nil) if the chat is not registered.
func (p *Plugin) GetChatByChannel(ctx context.Context, tenantID entity.TenantID, channelID, externalChatID string) (*entity.Chat, error) {
	const q = `
		SELECT id, tenant_id, channel_id, external_chat_id, user_id, is_group, name, agent_id, delivery, indication, created_at
		FROM chats
		WHERE tenant_id = ? AND channel_id = ? AND external_chat_id = ?`
	row := p.db.QueryRowContext(ctx, q, string(tenantID), channelID, externalChatID)
	return scanChat(row)
}

// GetDMChat retrieves the DM chat for a user within a tenant.
// Returns (nil, nil) if no DM chat exists for the user.
func (p *Plugin) GetDMChat(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) (*entity.Chat, error) {
	const q = `
		SELECT id, tenant_id, channel_id, external_chat_id, user_id, is_group, name, agent_id, delivery, indication, created_at
		FROM chats
		WHERE tenant_id = ? AND user_id = ? AND is_group = 0
		LIMIT 1`
	row := p.db.QueryRowContext(ctx, q, string(tenantID), string(userID))
	return scanChat(row)
}

// SearchChats searches for group chats the user has participated in (has messages in).
// Results are filtered by name using a LIKE query.
func (p *Plugin) SearchChats(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, query string) ([]entity.Chat, error) {
	const q = `
		SELECT DISTINCT c.id, c.tenant_id, c.channel_id, c.external_chat_id, c.user_id, c.is_group, c.name, c.agent_id, c.delivery, c.indication, c.created_at
		FROM chats c
		INNER JOIN messages m ON m.chat_id = c.id AND m.tenant_id = c.tenant_id AND m.user_id = ?
		WHERE c.tenant_id = ? AND c.is_group = 1 AND c.name LIKE ?`
	rows, err := p.db.QueryContext(ctx, q, string(userID), string(tenantID), "%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("SearchChats query: %w", err)
	}
	defer rows.Close()

	chats := make([]entity.Chat, 0)
	for rows.Next() {
		c, err := scanChatRow(rows)
		if err != nil {
			return nil, err
		}
		chats = append(chats, c)
	}
	return chats, rows.Err()
}

// scanChatRow scans a single row from *sql.Rows into entity.Chat.
func scanChatRow(rows *sql.Rows) (entity.Chat, error) {
	var c entity.Chat
	var id, tenantID, userID, agentID, createdAt string
	var deliveryStr, indicationStr string
	var isGroup int

	if err := rows.Scan(&id, &tenantID, &c.ChannelID, &c.ExternalChatID, &userID, &isGroup, &c.Name, &agentID, &deliveryStr, &indicationStr, &createdAt); err != nil {
		return c, fmt.Errorf("scanChatRow: %w", err)
	}

	c.ID = entity.ChatID(id)
	c.TenantID = entity.TenantID(tenantID)
	c.UserID = entity.UserID(userID)
	c.AgentID = entity.AgentID(agentID)
	c.IsGroup = isGroup != 0

	if deliveryStr != "" && deliveryStr != "null" {
		if err := json.Unmarshal([]byte(deliveryStr), &c.Delivery); err != nil {
			return c, fmt.Errorf("scanChatRow unmarshal delivery: %w", err)
		}
	}
	if indicationStr != "" && indicationStr != "null" {
		if err := json.Unmarshal([]byte(indicationStr), &c.Indication); err != nil {
			return c, fmt.Errorf("scanChatRow unmarshal indication: %w", err)
		}
	}

	var err error
	c.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		c.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		if err != nil {
			return c, fmt.Errorf("scanChatRow parse created_at: %w", err)
		}
	}
	return c, nil
}

// scanChat scans a single chat row into entity.Chat.
func scanChat(row *sql.Row) (*entity.Chat, error) {
	var c entity.Chat
	var id, tenantID, userID, agentID, createdAt string
	var deliveryStr, indicationStr string
	var isGroup int

	err := row.Scan(&id, &tenantID, &c.ChannelID, &c.ExternalChatID, &userID, &isGroup, &c.Name, &agentID, &deliveryStr, &indicationStr, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanChat: %w", err)
	}

	c.ID = entity.ChatID(id)
	c.TenantID = entity.TenantID(tenantID)
	c.UserID = entity.UserID(userID)
	c.AgentID = entity.AgentID(agentID)
	c.IsGroup = isGroup != 0

	if deliveryStr != "" && deliveryStr != "null" {
		if err := json.Unmarshal([]byte(deliveryStr), &c.Delivery); err != nil {
			return nil, fmt.Errorf("scanChat unmarshal delivery: %w", err)
		}
	}
	if indicationStr != "" && indicationStr != "null" {
		if err := json.Unmarshal([]byte(indicationStr), &c.Indication); err != nil {
			return nil, fmt.Errorf("scanChat unmarshal indication: %w", err)
		}
	}

	c.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		c.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		if err != nil {
			return nil, fmt.Errorf("scanChat parse created_at: %w", err)
		}
	}
	return &c, nil
}
