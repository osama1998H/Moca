package queue

import (
	"context"
	"log/slog"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestProcessDLQ(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	stream := StreamKey("acme", QueueDefault)
	group := GroupName("acme")
	dlqStream := DLQKey("acme")

	// Enqueue a job.
	job := testJob("dlq-job", "acme", "failing_task")
	if _, err := p.Enqueue(ctx, "acme", QueueDefault, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Create consumer group and read the message (puts it in PEL).
	if err := ensureGroup(ctx, rdb, stream, group); err != nil {
		t.Fatalf("ensureGroup: %v", err)
	}

	_, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: "test-consumer",
		Streams:  []string{stream, ">"},
		Count:    10,
	}).Result()
	if err != nil {
		t.Fatalf("XReadGroup: %v", err)
	}
	// Don't ACK — message stays in PEL.

	// Simulate multiple deliveries by reading the same message multiple times
	// using XCLAIM to increment the retry count.
	pending, _ := rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: stream,
		Group:  group,
		Start:  "-",
		End:    "+",
		Count:  10,
	}).Result()
	if len(pending) == 0 {
		t.Fatal("expected pending messages")
	}

	msgID := pending[0].ID

	// Claim the message multiple times to bump retry count above maxRetries (3).
	for range 4 {
		rdb.XClaim(ctx, &redis.XClaimArgs{
			Stream:   stream,
			Group:    group,
			Consumer: "retry-consumer",
			MinIdle:  0,
			Messages: []string{msgID},
		})
	}

	// Now ProcessDLQ should move it.
	moved, err := ProcessDLQ(ctx, rdb, stream, group, dlqStream, 3)
	if err != nil {
		t.Fatalf("ProcessDLQ: %v", err)
	}
	if moved != 1 {
		t.Errorf("moved = %d, want 1", moved)
	}

	// Verify the message is in the DLQ stream.
	dlqMsgs, err := rdb.XRange(ctx, dlqStream, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRANGE DLQ: %v", err)
	}
	if len(dlqMsgs) != 1 {
		t.Fatalf("DLQ messages = %d, want 1", len(dlqMsgs))
	}

	// Verify DLQ metadata.
	dlqValues := dlqMsgs[0].Values
	if dlqValues["dlq_original_id"] == nil {
		t.Error("expected dlq_original_id in DLQ entry")
	}
	if dlqValues["dlq_retry_count"] == nil {
		t.Error("expected dlq_retry_count in DLQ entry")
	}
	if dlqValues["dlq_moved_at"] == nil {
		t.Error("expected dlq_moved_at in DLQ entry")
	}

	// Verify original message is no longer in PEL.
	pendingAfter, _ := rdb.XPending(ctx, stream, group).Result()
	if pendingAfter.Count != 0 {
		t.Errorf("pending count after DLQ = %d, want 0", pendingAfter.Count)
	}
}

func TestProcessDLQNoExceeded(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	stream := StreamKey("acme", QueueDefault)
	group := GroupName("acme")
	dlqStream := DLQKey("acme")

	job := testJob("ok-job", "acme", "task")
	if _, err := p.Enqueue(ctx, "acme", QueueDefault, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := ensureGroup(ctx, rdb, stream, group); err != nil {
		t.Fatalf("ensureGroup: %v", err)
	}

	// Read but don't ack (1 delivery = under threshold of 3).
	rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: "test-consumer",
		Streams:  []string{stream, ">"},
		Count:    10,
	})

	moved, err := ProcessDLQ(ctx, rdb, stream, group, dlqStream, 3)
	if err != nil {
		t.Fatalf("ProcessDLQ: %v", err)
	}
	if moved != 0 {
		t.Errorf("moved = %d, want 0 (retry count under threshold)", moved)
	}
}
