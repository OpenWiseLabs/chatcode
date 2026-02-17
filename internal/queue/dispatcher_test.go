package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"chatcode/internal/domain"
)

func TestDispatcherPerSessionSerial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	order := make([]string, 0, 2)
	d := NewDispatcher(4, 16, func(_ context.Context, job domain.Job) {
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		order = append(order, job.ID)
		mu.Unlock()
	})

	key := domain.SessionKey{Platform: domain.PlatformTelegram, ChatID: "1"}
	d.Enqueue(ctx, domain.Job{ID: "a", SessionKey: key})
	d.Enqueue(ctx, domain.Job{ID: "b", SessionKey: key})
	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("unexpected order: %#v", order)
	}
}
