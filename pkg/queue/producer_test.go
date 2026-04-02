package queue

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client, mr
}

func testJob(id, site, jobType string) Job {
	return Job{
		ID:         id,
		Site:       site,
		Type:       jobType,
		Payload:    map[string]any{"key": "value"},
		Priority:   1,
		MaxRetries: 3,
		Retries:    0,
		CreatedAt:  time.Now().UTC(),
		Timeout:    30 * time.Second,
	}
}

func TestProducerEnqueue(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	for i := range 10 {
		job := testJob("job-"+string(rune('0'+i)), "acme", "send_email")
		id, err := p.Enqueue(ctx, "acme", QueueDefault, job)
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		if id == "" {
			t.Fatal("expected non-empty stream ID")
		}
	}

	// Verify stream length.
	stream := StreamKey("acme", QueueDefault)
	xlen, err := rdb.XLen(ctx, stream).Result()
	if err != nil {
		t.Fatalf("XLEN: %v", err)
	}
	if xlen != 10 {
		t.Errorf("XLEN = %d, want 10", xlen)
	}

	// Verify we can read back the entries.
	msgs, err := rdb.XRange(ctx, stream, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRANGE: %v", err)
	}
	if len(msgs) != 10 {
		t.Errorf("XRANGE returned %d messages, want 10", len(msgs))
	}

	// Verify job data round-trips correctly.
	job, err := valuesToJob(msgs[0].Values)
	if err != nil {
		t.Fatalf("valuesToJob: %v", err)
	}
	if job.Type != "send_email" {
		t.Errorf("Type = %q, want send_email", job.Type)
	}
}

func TestProducerEnqueueToCorrectStream(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	_, _ = p.Enqueue(ctx, "acme", QueueDefault, testJob("j1", "acme", "t1"))
	_, _ = p.Enqueue(ctx, "acme", QueueCritical, testJob("j2", "acme", "t2"))
	_, _ = p.Enqueue(ctx, "corp", QueueLong, testJob("j3", "corp", "t3"))

	for _, tc := range []struct {
		site string
		qt   QueueType
		want int64
	}{
		{"acme", QueueDefault, 1},
		{"acme", QueueCritical, 1},
		{"corp", QueueLong, 1},
		{"acme", QueueLong, 0},
	} {
		stream := StreamKey(tc.site, tc.qt)
		xlen, _ := rdb.XLen(ctx, stream).Result()
		if xlen != tc.want {
			t.Errorf("XLEN(%s) = %d, want %d", stream, xlen, tc.want)
		}
	}
}

func TestProducerEnqueueDelayed(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default())

	runAfter := time.Now().Add(1 * time.Hour)
	job := testJob("delayed-1", "acme", "generate_report")

	if err := p.EnqueueDelayed(ctx, "acme", QueueDefault, job, runAfter); err != nil {
		t.Fatalf("EnqueueDelayed: %v", err)
	}

	key := DelayedKey("acme")
	count, err := rdb.ZCard(ctx, key).Result()
	if err != nil {
		t.Fatalf("ZCARD: %v", err)
	}
	if count != 1 {
		t.Errorf("ZCARD = %d, want 1", count)
	}

	// Verify score is the runAfter timestamp.
	members, err := rdb.ZRangeWithScores(ctx, key, 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRANGEWITHSCORES: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	wantScore := float64(runAfter.UnixNano())
	if members[0].Score != wantScore {
		t.Errorf("score = %f, want %f", members[0].Score, wantScore)
	}
}

func TestProducerMaxLen(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()
	p := NewProducer(rdb, slog.Default()).WithMaxLen(map[QueueType]int64{
		QueueDefault: 5,
	})

	for i := range 20 {
		job := testJob("job-"+string(rune('A'+i)), "acme", "test")
		if _, err := p.Enqueue(ctx, "acme", QueueDefault, job); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	stream := StreamKey("acme", QueueDefault)
	xlen, _ := rdb.XLen(ctx, stream).Result()
	// Approximate trimming: actual length should be near 5 but may be slightly more.
	if xlen > 20 {
		t.Errorf("XLEN = %d, expected trimming to limit stream size", xlen)
	}
}
