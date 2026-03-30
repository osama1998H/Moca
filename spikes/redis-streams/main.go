// Package main implements a spike validating Redis Streams consumer groups
// as a job queue for the MOCA framework.
//
// Spike: MS-00-T3
// Design ref: MOCA_SYSTEM_DESIGN.md §5.2 (lines 1073-1107), ADR-002
//
// Key architectural bet being validated:
//
//	Redis Streams with consumer groups provide sufficient at-least-once
//	delivery, load balancing, and dead-letter queue semantics to replace
//	a dedicated message broker (RabbitMQ/SQS) for MOCA's background
//	job workloads.
//
// This is throwaway spike code. Do not promote to pkg/.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Job represents a background task enqueued to a Redis Stream.
// Field layout matches MOCA_SYSTEM_DESIGN.md §5.2 lines 1088-1099.
type Job struct {
	ID         string         `json:"id"`
	Site       string         `json:"site"`
	Type       string         `json:"type"`
	Payload    map[string]any `json:"payload"`
	Priority   int            `json:"priority"`
	MaxRetries int            `json:"max_retries"`
	Retries    int            `json:"retries"`
	CreatedAt  time.Time      `json:"created_at"`
	RunAfter   *time.Time     `json:"run_after,omitempty"`
	Timeout    time.Duration  `json:"timeout"`
}

// JobHandler is a callback invoked for each job consumed from a stream.
// Return nil to acknowledge. Return an error to leave unacknowledged (stays
// in the Pending Entries List for redelivery or DLQ processing).
type JobHandler func(ctx context.Context, job Job) error

// StreamKey returns the Redis stream key for a site and queue type.
// Convention from MOCA_SYSTEM_DESIGN.md §5.2: moca:queue:{site}:{queueType}
// Valid queueType values: "default", "long", "critical", "scheduler"
func StreamKey(site, queueType string) string {
	return fmt.Sprintf("moca:queue:%s:%s", site, queueType)
}

// DLQKey returns the dead-letter queue stream key for a site.
// Convention: moca:deadletter:{site}
func DLQKey(site string) string {
	return fmt.Sprintf("moca:deadletter:%s", site)
}

// jobToValues converts a Job to a flat map suitable for XAdd.
// Each Job field becomes a separate stream entry field. The Payload is
// JSON-encoded because it is map[string]any (cannot be stored flat).
// Flat-map serialization lets consumers inspect individual fields
// (type, site, run_after) without full deserialization.
func jobToValues(j Job) (map[string]interface{}, error) {
	payloadJSON, err := json.Marshal(j.Payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	m := map[string]interface{}{
		"id":          j.ID,
		"site":        j.Site,
		"type":        j.Type,
		"payload":     string(payloadJSON),
		"priority":    strconv.Itoa(j.Priority),
		"max_retries": strconv.Itoa(j.MaxRetries),
		"retries":     strconv.Itoa(j.Retries),
		"created_at":  j.CreatedAt.UTC().Format(time.RFC3339Nano),
		"timeout":     j.Timeout.String(),
	}
	if j.RunAfter != nil {
		m["run_after"] = j.RunAfter.UTC().Format(time.RFC3339Nano)
	}
	return m, nil
}

// valuesToJob reconstructs a Job from a Redis stream message's Values map.
// Inverse of jobToValues.
func valuesToJob(values map[string]interface{}) (Job, error) {
	getString := func(key string) string {
		v, _ := values[key]
		s, _ := v.(string)
		return s
	}

	createdAt, err := time.Parse(time.RFC3339Nano, getString("created_at"))
	if err != nil {
		return Job{}, fmt.Errorf("parse created_at: %w", err)
	}

	priority, _ := strconv.Atoi(getString("priority"))
	maxRetries, _ := strconv.Atoi(getString("max_retries"))
	retries, _ := strconv.Atoi(getString("retries"))

	timeout, err := time.ParseDuration(getString("timeout"))
	if err != nil {
		return Job{}, fmt.Errorf("parse timeout: %w", err)
	}

	var payload map[string]any
	if p := getString("payload"); p != "" {
		if err := json.Unmarshal([]byte(p), &payload); err != nil {
			return Job{}, fmt.Errorf("unmarshal payload: %w", err)
		}
	}

	j := Job{
		ID:         getString("id"),
		Site:       getString("site"),
		Type:       getString("type"),
		Payload:    payload,
		Priority:   priority,
		MaxRetries: maxRetries,
		Retries:    retries,
		CreatedAt:  createdAt,
		Timeout:    timeout,
	}

	if ra := getString("run_after"); ra != "" {
		t, err := time.Parse(time.RFC3339Nano, ra)
		if err != nil {
			return Job{}, fmt.Errorf("parse run_after: %w", err)
		}
		j.RunAfter = &t
	}

	return j, nil
}

// Enqueue adds a job to the specified Redis stream using XADD.
// Returns the Redis-assigned stream entry ID (e.g. "1711800000000-0").
//
// When maxLen > 0, approximate stream trimming is applied (MAXLEN ~maxLen)
// to prevent unbounded memory growth. Pass 0 to skip trimming.
func Enqueue(ctx context.Context, rdb *redis.Client, streamKey string, job Job, maxLen int64) (string, error) {
	values, err := jobToValues(job)
	if err != nil {
		return "", fmt.Errorf("enqueue: serialize job: %w", err)
	}

	args := &redis.XAddArgs{
		Stream: streamKey,
		Values: values,
	}
	if maxLen > 0 {
		args.MaxLen = maxLen
		args.Approx = true
	}

	id, err := rdb.XAdd(ctx, args).Result()
	if err != nil {
		return "", fmt.Errorf("enqueue: XAdd: %w", err)
	}
	return id, nil
}

// ensureGroup creates a consumer group on the stream if it does not already exist.
// Uses MKSTREAM so the stream is created if absent. Ignores BUSYGROUP errors
// (group already exists).
func ensureGroup(ctx context.Context, rdb *redis.Client, stream, group string) error {
	err := rdb.XGroupCreateMkStream(ctx, stream, group, "$").Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		// BUSYGROUP Consumer Group name already exists
		if err.Error() == "BUSYGROUP Consumer Group name already exists" {
			return nil
		}
		return fmt.Errorf("XGroupCreateMkStream: %w", err)
	}
	return nil
}

