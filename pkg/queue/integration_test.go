//go:build integration

package queue_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/queue"
)

const (
	integRedisHost = "localhost"
	integRedisPort = 6380
)

func integrationRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%d", integRedisHost, integRedisPort),
		DB:   1, // queue DB
	})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available at %s:%d — start with: docker compose up -d", integRedisHost, integRedisPort)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// flushRedis clears all keys in the queue DB to isolate tests.
func flushRedis(t *testing.T, rdb *redis.Client) {
	t.Helper()
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
}

func TestIntegration_EnqueueAndConsumeJobs(t *testing.T) {
	rdb := integrationRedisClient(t)
	flushRedis(t, rdb)

	ctx := context.Background()
	logger := slog.Default()
	producer := queue.NewProducer(rdb, logger)

	site := "integ_site"
	const totalJobs = 100

	// Enqueue 100 jobs across 3 queue types.
	queueTypes := []queue.QueueType{queue.QueueDefault, queue.QueueCritical, queue.QueueLong}
	for i := range totalJobs {
		qt := queueTypes[i%len(queueTypes)]
		job := queue.Job{
			ID:         fmt.Sprintf("job-%03d", i),
			Site:       site,
			Type:       "integ_task",
			Payload:    map[string]any{"index": i},
			CreatedAt:  time.Now().UTC(),
			MaxRetries: 3,
			Timeout:    10 * time.Second,
		}
		if _, err := producer.Enqueue(ctx, site, qt, job); err != nil {
			t.Fatalf("Enqueue job %d: %v", i, err)
		}
	}

	// Run worker pool and count processed jobs.
	var processed atomic.Int32

	cfg := queue.DefaultWorkerPoolConfig()
	cfg.Sites = []string{site}
	cfg.QueueTypes = queueTypes
	cfg.ConsumersPerQueue = 2
	cfg.BlockDuration = 200 * time.Millisecond
	cfg.DLQInterval = 200 * time.Millisecond
	cfg.DelayedPollInterval = 200 * time.Millisecond
	cfg.ClaimInterval = 200 * time.Millisecond
	cfg.Logger = logger

	wp := queue.NewWorkerPool(rdb, cfg)
	wp.Handle("integ_task", func(_ context.Context, _ queue.Job) error {
		processed.Add(1)
		return nil
	})

	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- wp.Run(runCtx) }()

	// Poll until all jobs processed.
	deadline := time.After(15 * time.Second)
	for processed.Load() < totalJobs {
		select {
		case <-deadline:
			t.Fatalf("timed out: processed %d/%d", processed.Load(), totalJobs)
		case <-time.After(100 * time.Millisecond):
		}
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("WorkerPool.Run: %v", err)
	}

	if got := processed.Load(); got != totalJobs {
		t.Errorf("processed = %d, want %d", got, totalJobs)
	}
}

func TestIntegration_FailedJobsMoveToDLQ(t *testing.T) {
	rdb := integrationRedisClient(t)
	flushRedis(t, rdb)

	ctx := context.Background()
	logger := slog.Default()
	producer := queue.NewProducer(rdb, logger)

	site := "integ_dlq"
	const maxRetries = 3

	// Enqueue a job that will always fail.
	job := queue.Job{
		ID:         "fail-job-1",
		Site:       site,
		Type:       "failing_task",
		Payload:    map[string]any{"fail": true},
		CreatedAt:  time.Now().UTC(),
		MaxRetries: maxRetries,
		Timeout:    5 * time.Second,
	}
	if _, err := producer.Enqueue(ctx, site, queue.QueueDefault, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	var failCount atomic.Int32

	cfg := queue.DefaultWorkerPoolConfig()
	cfg.Sites = []string{site}
	cfg.QueueTypes = []queue.QueueType{queue.QueueDefault}
	cfg.ConsumersPerQueue = 1
	cfg.BlockDuration = 100 * time.Millisecond
	cfg.DLQInterval = 500 * time.Millisecond
	cfg.DelayedPollInterval = 500 * time.Millisecond
	cfg.ClaimInterval = 500 * time.Millisecond
	cfg.ClaimMinIdle = 500 * time.Millisecond // Short idle for test speed.
	cfg.MaxRetries = maxRetries
	cfg.Logger = logger

	wp := queue.NewWorkerPool(rdb, cfg)
	wp.Handle("failing_task", func(_ context.Context, _ queue.Job) error {
		failCount.Add(1)
		return fmt.Errorf("intentional failure")
	})

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- wp.Run(runCtx) }()

	// Wait for DLQ processing — needs multiple retry cycles (claim → fail → claim → fail → DLQ).
	deadline := time.After(30 * time.Second)
	for {
		dlqKey := queue.DLQKey(site)
		length, err := rdb.XLen(ctx, dlqKey).Result()
		if err == nil && length > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for DLQ entry; failCount=%d", failCount.Load())
		case <-time.After(200 * time.Millisecond):
		}
	}

	cancel()
	<-done

	// Verify DLQ has the failed job.
	dlqKey := queue.DLQKey(site)
	msgs, err := rdb.XRange(ctx, dlqKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange DLQ: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least one message in DLQ")
	}
}

func TestIntegration_DelayedJobPromotion(t *testing.T) {
	rdb := integrationRedisClient(t)
	flushRedis(t, rdb)

	ctx := context.Background()
	logger := slog.Default()
	producer := queue.NewProducer(rdb, logger)

	site := "integ_delayed"

	// Enqueue a delayed job due in 1 second.
	job := queue.Job{
		ID:         "delayed-1",
		Site:       site,
		Type:       "delayed_task",
		Payload:    map[string]any{"delayed": true},
		CreatedAt:  time.Now().UTC(),
		MaxRetries: 3,
		Timeout:    10 * time.Second,
	}
	runAfter := time.Now().Add(1 * time.Second)
	if err := producer.EnqueueDelayed(ctx, site, queue.QueueDefault, job, runAfter); err != nil {
		t.Fatalf("EnqueueDelayed: %v", err)
	}

	// Verify it's in the delayed sorted set.
	delayedKey := queue.DelayedKey(site)
	count, err := rdb.ZCard(ctx, delayedKey).Result()
	if err != nil {
		t.Fatalf("ZCard: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 delayed job, got %d", count)
	}

	var processed atomic.Int32

	cfg := queue.DefaultWorkerPoolConfig()
	cfg.Sites = []string{site}
	cfg.QueueTypes = []queue.QueueType{queue.QueueDefault}
	cfg.ConsumersPerQueue = 1
	cfg.BlockDuration = 100 * time.Millisecond
	cfg.DLQInterval = 1 * time.Second
	cfg.DelayedPollInterval = 200 * time.Millisecond
	cfg.ClaimInterval = 1 * time.Second
	cfg.Logger = logger

	wp := queue.NewWorkerPool(rdb, cfg)
	wp.Handle("delayed_task", func(_ context.Context, _ queue.Job) error {
		processed.Add(1)
		return nil
	})

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- wp.Run(runCtx) }()

	// Wait for the delayed job to be promoted and consumed.
	deadline := time.After(10 * time.Second)
	for processed.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for delayed job to be processed")
		case <-time.After(100 * time.Millisecond):
		}
	}

	cancel()
	<-done

	// Verify it was removed from delayed set.
	count, err = rdb.ZCard(ctx, delayedKey).Result()
	if err != nil {
		t.Fatalf("ZCard after: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 delayed jobs after promotion, got %d", count)
	}
}
