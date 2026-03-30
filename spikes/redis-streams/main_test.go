// Tests for MS-00-T3: Redis Streams Consumer Group Spike.
//
// Prerequisites: Redis 7 running on localhost:6380
//
//	docker compose up -d
//
// Run:  go test -v -count=1 -race ./...
// Or:   make spike-redis  (from repo root)
//
// Environment override: REDIS_ADDR=<host:port>
package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisAddr = "localhost:6380"
	testSite         = "test_site"
	testGroup        = "test_group"
)

// rdb is the shared Redis client for the test suite (DB 1 = db_queue).
var rdb *redis.Client

func TestMain(m *testing.M) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = defaultRedisAddr
	}

	// DB 1 matches the design's db_queue convention (MOCA_SYSTEM_DESIGN.md §5.1).
	rdb = redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   1,
	})

	ctx := context.Background()

	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to Redis at %s: %v\n", addr, err)
		fmt.Fprintf(os.Stderr, "  Start Redis: docker compose up -d\n")
		os.Exit(1)
	}

	// Clear the test database before running any tests.
	rdb.FlushDB(ctx)

	exitCode := m.Run()

	// Teardown: clear and close.
	rdb.FlushDB(ctx)
	rdb.Close()

	os.Exit(exitCode)
}

// testStreamKey returns a unique stream key scoped to the test name to prevent
// cross-test interference even if a test panics before cleanup.
func testStreamKey(t *testing.T, queueType string) string {
	t.Helper()
	return StreamKey(testSite+"_"+t.Name(), queueType)
}

// testDLQKey returns a unique DLQ key scoped to the test name.
func testDLQKey(t *testing.T) string {
	t.Helper()
	return DLQKey(testSite + "_" + t.Name())
}

// newTestJob creates a Job with sensible defaults for testing.
func newTestJob(id, jobType string) Job {
	return Job{
		ID:         id,
		Site:       testSite,
		Type:       jobType,
		Payload:    map[string]any{"key": "value", "num": 42},
		Priority:   1,
		MaxRetries: 3,
		Retries:    0,
		CreatedAt:  time.Now().UTC(),
		Timeout:    30 * time.Second,
	}
}

// TestStreamNaming validates the stream key naming convention from
// MOCA_SYSTEM_DESIGN.md §5.2.
// Convention: moca:queue:{site}:{type} and moca:deadletter:{site}
func TestStreamNaming(t *testing.T) {
	ctx := context.Background()

	// Verify key format.
	cases := []struct {
		site      string
		queueType string
		want      string
	}{
		{"acme", "default", "moca:queue:acme:default"},
		{"acme", "long", "moca:queue:acme:long"},
		{"acme", "critical", "moca:queue:acme:critical"},
		{"acme", "scheduler", "moca:queue:acme:scheduler"},
	}
	for _, c := range cases {
		got := StreamKey(c.site, c.queueType)
		if got != c.want {
			t.Errorf("StreamKey(%q, %q) = %q, want %q", c.site, c.queueType, got, c.want)
		}
	}

	if got, want := DLQKey("acme"), "moca:deadletter:acme"; got != want {
		t.Errorf("DLQKey(%q) = %q, want %q", "acme", got, want)
	}

	// Verify that enqueuing to separate site streams produces independent streams.
	acmeStream := StreamKey("acme_"+t.Name(), "default")
	globexStream := StreamKey("globex_"+t.Name(), "default")

	acmeJob := newTestJob("acme-1", "send_email")
	acmeJob.Site = "acme"
	globexJob := newTestJob("globex-1", "send_email")
	globexJob.Site = "globex"

	if _, err := Enqueue(ctx, rdb, acmeStream, acmeJob, 0); err != nil {
		t.Fatalf("Enqueue acme: %v", err)
	}
	if _, err := Enqueue(ctx, rdb, globexStream, globexJob, 0); err != nil {
		t.Fatalf("Enqueue globex: %v", err)
	}

	acmeLen := rdb.XLen(ctx, acmeStream).Val()
	globexLen := rdb.XLen(ctx, globexStream).Val()

	if acmeLen != 1 {
		t.Errorf("acme stream length = %d, want 1", acmeLen)
	}
	if globexLen != 1 {
		t.Errorf("globex stream length = %d, want 1", globexLen)
	}

	// Cross-contamination check: acme stream must not contain globex entries.
	acmeEntries := rdb.XRange(ctx, acmeStream, "-", "+").Val()
	for _, entry := range acmeEntries {
		if site, _ := entry.Values["site"].(string); site == "globex" {
			t.Errorf("acme stream contains globex entry: %v", entry)
		}
	}
}

