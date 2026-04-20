-- Consolidated schema for whiteagent SQLite store.
-- Single migration representing the final state of all prior migrations.
-- Drop-and-recreate: destroys all existing data on fresh databases.

--------------------------------------------------------------------------------
-- DROP everything in reverse dependency order (idempotent for fresh DBs)
--------------------------------------------------------------------------------

DROP TRIGGER IF EXISTS messages_au;
DROP TRIGGER IF EXISTS messages_ad;
DROP TRIGGER IF EXISTS messages_ai;
DROP TABLE IF EXISTS messages_fts;

DROP TRIGGER IF EXISTS journal_au;
DROP TRIGGER IF EXISTS journal_ad;
DROP TRIGGER IF EXISTS journal_ai;
DROP TABLE IF EXISTS journal_fts;

DROP TABLE IF EXISTS error_log;
DROP TABLE IF EXISTS cron_runs;
DROP TABLE IF EXISTS cron_entries;
DROP TABLE IF EXISTS secret_tokens;
DROP TABLE IF EXISTS secrets;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS journal;
DROP TABLE IF EXISTS chats;
DROP TABLE IF EXISTS memories;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS invite_codes;
DROP TABLE IF EXISTS tenant_mappings;
DROP TABLE IF EXISTS workspace_mappings;
DROP TABLE IF EXISTS user_identities;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;

--------------------------------------------------------------------------------
-- 1. tenants
--------------------------------------------------------------------------------
CREATE TABLE tenants (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    instructions      TEXT NOT NULL DEFAULT '',
    default_agent_id  TEXT NOT NULL DEFAULT '',
    join_policy       TEXT NOT NULL DEFAULT 'invite_required',
    rejection_message TEXT NOT NULL DEFAULT 'Invalid invite code. Contact your admin.',
    group_mode        TEXT NOT NULL DEFAULT 'mention_only',
    deleted_at        DATETIME,
    created_at        DATETIME NOT NULL
);

