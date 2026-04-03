//go:build integration

package orm_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
)

// Default connection parameters matching docker-compose.yml at repo root.
const (
	defaultHost     = "localhost"
	defaultPort     = 5433
	defaultUser     = "moca"
	defaultPassword = "moca_test"
	defaultDB       = "moca_test"
	numTenants      = 10
	numGoroutines   = 100
)

// adminPool is a raw pool used only for fixture setup/teardown.
var adminPool *pgxpool.Pool

// TestMain sets up the PostgreSQL fixtures (moca_system + 10 tenant schemas),
// runs all integration tests, then tears down the schemas.
func TestMain(m *testing.M) {
	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			defaultUser, defaultPassword, defaultHost, defaultPort, defaultDB,
		)
	}

	ctx := context.Background()

	var err error
	adminPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create admin pool: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Start PostgreSQL: docker compose up -d\n")
		os.Exit(1)
	}
	defer adminPool.Close()

	if err := adminPool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to PostgreSQL at %s: %v\n", connStr, err)
		fmt.Fprintf(os.Stderr, "  Start PostgreSQL: docker compose up -d\n")
		os.Exit(1)
	}

	if err := setupFixtures(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "fixture setup failed: %v\n", err)
		os.Exit(1)
	}

	exitCode := m.Run()

	if err := teardownFixtures(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "fixture teardown failed: %v\n", err)
	}

	os.Exit(exitCode)
}

// setupFixtures creates moca_system and 10 tenant schemas, each with a tab_test
// table.
func setupFixtures(ctx context.Context) error {
	if _, err := adminPool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS moca_system;
		CREATE TABLE IF NOT EXISTS moca_system.sites (
			name        TEXT PRIMARY KEY,
			db_schema   TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'active',
			admin_email TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS moca_system.tx_test (
			id    SERIAL PRIMARY KEY,
			value TEXT NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("create moca_system: %w", err)
	}

	for i := 1; i <= numTenants; i++ {
		schema := tenantSchema(i)
		if _, err := adminPool.Exec(ctx, fmt.Sprintf(`
			CREATE SCHEMA IF NOT EXISTS %s;
			CREATE TABLE IF NOT EXISTS %s.tab_test (
				id    SERIAL PRIMARY KEY,
				name  TEXT NOT NULL,
				value TEXT NOT NULL
			);
		`, schema, schema)); err != nil {
			return fmt.Errorf("create schema %s: %w", schema, err)
		}
	}
	return nil
}

// teardownFixtures drops only the tenant schemas owned by this package and
// clears its transaction fixture table. The shared moca_system schema is left
// in place because other integration-test packages use the same database in
// parallel during `go test ./...`.
func teardownFixtures(ctx context.Context) error {
	var errs []error
	for i := 1; i <= numTenants; i++ {
		schema := tenantSchema(i)
		if _, err := adminPool.Exec(ctx,
			fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema),
		); err != nil {
			errs = append(errs, fmt.Errorf("drop %s: %w", schema, err))
		}
	}
	if _, err := adminPool.Exec(ctx, "TRUNCATE TABLE IF EXISTS moca_system.tx_test RESTART IDENTITY"); err != nil {
		errs = append(errs, fmt.Errorf("truncate moca_system.tx_test: %w", err))
	}
	return errors.Join(errs...)
}

// tenantSchema returns the schema name for tenant i (e.g. "tenant_01").
func tenantSchema(i int) string {
	return fmt.Sprintf("tenant_%02d", i)
}

// tenantSiteName returns the site name used in ForSite for tenant i (e.g. "01").
// ForSite prepends "tenant_" to yield "tenant_01".
func tenantSiteName(i int) string {
	return fmt.Sprintf("%02d", i)
}

// testConfig returns a DatabaseConfig pointing at the test PostgreSQL instance.
func testConfig() config.DatabaseConfig {
	host := os.Getenv("PG_HOST")
	if host == "" {
		host = defaultHost
	}
	return config.DatabaseConfig{
		Host:     host,
		Port:     defaultPort,
		User:     defaultUser,
		Password: defaultPassword,
		SystemDB: defaultDB,
		PoolSize: 50,
	}
}

// newTestManager creates a DBManager for tests and registers t.Cleanup(Close).
func newTestManager(t *testing.T) *orm.DBManager {
	t.Helper()
	logger := observe.NewLogger(slog.LevelWarn) // suppress info noise in tests
	mgr, err := orm.NewDBManager(context.Background(), testConfig(), logger)
	if err != nil {
		t.Fatalf("NewDBManager: %v", err)
	}
	t.Cleanup(mgr.Close)
	return mgr
}

// ── Test 1: Concurrent Isolation (core acceptance criterion) ──────────────────

