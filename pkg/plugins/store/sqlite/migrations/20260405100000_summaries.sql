CREATE TABLE summaries (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    content TEXT NOT NULL,
    message_id TEXT NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE INDEX idx_summaries_conv_created_at
ON summaries(tenant_id, conversation_id, created_at);