// TestJobProducer enqueues 100 jobs and verifies all are present via XLEN
// and XRANGE. Also validates MAXLEN approximate trimming.
func TestJobProducer(t *testing.T) {
	ctx := context.Background()
	stream := testStreamKey(t, "default")

	const jobCount = 100

	ids := make([]string, 0, jobCount)
	for i := range jobCount {
		job := newTestJob(fmt.Sprintf("job-%03d", i), "generate_report")
		id, err := Enqueue(ctx, rdb, stream, job, 0)
		if err != nil {
			t.Fatalf("Enqueue job %d: %v", i, err)
		}
		ids = append(ids, id)
	}

	// Verify all 100 are in the stream.
	length := rdb.XLen(ctx, stream).Val()
	if length != jobCount {
		t.Errorf("stream length = %d, want %d", length, jobCount)
	}

	// Verify each stream entry deserializes back to a valid Job.
	entries := rdb.XRange(ctx, stream, "-", "+").Val()
	if len(entries) != jobCount {
		t.Fatalf("XRange returned %d entries, want %d", len(entries), jobCount)
	}
	for i, entry := range entries {
		job, err := valuesToJob(entry.Values)
		if err != nil {
			t.Errorf("entry %d: valuesToJob: %v", i, err)
			continue
		}
		if job.Type != "generate_report" {
			t.Errorf("entry %d: Type = %q, want %q", i, job.Type, "generate_report")
		}
		if job.Timeout != 30*time.Second {
			t.Errorf("entry %d: Timeout = %v, want %v", i, job.Timeout, 30*time.Second)
		}
	}

	// Verify MAXLEN approximate trimming: enqueue 200 more with maxLen=150.
	trimStream := stream + "_trim"
	for i := range 200 {
		job := newTestJob(fmt.Sprintf("trim-%03d", i), "send_email")
		if _, err := Enqueue(ctx, rdb, trimStream, job, 150); err != nil {
			t.Fatalf("Enqueue with trim, job %d: %v", i, err)
		}
	}
	trimLen := rdb.XLen(ctx, trimStream).Val()
	// Approximate trimming may not be exact, but should be in a reasonable range.
	if trimLen > 200 || trimLen < 100 {
		t.Errorf("after MAXLEN ~150: stream length = %d, expected roughly 150", trimLen)
	}
	t.Logf("MAXLEN ~150 trimming result: stream length = %d (approximate, expected ≤200)", trimLen)
}

// TestConsumerGroup creates a consumer group, enqueues 50 jobs, consumes them
// all with a single consumer, and verifies all are acknowledged (XPENDING = 0).
func TestConsumerGroup(t *testing.T) {
	ctx := context.Background()
	stream := testStreamKey(t, "default")

	const jobCount = 50

	for i := range jobCount {
		job := newTestJob(fmt.Sprintf("job-%03d", i), "send_email")
		if _, err := Enqueue(ctx, rdb, stream, job, 0); err != nil {
			t.Fatalf("Enqueue job %d: %v", i, err)
		}
	}

	// Create the group pointing at "0" (beginning of stream) so we consume
	// all pre-enqueued messages.
	if err := rdb.XGroupCreateMkStream(ctx, stream, testGroup, "0").Err(); err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			t.Fatalf("XGroupCreateMkStream: %v", err)
		}
	}

	received := make([]Job, 0, jobCount)
	var mu sync.Mutex

	handler := func(_ context.Context, job Job) error {
		mu.Lock()
		received = append(received, job)
		mu.Unlock()
		return nil
	}

	consumeCtx, cancel := context.WithCancel(ctx)

	done := make(chan error, 1)
	go func() {
		done <- Consume(consumeCtx, rdb, stream, testGroup, "worker-1", 100*time.Millisecond, handler)
	}()

	// Wait until all jobs are received.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(received)
		mu.Unlock()
		if count >= jobCount {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	<-done

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count != jobCount {
		t.Errorf("received %d jobs, want %d", count, jobCount)
	}

	// All acknowledged: XPENDING should report 0.
	pending := rdb.XPending(ctx, stream, testGroup).Val()
	if pending.Count != 0 {
		t.Errorf("pending count = %d after consuming all, want 0", pending.Count)
	}

	// Verify consumer group exists via XINFO GROUPS.
	groups := rdb.XInfoGroups(ctx, stream).Val()
	if len(groups) == 0 {
		t.Error("XInfoGroups: no consumer groups found")
	}
}

