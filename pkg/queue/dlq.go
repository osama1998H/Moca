package queue

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// ProcessDLQ inspects the Pending Entries List for messages that have exceeded
// maxRetries delivery attempts. Such messages are moved to the dead-letter
// stream, then acknowledged in the original stream to clear the PEL.
//
// Returns the number of messages moved to the DLQ.
func ProcessDLQ(ctx context.Context, rdb *redis.Client, stream, group, dlqStream string, maxRetries int64) (int, error) {
	pending, err := rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: stream,
		Group:  group,
		Start:  "-",
		End:    "+",
		Count:  100,
	}).Result()
	if err != nil {
		return 0, fmt.Errorf("XPendingExt: %w", err)
	}

	moved := 0
	for _, p := range pending {
		if p.RetryCount <= maxRetries {
			continue
		}

		claimed, err := rdb.XClaim(ctx, &redis.XClaimArgs{
			Stream:   stream,
			Group:    group,
			Consumer: "dlq-processor",
			MinIdle:  0,
			Messages: []string{p.ID},
		}).Result()
		if err != nil || len(claimed) == 0 {
			continue
		}

		msg := claimed[0]

		dlqValues := make(map[string]interface{}, len(msg.Values)+3)
		for k, v := range msg.Values {
			dlqValues[k] = v
		}
		dlqValues["dlq_original_id"] = msg.ID
		dlqValues["dlq_retry_count"] = strconv.FormatInt(p.RetryCount, 10)
		dlqValues["dlq_moved_at"] = time.Now().UTC().Format(time.RFC3339Nano)

		if _, err := rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: dlqStream,
			Values: dlqValues,
		}).Result(); err != nil {
			return moved, fmt.Errorf("XAdd to DLQ: %w", err)
		}

		if err := rdb.XAck(ctx, stream, group, msg.ID).Err(); err != nil {
			return moved, fmt.Errorf("XAck after DLQ move: %w", err)
		}

		moved++
	}

	return moved, nil
}

// dlqProcessor is a long-running goroutine that periodically sweeps
// all streams for messages that should be moved to the DLQ.
type dlqProcessor struct {
	rdb        *redis.Client
	logger     *slog.Logger
	sites      []string
	queueTypes []QueueType
	interval   time.Duration
	maxRetries int
}

// run blocks until ctx is cancelled, sweeping on each tick.
func (d *dlqProcessor) run(ctx context.Context) error {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			d.sweep(ctx)
		}
	}
}

func (d *dlqProcessor) sweep(ctx context.Context) {
	for _, site := range d.sites {
		dlqStream := DLQKey(site)
		group := GroupName(site)
		for _, qt := range d.queueTypes {
			stream := StreamKey(site, qt)
			moved, err := ProcessDLQ(ctx, d.rdb, stream, group, dlqStream, int64(d.maxRetries))
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				d.logger.Error("DLQ sweep error",
					slog.String("stream", stream),
					slog.String("error", err.Error()),
				)
				continue
			}
			if moved > 0 {
				d.logger.Info("moved messages to DLQ",
					slog.String("site", site),
					slog.String("queue", string(qt)),
					slog.Int("count", moved),
				)
			}
		}
	}
}
