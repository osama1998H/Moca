//go:build integration

package orm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/pkg/observe"
)

// integrationRedisConfig returns a RedisConfig pointing at the test Redis
// instance defined in docker-compose.yml (port 6380, no auth).
// Mirrors the pattern in internal/drivers/redis_test.go.
func integrationRedisConfig() config.RedisConfig {
	host := os.Getenv("REDIS_HOST")
	if host == "" {
		host = "localhost"
	}
	return config.RedisConfig{
		Host:      host,
		Port:      6380,
		Password:  "",
		DbCache:   0,
		DbQueue:   1,
		DbSession: 2,
		DbPubSub:  3,
	}
}

// TestHealthReadyEndToEnd wires a real DBManager (PostgreSQL) and real
// RedisClients into a HealthChecker and verifies that GET /health/ready
// returns 200 with all checks passing.
//
// This test covers the MS-02 acceptance criterion:
//
//	"/health/ready returns 200 when PG+Redis are up"
//
// The 503 path is verified by unit tests in pkg/observe/health_test.go
// using mock Pingers, so we do not simulate dependency failures here.
func TestHealthReadyEndToEnd(t *testing.T) {
	// ── 1. Probe Redis ────────────────────────────────────────────────────────
	// The existing TestMain in postgres_test.go already verifies PG is reachable.
	// We probe Redis separately and skip gracefully if it is unavailable, so that
	// the PG-only tests in this package still run when Redis is down.
	redisCfg := integrationRedisConfig()
	probe := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%d", redisCfg.Host, redisCfg.Port),
	})
	defer probe.Close()

	if err := probe.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not available at %s:%d — start with: docker compose up -d\n  (%v)",
			redisCfg.Host, redisCfg.Port, err)
	}

	// ── 2. Create DBManager (PostgreSQL) ─────────────────────────────────────
	// newTestManager reuses the adminPool config from postgres_test.go and
	// registers t.Cleanup(mgr.Close).
	mgr := newTestManager(t)

	// ── 3. Create RedisClients ───────────────────────────────────────────────
	logger := observe.NewLogger(slog.LevelWarn)
	rc := drivers.NewRedisClients(redisCfg, logger)
	t.Cleanup(func() { _ = rc.Close() })

	// Verify all four Redis databases are reachable.
	if err := rc.Ping(context.Background()); err != nil {
		t.Fatalf("RedisClients.Ping: %v", err)
	}

	// ── 4. Wire HealthChecker ────────────────────────────────────────────────
	// mgr.SystemPool() satisfies observe.Pinger via *pgxpool.Pool.Ping(ctx) error.
	// rc satisfies observe.Pinger via *drivers.RedisClients.Ping(ctx) error.
	hc := observe.NewHealthChecker(mgr.SystemPool(), rc, "integration-test", logger)
	mux := http.NewServeMux()
	hc.RegisterRoutes(mux)

	// ── 5. Hit GET /health/ready ─────────────────────────────────────────────
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// ── 6. Assert ─────────────────────────────────────────────────────────────
	if rec.Code != http.StatusOK {
		t.Errorf("/health/ready: status = %d, want 200\nbody: %s",
			rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks field missing or wrong type: %T = %v", body["checks"], body["checks"])
	}
	if checks["postgres"] != "ok" {
		t.Errorf("checks.postgres = %v, want ok", checks["postgres"])
	}
	if checks["redis"] != "ok" {
		t.Errorf("checks.redis = %v, want ok", checks["redis"])
	}

	t.Logf("TestHealthReadyEndToEnd: /health/ready returned 200 with postgres=ok, redis=ok")
}
