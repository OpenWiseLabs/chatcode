package executor

import (
	"context"

	"chatcode/internal/domain"
)

type Executor interface {
	Name() string
	BuildCommand(context.Context, domain.Job) ([]string, error)
}

type SessionStore interface {
	GetExecutorSession(ctx context.Context, executor string, key domain.SessionKey, workdir string) (string, error)
	UpsertExecutorSession(ctx context.Context, executor string, key domain.SessionKey, workdir, sessionID string) error
}

// SessionAware is optional. Executors that support session recovery can
// implement this interface so orchestrator logic stays executor-agnostic.
type SessionAware interface {
	LoadSession(ctx context.Context, job domain.Job) (string, error)
	SaveSession(ctx context.Context, job domain.Job, sessionID string) error
	HandleEvent(ev *domain.StreamEvent) (sessionID string)
}

type Sink interface {
	OnEvent(context.Context, domain.StreamEvent) error
}