// Consume reads messages from a Redis stream as part of a consumer group.
// It loops, calling XReadGroup with the given block duration, invokes handler
// for each message, and XAcks on success. Messages where handler returns an
// error are left unacknowledged (they remain in the Pending Entries List).
//
// The consumer exits when ctx is cancelled.
func Consume(ctx context.Context, rdb *redis.Client, stream, group, consumer string, blockDuration time.Duration, handler JobHandler) error {
	if err := ensureGroup(ctx, rdb, stream, group); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		streams, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: consumer,
			Streams:  []string{stream, ">"},
			Count:    10,
			Block:    blockDuration,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				// Timeout — no new messages; loop and try again
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("XReadGroup: %w", err)
		}

		for _, s := range streams {
			for _, msg := range s.Messages {
				job, err := valuesToJob(msg.Values)
				if err != nil {
					// Cannot parse the job — move to DLQ or skip
					// For the spike, just acknowledge malformed entries.
					_ = rdb.XAck(ctx, stream, group, msg.ID).Err()
					continue
				}

				if err := handler(ctx, job); err == nil {
					_ = rdb.XAck(ctx, stream, group, msg.ID).Err()
				}
				// On handler error: leave in PEL for redelivery/DLQ processing.
			}
		}
	}
}

// ClaimPending uses XAutoClaim to reclaim messages that have been pending
// longer than minIdle. This is the at-least-once delivery recovery mechanism:
// when a consumer dies, another consumer claims its pending messages.
//
// Returns the claimed messages and a cursor for pagination ("0-0" means done).
func ClaimPending(ctx context.Context, rdb *redis.Client, stream, group, consumer string, minIdle time.Duration) ([]redis.XMessage, string, error) {
	messages, next, err := rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   stream,
		Group:    group,
		Consumer: consumer,
		MinIdle:  minIdle,
		Start:    "0-0",
		Count:    100,
	}).Result()
	if err != nil {
		return nil, "", fmt.Errorf("XAutoClaim: %w", err)
	}
	return messages, next, nil
}

// ProcessDLQ inspects the Pending Entries List for messages that have exceeded
// maxRetries delivery attempts. Such messages are moved to the dead-letter
// stream via XAdd, then acknowledged in the original stream to remove them
// from the PEL.
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

		// Claim the message so we can read its content.
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

		// Enrich with retry metadata and add to DLQ stream.
		dlqValues := make(map[string]interface{}, len(msg.Values)+2)
		for k, v := range msg.Values {
			dlqValues[k] = v
		}
		dlqValues["dlq_original_id"] = msg.ID
		dlqValues["dlq_retry_count"] = strconv.FormatInt(p.RetryCount, 10)

		if _, err := rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: dlqStream,
			Values: dlqValues,
		}).Result(); err != nil {
			return moved, fmt.Errorf("XAdd to DLQ: %w", err)
		}

		// Acknowledge in original stream to remove from PEL.
		if err := rdb.XAck(ctx, stream, group, msg.ID).Err(); err != nil {
			return moved, fmt.Errorf("XAck after DLQ move: %w", err)
		}

		moved++
	}

	return moved, nil
}

func main() {
	// The spike is test-driven. Run: go test -v -count=1 -race ./...
	// See main_test.go for all validation scenarios.
	fmt.Println("MS-00-T3: Redis Streams Consumer Group Spike")
	fmt.Println("Run: go test -v -count=1 -race ./...")
}
