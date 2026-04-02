package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// delayedPromoter polls moca:delayed:{site} sorted sets and moves
// due jobs to their target streams.
type delayedPromoter struct {
	rdb      *redis.Client
	logger   *slog.Logger
	maxLen   map[QueueType]int64
	sites    []string
	interval time.Duration
}

// run blocks until ctx is cancelled, promoting on each tick.
func (dp *delayedPromoter) run(ctx context.Context) error {
	ticker := time.NewTicker(dp.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for _, site := range dp.sites {
				n, err := dp.promoteReady(ctx, site)
				if err != nil {
					if ctx.Err() != nil {
						return nil
					}
					dp.logger.Error("delayed promoter error",
						slog.String("site", site),
						slog.String("error", err.Error()),
					)
					continue
				}
				if n > 0 {
					dp.logger.Info("promoted delayed jobs",
						slog.String("site", site),
						slog.Int("count", n),
					)
				}
			}
		}
	}
}

// promoteReady finds and promotes all due jobs for one site.
func (dp *delayedPromoter) promoteReady(ctx context.Context, site string) (int, error) {
	key := DelayedKey(site)
	now := time.Now().UnixNano()

	members, err := dp.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     key,
		Start:   "-inf",
		Stop:    strconv.FormatInt(now, 10),
		ByScore: true,
	}).Result()
	if err != nil {
		return 0, fmt.Errorf("ZRANGEBYSCORE: %w", err)
	}

	if len(members) == 0 {
		return 0, nil
	}

	promoted := 0
	for _, member := range members {
		var entry delayedEntry
		if err := json.Unmarshal([]byte(member), &entry); err != nil {
			dp.logger.Error("malformed delayed entry, removing",
				slog.String("site", site),
				slog.String("error", err.Error()),
			)
			dp.rdb.ZRem(ctx, key, member)
			continue
		}

		values, err := jobToValues(entry.Job)
		if err != nil {
			dp.logger.Error("failed to serialize delayed job, removing",
				slog.String("site", site),
				slog.String("job_id", entry.Job.ID),
				slog.String("error", err.Error()),
			)
			dp.rdb.ZRem(ctx, key, member)
			continue
		}

		stream := StreamKey(site, entry.QueueType)
		args := &redis.XAddArgs{
			Stream: stream,
			Values: values,
		}
		if ml, ok := dp.maxLen[entry.QueueType]; ok && ml > 0 {
			args.MaxLen = ml
			args.Approx = true
		}

		// Use pipeline to minimize the window between XADD and ZREM.
		pipe := dp.rdb.Pipeline()
		pipe.XAdd(ctx, args)
		pipe.ZRem(ctx, key, member)
		_, err = pipe.Exec(ctx)
		if err != nil {
			return promoted, fmt.Errorf("promote delayed job: %w", err)
		}

		promoted++
	}

	return promoted, nil
}
