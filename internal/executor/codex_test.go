package executor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"chatcode/internal/domain"
	"chatcode/internal/store"
)

func TestCodexBuildCommandWithoutSession(t *testing.T) {
	ex := CodexExecutor{Binary: "codex"}
	args, err := ex.BuildCommand(context.Background(), domain.Job{
		Prompt: "hello",
	})
	if err != nil {
		t.Fatalf("BuildCommand error: %v", err)
	}
	if len(args) < 8 {
		t.Fatalf("unexpected args: %#v", args)
	}
	if args[4] != "exec" {
		t.Fatalf("expected exec mode, got: %#v", args)
	}
	if args[2] != "--sandbox" || args[3] != "workspace-write" {
		t.Fatalf("expected sandbox workspace-write, got: %#v", args)
	}
	foundJSON := false
	for _, arg := range args {
		if arg == "--json" {
			foundJSON = true
			break
		}
	}
	if !foundJSON {
		t.Fatalf("expected --json in args: %#v", args)
	}
}

func TestCodexBuildCommandFullAccessMode(t *testing.T) {
	ex := CodexExecutor{Binary: "codex"}
	args, err := ex.BuildCommand(context.Background(), domain.Job{
		Prompt:         "hello",
		PermissionMode: domain.PermissionModeFullAccess,
	})
	if err != nil {
		t.Fatalf("BuildCommand error: %v", err)
	}
	if len(args) < 4 || args[2] != "--sandbox" || args[3] != "danger-full-access" {
		t.Fatalf("expected danger-full-access sandbox, got: %#v", args)
	}
}

func TestCodexHandleEventJSON(t *testing.T) {
	ex := CodexExecutor{}
	ev := &domain.StreamEvent{Chunk: `{"type":"thread.started","thread_id":"019c5a9e-f025-7330-8911-56a4519ce9fa"}` + "\n"}
	sid := ex.HandleEvent(ev)
	if sid != "019c5a9e-f025-7330-8911-56a4519ce9fa" {
		t.Fatalf("unexpected sid: %q", sid)
	}
	if ev.Chunk != "" {
		t.Fatalf("expected control event to produce empty text, got %q", ev.Chunk)
	}
}

func TestCodexHandleEventTurnFailed(t *testing.T) {
	ex := CodexExecutor{}
	ev := &domain.StreamEvent{Chunk: `{"type":"turn.failed","error":{"message":"stream disconnected"}}`}
	_ = ex.HandleEvent(ev)
	if ev.Chunk != "stream disconnected\n" {
		t.Fatalf("unexpected text: %q", ev.Chunk)
	}
}

func TestCodexHandleEventItemCompletedAgentMessage(t *testing.T) {
	ex := CodexExecutor{}
	ev := &domain.StreamEvent{Chunk: `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"hello"}}`}
	_ = ex.HandleEvent(ev)
	if ev.Chunk != "hello\n" {
		t.Fatalf("unexpected text: %q", ev.Chunk)
	}
}

func TestCodexHandleEventItemCompletedCommandExecution(t *testing.T) {
	ex := CodexExecutor{}
	ev := &domain.StreamEvent{Chunk: `{"type":"item.completed","item":{"id":"item_1","type":"command_execution","aggregated_output":"line1\nline2\n"}}`}
	_ = ex.HandleEvent(ev)
	want := "<b>command_execution</b>\n<pre>line1\nline2</pre>\n"
	if ev.Chunk != want {
		t.Fatalf("unexpected text: %q", ev.Chunk)
	}
	if ev.Format != "html" {
		t.Fatalf("expected format=html, got %q", ev.Format)
	}
}