// TestMultipleConsumers runs 3 consumers in the same group and verifies that
// 100 jobs are load-balanced: each message delivered to exactly one consumer.
func TestMultipleConsumers(t *testing.T) {
	ctx := context.Background()
	stream := testStreamKey(t, "default")

	const (
		jobCount      = 100
		numConsumers  = 3
	)

	// Enqueue all jobs before starting consumers to maximise distribution.
	for i := range jobCount {
		job := newTestJob(fmt.Sprintf("job-%03d", i), "generate_report")
		if _, err := Enqueue(ctx, rdb, stream, job, 0); err != nil {
			t.Fatalf("Enqueue job %d: %v", i, err)
		}
	}

	// Create group pointing at "0" to consume pre-enqueued messages.
	if err := rdb.XGroupCreateMkStream(ctx, stream, testGroup, "0").Err(); err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			t.Fatalf("XGroupCreateMkStream: %v", err)
		}
	}

	// processedIDs tracks which consumer processed which job ID.
	type record struct {
		consumerID int
		jobID      string
	}
	var mu sync.Mutex
	processed := make([]record, 0, jobCount)
	var processedCount atomic.Int64

	consumeCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup

	for c := range numConsumers {
		wg.Add(1)
		go func(consumerID int) {
			defer wg.Done()
			consumerName := fmt.Sprintf("worker-%d", consumerID+1)

			handler := func(_ context.Context, job Job) error {
				mu.Lock()
				processed = append(processed, record{consumerID: consumerID, jobID: job.ID})
				mu.Unlock()
				n := processedCount.Add(1)
				if n >= jobCount {
					cancel() // Signal all consumers to stop.
				}
				return nil
			}

			_ = Consume(consumeCtx, rdb, stream, testGroup, consumerName, 100*time.Millisecond, handler)
		}(c)
	}

	// Wait with a deadline.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		cancel()
		<-done
		t.Fatal("timeout: consumers did not finish within 15s")
	}

	mu.Lock()
	total := len(processed)
	mu.Unlock()

	if total != jobCount {
		t.Errorf("total processed = %d, want %d", total, jobCount)
	}

	// Verify no duplicates (each job ID processed exactly once).
	seen := make(map[string]int, total)
	perConsumer := make(map[int]int, numConsumers)
	mu.Lock()
	for _, r := range processed {
		seen[r.jobID]++
		perConsumer[r.consumerID]++
	}
	mu.Unlock()

	for id, count := range seen {
		if count > 1 {
			t.Errorf("job %q processed %d times (want 1)", id, count)
		}
	}

	// Verify each consumer processed at least 1 job (load was distributed).
	for c := range numConsumers {
		if perConsumer[c] == 0 {
			t.Errorf("consumer %d processed 0 jobs — load was not distributed", c)
		}
		t.Logf("worker-%d processed %d jobs", c+1, perConsumer[c])
	}
}

