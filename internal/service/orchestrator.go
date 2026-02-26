package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"chatcode/internal/domain"
	"chatcode/internal/executor"
	"chatcode/internal/queue"
	"chatcode/internal/security"
	"chatcode/internal/session"
	"chatcode/internal/store"
	"chatcode/internal/stream"
)

type Orchestrator struct {
	store      *store.SQLiteStore
	sessions   *session.Manager
	policy     *security.Policy
	runner     executor.Runner
	executors  map[string]executor.Executor
	transport  map[domain.Platform]domain.Transport
	dispatcher *queue.Dispatcher
	jobs       sync.Map

	batchInterval time.Duration
	maxChunkBytes int
}

func NewOrchestrator(
	ctx context.Context,
	st *store.SQLiteStore,
	sm *session.Manager,
	policy *security.Policy,
	r executor.Runner,
	execs map[string]executor.Executor,
	transports map[domain.Platform]domain.Transport,
	maxConcurrent int,
	perSessionBuffer int,
	batchInterval time.Duration,
	maxChunkBytes int,
) *Orchestrator {
	o := &Orchestrator{
		store:         st,
		sessions:      sm,
		policy:        policy,
		runner:        r,
		executors:     execs,
		transport:     transports,
		batchInterval: batchInterval,
		maxChunkBytes: maxChunkBytes,
	}
	o.dispatcher = queue.NewDispatcher(maxConcurrent, perSessionBuffer, o.runJob)
	_ = ctx
	return o
}

func (o *Orchestrator) HandleIncomingMessage(ctx context.Context, msg domain.Message) error {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return nil
	}
	slog.Info("message received",
		"platform", msg.SessionKey.Platform,
		"chat_id", msg.SessionKey.ChatID,
		"thread_id", msg.SessionKey.ThreadID,
		"sender_id", msg.SenderID,
		"text", shorten(text, 200),
	)
	if action := o.sessions.TakePendingInput(msg.SessionKey); action != "" {
		switch action {
		case "cd":
			return o.setWorkdir(ctx, msg.SessionKey, text)
		case "new":
			return o.createAndSetWorkdir(ctx, msg.SessionKey, text)
		}
	}
	if strings.HasPrefix(text, "/") {
		return o.handleCommand(ctx, msg, text)
	}
	exName := o.defaultExecutor(msg.SessionKey)
	return o.enqueueJob(ctx, msg.SessionKey, exName, text)
}

func (o *Orchestrator) handleCommand(ctx context.Context, msg domain.Message, text string) error {
	if text == "/new" {
		o.sessions.SetPendingInput(msg.SessionKey, "new")
		return o.reply(ctx, msg.SessionKey, "send project directory path for /new")
	}
	if strings.HasPrefix(text, "/new ") {
		return o.createAndSetWorkdir(ctx, msg.SessionKey, strings.TrimSpace(strings.TrimPrefix(text, "/new ")))
	}
	if text == "/cd" {
		root := o.policy.PrimaryRoot()
		if root == "" {
			return o.reply(ctx, msg.SessionKey, "project root is not configured")
		}
		return o.setWorkdir(ctx, msg.SessionKey, root)
	}
	if strings.HasPrefix(text, "/cd ") {
		return o.setWorkdir(ctx, msg.SessionKey, strings.TrimSpace(strings.TrimPrefix(text, "/cd ")))
	}
	if text == "/list" {
		return o.listProjects(ctx, msg.SessionKey)
	}
	if text == "/codex" || strings.HasPrefix(text, "/codex ") {
		o.sessions.SetDefaultExecutor(msg.SessionKey, "codex")
		if text == "/codex" {
			return o.reply(ctx, msg.SessionKey, "default executor set to: codex")
		}
		return o.enqueueJob(ctx, msg.SessionKey, "codex", strings.TrimSpace(strings.TrimPrefix(text, "/codex ")))
	}
	if text == "/claude" || strings.HasPrefix(text, "/claude ") {
		o.sessions.SetDefaultExecutor(msg.SessionKey, "claude")
		if text == "/claude" {
			return o.reply(ctx, msg.SessionKey, "default executor set to: claude")
		}
		return o.enqueueJob(ctx, msg.SessionKey, "claude", strings.TrimSpace(strings.TrimPrefix(text, "/claude ")))
	}
	if text == "/reset" {
		o.sessions.Reset(msg.SessionKey)
		return o.reply(ctx, msg.SessionKey, "session reset")
	}
	if text == "/status" {
		wd, err := o.sessions.Workdir(ctx, msg.SessionKey)
		if err != nil {
			return err
		}
		mode, err := o.sessions.PermissionMode(ctx, msg.SessionKey)
		if err != nil {
			return err
		}
		exName := o.defaultExecutor(msg.SessionKey)
		if wd == "" {
			wd = "(unset)"
		}
		sessionID := "(unset)"
		if wd != "(unset)" {
			if ex, ok := o.executors[exName]; ok {
				if sessionAware, ok := ex.(executor.SessionAware); ok {
					sid, loadErr := sessionAware.LoadSession(ctx, domain.Job{
						SessionKey: msg.SessionKey,
						Executor:   exName,
						Workdir:    wd,
					})
					if loadErr != nil {
						sessionID = "(load failed)"
					} else if sid != "" {
						sessionID = sid
					}
				}
			}
		}
		return o.reply(ctx, msg.SessionKey, fmt.Sprintf(
			"Status:\nWorkdir: %s\nExecutor: %s\nMode: %s\nExecutor session_id: %s",
			wd, exName, mode, sessionID,
		))
	}
	if text == "/mode" {
		mode, err := o.sessions.PermissionMode(ctx, msg.SessionKey)
		if err != nil {
			return err
		}
		return o.reply(ctx, msg.SessionKey, "mode: "+mode)
	}
	if strings.HasPrefix(text, "/mode ") {
		mode := strings.TrimSpace(strings.TrimPrefix(text, "/mode "))
		if mode != domain.PermissionModeSandbox && mode != domain.PermissionModeFullAccess {
			return o.reply(ctx, msg.SessionKey, "usage: /mode <sandbox|full-access>")
		}
		if err := o.sessions.SetPermissionMode(ctx, msg.SessionKey, mode); err != nil {
			return err
		}
		return o.reply(ctx, msg.SessionKey, "mode set to: "+mode)
	}
	if strings.HasPrefix(text, "/stop ") {
		jobID := strings.TrimSpace(strings.TrimPrefix(text, "/stop "))
		if cancel, ok := o.jobs.Load(jobID); ok {
			cancel.(context.CancelFunc)()
			return o.reply(ctx, msg.SessionKey, "stop signal sent for job "+jobID)
		}
		return o.reply(ctx, msg.SessionKey, "job not found: "+jobID)
	}
	return o.reply(ctx, msg.SessionKey, "unsupported command")
}

