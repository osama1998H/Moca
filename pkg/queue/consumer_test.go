package queue

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestConsumerBasic(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	// Enqueue 5 jobs.
	for i := range 5 {
		job := testJob("job-"+string(rune('A'+i)), "acme", "test_job")
		if _, err := p.Enqueue(ctx, "acme", QueueDefault, job); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	var consumed atomic.Int32
	handlers := map[string]JobHandler{
		"test_job": func(_ context.Context, j Job) error {
			consumed.Add(1)
			return nil
		},
	}

	c := &consumer{
		rdb:           rdb,
		stream:        StreamKey("acme", QueueDefault),
		group:         GroupName("acme"),
		name:          "test-consumer-0",
		blockDuration: 100 * time.Millisecond,
		handlers:      handlers,
		logger:        slog.Default(),
	}

	consumeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_ = c.run(consumeCtx)

	if got := consumed.Load(); got != 5 {
		t.Errorf("consumed = %d, want 5", got)
	}

	// Verify PEL is empty (all acknowledged).
	pending, err := rdb.XPending(ctx, StreamKey("acme", QueueDefault), GroupName("acme")).Result()
	if err != nil {
		t.Fatalf("XPENDING: %v", err)
	}
	if pending.Count != 0 {
		t.Errorf("pending count = %d, want 0", pending.Count)
	}
}

func TestConsumerHandlerError(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	job := testJob("fail-job", "acme", "failing_job")
	if _, err := p.Enqueue(ctx, "acme", QueueDefault, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	handlers := map[string]JobHandler{
		"failing_job": func(_ context.Context, _ Job) error {
			return errTestHandler
		},
	}

	c := &consumer{
		rdb:           rdb,
		stream:        StreamKey("acme", QueueDefault),
		group:         GroupName("acme"),
		name:          "test-consumer-err",
		blockDuration: 100 * time.Millisecond,
		handlers:      handlers,
		logger:        slog.Default(),
	}

	consumeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_ = c.run(consumeCtx)

	// Message should still be in PEL (not acknowledged).
	pending, err := rdb.XPending(ctx, StreamKey("acme", QueueDefault), GroupName("acme")).Result()
	if err != nil {
		t.Fatalf("XPENDING: %v", err)
	}
	if pending.Count == 0 {
		t.Error("expected pending count > 0, message should be in PEL")
	}
}

var errTestHandler = errorString("test handler error")

type errorString string

func (e errorString) Error() string { return string(e) }

func TestConsumerGracefulShutdown(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()

	c := &consumer{
		rdb:           rdb,
		stream:        StreamKey("acme", QueueDefault),
		group:         GroupName("acme"),
		name:          "test-shutdown",
		blockDuration: 100 * time.Millisecond,
		handlers:      map[string]JobHandler{},
		logger:        slog.Default(),
	}

	consumeCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- c.run(consumeCtx)
	}()

	// Cancel immediately.
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on shutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not shut down within 5s")
	}
}

func TestEnsureGroupIdempotent(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()

	stream := StreamKey("acme", QueueDefault)
	group := GroupName("acme")

	// First call creates the group.
	if err := ensureGroup(ctx, rdb, stream, group); err != nil {
		t.Fatalf("first ensureGroup: %v", err)
	}

	// Second call should not error (idempotent).
	if err := ensureGroup(ctx, rdb, stream, group); err != nil {
		t.Fatalf("second ensureGroup: %v", err)
	}
}
