package queue

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_Register(t *testing.T) {
	rdb, _ := newTestRedis(t)
	p := NewProducer(rdb, slog.Default())
	s := NewScheduler(p)

	// Valid expressions.
	for _, expr := range []string{
		"* * * * *",
		"*/5 * * * *",
		"@every 1h",
		"@daily",
		"0 9 * * 1-5",
	} {
		err := s.Register(CronEntry{
			Name:     "test-" + expr,
			CronExpr: expr,
			Site:     "acme",
			JobType:  "test",
		})
		if err != nil {
			t.Errorf("Register(%q) returned error: %v", expr, err)
		}
	}

	// Invalid expressions.
	for _, expr := range []string{
		"not a cron",
		"* * *",
		"",
	} {
		err := s.Register(CronEntry{
			Name:     "bad",
			CronExpr: expr,
			Site:     "acme",
			JobType:  "test",
		})
		if err == nil {
			t.Errorf("Register(%q) should have returned error", expr)
		}
	}
}

func TestScheduler_FiresOnSchedule(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := NewProducer(rdb, slog.Default())
	s := NewScheduler(p,
		WithTickInterval(100*time.Millisecond),
		WithSchedulerLogger(slog.Default()),
	)

	// "every minute" — but with a short tick interval, the first fire
	// should happen within 1 minute. We'll use @every 1s for testing.
	err := s.Register(CronEntry{
		Name:     "fast-cron",
		CronExpr: "@every 1s",
		Site:     "acme",
		JobType:  "ping",
		Payload:  map[string]any{"key": "value"},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	go func() {
		_ = s.Run(ctx)
	}()

	// Poll the stream for enqueued jobs.
	stream := StreamKey("acme", QueueScheduler)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for cron job to be enqueued")
		default:
		}

		xlen, err := rdb.XLen(ctx, stream).Result()
		if err != nil {
			t.Fatalf("XLEN: %v", err)
		}
		if xlen > 0 {
			// Verify the job content.
			msgs, err := rdb.XRange(ctx, stream, "-", "+").Result()
			if err != nil {
				t.Fatalf("XRANGE: %v", err)
			}
			job, err := valuesToJob(msgs[0].Values)
			if err != nil {
				t.Fatalf("valuesToJob: %v", err)
			}
			if job.Type != "ping" {
				t.Errorf("job type = %q, want %q", job.Type, "ping")
			}
			if job.Site != "acme" {
				t.Errorf("job site = %q, want %q", job.Site, "acme")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestScheduler_MultipleEntries(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := NewProducer(rdb, slog.Default())
	s := NewScheduler(p, WithTickInterval(100*time.Millisecond))

	_ = s.Register(CronEntry{
		Name:     "job-a",
		CronExpr: "@every 1s",
		Site:     "acme",
		JobType:  "type-a",
	})
	_ = s.Register(CronEntry{
		Name:     "job-b",
		CronExpr: "@every 1s",
		Site:     "corp",
		JobType:  "type-b",
	})

	go func() {
		_ = s.Run(ctx)
	}()

	// Wait for both streams to have at least one job.
	streamA := StreamKey("acme", QueueScheduler)
	streamB := StreamKey("corp", QueueScheduler)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for both cron jobs")
		default:
		}

		lenA, _ := rdb.XLen(ctx, streamA).Result()
		lenB, _ := rdb.XLen(ctx, streamB).Result()
		if lenA > 0 && lenB > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestScheduler_GracefulShutdown(t *testing.T) {
	rdb, _ := newTestRedis(t)
	p := NewProducer(rdb, slog.Default())
	s := NewScheduler(p, WithTickInterval(100*time.Millisecond))

	_ = s.Register(CronEntry{
		Name:     "shutdown-test",
		CronExpr: "@every 1s",
		Site:     "acme",
		JobType:  "noop",
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx)
		close(done)
	}()

	// Let it run briefly then cancel.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Clean shutdown.
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler did not shut down gracefully")
	}
}

func TestScheduler_DefaultQueueType(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := NewProducer(rdb, slog.Default())
	s := NewScheduler(p, WithTickInterval(100*time.Millisecond))

	// Register without explicit QueueType — should default to QueueScheduler.
	_ = s.Register(CronEntry{
		Name:     "default-queue",
		CronExpr: "@every 1s",
		Site:     "acme",
		JobType:  "test",
	})

	go func() {
		_ = s.Run(ctx)
	}()

	stream := StreamKey("acme", QueueScheduler)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for job on scheduler queue")
		default:
		}
		xlen, _ := rdb.XLen(ctx, stream).Result()
		if xlen > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestScheduler_RunWithLeader(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := NewProducer(rdb, slog.Default())
	s := NewScheduler(p, WithTickInterval(100*time.Millisecond))

	_ = s.Register(CronEntry{
		Name:     "leader-test",
		CronExpr: "@every 1s",
		Site:     "acme",
		JobType:  "leader-job",
	})

	le := NewLeaderElection(rdb, LeaderElectionConfig{
		InstanceID:    "sched-1",
		TTL:           5 * time.Second,
		RenewInterval: 1 * time.Second,
		PollInterval:  100 * time.Millisecond,
		Logger:        slog.Default(),
	})

	var fired atomic.Bool
	done := make(chan struct{})
	go func() {
		_ = s.RunWithLeader(ctx, le)
		close(done)
	}()

	// Poll for the job.
	stream := StreamKey("acme", QueueScheduler)
	deadline := time.After(4 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for leader-elected cron job")
		default:
		}
		xlen, _ := rdb.XLen(ctx, stream).Result()
		if xlen > 0 {
			fired.Store(true)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !fired.Load() {
		t.Error("cron job was never fired under leader election")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunWithLeader did not shut down")
	}
}