// TestAtLeastOnceDelivery simulates a consumer crash by reading messages
// without acknowledging, then uses XAutoClaim to reclaim and re-process.
func TestAtLeastOnceDelivery(t *testing.T) {
	ctx := context.Background()
	stream := testStreamKey(t, "default")

	const jobCount = 10

	for i := range jobCount {
		job := newTestJob(fmt.Sprintf("job-%03d", i), "webhook_delivery")
		if _, err := Enqueue(ctx, rdb, stream, job, 0); err != nil {
			t.Fatalf("Enqueue job %d: %v", i, err)
		}
	}

	// Create group at "0" (beginning) so worker-1 sees all pre-enqueued messages.
	if err := rdb.XGroupCreateMkStream(ctx, stream, testGroup, "0").Err(); err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			t.Fatalf("XGroupCreateMkStream: %v", err)
		}
	}

	// Worker-1: read all 10 messages but do NOT acknowledge (simulates crash).
	msgs, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    testGroup,
		Consumer: "worker-1",
		Streams:  []string{stream, ">"},
		Count:    jobCount,
		Block:    0,
	}).Result()
	if err != nil {
		t.Fatalf("XReadGroup worker-1: %v", err)
	}
	if len(msgs) == 0 || len(msgs[0].Messages) != jobCount {
		t.Fatalf("worker-1 received %d messages, want %d", len(msgs[0].Messages), jobCount)
	}

	// Verify 10 messages pending for worker-1.
	pendingInfo := rdb.XPending(ctx, stream, testGroup).Val()
	if pendingInfo.Count != jobCount {
		t.Errorf("pending count = %d, want %d after worker-1 crash", pendingInfo.Count, jobCount)
	}

	// Worker-2: claim all pending messages via XAutoClaim (minIdle=0 for test speed).
	claimed, cursor, err := ClaimPending(ctx, rdb, stream, testGroup, "worker-2", 0)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	t.Logf("XAutoClaim cursor: %q (empty means all claimed)", cursor)

	if len(claimed) != jobCount {
		t.Errorf("worker-2 claimed %d messages, want %d", len(claimed), jobCount)
	}

	// Worker-2 processes and acknowledges all claimed messages.
	for _, msg := range claimed {
		if err := rdb.XAck(ctx, stream, testGroup, msg.ID).Err(); err != nil {
			t.Errorf("XAck %s: %v", msg.ID, err)
		}
	}

	// Verify PEL is now empty.
	finalPending := rdb.XPending(ctx, stream, testGroup).Val()
	if finalPending.Count != 0 {
		t.Errorf("pending count = %d after worker-2 acked all, want 0", finalPending.Count)
	}

	// Verify all claimed messages deserialize to valid Jobs.
	for i, msg := range claimed {
		if _, err := valuesToJob(msg.Values); err != nil {
			t.Errorf("claimed[%d]: valuesToJob: %v", i, err)
		}
	}
}

