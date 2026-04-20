package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// GetTenant retrieves a tenant by ID, excluding soft-deleted tenants.
// Returns (nil, nil) if the tenant does not exist — caller decides how to handle missing tenant.
func (p *Plugin) GetTenant(ctx context.Context, tenantID entity.TenantID) (*entity.Tenant, error) {
	const q = `
        SELECT id, name, instructions, default_agent_id, join_policy, rejection_message, group_mode, allowed_skills, allowed_tools, created_at
        FROM tenants
        WHERE id = ? AND deleted_at IS NULL`
	row := p.db.QueryRowContext(ctx, q, string(tenantID))

	var t entity.Tenant
	var skillsJSON, toolsJSON, createdAt string
	err := row.Scan(&t.ID, &t.Name, &t.Instructions, &t.DefaultAgentID, &t.JoinPolicy, &t.RejectionMessage, &t.GroupMode, &skillsJSON, &toolsJSON, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetTenant scan: %w", err)
	}
	if err := json.Unmarshal([]byte(skillsJSON), &t.AllowedSkills); err != nil {
		return nil, fmt.Errorf("GetTenant unmarshal allowed_skills: %w", err)
	}
	if err := json.Unmarshal([]byte(toolsJSON), &t.AllowedTools); err != nil {
		return nil, fmt.Errorf("GetTenant unmarshal allowed_tools: %w", err)
	}
	t.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		// SQLite datetime() returns "2006-01-02 15:04:05" format without T separator.
		t.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		if err != nil {
			return nil, fmt.Errorf("GetTenant parse created_at: %w", err)
		}
	}
	return &t, nil
}

// ListTenants returns all non-deleted tenants in the store.
// Returns an empty slice (not nil) if no tenants exist.
func (p *Plugin) ListTenants(ctx context.Context) ([]entity.Tenant, error) {
	const q = `SELECT id, name, instructions, default_agent_id, join_policy, rejection_message, group_mode, allowed_skills, allowed_tools, created_at
        FROM tenants WHERE deleted_at IS NULL`
	rows, err := p.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ListTenants query: %w", err)
	}
	defer rows.Close()

	tenants := make([]entity.Tenant, 0)
	for rows.Next() {
		var t entity.Tenant
		var skillsJSON, toolsJSON, createdAt string
		if err := rows.Scan(&t.ID, &t.Name, &t.Instructions, &t.DefaultAgentID, &t.JoinPolicy, &t.RejectionMessage, &t.GroupMode, &skillsJSON, &toolsJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("ListTenants scan: %w", err)
		}
		if err := json.Unmarshal([]byte(skillsJSON), &t.AllowedSkills); err != nil {
			return nil, fmt.Errorf("ListTenants unmarshal allowed_skills: %w", err)
		}
		if err := json.Unmarshal([]byte(toolsJSON), &t.AllowedTools); err != nil {
			return nil, fmt.Errorf("ListTenants unmarshal allowed_tools: %w", err)
		}
		t.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			t.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
			if err != nil {
				return nil, fmt.Errorf("ListTenants parse created_at: %w", err)
			}
		}
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListTenants rows: %w", err)
	}
	return tenants, nil
}

// SaveTenant upserts a tenant record. tenantID is explicit (TNNT-02).
// Updates name, instructions, default_agent_id, join_policy, rejection_message,
// allowed_skills, and allowed_tools on conflict.
func (p *Plugin) SaveTenant(ctx context.Context, tenantID entity.TenantID, tenant entity.Tenant) error {
	skillsJSON, err := json.Marshal(tenant.AllowedSkills)
	if err != nil {
		return fmt.Errorf("SaveTenant marshal allowed_skills: %w", err)
	}
	if tenant.AllowedSkills == nil {
		skillsJSON = []byte("[]")
	}
	toolsJSON, err := json.Marshal(tenant.AllowedTools)
	if err != nil {
		return fmt.Errorf("SaveTenant marshal allowed_tools: %w", err)
	}
	if tenant.AllowedTools == nil {
		toolsJSON = []byte("[]")
	}

	const q = `
        INSERT INTO tenants (id, name, instructions, default_agent_id, join_policy, rejection_message, group_mode, allowed_skills, allowed_tools, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name              = excluded.name,
            instructions      = excluded.instructions,
            default_agent_id  = excluded.default_agent_id,
            join_policy       = excluded.join_policy,
            rejection_message = excluded.rejection_message,
            group_mode        = excluded.group_mode,
            allowed_skills    = excluded.allowed_skills,
            allowed_tools     = excluded.allowed_tools`
	_, err = p.db.ExecContext(ctx, q,
		string(tenantID),
		tenant.Name,
		tenant.Instructions,
		string(tenant.DefaultAgentID),
		tenant.JoinPolicy,
		tenant.RejectionMessage,
		tenant.GroupMode,
		string(skillsJSON),
		string(toolsJSON),
		tenant.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("SaveTenant: %w", err)
	}
	return nil
}

