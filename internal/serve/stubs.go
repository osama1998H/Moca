package serve

import (
	"context"
	"log/slog"
)

// WorkerStub is a placeholder subsystem for the background worker.
// It logs once at startup, then blocks until the context is cancelled.
func WorkerStub(logger *slog.Logger) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		logger.Info("worker: stub (not yet implemented)")
		<-ctx.Done()
		return nil
	}
}

// SchedulerStub is a placeholder subsystem for the cron scheduler.
// It logs once at startup, then blocks until the context is cancelled.
func SchedulerStub(logger *slog.Logger) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		logger.Info("scheduler: stub (not yet implemented)")
		<-ctx.Done()
		return nil
	}
}

// OutboxStub is a placeholder subsystem for the transactional outbox poller.
// It logs once at startup, then blocks until the context is cancelled.
func OutboxStub(logger *slog.Logger) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		logger.Info("outbox: stub (not yet implemented)")
		<-ctx.Done()
		return nil
	}
}
