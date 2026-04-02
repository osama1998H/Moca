package queue

import (
	"log/slog"
	"time"
)

// WorkerPoolConfig configures the WorkerPool.
type WorkerPoolConfig struct {
	// Logger for structured logging. Falls back to slog.Default() if nil.
	Logger *slog.Logger

	// MaxLen is the approximate MAXLEN cap per queue type for stream trimming.
	MaxLen map[QueueType]int64

	// Sites is the list of tenant sites to consume from.
	Sites []string

	// QueueTypes specifies which queue types to consume. Defaults to AllQueueTypes.
	QueueTypes []QueueType

	// BlockDuration is the XReadGroup block timeout. Default: 2s.
	BlockDuration time.Duration

	// ClaimInterval is how often the claimer sweeps for orphaned messages. Default: 30s.
	ClaimInterval time.Duration

	// ClaimMinIdle is the minimum idle time before a pending message can be reclaimed.
	// Default: 30s.
	ClaimMinIdle time.Duration

	// DLQInterval is how often the DLQ processor checks for over-retried messages.
	// Default: 10s.
	DLQInterval time.Duration

	// DelayedPollInterval is how often the delayed promoter polls for due jobs.
	// Default: 500ms.
	DelayedPollInterval time.Duration

	// ConsumersPerQueue is the number of consumer goroutines per (site, queueType) pair.
	// Default: 2.
	ConsumersPerQueue int

	// MaxRetries is the delivery attempt threshold before a message moves to DLQ.
	// Default: 3.
	MaxRetries int
}

// DefaultWorkerPoolConfig returns a WorkerPoolConfig with production defaults.
func DefaultWorkerPoolConfig() WorkerPoolConfig {
	return WorkerPoolConfig{
		QueueTypes:          AllQueueTypes,
		ConsumersPerQueue:   2,
		BlockDuration:       2 * time.Second,
		ClaimInterval:       30 * time.Second,
		ClaimMinIdle:        30 * time.Second,
		DLQInterval:         10 * time.Second,
		MaxRetries:          3,
		DelayedPollInterval: 500 * time.Millisecond,
		MaxLen: map[QueueType]int64{
			QueueDefault:   10_000,
			QueueCritical:  10_000,
			QueueLong:      1_000,
			QueueScheduler: 1_000,
		},
	}
}
