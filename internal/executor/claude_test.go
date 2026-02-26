package executor

import (
	"context"
	"path/filepath"
	"testing"

	"chatcode/internal/domain"
	"chatcode/internal/store"
)

func TestClaudeBuildCommandWithoutSession(t *testing.T) {
	ex := ClaudeExecutor{Binary: "claude"}
	args, err := ex.BuildCommand(context.Background(), domain.Job{
		Prompt: "hello",
	})
	if err != nil {
		t.Fatalf("BuildCommand error: %v", err)
	}
	// claude --output-format stream-json --verbose --permission-mode bypassPermissions -p hello
	if len(args) != 8 || args[1] != "--output-format" || args[2] != "stream-json" ||
		args[3] != "--verbose" || args[4] != "--permission-mode" || args[5] != "bypassPermissions" || args[6] != "-p" || args[7] != "hello" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestClaudeHandleEventAssistantText(t *testing.T) {
	ex := ClaudeExecutor{}
	chunk := `{"type":"assistant","message":{"content":[{"type":"text","text":"hello world"}]}}`
	ev := &domain.StreamEvent{Chunk: chunk, Stream: "stdout"}
	sid := ex.HandleEvent(ev)
	if sid != "" {
		t.Fatalf("unexpected session id: %q", sid)
	}
	if ev.Chunk != "hello world\n" {
		t.Fatalf("unexpected chunk: %q", ev.Chunk)
	}
}

func TestClaudeHandleEventSystemInit(t *testing.T) {
	ex := ClaudeExecutor{}
	chunk := `{"type":"system","subtype":"init","session_id":"019c5a28-dae2-7ea1-a041-da86e3b12a95"}`
	ev := &domain.StreamEvent{Chunk: chunk, Stream: "stdout"}
	sid := ex.HandleEvent(ev)
	if sid != "019c5a28-dae2-7ea1-a041-da86e3b12a95" {
		t.Fatalf("unexpected session id: %q", sid)
	}
	if ev.Chunk != "" {
		t.Fatalf("expected empty chunk, got: %q", ev.Chunk)
	}
}

func TestClaudeHandleEventResult(t *testing.T) {
	ex := ClaudeExecutor{}
	chunk := `{"type":"result","subtype":"success","session_id":"019c5a28-dae2-7ea1-a041-da86e3b12a95","cost_usd":0.01}`
	ev := &domain.StreamEvent{Chunk: chunk, Stream: "stdout"}
	sid := ex.HandleEvent(ev)
	if sid != "019c5a28-dae2-7ea1-a041-da86e3b12a95" {
		t.Fatalf("unexpected session id: %q", sid)
	}
	if ev.Chunk != "" {
		t.Fatalf("expected empty chunk, got: %q", ev.Chunk)
	}
}

func TestClaudeHandleEventNonJSONStdout(t *testing.T) {
	// Non-JSON on stdout is suppressed (unexpected output during stream-json mode).
	ex := ClaudeExecutor{}
	ev := &domain.StreamEvent{Chunk: "some plain text on stdout", Stream: "stdout"}
	sid := ex.HandleEvent(ev)
	if sid != "" {
		t.Fatalf("unexpected session id: %q", sid)
	}
	if ev.Chunk != "" {
		t.Fatalf("expected empty chunk for non-json stdout, got: %q", ev.Chunk)
	}
}

func TestClaudeHandleEventStderrPassthrough(t *testing.T) {
	// stderr is passed through unchanged so CLI errors remain visible to the user.
	ex := ClaudeExecutor{}
	errMsg := "Error: unknown flag --output-format\n"
	ev := &domain.StreamEvent{Chunk: errMsg, Stream: "stderr"}
	sid := ex.HandleEvent(ev)
	if sid != "" {
		t.Fatalf("unexpected session id: %q", sid)
	}
	if ev.Chunk != errMsg {
		t.Fatalf("expected stderr chunk to be unchanged, got: %q", ev.Chunk)
	}
}

func TestClaudeHandleEventToolUse(t *testing.T) {
	ex := ClaudeExecutor{}
	chunk := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls -la"}}]}}`
	ev := &domain.StreamEvent{Chunk: chunk, Stream: "stdout"}
	sid := ex.HandleEvent(ev)
	if sid != "" {
		t.Fatalf("unexpected session id: %q", sid)
	}
	if !containsSubstring(ev.Chunk, "Bash") {
		t.Fatalf("expected tool name in chunk, got: %q", ev.Chunk)
	}
	if ev.Format != "html" {
		t.Fatalf("expected format=html, got %q", ev.Format)
	}
}

func TestClaudeHandleEventTextNoFormat(t *testing.T) {
	ex := ClaudeExecutor{}
	chunk := `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`
	ev := &domain.StreamEvent{Chunk: chunk, Stream: "stdout"}
	ex.HandleEvent(ev)
	if ev.Format != "" {
		t.Fatalf("expected empty format for plain text, got %q", ev.Format)
	}
}

func TestClaudeSessionIsolatedBySessionKey(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	workdir := t.TempDir()
	jobA := domain.Job{
		Executor: "claude",
		Workdir:  workdir,
		SessionKey: domain.SessionKey{
			Platform: domain.PlatformTelegram,
			ChatID:   "1",
		},
	}
	jobB := domain.Job{
		Executor: "claude",
		Workdir:  workdir,
		SessionKey: domain.SessionKey{
			Platform: domain.PlatformTelegram,
			ChatID:   "2",
		},
	}
	ex := ClaudeExecutor{SessionStore: st}
	if err := ex.SaveSession(context.Background(), jobA, "sid-a"); err != nil {
		t.Fatalf("save jobA session: %v", err)
	}
	if err := ex.SaveSession(context.Background(), jobB, "sid-b"); err != nil {
		t.Fatalf("save jobB session: %v", err)
	}
	gotA, err := ex.LoadSession(context.Background(), jobA)
	if err != nil {
		t.Fatalf("load jobA session: %v", err)
	}
	gotB, err := ex.LoadSession(context.Background(), jobB)
	if err != nil {
		t.Fatalf("load jobB session: %v", err)
	}
	if gotA != "sid-a" || gotB != "sid-b" {
		t.Fatalf("expected isolated sessions, got a=%q b=%q", gotA, gotB)
	}
}

func TestClaudeRequiresSessionStore(t *testing.T) {
	ex := ClaudeExecutor{}
	job := domain.Job{
		Executor: "claude",
		Workdir:  "/tmp/work",
		SessionKey: domain.SessionKey{
			Platform: domain.PlatformTelegram,
			ChatID:   "1",
		},
	}
	if _, err := ex.LoadSession(context.Background(), job); err == nil {
		t.Fatalf("expected error when session store is missing")
	}
}

func TestClaudeBuildCommandWithSession(t *testing.T) {
	ex := ClaudeExecutor{Binary: "claude"}
	sid := "019c5a28-dae2-7ea1-a041-da86e3b12a95"
	args, err := ex.BuildCommand(context.Background(), domain.Job{
		Prompt:  "continue",
		Session: sid,
	})
	if err != nil {
		t.Fatalf("BuildCommand error: %v", err)
	}
	found := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--resume" && args[i+1] == sid {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --resume with session id, got: %#v", args)
	}
	foundJSON := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--output-format" && args[i+1] == "stream-json" {
			foundJSON = true
			break
		}
	}
	if !foundJSON {
		t.Fatalf("expected --output-format stream-json, got: %#v", args)
	}
}

func TestClaudeBuildCommandWithFullAccessMode(t *testing.T) {
	ex := ClaudeExecutor{Binary: "claude"}
	args, err := ex.BuildCommand(context.Background(), domain.Job{
		Prompt:         "hello",
		PermissionMode: domain.PermissionModeFullAccess,
	})
	if err != nil {
		t.Fatalf("BuildCommand error: %v", err)
	}
	foundMode := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--permission-mode" && args[i+1] == "bypassPermissions" {
			foundMode = true
			break
		}
	}
	if !foundMode {
		t.Fatalf("expected --permission-mode bypassPermissions, got: %#v", args)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
