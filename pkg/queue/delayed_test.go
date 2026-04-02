package queue

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestDelayedPromoterBasic(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	// Enqueue 3 delayed jobs with past timestamps (should be immediately promotable).
	past := time.Now().Add(-1 * time.Hour)
	for i := range 3 {
		job := testJob("delayed-"+string(rune('A'+i)), "acme", "report")
		if err := p.EnqueueDelayed(ctx, "acme", QueueDefault, job, past); err != nil {
			t.Fatalf("EnqueueDelayed: %v", err)
		}
	}

	dp := &delayedPromoter{
		rdb:      rdb,
		sites:    []string{"acme"},
		maxLen:   map[QueueType]int64{QueueDefault: 10_000},
		interval: 100 * time.Millisecond,
		logger:   slog.Default(),
	}

	n, err := dp.promoteReady(ctx, "acme")
	if err != nil {
		t.Fatalf("promoteReady: %v", err)
	}
	if n != 3 {
		t.Errorf("promoted = %d, want 3", n)
	}

	// Verify jobs are now in the stream.
	stream := StreamKey("acme", QueueDefault)
	xlen, _ := rdb.XLen(ctx, stream).Result()
	if xlen != 3 {
		t.Errorf("XLEN = %d, want 3", xlen)
	}

	// Verify delayed set is empty.
	zcard, _ := rdb.ZCard(ctx, DelayedKey("acme")).Result()
	if zcard != 0 {
		t.Errorf("ZCARD delayed = %d, want 0", zcard)
	}
}

func TestDelayedPromoterFutureJobs(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	// Enqueue jobs with future timestamps (should NOT be promoted).
	future := time.Now().Add(1 * time.Hour)
	for i := range 3 {
		job := testJob("future-"+string(rune('A'+i)), "acme", "report")
		if err := p.EnqueueDelayed(ctx, "acme", QueueDefault, job, future); err != nil {
			t.Fatalf("EnqueueDelayed: %v", err)
		}
	}

	dp := &delayedPromoter{
		rdb:      rdb,
		sites:    []string{"acme"},
		maxLen:   map[QueueType]int64{QueueDefault: 10_000},
		interval: 100 * time.Millisecond,
		logger:   slog.Default(),
	}

	n, err := dp.promoteReady(ctx, "acme")
	if err != nil {
		t.Fatalf("promoteReady: %v", err)
	}
	if n != 0 {
		t.Errorf("promoted = %d, want 0 (all future)", n)
	}

	// Verify delayed set still has all jobs.
	zcard, _ := rdb.ZCard(ctx, DelayedKey("acme")).Result()
	if zcard != 3 {
		t.Errorf("ZCARD delayed = %d, want 3", zcard)
	}

	// Verify no jobs in stream.
	stream := StreamKey("acme", QueueDefault)
	xlen, _ := rdb.XLen(ctx, stream).Result()
	if xlen != 0 {
		t.Errorf("XLEN = %d, want 0", xlen)
	}
}

func TestDelayedPromoterMixedTimestamps(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)

	// 2 past, 1 future.
	_ = p.EnqueueDelayed(ctx, "acme", QueueDefault, testJob("p1", "acme", "r"), past)
	_ = p.EnqueueDelayed(ctx, "acme", QueueDefault, testJob("p2", "acme", "r"), past)
	_ = p.EnqueueDelayed(ctx, "acme", QueueDefault, testJob("f1", "acme", "r"), future)

	dp := &delayedPromoter{
		rdb:      rdb,
		sites:    []string{"acme"},
		maxLen:   map[QueueType]int64{QueueDefault: 10_000},
		interval: 100 * time.Millisecond,
		logger:   slog.Default(),
	}

	n, err := dp.promoteReady(ctx, "acme")
	if err != nil {
		t.Fatalf("promoteReady: %v", err)
	}
	if n != 2 {
		t.Errorf("promoted = %d, want 2", n)
	}

	zcard, _ := rdb.ZCard(ctx, DelayedKey("acme")).Result()
	if zcard != 1 {
		t.Errorf("ZCARD delayed = %d, want 1 (future job remains)", zcard)
	}
}
