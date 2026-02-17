package session

import (
	"context"
	"sync"
	"time"

	"chatcode/internal/domain"
	"chatcode/internal/store"
)

type Manager struct {
	store     *store.SQLiteStore
	retention time.Duration
	mu        sync.RWMutex
	workdirs  map[string]string
	executors map[string]string
	pending   map[string]string
}

func NewManager(st *store.SQLiteStore, retention time.Duration) *Manager {
	return &Manager{
		store:     st,
		retention: retention,
		workdirs:  make(map[string]string),
		executors: make(map[string]string),
		pending:   make(map[string]string),
	}
}

func (m *Manager) SetWorkdir(ctx context.Context, key domain.SessionKey, workdir string) error {
	m.mu.Lock()
	m.workdirs[key.String()] = workdir
	m.mu.Unlock()
	return m.store.UpsertSession(ctx, key, workdir, time.Now().Add(m.retention))
}

func (m *Manager) Workdir(ctx context.Context, key domain.SessionKey) (string, error) {
	m.mu.RLock()
	if wd, ok := m.workdirs[key.String()]; ok {
		m.mu.RUnlock()
		return wd, nil
	}
	m.mu.RUnlock()
	wd, err := m.store.SessionWorkdir(ctx, key)
	if err != nil {
		return "", err
	}
	if wd != "" {
		m.mu.Lock()
		m.workdirs[key.String()] = wd
		m.mu.Unlock()
	}
	return wd, nil
}

func (m *Manager) Reset(key domain.SessionKey) {
	m.mu.Lock()
	delete(m.workdirs, key.String())
	delete(m.executors, key.String())
	delete(m.pending, key.String())
	m.mu.Unlock()
}

func (m *Manager) SetDefaultExecutor(key domain.SessionKey, executor string) {
	m.mu.Lock()
	m.executors[key.String()] = executor
	m.mu.Unlock()
}

func (m *Manager) DefaultExecutor(key domain.SessionKey) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.executors[key.String()]
}

func (m *Manager) SetPendingInput(key domain.SessionKey, action string) {
	m.mu.Lock()
	m.pending[key.String()] = action
	m.mu.Unlock()
}

func (m *Manager) TakePendingInput(key domain.SessionKey) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	action := m.pending[key.String()]
	delete(m.pending, key.String())
	return action
}
