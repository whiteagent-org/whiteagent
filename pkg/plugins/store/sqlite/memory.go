package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// GetMemory retrieves a memory entry by tenant, owner type, and owner ID.
// Returns (nil, nil) if no memory exists for the owner.
func (p *Plugin) GetMemory(ctx context.Context, tenantID entity.TenantID, ownerType, ownerID string) (*entity.Memory, error) {
	const q = `
		SELECT tenant_id, owner_type, owner_id, content, updated_at
		FROM memories
		WHERE tenant_id = ? AND owner_type = ? AND owner_id = ?`

	row := p.db.QueryRowContext(ctx, q, string(tenantID), ownerType, ownerID)

	var m entity.Memory
	var tid, updatedAt string
	err := row.Scan(&tid, &m.OwnerType, &m.OwnerID, &m.Content, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetMemory scan: %w", err)
	}
	m.TenantID = entity.TenantID(tid)
	m.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		m.UpdatedAt, err = time.Parse("2006-01-02 15:04:05", updatedAt)
		if err != nil {
			return nil, fmt.Errorf("GetMemory parse updated_at: %w", err)
		}
	}
	return &m, nil
}

// SaveMemory upserts a memory entry by its composite primary key (tenant_id, owner_type, owner_id).
func (p *Plugin) SaveMemory(ctx context.Context, tenantID entity.TenantID, memory entity.Memory) error {
	const q = `
		INSERT INTO memories (tenant_id, owner_type, owner_id, content, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, owner_type, owner_id) DO UPDATE SET
			content    = excluded.content,
			updated_at = excluded.updated_at`

	updatedAt := memory.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	_, err := p.db.ExecContext(ctx, q,
		string(tenantID),
		memory.OwnerType,
		memory.OwnerID,
		memory.Content,
		updatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("SaveMemory: %w", err)
	}
	return nil
}
