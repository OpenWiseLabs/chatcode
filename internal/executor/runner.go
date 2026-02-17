package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"chatcode/internal/domain"
)

type Runner struct {
	Timeout time.Duration
}

func (r Runner) RunJob(ctx context.Context, ex Executor, job domain.Job, sink Sink) error {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args, err := ex.BuildCommand(ctx, job)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("empty command for executor %s", ex.Name())
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = job.Workdir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	var seq int64
	var wg sync.WaitGroup
	emit := func(stream string, r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 1024), 1024*1024)
		for scanner.Scan() {
			next := atomic.AddInt64(&seq, 1)
			_ = sink.OnEvent(ctx, domain.StreamEvent{
				JobID:  job.ID,
				Seq:    next,
				Chunk:  scanner.Text() + "\n",
				Stream: stream,
				TS:     time.Now().UTC(),
			})
		}
	}
	wg.Add(2)
	go emit("stdout", stdout)
	go emit("stderr", stderr)

	waitErr := cmd.Wait()
	wg.Wait()
	next := atomic.AddInt64(&seq, 1)
	exitCode := 0
	if waitErr != nil {
		exitCode = 1
	}
	_ = sink.OnEvent(ctx, domain.StreamEvent{
		JobID:    job.ID,
		Seq:      next,
		IsFinal:  true,
		Stream:   "meta",
		TS:       time.Now().UTC(),
		ExitCode: &exitCode,
	})

	if waitErr != nil {
		return fmt.Errorf("command failed: %w", waitErr)
	}
	return nil
}
