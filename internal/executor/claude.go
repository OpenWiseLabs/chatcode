package executor

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"chatcode/internal/domain"
)

type ClaudeExecutor struct {
	Binary       string
	SessionStore SessionStore
}

var claudeSessionRegex = regexp.MustCompile(`(?i)(session id|conversation id):\s*([0-9a-f-]{36})`)

func (e ClaudeExecutor) Name() string { return "claude" }

func (e ClaudeExecutor) BuildCommand(_ context.Context, job domain.Job) ([]string, error) {
	if e.Binary == "" {
		return nil, fmt.Errorf("claude binary is empty")
	}
	if job.Session != "" {
		return []string{e.Binary, "--resume", job.Session, "-p", job.Prompt}, nil
	}
	return []string{e.Binary, "-p", job.Prompt}, nil
}

func (e ClaudeExecutor) LoadSession(ctx context.Context, job domain.Job) (string, error) {
	if e.SessionStore == nil {
		return "", fmt.Errorf("claude session store is required")
	}
	sessionID, err := e.SessionStore.GetExecutorSession(ctx, e.Name(), job.SessionKey, job.Workdir)
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

func (e ClaudeExecutor) SaveSession(ctx context.Context, job domain.Job, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if e.SessionStore == nil {
		return fmt.Errorf("claude session store is required")
	}
	return e.SessionStore.UpsertExecutorSession(ctx, e.Name(), job.SessionKey, job.Workdir, sessionID)
}

func (e ClaudeExecutor) HandleEvent(ev *domain.StreamEvent) string {
	return extractSessionIDByRegex(ev.Chunk, claudeSessionRegex, 2)
}