// GetAgent retrieves an agent by tenant ID and agent ID.
// Returns (nil, nil) if the agent does not exist.
func (p *Plugin) GetAgent(ctx context.Context, tenantID entity.TenantID, agentID entity.AgentID) (*entity.Agent, error) {
	const q = `
        SELECT id, tenant_id, name, instructions, created_at
        FROM agents
        WHERE tenant_id = ? AND id = ?`
	row := p.db.QueryRowContext(ctx, q, string(tenantID), string(agentID))

	var a entity.Agent
	var tid, aid, createdAt string
	err := row.Scan(&aid, &tid, &a.Name, &a.Instructions, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetAgent scan: %w", err)
	}
	a.ID = entity.AgentID(aid)
	a.TenantID = entity.TenantID(tid)

	a.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		a.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		if err != nil {
			return nil, fmt.Errorf("GetAgent parse created_at: %w", err)
		}
	}
	return &a, nil
}

// SaveAgent upserts an agent record. tenantID is explicit (TNNT-02).
func (p *Plugin) SaveAgent(ctx context.Context, tenantID entity.TenantID, agent entity.Agent) error {
	const q = `
        INSERT INTO agents (id, tenant_id, name, instructions, created_at)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name         = excluded.name,
            instructions = excluded.instructions`
	_, err := p.db.ExecContext(ctx, q,
		string(agent.ID),
		string(tenantID),
		agent.Name,
		agent.Instructions,
		agent.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("SaveAgent: %w", err)
	}
	return nil
}

// ListAgents returns all agents for a tenant ordered by creation time.
// Returns an empty slice (not nil) if no agents exist.
func (p *Plugin) ListAgents(ctx context.Context, tenantID entity.TenantID) ([]entity.Agent, error) {
	const q = `
        SELECT id, tenant_id, name, instructions, created_at
        FROM agents
        WHERE tenant_id = ?
        ORDER BY created_at`
	rows, err := p.db.QueryContext(ctx, q, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("ListAgents query: %w", err)
	}
	defer rows.Close()

	agents := make([]entity.Agent, 0)
	for rows.Next() {
		var a entity.Agent
		var tid, aid, createdAt string
		if err := rows.Scan(&aid, &tid, &a.Name, &a.Instructions, &createdAt); err != nil {
			return nil, fmt.Errorf("ListAgents scan: %w", err)
		}
		a.ID = entity.AgentID(aid)
		a.TenantID = entity.TenantID(tid)

		a.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			a.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
			if err != nil {
				return nil, fmt.Errorf("ListAgents parse created_at: %w", err)
			}
		}
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListAgents rows: %w", err)
	}
	return agents, nil
}

// GetUserByChannel looks up a user by their external identity on a specific channel.
// Joins user_identities to users for an indexed lookup.
// Returns (nil, nil) if no user is found.
func (p *Plugin) GetUserByChannel(ctx context.Context, tenantID entity.TenantID, channelID, userExternalID string) (*entity.User, error) {
	const q = `
        SELECT u.id, u.tenant_id, u.name, u.preferred_channel, u.created_at
        FROM users u
        JOIN user_identities ci
            ON ci.tenant_id = u.tenant_id AND ci.user_id = u.id
        WHERE ci.tenant_id = ?
          AND ci.channel_id = ?
          AND ci.user_external_id = ?
          AND u.deleted_at IS NULL`
	row := p.db.QueryRowContext(ctx, q, string(tenantID), channelID, userExternalID)
	return scanUser(row)
}

// SaveUser upserts the user record.
func (p *Plugin) SaveUser(ctx context.Context, tenantID entity.TenantID, user entity.User) error {
	const q = `
        INSERT INTO users (id, tenant_id, name, preferred_channel, created_at)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(tenant_id, id) DO UPDATE SET
            name              = excluded.name,
            preferred_channel = excluded.preferred_channel`
	_, err := p.db.ExecContext(ctx, q,
		string(user.ID),
		string(tenantID),
		user.Name,
		user.PreferredChannel,
		user.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("SaveUser: %w", err)
	}
	return nil
}

// scanUser scans a users row into an entity.User.
// Returns (nil, nil) on sql.ErrNoRows.
func scanUser(row *sql.Row) (*entity.User, error) {
	var u entity.User
	var tenantID, userID, createdAt string
	err := row.Scan(&userID, &tenantID, &u.Name, &u.PreferredChannel, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanUser: %w", err)
	}
	u.ID = entity.UserID(userID)
	u.TenantID = entity.TenantID(tenantID)

	u.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		u.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		if err != nil {
			return nil, fmt.Errorf("scanUser parse created_at: %w", err)
		}
	}
	return &u, nil
}