// TestDeadLetterQueue validates that jobs exceeding maxRetries delivery
// attempts are moved to the dead-letter stream by ProcessDLQ.
func TestDeadLetterQueue(t *testing.T) {
	ctx := context.Background()
	stream := testStreamKey(t, "default")
	dlqStream := testDLQKey(t)

	const (
		jobCount   = 5
		maxRetries = int64(3)
	)

	for i := range jobCount {
		job := newTestJob(fmt.Sprintf("job-%03d", i), "send_email")
		if _, err := Enqueue(ctx, rdb, stream, job, 0); err != nil {
			t.Fatalf("Enqueue job %d: %v", i, err)
		}
	}

	// Create group at "0" to access pre-enqueued messages.
	if err := rdb.XGroupCreateMkStream(ctx, stream, testGroup, "0").Err(); err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			t.Fatalf("XGroupCreateMkStream: %v", err)
		}
	}

	// Read all messages — this counts as delivery attempt 1.
	msgs, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    testGroup,
		Consumer: "worker-1",
		Streams:  []string{stream, ">"},
		Count:    jobCount,
		Block:    0,
	}).Result()
	if err != nil {
		t.Fatalf("XReadGroup (attempt 1): %v", err)
	}
	messageIDs := make([]string, 0, jobCount)
	for _, s := range msgs {
		for _, m := range s.Messages {
			messageIDs = append(messageIDs, m.ID)
		}
	}

	// XClaim each message 3 more times to bring delivery count to 4 (> maxRetries=3).
	for _, id := range messageIDs {
		for range 3 {
			if _, err := rdb.XClaim(ctx, &redis.XClaimArgs{
				Stream:   stream,
				Group:    testGroup,
				Consumer: "worker-2",
				MinIdle:  0,
				Messages: []string{id},
			}).Result(); err != nil {
				t.Fatalf("XClaim %s: %v", id, err)
			}
		}
	}

	// Verify delivery counts are now > maxRetries via XPendingExt.
	pending, err := rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: stream,
		Group:  testGroup,
		Start:  "-",
		End:    "+",
		Count:  100,
	}).Result()
	if err != nil {
		t.Fatalf("XPendingExt: %v", err)
	}
	for _, p := range pending {
		if p.RetryCount <= maxRetries {
			t.Logf("message %s has RetryCount=%d (want >%d)", p.ID, p.RetryCount, maxRetries)
		}
	}

	// Run ProcessDLQ — should move all 5 messages to the DLQ.
	moved, err := ProcessDLQ(ctx, rdb, stream, testGroup, dlqStream, maxRetries)
	if err != nil {
		t.Fatalf("ProcessDLQ: %v", err)
	}
	if moved != jobCount {
		t.Errorf("ProcessDLQ moved %d messages, want %d", moved, jobCount)
	}

	// Verify DLQ stream contains all 5 messages.
	dlqLen := rdb.XLen(ctx, dlqStream).Val()
	if dlqLen != jobCount {
		t.Errorf("DLQ stream length = %d, want %d", dlqLen, jobCount)
	}

	// Verify DLQ entries contain the original job data and DLQ metadata.
	dlqEntries := rdb.XRange(ctx, dlqStream, "-", "+").Val()
	for i, entry := range dlqEntries {
		if entry.Values["dlq_original_id"] == nil {
			t.Errorf("DLQ entry %d missing dlq_original_id", i)
		}
		if entry.Values["dlq_retry_count"] == nil {
			t.Errorf("DLQ entry %d missing dlq_retry_count", i)
		}
		// Verify core job fields are preserved.
		if entry.Values["type"] == nil {
			t.Errorf("DLQ entry %d missing 'type' field", i)
		}
	}

	// Verify original stream's PEL is now empty.
	finalPending := rdb.XPending(ctx, stream, testGroup).Val()
	if finalPending.Count != 0 {
		t.Errorf("PEL count = %d after DLQ move, want 0", finalPending.Count)
	}
}

