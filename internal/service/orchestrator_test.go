package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"chatcode/internal/domain"
	"chatcode/internal/executor"
	"chatcode/internal/security"
	"chatcode/internal/session"
	"chatcode/internal/store"
)

type fakeTransport struct {
	mu   sync.Mutex
	msgs []string
}

func (f *fakeTransport) Name() string                                       { return "fake" }
func (f *fakeTransport) Start(context.Context, domain.MessageHandler) error { return nil }
func (f *fakeTransport) Send(_ context.Context, msg domain.OutboundMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg.Text)
	return nil
}

type fakeExec struct{}

func (f fakeExec) Name() string { return "codex" }
func (f fakeExec) BuildCommand(context.Context, domain.Job) ([]string, error) {
	return []string{"/bin/sh", "-c", "echo ok"}, nil
}

func TestOrchestratorExec(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	sm := session.NewManager(st, time.Hour)
	if err := sm.SetWorkdir(ctx, domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}, "/tmp"); err != nil {
		t.Fatalf("set workdir: %v", err)
	}
	pol := security.New([]string{"codex"}, []string{"/tmp"})
	tg := &fakeTransport{}
	o := NewOrchestrator(
		ctx,
		st,
		sm,
		pol,
		executor.Runner{Timeout: time.Second},
		map[string]executor.Executor{"codex": fakeExec{}},
		map[domain.Platform]domain.Transport{domain.PlatformTelegram: tg},
		2,
		8,
		300*time.Millisecond,
		3500,
	)

	msg := domain.Message{SessionKey: domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}, Text: "/codex hello"}
	if err := o.HandleIncomingMessage(ctx, msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	tg.mu.Lock()
	defer tg.mu.Unlock()
	if len(tg.msgs) == 0 {
		t.Fatalf("expected messages")
	}
}

func TestOrchestratorPlainMessageNeedsSessionSetup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	sm := session.NewManager(st, time.Hour)
	pol := security.New([]string{"codex"}, []string{"/tmp"})
	tg := &fakeTransport{}
	o := NewOrchestrator(
		ctx,
		st,
		sm,
		pol,
		executor.Runner{Timeout: time.Second},
		map[string]executor.Executor{"codex": fakeExec{}},
		map[domain.Platform]domain.Transport{domain.PlatformTelegram: tg},
		2,
		8,
		300*time.Millisecond,
		3500,
	)

	msg := domain.Message{SessionKey: domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}, Text: "hello"}
	if err := o.HandleIncomingMessage(ctx, msg); err != nil {
		t.Fatalf("handle: %v", err)
	}

	tg.mu.Lock()
	defer tg.mu.Unlock()
	if len(tg.msgs) == 0 {
		t.Fatalf("expected response message")
	}
}

func TestOrchestratorPlainMessageExecutesAfterSetup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	sm := session.NewManager(st, time.Hour)
	pol := security.New([]string{"codex"}, []string{"/tmp"})
	tg := &fakeTransport{}
	o := NewOrchestrator(
		ctx,
		st,
		sm,
		pol,
		executor.Runner{Timeout: time.Second},
		map[string]executor.Executor{"codex": fakeExec{}},
		map[domain.Platform]domain.Transport{domain.PlatformTelegram: tg},
		2,
		8,
		300*time.Millisecond,
		3500,
	)
	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "/cd /tmp"}); err != nil {
		t.Fatalf("cd: %v", err)
	}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "/codex"}); err != nil {
		t.Fatalf("codex set default: %v", err)
	}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "hello"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	tg.mu.Lock()
	defer tg.mu.Unlock()
	if len(tg.msgs) < 3 {
		t.Fatalf("expected setup + job messages, got %d", len(tg.msgs))
	}
}

func TestOrchestratorNewCreatesAndSetsWorkdir(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	root := t.TempDir()
	target := filepath.Join(root, "project-a")
	sm := session.NewManager(st, time.Hour)
	pol := security.New([]string{"codex"}, []string{root})
	tg := &fakeTransport{}
	o := NewOrchestrator(
		ctx,
		st,
		sm,
		pol,
		executor.Runner{Timeout: time.Second},
		map[string]executor.Executor{"codex": fakeExec{}},
		map[domain.Platform]domain.Transport{domain.PlatformTelegram: tg},
		2,
		8,
		300*time.Millisecond,
		3500,
	)
	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "/new " + target}); err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected dir to exist: %v", err)
	}
	wd, err := sm.Workdir(ctx, key)
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}
	if wd != target {
		t.Fatalf("expected workdir %q, got %q", target, wd)
	}
}

func TestOrchestratorCommandWithoutArgsUsesNextMessageAsPath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	root := t.TempDir()
	target := filepath.Join(root, "project-b")
	sm := session.NewManager(st, time.Hour)
	pol := security.New([]string{"codex"}, []string{root})
	tg := &fakeTransport{}
	o := NewOrchestrator(
		ctx,
		st,
		sm,
		pol,
		executor.Runner{Timeout: time.Second},
		map[string]executor.Executor{"codex": fakeExec{}},
		map[domain.Platform]domain.Transport{domain.PlatformTelegram: tg},
		2,
		8,
		300*time.Millisecond,
		3500,
	)
	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "/new"}); err != nil {
		t.Fatalf("new cmd: %v", err)
	}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: target}); err != nil {
		t.Fatalf("new path: %v", err)
	}
	wd, err := sm.Workdir(ctx, key)
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}
	if wd != target {
		t.Fatalf("expected workdir %q, got %q", target, wd)
	}
}

