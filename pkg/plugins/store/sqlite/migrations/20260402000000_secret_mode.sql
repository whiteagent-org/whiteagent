-- Add mode column to secrets table for value/file mode support.
ALTER TABLE secrets ADD COLUMN mode TEXT NOT NULL DEFAULT 'value';

-- Add modes column to secret_tokens table for per-key mode hints.
ALTER TABLE secret_tokens ADD COLUMN modes TEXT NOT NULL DEFAULT '{}';