func (o *Orchestrator) setWorkdir(ctx context.Context, key domain.SessionKey, wd string) error {
	target := wd
	if !filepath.IsAbs(target) {
		base := o.policy.PrimaryRoot()
		if base == "" {
			return o.reply(ctx, key, "project root is not configured")
		}
		target = filepath.Join(base, target)
	}
	target = filepath.Clean(target)
	if err := o.policy.ValidateWorkdir(target); err != nil {
		return o.reply(ctx, key, "workdir rejected: "+err.Error())
	}
	if err := o.sessions.SetWorkdir(ctx, key, target); err != nil {
		return err
	}
	return o.reply(ctx, key, "workdir set to: "+target)
}

func (o *Orchestrator) createAndSetWorkdir(ctx context.Context, key domain.SessionKey, wd string) error {
	if wd == "" {
		return o.reply(ctx, key, "workdir cannot be empty")
	}
	target := wd
	if !filepath.IsAbs(target) {
		base := o.policy.PrimaryRoot()
		if base == "" {
			return o.reply(ctx, key, "project root is not configured")
		}
		target = filepath.Join(base, target)
	}
	target = filepath.Clean(target)
	if err := o.policy.ValidateWorkdir(target); err != nil {
		return o.reply(ctx, key, "workdir rejected: "+err.Error())
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return o.reply(ctx, key, "create workdir failed: "+err.Error())
	}
	if err := o.sessions.SetWorkdir(ctx, key, target); err != nil {
		return err
	}
	return o.reply(ctx, key, "workdir created and set: "+target)
}

func (o *Orchestrator) listProjects(ctx context.Context, key domain.SessionKey) error {
	root := o.policy.PrimaryRoot()
	if root == "" {
		return o.reply(ctx, key, "project root is not configured")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return o.reply(ctx, key, "list projects failed: "+err.Error())
	}
	projects := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			projects = append(projects, e.Name())
		}
	}
	sort.Strings(projects)
	if len(projects) == 0 {
		return o.reply(ctx, key, "no projects found under "+root)
	}
	return o.reply(ctx, key, "projects:\n- "+strings.Join(projects, "\n- "))
}

