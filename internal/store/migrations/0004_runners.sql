CREATE TABLE runners (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    token_hash TEXT NOT NULL,
    capabilities TEXT NOT NULL,
    capacity INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    last_seen_at TEXT,
    revoked_at TEXT,
    version TEXT
);
