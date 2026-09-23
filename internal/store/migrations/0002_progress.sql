CREATE TABLE progress (
    user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('doc', 'lab')),
    ref TEXT NOT NULL,
    completed_at TEXT NOT NULL,
    PRIMARY KEY (user_id, kind, ref)
);

ALTER TABLE attempts ADD COLUMN submit_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE attempts ADD COLUMN seed INTEGER;
