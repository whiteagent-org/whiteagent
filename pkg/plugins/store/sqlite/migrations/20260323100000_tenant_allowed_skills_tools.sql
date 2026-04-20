-- Migration: Move allowed_skills and allowed_tools from agents to tenants.
-- Best-effort data migration: copy values from first agent per tenant.

-- Step 1: Add columns to tenants table.
ALTER TABLE tenants ADD COLUMN allowed_skills TEXT NOT NULL DEFAULT '[]';
ALTER TABLE tenants ADD COLUMN allowed_tools TEXT NOT NULL DEFAULT '[]';

-- Step 2: Best-effort data migration from first agent per tenant.
UPDATE tenants SET
    allowed_tools = COALESCE(
        (SELECT a.allowed_tools FROM agents a WHERE a.tenant_id = tenants.id ORDER BY a.created_at LIMIT 1),
        '[]'
    ),
    allowed_skills = COALESCE(
        (SELECT a.allowed_skills FROM agents a WHERE a.tenant_id = tenants.id ORDER BY a.created_at LIMIT 1),
        '[]'
    );

-- Step 3: Drop columns from agents table.
-- SQLite does not support DROP COLUMN before 3.35.0; recreate table.
CREATE TABLE agents_new (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    name         TEXT NOT NULL,
    instructions TEXT NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

INSERT INTO agents_new (id, tenant_id, name, instructions, created_at)
SELECT id, tenant_id, name, instructions, created_at FROM agents;

DROP TABLE agents;
ALTER TABLE agents_new RENAME TO agents;

CREATE INDEX idx_agents_tenant ON agents(tenant_id);