// TestDBManagerConcurrentIsolation launches 100 goroutines across 10 tenant
// schemas. Each goroutine inserts a row tagged with its schema name and reads it
// back immediately. Zero cross-contamination means every row has value == its
// own schema name.
func TestDBManagerConcurrentIsolation(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManager(t)

	// Pre-create all tenant pools before the concurrent phase to isolate pool
	// creation from the concurrent insert/select race.
	for i := 1; i <= numTenants; i++ {
		if _, err := mgr.ForSite(ctx, tenantSiteName(i)); err != nil {
			t.Fatalf("pre-create pool for tenant %d: %v", i, err)
		}
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		errList []string
	)

	for id := 0; id < numGoroutines; id++ {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()

			i := (id % numTenants) + 1
			siteName := tenantSiteName(i)
			schema := tenantSchema(i)

			pool, err := mgr.ForSite(ctx, siteName)
			if err != nil {
				mu.Lock()
				errList = append(errList, fmt.Sprintf("g%d ForSite(%q): %v", id, siteName, err))
				mu.Unlock()
				return
			}

			rowName := fmt.Sprintf("concurrent_g%d", id)
			if _, err := pool.Exec(ctx,
				"INSERT INTO tab_test (name, value) VALUES ($1, $2)",
				rowName, schema,
			); err != nil {
				mu.Lock()
				errList = append(errList, fmt.Sprintf("g%d insert: %v", id, err))
				mu.Unlock()
				return
			}

			var got string
			if err := pool.QueryRow(ctx,
				"SELECT value FROM tab_test WHERE name = $1", rowName,
			).Scan(&got); err != nil {
				mu.Lock()
				errList = append(errList, fmt.Sprintf("g%d select: %v", id, err))
				mu.Unlock()
				return
			}

			if got != schema {
				mu.Lock()
				errList = append(errList, fmt.Sprintf(
					"CROSS-CONTAMINATION g%d: wrote to %q, read back %q", id, schema, got))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	for _, e := range errList {
		t.Error(e)
	}
	if len(errList) > 0 {
		t.Fatalf("%d goroutine errors (see above)", len(errList))
	}

	// Post-check: every row in each tenant schema must have value == that schema.
	for i := 1; i <= numTenants; i++ {
		pool, _ := mgr.ForSite(ctx, tenantSiteName(i))
		schema := tenantSchema(i)

		rows, err := pool.Query(ctx, "SELECT value FROM tab_test")
		if err != nil {
			t.Fatalf("post-check query %q: %v", schema, err)
		}
		var contaminated []string
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				t.Fatalf("post-check scan: %v", err)
			}
			if v != schema {
				contaminated = append(contaminated, v)
			}
		}
		rows.Close()
		if len(contaminated) > 0 {
			t.Errorf("CROSS-CONTAMINATION in %q: found foreign values: %v", schema, contaminated)
		}
	}
	t.Logf("TestDBManagerConcurrentIsolation: %d goroutines x %d tenants — zero cross-contamination",
		numGoroutines, numTenants)
}

// ── Test 2: AssertSchema defense-in-depth ─────────────────────────────────────

// TestAssertSchema verifies that AssertSchema correctly validates the current
// schema and returns an error on mismatch.
func TestAssertSchema(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManager(t)

	pool1, err := mgr.ForSite(ctx, "01")
	if err != nil {
		t.Fatalf("ForSite(01): %v", err)
	}

	// Correct assertion must succeed.
	if err := mgr.AssertSchema(ctx, pool1, "tenant_01"); err != nil {
		t.Errorf("AssertSchema(tenant_01 pool, want tenant_01): unexpected error: %v", err)
	}

	// Wrong expectation must fail.
	if err := mgr.AssertSchema(ctx, pool1, "tenant_02"); err == nil {
		t.Error("AssertSchema(tenant_01 pool, want tenant_02): expected error, got nil")
	} else {
		t.Logf("AssertSchema correctly returned error: %v", err)
	}

	// System pool is bound to moca_system.
	if err := mgr.AssertSchema(ctx, mgr.SystemPool(), "moca_system"); err != nil {
		t.Errorf("AssertSchema(systemPool, want moca_system): %v", err)
	}
}

// ── Test 3: Idle Pool Eviction ────────────────────────────────────────────────

// TestEvictIdlePools validates the three phases: create, evict when idle, and
// transparent re-creation on next ForSite.
func TestEvictIdlePools(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManager(t)

	if n := mgr.SitePoolCount(); n != 0 {
		t.Fatalf("expected 0 site pools initially, got %d", n)
	}

	// Phase 1: Create a pool lazily.
	pool1, err := mgr.ForSite(ctx, "06")
	if err != nil {
		t.Fatalf("ForSite(06): %v", err)
	}
	if n := mgr.SitePoolCount(); n != 1 {
		t.Fatalf("expected 1 pool after ForSite, got %d", n)
	}

	// Insert data to verify re-created pool can still read it.
	if _, err := pool1.Exec(ctx,
		"INSERT INTO tab_test (name, value) VALUES ($1, $2)",
		"evict_test", "tenant_06",
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Phase 2: Artificially age the pool then evict.
	mgr.SetLastUsed("tenant_06", time.Now().Add(-2*time.Hour))
	evicted := mgr.EvictIdlePools(1 * time.Hour)
	if evicted != 1 {
		t.Fatalf("expected 1 eviction, got %d", evicted)
	}
	if n := mgr.SitePoolCount(); n != 0 {
		t.Fatalf("expected 0 pools after eviction, got %d", n)
	}

	// Phase 3: Re-creation on next ForSite.
	pool2, err := mgr.ForSite(ctx, "06")
	if err != nil {
		t.Fatalf("ForSite after eviction: %v", err)
	}
	if n := mgr.SitePoolCount(); n != 1 {
		t.Fatalf("expected 1 pool after re-creation, got %d", n)
	}

	// Data persists in PostgreSQL; new pool reads it correctly.
	var val string
	if err := pool2.QueryRow(ctx,
		"SELECT value FROM tab_test WHERE name = $1", "evict_test",
	).Scan(&val); err != nil {
		t.Fatalf("read after re-creation: %v", err)
	}
	if val != "tenant_06" {
		t.Errorf("re-created pool in wrong schema: expected tenant_06, got %q", val)
	}

	// New pool has correct search_path.
	if err := mgr.AssertSchema(ctx, pool2, "tenant_06"); err != nil {
		t.Errorf("AssertSchema after re-creation: %v", err)
	}
	t.Log("Idle pool eviction and transparent re-creation — correct")
}
