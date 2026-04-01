package process_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/moca-framework/moca/internal/process"
)

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestSupervisor_EmptyReturnsNil(t *testing.T) {
	var buf bytes.Buffer
	sup := process.NewSupervisor(testLogger(&buf))

	err := sup.Run(context.Background())
	if err != nil {
		t.Errorf("Run with no subsystems should return nil, got: %v", err)
	}
}

func TestSupervisor_AllSubsystemsStart(t *testing.T) {
	var buf bytes.Buffer
	sup := process.NewSupervisor(testLogger(&buf))

	var started sync.Map
	for i := range 3 {
		name := fmt.Sprintf("sub-%d", i)
		sup.Add(process.Subsystem{
			Name: name,
			Run: func(ctx context.Context) error {
				started.Store(name, true)
				<-ctx.Done()
				return nil
			},
		})
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx) }()

	// Give subsystems time to start.
	time.Sleep(50 * time.Millisecond)
	cancel()

	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	for i := range 3 {
		name := fmt.Sprintf("sub-%d", i)
		if _, ok := started.Load(name); !ok {
			t.Errorf("subsystem %s did not start", name)
		}
	}
}

func TestSupervisor_ContextCancellationTriggersShutdown(t *testing.T) {
	var buf bytes.Buffer
	sup := process.NewSupervisor(testLogger(&buf))

	for i := range 2 {
		name := fmt.Sprintf("sub-%d", i)
		sup.Add(process.Subsystem{
			Name: name,
			Run: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			},
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx) }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run should return nil on clean cancellation, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s after cancellation")
	}
}

func TestSupervisor_CriticalFailureCascades(t *testing.T) {
	var buf bytes.Buffer
	sup := process.NewSupervisor(testLogger(&buf))

	critErr := errors.New("critical boom")
	nonCritDone := make(chan struct{})

	sup.Add(process.Subsystem{
		Name:     "critical",
		Critical: true,
		Run: func(ctx context.Context) error {
			time.Sleep(20 * time.Millisecond)
			return critErr
		},
	})

	sup.Add(process.Subsystem{
		Name: "follower",
		Run: func(ctx context.Context) error {
			<-ctx.Done()
			close(nonCritDone)
			return nil
		},
	})

	err := sup.Run(context.Background())
	if err == nil {
		t.Fatal("Run should return error when critical subsystem fails")
	}
	if !errors.Is(err, critErr) {
		t.Errorf("Run error should wrap critErr, got: %v", err)
	}

	// The follower's context should have been cancelled.
	select {
	case <-nonCritDone:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("follower subsystem was not cancelled after critical failure")
	}
}

func TestSupervisor_NonCriticalFailureNoCascade(t *testing.T) {
	var buf bytes.Buffer
	sup := process.NewSupervisor(testLogger(&buf))

	sup.Add(process.Subsystem{
		Name:     "flaky",
		Critical: false,
		Run: func(ctx context.Context) error {
			return errors.New("non-critical oops")
		},
	})

	critCtxCancelled := make(chan struct{})
	sup.Add(process.Subsystem{
		Name:     "stable",
		Critical: true,
		Run: func(ctx context.Context) error {
			<-ctx.Done()
			close(critCtxCancelled)
			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx) }()

	// Wait long enough for the non-critical to have failed but stable should still be running.
	time.Sleep(100 * time.Millisecond)

	// Stable should still be running — verify its context was NOT cancelled.
	select {
	case <-critCtxCancelled:
		t.Fatal("stable subsystem's context was cancelled by non-critical failure")
	default:
		// expected
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run should return nil (no critical failure), got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s")
	}
}

func TestSupervisor_ShutdownTimeoutRespected(t *testing.T) {
	var buf bytes.Buffer
	sup := process.NewSupervisor(testLogger(&buf), process.WithShutdownTimeout(100*time.Millisecond))

	sup.Add(process.Subsystem{
		Name:     "hung",
		Critical: true,
		Run: func(ctx context.Context) error {
			// Ignore context cancellation — simulate a stuck subsystem.
			time.Sleep(10 * time.Second)
			return nil
		},
	})

	sup.Add(process.Subsystem{
		Name:     "trigger",
		Critical: true,
		Run: func(ctx context.Context) error {
			return errors.New("boom")
		},
	})

	start := time.Now()
	_ = sup.Run(context.Background())
	elapsed := time.Since(start)

	// Should complete well within 2s (timeout is 100ms + some overhead).
	if elapsed > 2*time.Second {
		t.Errorf("Run took %v, expected ~100ms timeout", elapsed)
	}
}