// DeleteTenant soft-deletes a tenant by setting deleted_at.
func (p *Plugin) DeleteTenant(ctx context.Context, tenantID entity.TenantID) error {
	const q = `UPDATE tenants SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL`
	_, err := p.db.ExecContext(ctx, q, string(tenantID))
	if err != nil {
		return fmt.Errorf("DeleteTenant: %w", err)
	}
	return nil
}

// DeleteUser soft-deletes a user by setting deleted_at.
func (p *Plugin) DeleteUser(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) error {
	const q = `UPDATE users SET deleted_at = datetime('now') WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`
	_, err := p.db.ExecContext(ctx, q, string(tenantID), string(userID))
	if err != nil {
		return fmt.Errorf("DeleteUser: %w", err)
	}
	return nil
}

// ListUsers returns all non-deleted users for a tenant.
// Returns an empty slice (not nil) if no users exist.
func (p *Plugin) ListUsers(ctx context.Context, tenantID entity.TenantID) ([]entity.User, error) {
	const q = `
        SELECT id, tenant_id, name, preferred_channel, created_at
        FROM users
        WHERE tenant_id = ? AND deleted_at IS NULL`
	rows, err := p.db.QueryContext(ctx, q, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("ListUsers query: %w", err)
	}
	defer rows.Close()

	users := make([]entity.User, 0)
	for rows.Next() {
		var u entity.User
		var tenantID, userID, createdAt string
		if err := rows.Scan(&userID, &tenantID, &u.Name, &u.PreferredChannel, &createdAt); err != nil {
			return nil, fmt.Errorf("ListUsers scan: %w", err)
		}
		u.ID = entity.UserID(userID)
		u.TenantID = entity.TenantID(tenantID)

		u.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			u.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
			if err != nil {
				return nil, fmt.Errorf("ListUsers parse created_at: %w", err)
			}
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListUsers rows: %w", err)
	}
	return users, nil
}

// GetUser retrieves a user by tenant ID and user ID.
// Returns (nil, nil) if the user does not exist or is soft-deleted.
func (p *Plugin) GetUser(ctx context.Context, tenantID entity.TenantID, userID entity.UserID) (*entity.User, error) {
	const q = `SELECT id, tenant_id, name, preferred_channel, created_at
        FROM users WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`
	row := p.db.QueryRowContext(ctx, q, string(tenantID), string(userID))
	return scanUser(row)
}

// SearchUsers performs a case-insensitive LIKE search on user names within a tenant.
// Returns an empty slice (not nil) if no users match.
func (p *Plugin) SearchUsers(ctx context.Context, tenantID entity.TenantID, query string) ([]entity.User, error) {
	const q = `
        SELECT id, tenant_id, name, preferred_channel, created_at
        FROM users
        WHERE tenant_id = ? AND deleted_at IS NULL AND name LIKE ?`
	rows, err := p.db.QueryContext(ctx, q, string(tenantID), "%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("SearchUsers query: %w", err)
	}
	defer rows.Close()

	users := make([]entity.User, 0)
	for rows.Next() {
		var u entity.User
		var tid, uid, createdAt string
		if err := rows.Scan(&uid, &tid, &u.Name, &u.PreferredChannel, &createdAt); err != nil {
			return nil, fmt.Errorf("SearchUsers scan: %w", err)
		}
		u.ID = entity.UserID(uid)
		u.TenantID = entity.TenantID(tid)

		u.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			u.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
			if err != nil {
				return nil, fmt.Errorf("SearchUsers parse created_at: %w", err)
			}
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SearchUsers rows: %w", err)
	}
	return users, nil
}

// GetExternalID returns the external user ID for a given user on a specific channel.
// Returns empty string (not error) if no identity exists.
func (p *Plugin) GetExternalID(ctx context.Context, tenantID entity.TenantID, userID entity.UserID, channelID string) (string, error) {
	const q = `SELECT user_external_id FROM user_identities
        WHERE tenant_id = ? AND user_id = ? AND channel_id = ?`
	var extID string
	err := p.db.QueryRowContext(ctx, q, string(tenantID), string(userID), channelID).Scan(&extID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("GetExternalID: %w", err)
	}
	return extID, nil
}

// AddUserIdentity upserts a user identity mapping for a user.
func (p *Plugin) AddUserIdentity(ctx context.Context, tenantID entity.TenantID, channelID, externalID string, userID entity.UserID) error {
	const q = `INSERT INTO user_identities (tenant_id, channel_id, user_external_id, user_id)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(tenant_id, channel_id, user_external_id) DO UPDATE SET
            user_id = excluded.user_id`
	_, err := p.db.ExecContext(ctx, q, string(tenantID), channelID, externalID, string(userID))
	if err != nil {
		return fmt.Errorf("AddUserIdentity: %w", err)
	}
	return nil
}