func (o *Orchestrator) enqueueJob(ctx context.Context, key domain.SessionKey, exName, prompt string) error {
	if prompt == "" {
		return o.reply(ctx, key, "prompt cannot be empty")
	}
	wd, err := o.sessions.Workdir(ctx, key)
	if err != nil {
		return err
	}
	if wd == "" {
		return o.reply(ctx, key, "workdir is not set, use /cd <project_dir> first")
	}
	job := domain.Job{
		ID:         newJobID(),
		SessionKey: key,
		Executor:   exName,
		Prompt:     prompt,
		Workdir:    wd,
		Status:     domain.JobPending,
		CreatedAt:  time.Now().UTC(),
	}
	mode, err := o.sessions.PermissionMode(ctx, key)
	if err != nil {
		return err
	}
	job.PermissionMode = mode
	ex, ok := o.executors[exName]
	if !ok {
		return o.reply(ctx, key, "unknown executor: "+exName)
	}
	if sessionAware, ok := ex.(executor.SessionAware); ok {
		sessionID, err := sessionAware.LoadSession(ctx, job)
		if err != nil {
			slog.Error("load executor session failed", "executor", exName, "workdir", wd, "error", err)
		} else if sessionID != "" {
			job.Session = sessionID
		}
	}
	if err := o.policy.Validate(job); err != nil {
		return o.reply(ctx, key, "job rejected: "+err.Error())
	}
	if err := o.store.CreateJob(ctx, job); err != nil {
		return err
	}
	o.dispatcher.Enqueue(ctx, job)
	return o.reply(ctx, key, "job queued: "+job.ID)
}

func (o *Orchestrator) runJob(ctx context.Context, job domain.Job) {
	ex, ok := o.executors[job.Executor]
	if !ok {
		_ = o.reply(ctx, job.SessionKey, "unknown executor: "+job.Executor)
		return
	}
	started := time.Now().UTC()
	_ = o.store.UpdateJobStatus(ctx, job.ID, domain.JobRunning, &started, nil, "")

	runCtx, cancel := context.WithCancel(ctx)
	o.jobs.Store(job.ID, cancel)
	defer func() {
		o.jobs.Delete(job.ID)
		cancel()
	}()

	transport, ok := o.transport[job.SessionKey.Platform]
	if !ok {
		_ = o.reply(ctx, job.SessionKey, "transport missing for platform")
		return
	}
	batcher := stream.NewBatcher(o.batchInterval, o.maxChunkBytes, transport, job.SessionKey)
	sessionAware, hasSessionAware := ex.(executor.SessionAware)
	var sessionMu sync.Mutex
	sessionID := ""
	sink := &persistSink{store: o.store, downstream: batcher, onEvent: func(ev *domain.StreamEvent) {
		if hasSessionAware {
			if sid := sessionAware.HandleEvent(ev); sid != "" {
				sessionMu.Lock()
				sessionID = sid
				sessionMu.Unlock()
			}
		}
	}}

	err := o.runner.RunJob(runCtx, ex, job, sink)
	_ = batcher.Flush(ctx)
	if hasSessionAware {
		sessionMu.Lock()
		sid := sessionID
		sessionMu.Unlock()
		if sid != "" {
			if saveErr := sessionAware.SaveSession(ctx, job, sid); saveErr != nil {
				slog.Error("save executor session failed", "executor", job.Executor, "workdir", job.Workdir, "error", saveErr)
			} else {
				slog.Info("executor session saved", "executor", job.Executor, "workdir", job.Workdir, "session_id", sid)
			}
		}
	}
	finished := time.Now().UTC()
	if err != nil {
		_ = o.store.UpdateJobStatus(ctx, job.ID, domain.JobFailed, &started, &finished, err.Error())
		_ = o.reply(ctx, job.SessionKey, fmt.Sprintf("job failed: %s", err.Error()))
		return
	}
	_ = o.store.UpdateJobStatus(ctx, job.ID, domain.JobDone, &started, &finished, "")
	_ = o.reply(ctx, job.SessionKey, "job done: "+job.ID)
}

func (o *Orchestrator) reply(ctx context.Context, key domain.SessionKey, text string) error {
	t, ok := o.transport[key.Platform]
	if !ok {
		return nil
	}
	return t.Send(ctx, domain.OutboundMessage{
		SessionKey: key,
		Text:       text,
	})
}

type persistSink struct {
	store      *store.SQLiteStore
	downstream executor.Sink
	onEvent    func(*domain.StreamEvent)
}

func (p *persistSink) OnEvent(ctx context.Context, ev domain.StreamEvent) error {
	if p.onEvent != nil {
		p.onEvent(&ev)
	}
	if err := p.store.AppendEvent(ctx, ev); err != nil {
		return err
	}
	return p.downstream.OnEvent(ctx, ev)
}

func newJobID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func shorten(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

func (o *Orchestrator) defaultExecutor(key domain.SessionKey) string {
	exName := o.sessions.DefaultExecutor(key)
	if exName == "" {
		return "codex"
	}
	return exName
}
