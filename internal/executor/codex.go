package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"

	"chatcode/internal/domain"
)

type CodexExecutor struct {
	Binary       string
	SessionStore SessionStore
}

const (
	commandTruncateLimit = 800
)

var codexThreadIDRegex = regexp.MustCompile(`(?i)thread[_ ]id:\s*([0-9a-f-]{36})`)

func (e CodexExecutor) Name() string { return "codex" }

func (e CodexExecutor) BuildCommand(_ context.Context, job domain.Job) ([]string, error) {
	if e.Binary == "" {
		return nil, fmt.Errorf("codex binary is empty")
	}
	sandboxMode := "workspace-write"
	if domain.NormalizePermissionMode(job.PermissionMode) == domain.PermissionModeFullAccess {
		sandboxMode = "danger-full-access"
	}
	if job.Session != "" {
		return []string{
			e.Binary,
			"--full-auto",
			"--sandbox",
			sandboxMode,
			"exec",
			"--json",
			"--skip-git-repo-check",
			"resume",
			job.Session,
			job.Prompt,
		}, nil
	}
	return []string{
		e.Binary,
		"--full-auto",
		"--sandbox",
		sandboxMode,
		"exec",
		"--json",
		"--skip-git-repo-check",
		job.Prompt,
	}, nil
}

func (e CodexExecutor) LoadSession(ctx context.Context, job domain.Job) (string, error) {
	if e.SessionStore == nil {
		return "", fmt.Errorf("codex session store is required")
	}
	sessionID, err := e.SessionStore.GetExecutorSession(ctx, e.Name(), job.SessionKey, job.Workdir)
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

func (e CodexExecutor) SaveSession(ctx context.Context, job domain.Job, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if e.SessionStore == nil {
		return fmt.Errorf("codex session store is required")
	}
	return e.SessionStore.UpsertExecutorSession(ctx, e.Name(), job.SessionKey, job.Workdir, sessionID)
}

func (e CodexExecutor) HandleEvent(ev *domain.StreamEvent) string {
	sessionID, text, format, ok := parseCodexJSONEvent(ev.Chunk)
	if ok {
		ev.Chunk = text
		ev.Format = format
	} else {
		ev.Chunk = ""
		return ""
	}
	if sessionID != "" {
		return sessionID
	}
	return extractSessionIDByRegex(ev.Chunk, codexThreadIDRegex, 1)
}

func parseCodexJSONEvent(chunk string) (sessionID, text, format string, ok bool) {
	line := strings.TrimSpace(chunk)
	if !strings.HasPrefix(line, "{") {
		return "", "", "", false
	}
	var ev codexJSONEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return "", "", "", false
	}
	sessionID = extractCodexSessionID(ev)
	text, format = extractCodexEventText(ev)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return sessionID, text, format, true
}

func extractCodexSessionID(ev codexJSONEvent) string {
	for _, v := range []string{ev.ThreadID, ev.SessionID} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if ev.Session != nil && strings.TrimSpace(ev.Session.ID) != "" {
		return strings.TrimSpace(ev.Session.ID)
	}
	return ""
}

func extractCodexEventText(ev codexJSONEvent) (string, string) {
	switch ev.Type {
	case "error":
		return ev.Message, ""
	case "turn.failed":
		if ev.Error != nil && ev.Error.Message != "" {
			return ev.Error.Message, ""
		}
		return ev.Message, ""
	case "item.completed":
		if ev.Item == nil {
			return "", ""
		}
		switch ev.Item.Type {
		case "agent_message", "reasoning":
			return ev.Item.Text, ""
		case "command_execution":
			return formatCommandExecutionHTML(ev.Item.Command, ev.Item.AggregatedOutput), "html"
		}
	}
	if ev.Message != "" {
		return ev.Message, ""
	}
	return "", ""
}

type codexJSONEvent struct {
	Type      string            `json:"type"`
	ThreadID  string            `json:"thread_id"`
	SessionID string            `json:"session_id"`
	Message   string            `json:"message"`
	Error     *codexJSONError   `json:"error"`
	Item      *codexJSONItem    `json:"item"`
	Session   *codexJSONSession `json:"session"`
}

type codexJSONError struct {
	Message string `json:"message"`
}

type codexJSONSession struct {
	ID string `json:"id"`
}

type codexJSONItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	Text             string `json:"text"`
	Command          string `json:"command"`
	AggregatedOutput string `json:"aggregated_output"`
}

func formatCommandExecutionHTML(cmd, out string) string {
	cmd = strings.TrimSpace(cmd)
	out = strings.TrimSpace(out)
	cmd = truncateCommandForDisplay(cmd)
	if cmd == "" && out == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("<b>command_execution</b>")
	if cmd != "" {
		b.WriteString("\n<code>")
		b.WriteString(html.EscapeString(cmd))
		b.WriteString("</code>")
	}
	if out != "" {
		b.WriteString("\n<pre>")
		b.WriteString(html.EscapeString(out))
		b.WriteString("</pre>\n")
	}
	return b.String()
}

func truncateCommandForDisplay(cmd string) string {
	if len(cmd) <= commandTruncateLimit {
		return cmd
	}
	lines := strings.Split(cmd, "\n")
	if len(lines) <= 6 {
		return cmd
	}
	head := strings.Join(lines[:3], "\n")
	tail := strings.Join(lines[len(lines)-3:], "\n")
	return head + "\n ...... [truncated] ...... \n\n" + tail
}
