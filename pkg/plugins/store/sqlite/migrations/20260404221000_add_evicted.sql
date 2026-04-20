ALTER TABLE messages ADD COLUMN evicted INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_messages_not_evicted ON messages(evicted) WHERE evicted=0;
