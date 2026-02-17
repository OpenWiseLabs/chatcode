package stream

import (
	"context"
	"sync"
	"time"

	"chatcode/internal/domain"
)

type Sender interface {
	Send(context.Context, domain.OutboundMessage) error
}

type Batcher struct {
	interval time.Duration
	maxChunk int
	sender   Sender
	key      domain.SessionKey

	mu sync.Mutex
}

func NewBatcher(interval time.Duration, maxChunk int, sender Sender, key domain.SessionKey) *Batcher {
	if interval < 300*time.Millisecond {
		interval = 300 * time.Millisecond
	}
	if interval > 500*time.Millisecond {
		interval = 500 * time.Millisecond
	}
	if maxChunk <= 0 {
		maxChunk = 3500
	}
	return &Batcher{interval: interval, maxChunk: maxChunk, sender: sender, key: key}
}

func (b *Batcher) OnEvent(ctx context.Context, ev domain.StreamEvent) error {
	if ev.Chunk == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sendLocked(ctx, ev.Chunk)
}

func (b *Batcher) Flush(ctx context.Context) error {
	_ = ctx
	return nil
}

func (b *Batcher) sendLocked(ctx context.Context, msg string) error {
	if b.maxChunk <= 0 || len(msg) <= b.maxChunk {
		return b.sender.Send(ctx, domain.OutboundMessage{SessionKey: b.key, Text: msg})
	}
	for len(msg) > b.maxChunk {
		if err := b.sender.Send(ctx, domain.OutboundMessage{SessionKey: b.key, Text: msg[:b.maxChunk]}); err != nil {
			return err
		}
		msg = msg[b.maxChunk:]
	}
	if msg != "" {
		return b.sender.Send(ctx, domain.OutboundMessage{SessionKey: b.key, Text: msg})
	}
	return nil
}
