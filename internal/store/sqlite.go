package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"chatcode/internal/domain"
)

const initSQL = `
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
    error_message TEXT NOT NULL DEFAULT ''
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
    exit_code INTEGER
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
`

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &SQLiteStore{db: db}
	if err := s.Migrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, initSQL); err != nil {
		return fmt.Errorf("run migration: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpsertSession(ctx context.Context, key domain.SessionKey, workdir string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO sessions(session_key, platform, chat_id, thread_id, workdir, updated_at, expires_at)
	VALUES(?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(session_key) DO UPDATE SET
	workdir=excluded.workdir,
	updated_at=excluded.updated_at,
	expires_at=excluded.expires_at`,
		key.String(), string(key.Platform), key.ChatID, key.ThreadID, workdir, time.Now().UTC(), expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) SessionWorkdir(ctx context.Context, key domain.SessionKey) (string, error) {
	var workdir string
	err := s.db.QueryRowContext(ctx, `SELECT workdir FROM sessions WHERE session_key = ?`, key.String()).Scan(&workdir)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get session workdir: %w", err)
	}
	return workdir, nil
}

func (s *SQLiteStore) CreateJob(ctx context.Context, job domain.Job) error {
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO jobs(id, session_key, executor, prompt, workdir, status, created_at, error_message)
	VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.SessionKey.String(), job.Executor, job.Prompt, job.Workdir, job.Status, job.CreatedAt.UTC(), job.ErrorMessage)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateJobStatus(ctx context.Context, jobID string, status domain.JobStatus, startedAt, finishedAt *time.Time, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
	UPDATE jobs SET status=?, started_at=?, finished_at=?, error_message=? WHERE id=?`,
		status, startedAt, finishedAt, errMsg, jobID)
	if err != nil {
		return fmt.Errorf("update job status: %w", err)
	}
	return nil
}

func (s *SQLiteStore) AppendEvent(ctx context.Context, ev domain.StreamEvent) error {
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO events(job_id, seq, chunk, stream, is_final, ts, exit_code)
	VALUES(?, ?, ?, ?, ?, ?, ?)`,
		ev.JobID, ev.Seq, ev.Chunk, ev.Stream, ev.IsFinal, ev.TS.UTC(), ev.ExitCode)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetExecutorSession(ctx context.Context, executor string, key domain.SessionKey, workdir string) (string, error) {
	var sessionID string
	err := s.db.QueryRowContext(ctx, `
	SELECT session_id FROM executor_sessions
	WHERE executor=? AND platform=? AND chat_id=? AND thread_id=? AND workdir=?`,
		executor, string(key.Platform), key.ChatID, key.ThreadID, workdir).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get executor session: %w", err)
	}
	return sessionID, nil
}

func (s *SQLiteStore) UpsertExecutorSession(ctx context.Context, executor string, key domain.SessionKey, workdir, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO executor_sessions(executor, platform, chat_id, thread_id, workdir, session_id, updated_at)
	VALUES(?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(executor, platform, chat_id, thread_id, workdir) DO UPDATE SET
	session_id=excluded.session_id,
	updated_at=excluded.updated_at`,
		executor, string(key.Platform), key.ChatID, key.ThreadID, workdir, sessionID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("upsert executor session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) SessionPermissionMode(ctx context.Context, key domain.SessionKey) (string, error) {
	var contextJSON string
	err := s.db.QueryRowContext(ctx, `SELECT context_json FROM sessions WHERE session_key = ?`, key.String()).Scan(&contextJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PermissionModeSandbox, nil
	}
	if err != nil {
		return "", fmt.Errorf("get session permission mode: %w", err)
	}
	if strings.TrimSpace(contextJSON) == "" {
		return domain.PermissionModeSandbox, nil
	}
	var payload struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal([]byte(contextJSON), &payload); err != nil {
		return "", fmt.Errorf("decode session context_json: %w", err)
	}
	return domain.NormalizePermissionMode(payload.Mode), nil
}

func (s *SQLiteStore) SetSessionPermissionMode(ctx context.Context, key domain.SessionKey, mode string, expiresAt time.Time) error {
	mode = domain.NormalizePermissionMode(mode)

	var workdir string
	var contextJSON string
	err := s.db.QueryRowContext(ctx, `SELECT workdir, context_json FROM sessions WHERE session_key = ?`, key.String()).Scan(&workdir, &contextJSON)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("load session for permission mode update: %w", err)
	}

	payload := map[string]any{}
	if strings.TrimSpace(contextJSON) != "" {
		if err := json.Unmarshal([]byte(contextJSON), &payload); err != nil {
			return fmt.Errorf("decode session context_json: %w", err)
		}
	}
	payload["mode"] = mode
	contextBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode session context_json: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
	INSERT INTO sessions(session_key, platform, chat_id, thread_id, workdir, context_json, updated_at, expires_at)
	VALUES(?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(session_key) DO UPDATE SET
	context_json=excluded.context_json,
	updated_at=excluded.updated_at,
	expires_at=excluded.expires_at`,
		key.String(), string(key.Platform), key.ChatID, key.ThreadID, workdir, string(contextBytes), time.Now().UTC(), expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("set session permission mode: %w", err)
	}
	return nil
}
