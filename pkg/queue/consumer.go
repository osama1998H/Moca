package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// consumer represents a single goroutine consuming from one Redis Stream
// as part of a consumer group.
type consumer struct {
	rdb           *redis.Client
	handlers      map[string]JobHandler
	logger        *slog.Logger
	stream        string
	group         string
	name          string
	blockDuration time.Duration
}

// ensureGroup creates a consumer group on the stream if it doesn't exist.
// Uses MKSTREAM so the stream is created if absent. Idempotent: swallows
// BUSYGROUP errors when the group already exists.
func ensureGroup(ctx context.Context, rdb *redis.Client, stream, group string) error {
	err := rdb.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil {
		if err.Error() == "BUSYGROUP Consumer Group name already exists" {
			return nil
		}
		return fmt.Errorf("XGroupCreateMkStream(%s, %s): %w", stream, group, err)
	}
	return nil
}

// run is the consumer loop. It blocks until ctx is cancelled, reading
// messages via XReadGroup and dispatching to registered handlers.
func (c *consumer) run(ctx context.Context) error {
	if err := ensureGroup(ctx, c.rdb, c.stream, c.group); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	c.logger.Info("consumer started",
		slog.String("stream", c.stream),
		slog.String("group", c.group),
		slog.String("consumer", c.name),
	)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.group,
			Consumer: c.name,
			Streams:  []string{c.stream, ">"},
			Count:    10,
			Block:    c.blockDuration,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue // timeout, no new messages
			}
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("XReadGroup: %w", err)
		}

		for _, s := range streams {
			for _, msg := range s.Messages {
				c.processMessage(ctx, msg)
			}
		}
	}
}

// processMessage dispatches a single stream message to the appropriate handler.
func (c *consumer) processMessage(ctx context.Context, msg redis.XMessage) {
	job, err := valuesToJob(msg.Values)
	if err != nil {
		c.logger.Error("malformed job in stream, acknowledging to skip",
			slog.String("stream", c.stream),
			slog.String("message_id", msg.ID),
			slog.String("error", err.Error()),
		)
		_ = c.rdb.XAck(ctx, c.stream, c.group, msg.ID).Err()
		return
	}

	handler, ok := c.handlers[job.Type]
	if !ok {
		c.logger.Warn("no handler registered for job type, acknowledging to skip",
			slog.String("job_type", job.Type),
			slog.String("stream", c.stream),
			slog.String("message_id", msg.ID),
		)
		_ = c.rdb.XAck(ctx, c.stream, c.group, msg.ID).Err()
		return
	}

	if err := handler(ctx, job); err != nil {
		c.logger.Warn("job handler failed, leaving in PEL for retry",
			slog.String("job_type", job.Type),
			slog.String("job_id", job.ID),
			slog.String("message_id", msg.ID),
			slog.String("error", err.Error()),
		)
		return // no XAck — stays in PEL for redelivery/DLQ
	}

	_ = c.rdb.XAck(ctx, c.stream, c.group, msg.ID).Err()
}
