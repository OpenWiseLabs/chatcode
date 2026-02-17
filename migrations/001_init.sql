CREATE TABLE IF NOT EXISTS sessions (
    session_key TEXT PRIMARY KEY,
    platform TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    thread_id TEXT NOT NULL DEFAULT '',
    workdir TEXT NOT NULL DEFAULT '',
    context_json TEXT NOT NULL DEFAULT '{}',
    updated_at DATETIME NOT NULL,
    expires_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    session_key TEXT NOT NULL,
    executor TEXT NOT NULL,
    prompt TEXT NOT NULL,
    workdir TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    started_at DATETIME,
    finished_at DATETIME,
    error_message TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(session_key) REFERENCES sessions(session_key)
);

CREATE INDEX IF NOT EXISTS idx_jobs_session_key ON jobs(session_key);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    chunk TEXT NOT NULL,
    stream TEXT NOT NULL,
    is_final BOOLEAN NOT NULL DEFAULT 0,
    ts DATETIME NOT NULL,
    exit_code INTEGER,
    FOREIGN KEY(job_id) REFERENCES jobs(id)
);

CREATE INDEX IF NOT EXISTS idx_events_job_id_seq ON events(job_id, seq);

CREATE TABLE IF NOT EXISTS executor_sessions (
    executor TEXT NOT NULL,
    platform TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    thread_id TEXT NOT NULL DEFAULT '',
    workdir TEXT NOT NULL,
    session_id TEXT NOT NULL,
    updated_at DATETIME NOT NULL,
    PRIMARY KEY (executor, platform, chat_id, thread_id, workdir)
);

CREATE INDEX IF NOT EXISTS idx_executor_sessions_updated_at ON executor_sessions(updated_at);
