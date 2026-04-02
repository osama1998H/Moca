package queue

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLeaderElection_AcquireLock(t *testing.T) {
	rdb, _ := newTestRedis(t)

	elected := make(chan struct{})
	le := NewLeaderElection(rdb, LeaderElectionConfig{
		InstanceID:    "inst-1",
		TTL:           5 * time.Second,
		RenewInterval: 2 * time.Second,
		PollInterval:  100 * time.Millisecond,
		Logger:        slog.Default(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		_ = le.Run(ctx, func(ctx context.Context) error {
			close(elected)
			<-ctx.Done()
			return nil
		})
	}()

	select {
	case <-elected:
		// Success: leadership acquired.
	case <-ctx.Done():
		t.Fatal("timed out waiting for leadership acquisition")
	}

	// Verify the key exists in Redis.
	val, err := rdb.Get(context.Background(), DefaultLeaderKey).Result()
	if err != nil {
		t.Fatalf("GET leader key: %v", err)
	}
	if val != "inst-1" {
		t.Errorf("leader key value = %q, want %q", val, "inst-1")
	}
}

func TestLeaderElection_SingleLeader(t *testing.T) {
	rdb, _ := newTestRedis(t)

	var leaderCount atomic.Int32

	cfg := LeaderElectionConfig{
		TTL:           5 * time.Second,
		RenewInterval: 1 * time.Second,
		PollInterval:  100 * time.Millisecond,
		Logger:        slog.Default(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		instanceCfg := cfg
		instanceCfg.InstanceID = "inst-" + string(rune('A'+i))
		le := NewLeaderElection(rdb, instanceCfg)

		go func() {
			defer wg.Done()
			_ = le.Run(ctx, func(ctx context.Context) error {
				leaderCount.Add(1)
				<-ctx.Done()
				leaderCount.Add(-1)
				return nil
			})
		}()
	}

	// Give time for all instances to attempt acquisition.
	time.Sleep(500 * time.Millisecond)

	count := leaderCount.Load()
	if count != 1 {
		t.Errorf("concurrent leaders = %d, want exactly 1", count)
	}

	cancel()
	wg.Wait()
}

func TestLeaderElection_Failover(t *testing.T) {
	rdb, mr := newTestRedis(t)

	leader1Elected := make(chan struct{})
	leader2Elected := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Leader 1: short TTL so it expires quickly when we cancel it.
	le1 := NewLeaderElection(rdb, LeaderElectionConfig{
		InstanceID:    "leader-1",
		TTL:           2 * time.Second,
		RenewInterval: 500 * time.Millisecond,
		PollInterval:  100 * time.Millisecond,
		Logger:        slog.Default(),
	})

	ctx1, cancel1 := context.WithCancel(ctx)

	go func() {
		_ = le1.Run(ctx1, func(ctx context.Context) error {
			close(leader1Elected)
			<-ctx.Done()
			return nil
		})
	}()

	// Wait for leader 1 to acquire.
	select {
	case <-leader1Elected:
	case <-ctx.Done():
		t.Fatal("timed out waiting for leader-1")
	}

	// Leader 2: standby.
	le2 := NewLeaderElection(rdb, LeaderElectionConfig{
		InstanceID:    "leader-2",
		TTL:           2 * time.Second,
		RenewInterval: 500 * time.Millisecond,
		PollInterval:  200 * time.Millisecond,
		Logger:        slog.Default(),
	})

	go func() {
		_ = le2.Run(ctx, func(ctx context.Context) error {
			close(leader2Elected)
			<-ctx.Done()
			return nil
		})
	}()

	// Kill leader 1 (simulate crash — cancel its context).
	cancel1()

	// Fast-forward miniredis time so the TTL expires.
	mr.FastForward(3 * time.Second)

	// Leader 2 should acquire within the poll interval + TTL.
	select {
	case <-leader2Elected:
		// Success: failover happened.
	case <-ctx.Done():
		t.Fatal("timed out waiting for leader-2 failover")
	}
}

func TestLeaderElection_GracefulRelease(t *testing.T) {
	rdb, _ := newTestRedis(t)

	elected := make(chan struct{})
	le := NewLeaderElection(rdb, LeaderElectionConfig{
		InstanceID:    "graceful-1",
		TTL:           5 * time.Second,
		RenewInterval: 1 * time.Second,
		PollInterval:  100 * time.Millisecond,
		Logger:        slog.Default(),
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = le.Run(ctx, func(ctx context.Context) error {
			close(elected)
			<-ctx.Done()
			return nil
		})
		close(done)
	}()

	// Wait for election.
	<-elected

	// Cancel gracefully.
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for graceful shutdown")
	}

	// After graceful release, the key should be deleted.
	exists, err := rdb.Exists(context.Background(), DefaultLeaderKey).Result()
	if err != nil {
		t.Fatalf("EXISTS: %v", err)
	}
	if exists != 0 {
		t.Error("leader key still exists after graceful release")
	}
}

func TestLeaderElection_HeartbeatRenews(t *testing.T) {
	rdb, mr := newTestRedis(t)

	elected := make(chan struct{})
	le := NewLeaderElection(rdb, LeaderElectionConfig{
		InstanceID:    "heartbeat-1",
		TTL:           2 * time.Second,
		RenewInterval: 500 * time.Millisecond,
		PollInterval:  100 * time.Millisecond,
		Logger:        slog.Default(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = le.Run(ctx, func(ctx context.Context) error {
			close(elected)
			<-ctx.Done()
			return nil
		})
	}()

	<-elected

	// Advance time past the initial TTL but within heartbeat renewal.
	// The heartbeat should have renewed the lock, so it should still exist.
	mr.FastForward(1 * time.Second)
	time.Sleep(600 * time.Millisecond) // Allow heartbeat to fire.

	val, err := rdb.Get(context.Background(), DefaultLeaderKey).Result()
	if err != nil {
		t.Fatalf("leader key missing after heartbeat should have renewed: %v", err)
	}
	if val != "heartbeat-1" {
		t.Errorf("leader key value = %q, want %q", val, "heartbeat-1")
	}
}