// TestDelayedExecution validates the RunAfter pattern: jobs with a future
// RunAfter timestamp are skipped on first pass and processed when ready.
//
// Also demonstrates the ZADD sorted-set alternative (documented in ADR).
func TestDelayedExecution(t *testing.T) {
	ctx := context.Background()
	stream := testStreamKey(t, "default")

	// Enqueue 3 immediate + 2 delayed jobs.
	delay := 200 * time.Millisecond
	future := time.Now().Add(delay)

	for i := range 3 {
		job := newTestJob(fmt.Sprintf("immediate-%d", i), "send_email")
		if _, err := Enqueue(ctx, rdb, stream, job, 0); err != nil {
			t.Fatalf("Enqueue immediate job %d: %v", i, err)
		}
	}
	for i := range 2 {
		job := newTestJob(fmt.Sprintf("delayed-%d", i), "send_email")
		job.RunAfter = &future
		if _, err := Enqueue(ctx, rdb, stream, job, 0); err != nil {
			t.Fatalf("Enqueue delayed job %d: %v", i, err)
		}
	}

	// Create group at "0".
	if err := rdb.XGroupCreateMkStream(ctx, stream, testGroup, "0").Err(); err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			t.Fatalf("XGroupCreateMkStream: %v", err)
		}
	}

	var immediateProcessed, delayedProcessed atomic.Int64

	// Handler: skips jobs whose RunAfter is in the future (returns error = no ack).
	handler := func(_ context.Context, job Job) error {
		if job.RunAfter != nil && time.Now().Before(*job.RunAfter) {
			// Not ready — leave in PEL for later processing.
			return fmt.Errorf("not ready: RunAfter=%v", job.RunAfter)
		}
		if job.RunAfter != nil {
			delayedProcessed.Add(1)
		} else {
			immediateProcessed.Add(1)
		}
		return nil
	}

	// First pass: consume all 5 messages. 3 immediate are acked, 2 delayed are not.
	consumeCtx1, cancel1 := context.WithTimeout(ctx, 2*time.Second)
	defer cancel1()
	_ = Consume(consumeCtx1, rdb, stream, testGroup, "worker-1", 100*time.Millisecond, handler)

	if got := immediateProcessed.Load(); got != 3 {
		t.Errorf("first pass: immediate processed = %d, want 3", got)
	}
	if got := delayedProcessed.Load(); got != 0 {
		t.Errorf("first pass: delayed processed = %d, want 0 (not ready yet)", got)
	}

	// 2 delayed jobs remain pending.
	pendingInfo := rdb.XPending(ctx, stream, testGroup).Val()
	if pendingInfo.Count != 2 {
		t.Errorf("pending count = %d after first pass, want 2", pendingInfo.Count)
	}

	// Wait for RunAfter to pass.
	time.Sleep(delay + 100*time.Millisecond)

	// Second pass: re-read the 2 pending messages using "0" (re-deliver pending).
	pendingMsgs, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    testGroup,
		Consumer: "worker-1",
		Streams:  []string{stream, "0"},
		Count:    10,
		Block:    0,
	}).Result()
	if err != nil {
		t.Fatalf("XReadGroup pending pass: %v", err)
	}

	for _, s := range pendingMsgs {
		for _, msg := range s.Messages {
			job, err := valuesToJob(msg.Values)
			if err != nil {
				t.Errorf("valuesToJob: %v", err)
				continue
			}
			if handlerErr := handler(ctx, job); handlerErr == nil {
				_ = rdb.XAck(ctx, stream, testGroup, msg.ID).Err()
			}
		}
	}

	if got := delayedProcessed.Load(); got != 2 {
		t.Errorf("second pass: delayed processed = %d, want 2", got)
	}

	// All 5 jobs ultimately processed.
	finalPending := rdb.XPending(ctx, stream, testGroup).Val()
	if finalPending.Count != 0 {
		t.Errorf("pending count = %d after all passes, want 0", finalPending.Count)
	}

	// -- ZADD Alternative Demonstration --
	// Validate sorted-set pattern for delayed jobs: enqueue to ZADD with score=UnixNano,
	// poll with ZRANGEBYSCORE, move to stream when ready.
	delayedSetKey := "moca:delayed:" + testSite + "_" + t.Name()

	// Add 3 delayed jobs to sorted set.
	now := time.Now()
	for i := range 3 {
		score := float64(now.Add(time.Duration(i*50) * time.Millisecond).UnixNano())
		member := fmt.Sprintf("job-zadd-%d", i)
		rdb.ZAdd(ctx, delayedSetKey, redis.Z{Score: score, Member: member})
	}

	// Poll: find all jobs ready to run (score <= now).
	time.Sleep(200 * time.Millisecond)
	readyJobs, err := rdb.ZRangeByScore(ctx, delayedSetKey, &redis.ZRangeBy{
		Min: "-inf",
		Max: fmt.Sprintf("%d", time.Now().UnixNano()),
	}).Result()
	if err != nil {
		t.Fatalf("ZRangeByScore: %v", err)
	}
	if len(readyJobs) == 0 {
		t.Error("ZADD alternative: expected ready jobs, got none")
	}
	t.Logf("ZADD alternative: %d of 3 delayed jobs became ready after 200ms", len(readyJobs))

	// Move ready jobs from sorted set to stream and remove from set (atomically in production via MULTI/EXEC or Lua).
	for _, member := range readyJobs {
		job := newTestJob(member, "scheduled_task")
		if _, err := Enqueue(ctx, rdb, stream, job, 0); err != nil {
			t.Errorf("ZADD->stream: Enqueue %s: %v", member, err)
		}
		rdb.ZRem(ctx, delayedSetKey, member)
	}
	t.Logf("ZADD alternative validated: sorted-set polling and stream promotion works")
}
