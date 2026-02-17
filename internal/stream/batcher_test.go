package stream

import (
	"context"
	"sync"
	"testing"

	"chatcode/internal/domain"
)

type fakeSender struct {
	mu   sync.Mutex
	msgs []string
}

func (f *fakeSender) Send(_ context.Context, msg domain.OutboundMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg.Text)
	return nil
}

func TestBatcherSendImmediately(t *testing.T) {
	s := &fakeSender{}
	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	b := NewBatcher(300, 100, s, key)
	ctx := context.Background()

	_ = b.OnEvent(ctx, domain.StreamEvent{Chunk: "a"})

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.msgs) != 1 || s.msgs[0] != "a" {
		t.Fatalf("expected immediate single send, got %#v", s.msgs)
	}
}
