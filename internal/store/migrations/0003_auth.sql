ALTER TABLE users ADD COLUMN disabled_at TEXT;

ALTER TABLE users ADD COLUMN updated_at TEXT;

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    user_agent TEXT
);

CREATE INDEX idx_sessions_user ON sessions (user_id);

ALTER TABLE recordings ADD COLUMN bytes INTEGER;

UPDATE users
SET disabled_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE username = 'local' AND disabled_at IS NULL;
