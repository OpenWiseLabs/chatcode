package queue

import (
	"context"
	"sync"

	"chatcode/internal/domain"
)

type Worker func(context.Context, domain.Job)

type Dispatcher struct {
	mu           sync.Mutex
	sessionQueue map[string]chan domain.Job
	worker       Worker
	sem          chan struct{}
	buffer       int
}

func NewDispatcher(maxConcurrent, perSessionBuffer int, worker Worker) *Dispatcher {
	if maxConcurrent <= 0 {
		maxConcurrent = 8
	}
	if perSessionBuffer <= 0 {
		perSessionBuffer = 64
	}
	return &Dispatcher{
		sessionQueue: make(map[string]chan domain.Job),
		worker:       worker,
		sem:          make(chan struct{}, maxConcurrent),
		buffer:       perSessionBuffer,
	}
}

func (d *Dispatcher) Enqueue(ctx context.Context, job domain.Job) {
	key := job.SessionKey.String()
	d.mu.Lock()
	q, ok := d.sessionQueue[key]
	if !ok {
		q = make(chan domain.Job, d.buffer)
		d.sessionQueue[key] = q
		go d.consume(ctx, q)
	}
	d.mu.Unlock()
	q <- job
}

func (d *Dispatcher) consume(ctx context.Context, q <-chan domain.Job) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-q:
			d.sem <- struct{}{}
			d.worker(ctx, job)
			<-d.sem
		}
	}
}
