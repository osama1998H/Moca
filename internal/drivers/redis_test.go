//go:build integration

package drivers_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/pkg/observe"
)

// Default connection parameters matching docker-compose.yml at repo root.
const (
	defaultRedisHost = "localhost"
	defaultRedisPort = 6380 // maps to container port 6379
)

// TestMain probes Redis connectivity before running integration tests.
// If Redis is unreachable it prints a helpful message and exits cleanly.
func TestMain(m *testing.M) {
	cfg := testRedisConfig()
	probe := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
	})
	defer probe.Close()

	ctx := context.Background()
	if err := probe.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to Redis at %s:%d: %v\n",
			cfg.Host, cfg.Port, err)
		fmt.Fprintf(os.Stderr, "  Start Redis: docker compose up -d\n")
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// testRedisConfig returns a RedisConfig pointing at the test Redis instance.
func testRedisConfig() config.RedisConfig {
	host := os.Getenv("REDIS_HOST")
	if host == "" {
		host = defaultRedisHost
	}
	return config.RedisConfig{
		Host:      host,
		Port:      defaultRedisPort,
		Password:  "", // no auth in development
		DbCache:   0,
		DbQueue:   1,
		DbSession: 2,
		DbPubSub:  3,
	}
}

// newTestClients creates a RedisClients for tests and registers t.Cleanup(Close).
func newTestClients(t *testing.T) *drivers.RedisClients {
	t.Helper()
	logger := observe.NewLogger(slog.LevelWarn) // suppress info noise in tests
	rc := drivers.NewRedisClients(testRedisConfig(), logger)
	t.Cleanup(func() { _ = rc.Close() })
	return rc
}

// ── Test 1: Four clients on distinct DBs ─────────────────────────────────────

// TestNewRedisClients_FourDistinctDBs verifies that all four Redis clients
// connect successfully and target the expected logical database indices.
func TestNewRedisClients_FourDistinctDBs(t *testing.T) {
	rc := newTestClients(t)
	cfg := testRedisConfig()
	ctx := context.Background()

	if err := rc.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if got := rc.Cache.Options().DB; got != cfg.DbCache {
		t.Errorf("Cache DB = %d, want %d", got, cfg.DbCache)
	}
	if got := rc.Queue.Options().DB; got != cfg.DbQueue {
		t.Errorf("Queue DB = %d, want %d", got, cfg.DbQueue)
	}
	if got := rc.Session.Options().DB; got != cfg.DbSession {
		t.Errorf("Session DB = %d, want %d", got, cfg.DbSession)
	}
	if got := rc.PubSub.Options().DB; got != cfg.DbPubSub {
		t.Errorf("PubSub DB = %d, want %d", got, cfg.DbPubSub)
	}

	t.Logf("TestNewRedisClients_FourDistinctDBs: all 4 clients connected on correct DB indices")
}

// ── Test 2: Cross-DB isolation ───────────────────────────────────────────────

// TestRedisClients_CrossDBIsolation verifies that a key written to the Cache
// database is not visible from the Queue, Session, or PubSub databases.
// This is the core acceptance criterion: "Redis clients for DB 0 and DB 1 are
// distinct connections."
func TestRedisClients_CrossDBIsolation(t *testing.T) {
	rc := newTestClients(t)
	ctx := context.Background()

	if err := rc.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	key := "moca_isolation_test_key"
	value := "cache_only_value"

	// Ensure the key is removed after the test regardless of outcome.
	t.Cleanup(func() { rc.Cache.Del(ctx, key) })

	// Write key to Cache DB.
	if err := rc.Cache.Set(ctx, key, value, 0).Err(); err != nil {
		t.Fatalf("Cache SET: %v", err)
	}

	// Key must be readable from Cache DB.
	got, err := rc.Cache.Get(ctx, key).Result()
	if err != nil {
		t.Fatalf("Cache GET: %v", err)
	}
	if got != value {
		t.Errorf("Cache GET = %q, want %q", got, value)
	}

	// Key must NOT exist in the other three DBs.
	for _, nc := range []struct {
		name   string
		client *redis.Client
	}{
		{"Queue (DB 1)", rc.Queue},
		{"Session (DB 2)", rc.Session},
		{"PubSub (DB 3)", rc.PubSub},
	} {
		_, err := nc.client.Get(ctx, key).Result()
		if err != redis.Nil {
			t.Errorf("%s: expected redis.Nil for key %q, got err=%v", nc.name, key, err)
		}
	}

	t.Log("TestRedisClients_CrossDBIsolation: key written to DB 0 is invisible on DB 1, 2, 3")
}

// ── Test 3: Close ────────────────────────────────────────────────────────────

// TestRedisClients_Close verifies that Close() succeeds without error on a
// freshly created client set.
func TestRedisClients_Close(t *testing.T) {
	logger := observe.NewLogger(slog.LevelWarn)
	rc := drivers.NewRedisClients(testRedisConfig(), logger)

	ctx := context.Background()
	if err := rc.Ping(ctx); err != nil {
		t.Fatalf("Ping before close: %v", err)
	}

	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
