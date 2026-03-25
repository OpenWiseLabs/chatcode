package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"

	"chatcode/internal/domain"
)

type ClaudeExecutor struct {
	Binary       string
	SessionStore SessionStore
}

func (e ClaudeExecutor) Name() string { return "claude" }

func (e ClaudeExecutor) BuildCommand(_ context.Context, job domain.Job) ([]string, error) {
	if e.Binary == "" {
		return nil, fmt.Errorf("claude binary is empty")
	}
	sandboxMode := "bypassPermissions"
	if job.Session != "" {
		return []string{e.Binary, "--output-format", "stream-json", "--verbose", "--permission-mode", sandboxMode, "--resume", job.Session, "-p", job.Prompt}, nil
	}
	return []string{e.Binary, "--output-format", "stream-json", "--verbose", "--permission-mode", sandboxMode, "-p", job.Prompt}, nil
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

// IsSuccessExitCode treats exit code 1 as non-fatal. Claude CLI in stream-json
// mode exits with 1 when a tool call fails during execution even though a valid
// result event was already emitted. Exit code 2 (usage error) is still fatal.
func (e ClaudeExecutor) IsSuccessExitCode(code int) bool {
	return code == 0 || code == 1
}

func (e ClaudeExecutor) HandleEvent(ev *domain.StreamEvent) string {
	if ev.Stream != "stdout" {
		// stderr and meta events are passed through unchanged so that error
		// messages from the Claude CLI remain visible to the user.
		return ""
	}
	sessionID, text, format, ok := parseClaudeJSONEvent(ev.Chunk)
	if ok {
		ev.Chunk = text
		ev.Format = format
	} else {
		ev.Chunk = ""
		return ""
	}
	return sessionID
}

func parseClaudeJSONEvent(chunk string) (sessionID, text, format string, ok bool) {
	line := strings.TrimSpace(chunk)
	if !strings.HasPrefix(line, "{") {
		return "", "", "", false
	}
	var ev claudeJSONEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return "", "", "", false
	}
	switch ev.Type {
	case "system":
		if ev.Subtype == "init" {
			return ev.SessionID, "", "", true
		}
	case "result":
		sid := ev.SessionID
		if ev.Subtype == "error" && ev.Error != "" {
			return sid, ev.Error + "\n", "", true
		}
		return sid, "", "", true
	case "assistant":
		if ev.Message != nil {
			text, format = extractClaudeMessageText(ev.Message)
		}
		return "", text, format, true
	}
	return "", "", "", true
}

func extractClaudeMessageText(msg *claudeJSONMessage) (text, format string) {
	hasHTML := false
	for _, block := range msg.Content {
		if block.Type == "tool_use" {
			hasHTML = true
			break
		}
	}
	var parts []string
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			if t := strings.TrimSpace(block.Text); t != "" {
				if hasHTML {
					parts = append(parts, html.EscapeString(t))
				} else {
					parts = append(parts, t)
				}
			}
		case "tool_use":
			if s := formatClaudeToolUse(block.Name, block.Input); s != "" {
				parts = append(parts, s)
			}
		}
	}
	result := strings.Join(parts, "\n")
	if result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	if hasHTML {
		return result, "html"
	}
	return result, ""
}

const toolDisplayLimit = 80

func truncateDetail(s string) string {
	if len(s) > toolDisplayLimit {
		return s[:toolDisplayLimit] + "…"
	}
	return s
}

func extractJSONStr(fields map[string]json.RawMessage, key string) string {
	v, ok := fields[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return strings.Trim(string(v), `"`)
	}
	return s
}

func formatClaudeToolUse(name string, input json.RawMessage) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	var fields map[string]json.RawMessage
	_ = json.Unmarshal(input, &fields)

	var detail, extra string
	switch name {
	case "Read", "Write", "Edit":
		detail = extractJSONStr(fields, "file_path")
	case "Bash":
		detail = truncateDetail(extractJSONStr(fields, "command"))
	case "Glob":
		detail = extractJSONStr(fields, "pattern")
	case "Grep":
		detail = extractJSONStr(fields, "pattern")
		if p := extractJSONStr(fields, "path"); p != "" {
			extra = p
		}
	case "Agent":
		detail = extractJSONStr(fields, "description")
	default:
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			detail = truncateDetail(extractJSONStr(fields, keys[0]))
		}
	}

	var b strings.Builder
	b.WriteString("<b>")
	b.WriteString(html.EscapeString(name))
	b.WriteString("</b>")
	if detail != "" {
		b.WriteString(" <code>")
		b.WriteString(html.EscapeString(detail))
		b.WriteString("</code>")
	}
	if extra != "" {
		b.WriteString(" in <code>")
		b.WriteString(html.EscapeString(extra))
		b.WriteString("</code>")
	}
	b.WriteString("\n")
	return b.String()
}

type claudeJSONEvent struct {
	Type      string             `json:"type"`
	Subtype   string             `json:"subtype"`
	SessionID string             `json:"session_id"`
	Message   *claudeJSONMessage `json:"message"`
	Error     string             `json:"error"`
}

type claudeJSONMessage struct {
	Content []claudeJSONContentBlock `json:"content"`
}

type claudeJSONContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
