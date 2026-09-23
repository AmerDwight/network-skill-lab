CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    locale TEXT NOT NULL DEFAULT 'zh',
    created_at TEXT NOT NULL
);

INSERT INTO users (id, username, password_hash, role, locale, created_at)
VALUES ('01K5S3J8XQZ4NV7B0WGDHM2RCT', 'local', NULL, 'user', 'zh', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

CREATE TABLE attempts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    lab_id TEXT NOT NULL,
    lab_version INTEGER NOT NULL,
    case_id TEXT,
    mode TEXT NOT NULL,
    params_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('provisioning', 'running', 'passed', 'abandoned', 'expired', 'error')),
    error_message TEXT,
    started_at TEXT,
    ended_at TEXT,
    elapsed_ms INTEGER NOT NULL DEFAULT 0,
    runner_id TEXT,
    sandbox_id TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_attempts_user_status ON attempts (user_id, status);

CREATE TABLE checkpoint_runs (
    attempt_id TEXT NOT NULL REFERENCES attempts (id) ON DELETE CASCADE,
    checkpoint_id TEXT NOT NULL,
    first_passed_at TEXT,
    last_status TEXT NOT NULL CHECK (last_status IN ('pending', 'pass', 'fail', 'error')),
    last_run_at TEXT,
    PRIMARY KEY (attempt_id, checkpoint_id)
);

CREATE TABLE command_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    attempt_id TEXT NOT NULL REFERENCES attempts (id) ON DELETE CASCADE,
    node TEXT NOT NULL,
    ts TEXT NOT NULL,
    user TEXT,
    cwd TEXT,
    command TEXT NOT NULL,
    exit_code INTEGER
);

CREATE INDEX idx_command_log_attempt ON command_log (attempt_id);

CREATE TABLE recordings (
    id TEXT PRIMARY KEY,
    attempt_id TEXT NOT NULL REFERENCES attempts (id) ON DELETE CASCADE,
    node TEXT NOT NULL,
    tab_id TEXT NOT NULL,
    path TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at TEXT
);