func TestCodexSessionIsolatedBySessionKey(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	workdir := t.TempDir()
	ex := CodexExecutor{SessionStore: st}
	tgJob := domain.Job{
		Executor: "codex",
		Workdir:  workdir,
		SessionKey: domain.SessionKey{
			Platform: domain.PlatformTelegram,
			ChatID:   "same-chat",
		},
	}
	waJob := domain.Job{
		Executor: "codex",
		Workdir:  workdir,
		SessionKey: domain.SessionKey{
			Platform: domain.PlatformWhatsApp,
			ChatID:   "same-chat",
		},
	}
	if err := ex.SaveSession(context.Background(), tgJob, "tg-session"); err != nil {
		t.Fatalf("save tg session: %v", err)
	}
	if err := ex.SaveSession(context.Background(), waJob, "wa-session"); err != nil {
		t.Fatalf("save wa session: %v", err)
	}
	gotTG, err := ex.LoadSession(context.Background(), tgJob)
	if err != nil {
		t.Fatalf("load tg session: %v", err)
	}
	gotWA, err := ex.LoadSession(context.Background(), waJob)
	if err != nil {
		t.Fatalf("load wa session: %v", err)
	}
	if gotTG != "tg-session" || gotWA != "wa-session" {
		t.Fatalf("expected isolated sessions, got tg=%q wa=%q", gotTG, gotWA)
	}
}

func TestCodexSessionIsolatedByThreadID(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	workdir := t.TempDir()
	ex := CodexExecutor{SessionStore: st}
	threadA := domain.Job{
		Executor: "codex",
		Workdir:  workdir,
		SessionKey: domain.SessionKey{
			Platform: domain.PlatformTelegram,
			ChatID:   "same-chat",
			ThreadID: "1001",
		},
	}
	threadB := domain.Job{
		Executor: "codex",
		Workdir:  workdir,
		SessionKey: domain.SessionKey{
			Platform: domain.PlatformTelegram,
			ChatID:   "same-chat",
			ThreadID: "1002",
		},
	}
	if err := ex.SaveSession(context.Background(), threadA, "sid-a"); err != nil {
		t.Fatalf("save threadA session: %v", err)
	}
	if err := ex.SaveSession(context.Background(), threadB, "sid-b"); err != nil {
		t.Fatalf("save threadB session: %v", err)
	}
	gotA, err := ex.LoadSession(context.Background(), threadA)
	if err != nil {
		t.Fatalf("load threadA: %v", err)
	}
	gotB, err := ex.LoadSession(context.Background(), threadB)
	if err != nil {
		t.Fatalf("load threadB: %v", err)
	}
	if gotA != "sid-a" || gotB != "sid-b" {
		t.Fatalf("expected isolated thread sessions, got a=%q b=%q", gotA, gotB)
	}
}

func TestCodexRequiresSessionStore(t *testing.T) {
	ex := CodexExecutor{}
	job := domain.Job{
		Executor: "codex",
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

func TestCodexBuildCommandWithSession(t *testing.T) {
	ex := CodexExecutor{Binary: "codex"}
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
		if args[i] == "resume" && args[i+1] == sid {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected resume command with session id, got: %#v", args)
	}
}

func TestTruncateCommandForDisplay(t *testing.T) {
	cmd := strings.Join([]string{
		strings.Repeat("a", 260),
		strings.Repeat("b", 260),
		strings.Repeat("c", 260),
		strings.Repeat("d", 260),
		strings.Repeat("e", 260),
		strings.Repeat("f", 260),
		strings.Repeat("g", 260),
	}, "\n")
	got := truncateCommandForDisplay(cmd)
	if !strings.Contains(got, "\n ...... [truncated] ...... \n\n") {
		t.Fatalf("expected truncation separator, got: %q", got)
	}
	if strings.Contains(got, strings.Repeat("d", 260)) {
		t.Fatalf("expected middle lines removed")
	}
}

func TestTruncateCommandForDisplay_NoTruncate(t *testing.T) {
	cmd := "line1\nline2\nline3"
	got := truncateCommandForDisplay(cmd)
	if got != cmd {
		t.Fatalf("expected unchanged command, got: %q", got)
	}
}
