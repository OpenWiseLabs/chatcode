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
	if len(args) != 3 || args[1] != "-p" || args[2] != "hello" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestClaudeHandleEvent(t *testing.T) {
	ex := ClaudeExecutor{}
	ev := &domain.StreamEvent{Chunk: "conversation id: 019c5a28-dae2-7ea1-a041-da86e3b12a95\n"}
	sid := ex.HandleEvent(ev)
	if sid != "019c5a28-dae2-7ea1-a041-da86e3b12a95" {
		t.Fatalf("unexpected sid: %q", sid)
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
}