func TestOrchestratorNewRelativePathCreatesUnderProjectRoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	root := t.TempDir()
	sm := session.NewManager(st, time.Hour)
	pol := security.New([]string{"codex"}, []string{root})
	tg := &fakeTransport{}
	o := NewOrchestrator(
		ctx,
		st,
		sm,
		pol,
		executor.Runner{Timeout: time.Second},
		map[string]executor.Executor{"codex": fakeExec{}},
		map[domain.Platform]domain.Transport{domain.PlatformTelegram: tg},
		2,
		8,
		300*time.Millisecond,
		3500,
	)
	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "/new my-project"}); err != nil {
		t.Fatalf("new relative: %v", err)
	}

	target := filepath.Join(root, "my-project")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected dir to exist: %v", err)
	}
	wd, err := sm.Workdir(ctx, key)
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}
	if wd != target {
		t.Fatalf("expected workdir %q, got %q", target, wd)
	}
}

func TestOrchestratorCdWithoutPathUsesProjectRoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	root := t.TempDir()
	sm := session.NewManager(st, time.Hour)
	pol := security.New([]string{"codex"}, []string{root})
	tg := &fakeTransport{}
	o := NewOrchestrator(
		ctx,
		st,
		sm,
		pol,
		executor.Runner{Timeout: time.Second},
		map[string]executor.Executor{"codex": fakeExec{}},
		map[domain.Platform]domain.Transport{domain.PlatformTelegram: tg},
		2,
		8,
		300*time.Millisecond,
		3500,
	)
	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "/cd"}); err != nil {
		t.Fatalf("cd no path: %v", err)
	}
	wd, err := sm.Workdir(ctx, key)
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}
	if wd != root {
		t.Fatalf("expected workdir %q, got %q", root, wd)
	}
}

func TestOrchestratorListProjects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "b"), 0o755); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	sm := session.NewManager(st, time.Hour)
	pol := security.New([]string{"codex"}, []string{root})
	tg := &fakeTransport{}
	o := NewOrchestrator(
		ctx,
		st,
		sm,
		pol,
		executor.Runner{Timeout: time.Second},
		map[string]executor.Executor{"codex": fakeExec{}},
		map[domain.Platform]domain.Transport{domain.PlatformTelegram: tg},
		2,
		8,
		300*time.Millisecond,
		3500,
	)
	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "/list"}); err != nil {
		t.Fatalf("list: %v", err)
	}

	tg.mu.Lock()
	defer tg.mu.Unlock()
	if len(tg.msgs) == 0 {
		t.Fatalf("expected list response")
	}
	last := tg.msgs[len(tg.msgs)-1]
	if !strings.Contains(last, "projects:") || !strings.Contains(last, "- a") || !strings.Contains(last, "- b") {
		t.Fatalf("unexpected list output: %q", last)
	}
}

func TestOrchestratorCdRelativePathUsesProjectRoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	root := t.TempDir()
	target := filepath.Join(root, "web")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	sm := session.NewManager(st, time.Hour)
	pol := security.New([]string{"codex"}, []string{root})
	tg := &fakeTransport{}
	o := NewOrchestrator(
		ctx,
		st,
		sm,
		pol,
		executor.Runner{Timeout: time.Second},
		map[string]executor.Executor{"codex": fakeExec{}},
		map[domain.Platform]domain.Transport{domain.PlatformTelegram: tg},
		2,
		8,
		300*time.Millisecond,
		3500,
	)
	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "/cd web"}); err != nil {
		t.Fatalf("cd relative: %v", err)
	}
	wd, err := sm.Workdir(ctx, key)
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}
	if wd != target {
		t.Fatalf("expected workdir %q, got %q", target, wd)
	}
}

func TestOrchestratorDefaultExecutorIsCodex(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	root := t.TempDir()
	sm := session.NewManager(st, time.Hour)
	pol := security.New([]string{"codex"}, []string{root})
	tg := &fakeTransport{}
	o := NewOrchestrator(
		ctx,
		st,
		sm,
		pol,
		executor.Runner{Timeout: time.Second},
		map[string]executor.Executor{"codex": fakeExec{}},
		map[domain.Platform]domain.Transport{domain.PlatformTelegram: tg},
		2,
		8,
		300*time.Millisecond,
		3500,
	)
	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "/cd"}); err != nil {
		t.Fatalf("cd root: %v", err)
	}
	if err := o.HandleIncomingMessage(ctx, domain.Message{SessionKey: key, Text: "hello"}); err != nil {
		t.Fatalf("plain prompt: %v", err)
	}

	tg.mu.Lock()
	defer tg.mu.Unlock()
	foundQueued := false
	for _, m := range tg.msgs {
		if strings.Contains(m, "job queued:") {
			foundQueued = true
			break
		}
	}
	if !foundQueued {
		t.Fatalf("expected job queued response, got %#v", tg.msgs)
	}
}