--------------------------------------------------------------------------------
-- 2. users
--------------------------------------------------------------------------------
CREATE TABLE users (
    id                TEXT NOT NULL,
    tenant_id         TEXT NOT NULL,
    name              TEXT NOT NULL,
    preferred_channel TEXT NOT NULL DEFAULT '',
    deleted_at        DATETIME,
    created_at        DATETIME NOT NULL,
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX idx_users_tenant ON users(tenant_id);

--------------------------------------------------------------------------------
-- 3. user_identities
--------------------------------------------------------------------------------
CREATE TABLE user_identities (
    tenant_id        TEXT NOT NULL,
    channel_id       TEXT NOT NULL,
    user_external_id TEXT NOT NULL,
    user_id          TEXT NOT NULL,
    PRIMARY KEY (tenant_id, channel_id, user_external_id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id)
);

CREATE INDEX idx_user_identities_tenant ON user_identities(tenant_id, channel_id, user_external_id);

--------------------------------------------------------------------------------
-- 4. invite_codes
--------------------------------------------------------------------------------
CREATE TABLE invite_codes (
    code       TEXT     PRIMARY KEY,
    type       TEXT     NOT NULL DEFAULT 'user',
    tenant_id  TEXT     NOT NULL DEFAULT '',
    target_id  TEXT     NOT NULL DEFAULT '',
    used_by    TEXT     NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    revoked_at DATETIME
);

CREATE INDEX idx_invite_codes_tenant ON invite_codes(tenant_id);
CREATE INDEX idx_invite_codes_type   ON invite_codes(type);

--------------------------------------------------------------------------------
-- 5. tenant_mappings
--------------------------------------------------------------------------------
CREATE TABLE tenant_mappings (
    channel_id         TEXT NOT NULL,
    external_tenant_id TEXT NOT NULL,
    tenant_id          TEXT NOT NULL REFERENCES tenants(id),
    PRIMARY KEY (channel_id, external_tenant_id)
);

CREATE INDEX idx_tenant_mappings_tenant ON tenant_mappings(tenant_id);

--------------------------------------------------------------------------------
-- 6. agents
--------------------------------------------------------------------------------
CREATE TABLE agents (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    name           TEXT NOT NULL,
    instructions   TEXT NOT NULL DEFAULT '',
    allowed_tools  TEXT NOT NULL DEFAULT '[]',
    allowed_skills TEXT NOT NULL DEFAULT '[]',
    created_at     DATETIME NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX idx_agents_tenant ON agents(tenant_id);

--------------------------------------------------------------------------------
-- 7. memories
--------------------------------------------------------------------------------
CREATE TABLE memories (
    tenant_id  TEXT NOT NULL,
    owner_type TEXT NOT NULL,
    owner_id   TEXT NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL,
    PRIMARY KEY (tenant_id, owner_type, owner_id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

--------------------------------------------------------------------------------
-- 8. chats
--------------------------------------------------------------------------------
CREATE TABLE chats (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL,
    channel_id       TEXT NOT NULL,
    external_chat_id TEXT NOT NULL,
    user_id          TEXT NOT NULL DEFAULT '',
    is_group         INTEGER NOT NULL DEFAULT 0,
    name             TEXT NOT NULL DEFAULT '',
    agent_id         TEXT NOT NULL DEFAULT '',
    delivery         TEXT NOT NULL DEFAULT '{}',
    indication       TEXT NOT NULL DEFAULT '{}',
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, channel_id, external_chat_id)
);

CREATE INDEX idx_chats_tenant_id ON chats(tenant_id);

--------------------------------------------------------------------------------
-- 9. messages
--------------------------------------------------------------------------------
CREATE TABLE messages (
    id                   TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL,
    user_id              TEXT NOT NULL DEFAULT '',
    agent_id             TEXT NOT NULL,
    conversation_id      TEXT NOT NULL,
    chat_id              TEXT NOT NULL,
    kind                 TEXT NOT NULL DEFAULT '',
    replied_to_id        TEXT,
    target_id            TEXT,
    caused_by_id         TEXT,
    role                 TEXT NOT NULL,
    content              TEXT NOT NULL DEFAULT '',
    tool_calls           TEXT,
    tool_call_id         TEXT,
    tool_name            TEXT,
    attachments          TEXT,
    is_mention           INTEGER NOT NULL DEFAULT 0,
    metadata             TEXT,
    external_user_id     TEXT NOT NULL DEFAULT '',
    external_message_id  TEXT NOT NULL DEFAULT '',
    external_reply_to_id TEXT NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL
);

CREATE INDEX idx_messages_chat ON messages(chat_id, created_at);
CREATE INDEX idx_messages_conversation ON messages(conversation_id, created_at);
CREATE UNIQUE INDEX idx_messages_dedup ON messages(chat_id, external_message_id) WHERE external_message_id != '';

CREATE VIRTUAL TABLE messages_fts
    USING fts5(content, content='messages', content_rowid='rowid');

CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
END;

CREATE TRIGGER messages_au AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
    INSERT INTO messages_fts(rowid, content) VALUES (new.rowid, new.content);
END;

--------------------------------------------------------------------------------
-- 10. journal
--------------------------------------------------------------------------------
CREATE TABLE journal (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    chat_id         TEXT NOT NULL DEFAULT '',
    conversation_id TEXT NOT NULL,
    category        TEXT NOT NULL,
    content         TEXT NOT NULL,
    message_id      TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL
);

CREATE INDEX idx_journal_user ON journal(tenant_id, user_id);
CREATE INDEX idx_journal_chat ON journal(tenant_id, chat_id);
CREATE INDEX idx_journal_conversation ON journal(tenant_id, conversation_id);

CREATE VIRTUAL TABLE journal_fts
    USING fts5(content, content='journal', content_rowid='rowid');

CREATE TRIGGER journal_ai AFTER INSERT ON journal BEGIN
    INSERT INTO journal_fts(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TRIGGER journal_ad AFTER DELETE ON journal BEGIN
    INSERT INTO journal_fts(journal_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
END;

CREATE TRIGGER journal_au AFTER UPDATE ON journal BEGIN
    INSERT INTO journal_fts(journal_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
    INSERT INTO journal_fts(rowid, content) VALUES (new.rowid, new.content);
END;

--------------------------------------------------------------------------------
-- 11. cron_entries
--------------------------------------------------------------------------------
CREATE TABLE cron_entries (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    agent_id        TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    chat_id         TEXT NOT NULL DEFAULT '',
    is_group        INTEGER NOT NULL DEFAULT 0,
    name            TEXT NOT NULL DEFAULT '',
    instructions    TEXT NOT NULL DEFAULT '',
    type            TEXT NOT NULL DEFAULT 'recurring',
    cron_expr       TEXT NOT NULL DEFAULT '',
    next_run_at     DATETIME,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      DATETIME NOT NULL,
    metadata        TEXT NOT NULL DEFAULT '{}',
    conversation_id TEXT NOT NULL DEFAULT '',
    message_id      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_cron_entries_tenant_user ON cron_entries(tenant_id, user_id);
CREATE INDEX idx_cron_entries_status ON cron_entries(status, next_run_at);

--------------------------------------------------------------------------------
-- 12. cron_runs
--------------------------------------------------------------------------------
CREATE TABLE cron_runs (
    id            TEXT PRIMARY KEY,
    cron_entry_id TEXT NOT NULL,
    tenant_id     TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT '',
    started_at    DATETIME NOT NULL,
    finished_at   DATETIME
);

CREATE INDEX idx_cron_runs_entry ON cron_runs(cron_entry_id, started_at DESC);

--------------------------------------------------------------------------------
-- 13. secrets
--------------------------------------------------------------------------------
CREATE TABLE secrets (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    user_id         TEXT NOT NULL DEFAULT '',
    key             TEXT NOT NULL,
    encrypted_value BLOB NOT NULL,
    scope           TEXT NOT NULL DEFAULT 'user',
    created_at      DATETIME,
    updated_at      DATETIME,
    UNIQUE(tenant_id, user_id, key)
);

--------------------------------------------------------------------------------
-- 14. secret_tokens
--------------------------------------------------------------------------------
CREATE TABLE secret_tokens (
    token_id        TEXT PRIMARY KEY,
    keys            TEXT NOT NULL,
    tenant_id       TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    conversation_id TEXT NOT NULL DEFAULT '',
    chat_id         TEXT NOT NULL DEFAULT '',
    expires_at      DATETIME NOT NULL,
    used            INTEGER NOT NULL DEFAULT 0
);

--------------------------------------------------------------------------------
-- 15. error_log
--------------------------------------------------------------------------------
CREATE TABLE error_log (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    ref_type   TEXT NOT NULL DEFAULT '',
    ref_id     TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL
);

CREATE INDEX idx_error_log_user ON error_log(tenant_id, user_id, created_at DESC);
CREATE INDEX idx_error_log_ref ON error_log(tenant_id, ref_type, ref_id);

--------------------------------------------------------------------------------
-- Clean up old schema_migrations entries from prior migration files.
--------------------------------------------------------------------------------
DELETE FROM schema_migrations WHERE version != '20260322000000_consolidated_schema.sql';
