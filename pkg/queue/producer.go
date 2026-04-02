package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Producer enqueues jobs to Redis Streams.
type Producer struct {
	rdb    *redis.Client
	maxLen map[QueueType]int64
	logger *slog.Logger
}

// NewProducer creates a Producer using the Queue Redis client (db1).
func NewProducer(rdb *redis.Client, logger *slog.Logger) *Producer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Producer{
		rdb: rdb,
		maxLen: map[QueueType]int64{
			QueueDefault:   10_000,
			QueueCritical:  10_000,
			QueueLong:      1_000,
			QueueScheduler: 1_000,
		},
		logger: logger,
	}
}

// WithMaxLen returns a new Producer with overridden MAXLEN values.
func (p *Producer) WithMaxLen(m map[QueueType]int64) *Producer {
	cp := *p
	cp.maxLen = make(map[QueueType]int64, len(m))
	for k, v := range m {
		cp.maxLen[k] = v
	}
	return &cp
}

// Enqueue adds a job to the Redis Stream for the given site and queue type.
// Returns the Redis-assigned stream entry ID (e.g. "1711800000000-0").
//
// Approximate MAXLEN trimming is applied to prevent unbounded memory growth.
func (p *Producer) Enqueue(ctx context.Context, site string, qt QueueType, job Job) (string, error) {
	values, err := jobToValues(job)
	if err != nil {
		return "", fmt.Errorf("enqueue: serialize job: %w", err)
	}

	args := &redis.XAddArgs{
		Stream: StreamKey(site, qt),
		Values: values,
	}
	if ml, ok := p.maxLen[qt]; ok && ml > 0 {
		args.MaxLen = ml
		args.Approx = true
	}

	id, err := p.rdb.XAdd(ctx, args).Result()
	if err != nil {
		return "", fmt.Errorf("enqueue: XAdd: %w", err)
	}

	p.logger.Debug("job enqueued",
		slog.String("site", site),
		slog.String("queue", string(qt)),
		slog.String("job_type", job.Type),
		slog.String("job_id", job.ID),
		slog.String("stream_id", id),
	)
	return id, nil
}

// EnqueueDelayed adds a job to the delayed sorted set. The delayed promoter
// will move it to the target stream once time.Now() >= runAfter.
func (p *Producer) EnqueueDelayed(ctx context.Context, site string, qt QueueType, job Job, runAfter time.Time) error {
	entry := delayedEntry{Job: job, QueueType: qt}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("enqueue delayed: marshal: %w", err)
	}

	err = p.rdb.ZAdd(ctx, DelayedKey(site), redis.Z{
		Score:  float64(runAfter.UnixNano()),
		Member: string(data),
	}).Err()
	if err != nil {
		return fmt.Errorf("enqueue delayed: ZADD: %w", err)
	}

	p.logger.Debug("delayed job enqueued",
		slog.String("site", site),
		slog.String("queue", string(qt)),
		slog.String("job_type", job.Type),
		slog.String("job_id", job.ID),
		slog.Time("run_after", runAfter),
	)
	return nil
}
