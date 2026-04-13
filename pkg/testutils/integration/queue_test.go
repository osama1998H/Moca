//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/testutils"
)

func TestQueueRedisAvailability(t *testing.T) {
	env := testutils.NewTestEnv(t)

	redis := env.RequireRedis(t)

	// Verify Redis is responsive.
	result, err := redis.Ping(env.Ctx).Result()
	if err != nil {
		t.Fatalf("Redis ping: %v", err)
	}
	if result != "PONG" {
		t.Fatalf("expected PONG, got %q", result)
	}
}

func TestQueueRedisKeyIsolation(t *testing.T) {
	env := testutils.NewTestEnv(t)

	redis := env.RequireRedis(t)

	// Set a test key.
	key := env.MetaRedisKey("test_queue_key")
	if err := redis.Set(env.Ctx, key, "test_value", 0).Err(); err != nil {
		t.Fatalf("set key: %v", err)
	}

	// Verify it exists.
	val, err := redis.Get(env.Ctx, key).Result()
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	if val != "test_value" {
		t.Fatalf("expected 'test_value', got %q", val)
	}

	// Note: Full queue integration tests (enqueue, consume, DLQ, scheduled jobs)
	// are in pkg/queue/ and test against real Redis Streams (MS-15).
}
