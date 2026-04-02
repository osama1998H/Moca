package queue

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPoolRun(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	// Enqueue 10 jobs.
	for i := range 10 {
		job := testJob("wp-job-"+string(rune('A'+i)), "acme", "test_task")
		if _, err := p.Enqueue(ctx, "acme", QueueDefault, job); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	var processed atomic.Int32

	cfg := DefaultWorkerPoolConfig()
	cfg.Sites = []string{"acme"}
	cfg.QueueTypes = []QueueType{QueueDefault}
	cfg.ConsumersPerQueue = 2
	cfg.BlockDuration = 100 * time.Millisecond
	cfg.DLQInterval = 100 * time.Millisecond
	cfg.DelayedPollInterval = 100 * time.Millisecond
	cfg.ClaimInterval = 100 * time.Millisecond

	wp := NewWorkerPool(rdb, cfg)
	wp.Handle("test_task", func(_ context.Context, _ Job) error {
		processed.Add(1)
		return nil
	})

	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- wp.Run(runCtx)
	}()

	// Wait for jobs to be processed.
	deadline := time.After(3 * time.Second)
	for processed.Load() < 10 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for jobs; processed = %d", processed.Load())
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("WorkerPool.Run returned error: %v", err)
	}
}

func TestWorkerPoolGracefulShutdown(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()

	cfg := DefaultWorkerPoolConfig()
	cfg.Sites = []string{"acme"}
	cfg.QueueTypes = []QueueType{QueueDefault}
	cfg.ConsumersPerQueue = 1
	cfg.BlockDuration = 100 * time.Millisecond
	cfg.DLQInterval = 100 * time.Millisecond
	cfg.DelayedPollInterval = 100 * time.Millisecond
	cfg.ClaimInterval = 100 * time.Millisecond

	wp := NewWorkerPool(rdb, cfg)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- wp.Run(runCtx)
	}()

	// Give it a moment to start, then cancel.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on shutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WorkerPool did not shut down within 5s")
	}
}

func TestWorkerPoolNoSites(t *testing.T) {
	rdb, _ := newTestRedis(t)

	cfg := DefaultWorkerPoolConfig()
	cfg.Sites = nil // no sites

	wp := NewWorkerPool(rdb, cfg)

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- wp.Run(runCtx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not shut down")
	}
}
