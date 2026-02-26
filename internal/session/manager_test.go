package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"chatcode/internal/domain"
	"chatcode/internal/store"
)

func TestPermissionModeDefaultsToSandbox(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	m := NewManager(st, time.Hour)
	mode, err := m.PermissionMode(context.Background(), domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"})
	if err != nil {
		t.Fatalf("PermissionMode: %v", err)
	}
	if mode != domain.PermissionModeSandbox {
		t.Fatalf("expected sandbox, got %q", mode)
	}
}

func TestPermissionModeSetAndReload(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	m := NewManager(st, time.Hour)
	if err := m.SetPermissionMode(context.Background(), key, domain.PermissionModeFullAccess); err != nil {
		t.Fatalf("SetPermissionMode: %v", err)
	}
	mode, err := m.PermissionMode(context.Background(), key)
	if err != nil {
		t.Fatalf("PermissionMode: %v", err)
	}
	if mode != domain.PermissionModeFullAccess {
		t.Fatalf("expected full-access, got %q", mode)
	}

	m2 := NewManager(st, time.Hour)
	mode2, err := m2.PermissionMode(context.Background(), key)
	if err != nil {
		t.Fatalf("PermissionMode reload: %v", err)
	}
	if mode2 != domain.PermissionModeFullAccess {
		t.Fatalf("expected persisted full-access, got %q", mode2)
	}
}

func TestPermissionModeResetClearsCacheButKeepsPersistedValue(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	st, err := store.NewSQLiteStore(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	m := NewManager(st, time.Hour)
	if err := m.SetPermissionMode(context.Background(), key, domain.PermissionModeFullAccess); err != nil {
		t.Fatalf("SetPermissionMode: %v", err)
	}
	m.Reset(key)
	mode, err := m.PermissionMode(context.Background(), key)
	if err != nil {
		t.Fatalf("PermissionMode: %v", err)
	}
	if mode != domain.PermissionModeFullAccess {
		t.Fatalf("expected full-access from store after reset, got %q", mode)
	}
}
