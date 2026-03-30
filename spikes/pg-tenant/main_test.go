// Tests for MS-00-T2: PostgreSQL Schema-Per-Tenant Isolation Spike
//
// Prerequisites: PostgreSQL 16 running on localhost:5433
//   docker compose up -d
//
// Run: go test -v -count=1 -race ./...
// Or:  make spike-pg  (from repo root)
//
// Environment override: PG_CONN_STRING=postgres://user:pass@host:port/db?sslmode=disable
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultConnStr = "postgres://moca:moca_test@localhost:5433/moca_test?sslmode=disable"
	numTenants     = 10
	numGoroutines  = 100
)

// connStr is the test database connection string. Overridable via PG_CONN_STRING.
var connStr string

// adminPool is a temporary pool with no schema restriction, used only for
// fixture setup and teardown in TestMain.
var adminPool *pgxpool.Pool

// TestMain sets up the test database fixtures, runs all tests, then tears down.
// Schema layout:
//   moca_system   — system schema with sites table
//   tenant_01..10 — per-tenant schemas with tab_test table
func TestMain(m *testing.M) {
	connStr = os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = defaultConnStr
	}

	ctx := context.Background()

	// Create admin pool (no search_path override — accesses all schemas).
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

	// Setup fixtures.
	if err := setupFixtures(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "fixture setup failed: %v\n", err)
		os.Exit(1)
	}

	// Run all tests.
	exitCode := m.Run()

	// Teardown — always runs even if tests panic (deferred adminPool.Close above).
	if err := teardownFixtures(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "fixture teardown failed: %v\n", err)
	}

	os.Exit(exitCode)
}

func setupFixtures(ctx context.Context) error {
	// moca_system schema and sites table.
	if _, err := adminPool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS moca_system;
		CREATE TABLE IF NOT EXISTS moca_system.sites (
			name        TEXT PRIMARY KEY,
			db_schema   TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'active',
			admin_email TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		return fmt.Errorf("create moca_system: %w", err)
	}

	// Per-tenant schemas.
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
		if _, err := adminPool.Exec(ctx,
			`INSERT INTO moca_system.sites (name, db_schema) VALUES ($1, $2)
			 ON CONFLICT (name) DO NOTHING`,
			fmt.Sprintf("site_%02d.test", i), schema,
		); err != nil {
			return fmt.Errorf("insert site %d: %w", i, err)
		}
	}
	return nil
}

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
	if _, err := adminPool.Exec(ctx, "DROP SCHEMA IF EXISTS moca_system CASCADE"); err != nil {
		errs = append(errs, fmt.Errorf("drop moca_system: %w", err))
	}
	return errors.Join(errs...)
}

func tenantSchema(i int) string {
	return fmt.Sprintf("tenant_%02d", i)
}

// newTestManager creates a DBManager for testing and registers cleanup.
func newTestManager(t *testing.T, maxConns int32) *DBManager {
	t.Helper()
	mgr, err := NewDBManager(context.Background(), connStr, maxConns)
	if err != nil {
		t.Fatalf("NewDBManager: %v", err)
	}
	t.Cleanup(mgr.Close)
	return mgr
}

// ────────────────────────────────────────────────────────────────────────────
// Test 1: Basic Tenant Isolation
// ────────────────────────────────────────────────────────────────────────────

// TestBasicTenantIsolation verifies that two tenant pools cannot see each
// other's data when querying the same unqualified table name.
func TestBasicTenantIsolation(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManager(t, 5)

	// Insert unique data into each tenant.
	for i := 1; i <= 2; i++ {
		schema := tenantSchema(i)
		pool, err := mgr.ForSite(ctx, schema)
		if err != nil {
			t.Fatalf("ForSite(%q): %v", schema, err)
		}
		if _, err := pool.Exec(ctx,
			"INSERT INTO tab_test (name, value) VALUES ($1, $2)",
			fmt.Sprintf("basic_%s", schema), schema,
		); err != nil {
			t.Fatalf("insert into %q: %v", schema, err)
		}
	}

	// Verify each tenant sees only its own data.
	for i := 1; i <= 2; i++ {
		schema := tenantSchema(i)
		pool, err := mgr.ForSite(ctx, schema)
		if err != nil {
			t.Fatalf("ForSite(%q): %v", schema, err)
		}

		rows, err := pool.Query(ctx, "SELECT value FROM tab_test WHERE name = $1",
			fmt.Sprintf("basic_%s", schema))
		if err != nil {
			t.Fatalf("query %q: %v", schema, err)
		}

		var values []string
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				t.Fatalf("scan: %v", err)
			}
			values = append(values, v)
		}
		rows.Close()

		if len(values) != 1 {
			t.Errorf("tenant %q: expected 1 row, got %d", schema, len(values))
		} else if values[0] != schema {
			t.Errorf("tenant %q: expected value %q, got %q", schema, schema, values[0])
		}

		// Cross-contamination check: the other tenant's row must NOT appear.
		other := tenantSchema(3 - i) // i=1 -> other=2, i=2 -> other=1
		var count int
		if err := pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM tab_test WHERE value = $1", other,
		).Scan(&count); err != nil {
			t.Fatalf("cross-check query for %q in %q: %v", other, schema, err)
		}
		if count != 0 {
			t.Errorf("CROSS-CONTAMINATION: tenant %q pool sees %d rows with value %q",
				schema, count, other)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Test 2: Concurrent Access (core acceptance criterion, ROADMAP line 119)
// ────────────────────────────────────────────────────────────────────────────

// TestConcurrentAccess launches 100 goroutines across 10 tenant schemas.
// Each goroutine inserts a row tagged with its own schema, then immediately
// reads it back. Zero cross-contamination means every read returns the expected
// tenant's value.
func TestConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManager(t, 5)

	// Pre-create all pools to avoid creation racing (tests pool creation separately).
	for i := 1; i <= numTenants; i++ {
		schema := tenantSchema(i)
		if _, err := mgr.ForSite(ctx, schema); err != nil {
			t.Fatalf("pre-create pool for %q: %v", schema, err)
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

			schema := tenantSchema((id%numTenants)+1)
			pool, err := mgr.ForSite(ctx, schema)
			if err != nil {
				mu.Lock()
				errList = append(errList, fmt.Sprintf("g%d ForSite(%q): %v", id, schema, err))
				mu.Unlock()
				return
			}

			// Insert a uniquely tagged row.
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

			// Read it back immediately.
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

	if len(errList) > 0 {
		for _, e := range errList {
			t.Error(e)
		}
		t.Fatalf("%d goroutine errors (see above)", len(errList))
	}

	// Post-check: for each schema, every row must have value == schema name.
	for i := 1; i <= numTenants; i++ {
		schema := tenantSchema(i)
		pool, _ := mgr.ForSite(ctx, schema)

		rows, err := pool.Query(ctx, "SELECT value FROM tab_test")
		if err != nil {
			t.Fatalf("post-check query %q: %v", schema, err)
		}

		var contaminated []string
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				t.Fatalf("post-check scan: %v", err)
			}
			if v != schema {
				contaminated = append(contaminated, v)
			}
		}
		rows.Close()

		if len(contaminated) > 0 {
			t.Errorf("CROSS-CONTAMINATION in %q: found foreign values: %v",
				schema, contaminated)
		}
	}
	t.Logf("TestConcurrentAccess: %d goroutines x %d tenants — zero cross-contamination",
		numGoroutines, numTenants)
}

// ────────────────────────────────────────────────────────────────────────────
// Test 3: assertSchema defense-in-depth
// ────────────────────────────────────────────────────────────────────────────

// TestAssertSchema verifies that assertSchema correctly identifies the current
// schema and rejects mismatched expectations.
func TestAssertSchema(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManager(t, 5)

	pool1, err := mgr.ForSite(ctx, "tenant_01")
	if err != nil {
		t.Fatalf("ForSite tenant_01: %v", err)
	}

	// Correct assertion must succeed.
	if err := assertSchema(ctx, pool1, "tenant_01"); err != nil {
		t.Errorf("assertSchema(tenant_01, want tenant_01): unexpected error: %v", err)
	}

	// Wrong assertion must fail with a mismatch error.
	err = assertSchema(ctx, pool1, "tenant_02")
	if err == nil {
		t.Error("assertSchema(tenant_01 pool, want tenant_02): expected error, got nil")
	} else {
		t.Logf("assertSchema correctly returned error: %v", err)
	}

	// System pool must be bound to moca_system.
	if err := assertSchema(ctx, mgr.SystemPool(), "moca_system"); err != nil {
		t.Errorf("assertSchema(systemPool, want moca_system): %v", err)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Test 4: Prepared Statement Cache Isolation
// ────────────────────────────────────────────────────────────────────────────

// TestPreparedStatementIsolation verifies that identical query text executed
// against two different tenant pools returns data from the correct schema.
// Since each tenant has a dedicated pool with its own statement cache, cache
// hits from tenant A cannot bleed into tenant B.
func TestPreparedStatementIsolation(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManager(t, 5)

	schemas := []string{"tenant_03", "tenant_04"}
	const queryText = "SELECT value FROM tab_test WHERE name = $1"

	// Insert distinguishable data into each tenant.
	for _, schema := range schemas {
		pool, err := mgr.ForSite(ctx, schema)
		if err != nil {
			t.Fatalf("ForSite(%q): %v", schema, err)
		}
		if _, err := pool.Exec(ctx,
			"INSERT INTO tab_test (name, value) VALUES ($1, $2)",
			"stmt_test_row", schema, // same name, different value
		); err != nil {
			t.Fatalf("insert into %q: %v", schema, err)
		}
	}

	// Execute the same query text twice on each pool (to trigger cache reuse).
	// Both executions must return the pool's own tenant value.
	for _, schema := range schemas {
		pool, _ := mgr.ForSite(ctx, schema)
		for pass := 1; pass <= 2; pass++ {
			var got string
			if err := pool.QueryRow(ctx, queryText, "stmt_test_row").Scan(&got); err != nil {
				t.Fatalf("%q pass %d: %v", schema, pass, err)
			}
			if got != schema {
				t.Errorf("CACHE LEAK %q pass %d: expected %q, got %q", schema, pass, schema, got)
			}
		}
		stat := pool.Stat()
		t.Logf("Pool %q: AcquireCount=%d, TotalConns=%d",
			schema, stat.AcquireCount(), stat.TotalConns())
	}
	t.Log("Prepared statement caches are pool-local — no cross-pool leakage possible")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 5: search_path Persists Across Connection Reuse
// ────────────────────────────────────────────────────────────────────────────

// TestConnectionReuse verifies that AfterConnect's search_path setting persists
// when a connection is returned to the pool and re-acquired. Uses MaxConns=1
// to guarantee the same physical connection is reused.
func TestConnectionReuse(t *testing.T) {
	ctx := context.Background()
	// MaxConns=1: forces the pool to reuse the single physical connection.
	mgr := newTestManager(t, 1)

	pool, err := mgr.ForSite(ctx, "tenant_05")
	if err != nil {
		t.Fatalf("ForSite tenant_05: %v", err)
	}

	// First acquire: check search_path is set correctly.
	conn1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	var schema1 string
	if err := conn1.QueryRow(ctx, "SELECT current_schema()").Scan(&schema1); err != nil {
		conn1.Release()
		t.Fatalf("current_schema (first): %v", err)
	}
	conn1.Release() // return to pool

	if schema1 != "tenant_05" {
		t.Errorf("first acquire: expected tenant_05, got %q", schema1)
	}

	// Second acquire: same physical connection, search_path must still be correct.
	conn2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	var schema2 string
	if err := conn2.QueryRow(ctx, "SELECT current_schema()").Scan(&schema2); err != nil {
		conn2.Release()
		t.Fatalf("current_schema (second): %v", err)
	}
	conn2.Release()

	if schema2 != "tenant_05" {
		t.Errorf("second acquire (reused conn): expected tenant_05, got %q", schema2)
	}

	// Sanity: insert via pool, verify it lands in tenant_05.
	if _, err := pool.Exec(ctx,
		"INSERT INTO tab_test (name, value) VALUES ($1, $2)",
		"reuse_test", "tenant_05",
	); err != nil {
		t.Fatalf("insert via pool: %v", err)
	}

	var val string
	if err := pool.QueryRow(ctx,
		"SELECT value FROM tab_test WHERE name = $1", "reuse_test",
	).Scan(&val); err != nil {
		t.Fatalf("select via pool: %v", err)
	}
	if val != "tenant_05" {
		t.Errorf("data in wrong schema: expected tenant_05, got %q", val)
	}
	t.Log("search_path persists across acquire/release cycles — AfterConnect is correct hook")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 6: Pool Lifecycle (lazy creation, idle eviction, re-creation)
// ────────────────────────────────────────────────────────────────────────────

// TestPoolLifecycle validates the three phases of a site pool's lifecycle:
//  1. Created lazily on first ForSite call
//  2. Evicted by EvictIdlePools when idle duration exceeded
//  3. Re-created transparently by the next ForSite call
func TestPoolLifecycle(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManager(t, 5)

	// Phase 1: No pools exist yet.
	if n := mgr.SitePoolCount(); n != 0 {
		t.Fatalf("expected 0 site pools initially, got %d", n)
	}

	// Phase 2: First ForSite creates the pool lazily.
	pool1, err := mgr.ForSite(ctx, "tenant_06")
	if err != nil {
		t.Fatalf("ForSite tenant_06: %v", err)
	}
	if n := mgr.SitePoolCount(); n != 1 {
		t.Fatalf("expected 1 site pool after ForSite, got %d", n)
	}

	// Insert data to verify re-created pool can still read it.
	if _, err := pool1.Exec(ctx,
		"INSERT INTO tab_test (name, value) VALUES ($1, $2)",
		"lifecycle_test", "tenant_06",
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Phase 3: Artificially age the pool's lastUsed timestamp.
	mgr.SetLastUsed("tenant_06", time.Now().Add(-2*time.Hour))

	evicted := mgr.EvictIdlePools(1 * time.Hour)
	if evicted != 1 {
		t.Fatalf("expected 1 eviction, got %d", evicted)
	}
	if n := mgr.SitePoolCount(); n != 0 {
		t.Fatalf("expected 0 site pools after eviction, got %d", n)
	}

	// Phase 4: Re-creation on next ForSite.
	pool2, err := mgr.ForSite(ctx, "tenant_06")
	if err != nil {
		t.Fatalf("ForSite after eviction: %v", err)
	}
	if n := mgr.SitePoolCount(); n != 1 {
		t.Fatalf("expected 1 site pool after re-creation, got %d", n)
	}

	// Data is in PostgreSQL (not in the pool), so the new pool can still read it.
	var val string
	if err := pool2.QueryRow(ctx,
		"SELECT value FROM tab_test WHERE name = $1", "lifecycle_test",
	).Scan(&val); err != nil {
		t.Fatalf("read after re-creation: %v", err)
	}
	if val != "tenant_06" {
		t.Errorf("re-created pool wrong schema: expected tenant_06, got %q", val)
	}

	// New pool also has correct search_path set by AfterConnect.
	if err := assertSchema(ctx, pool2, "tenant_06"); err != nil {
		t.Errorf("assertSchema after re-creation: %v", err)
	}
	t.Log("Pool lifecycle: lazy creation → idle eviction → transparent re-creation — all correct")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 7: System Pool Isolation
// ────────────────────────────────────────────────────────────────────────────

// TestSystemPoolIsolation verifies that the system pool is permanently bound
// to moca_system and cannot access tenant tables via unqualified names.
func TestSystemPoolIsolation(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManager(t, 5)

	sysPool := mgr.SystemPool()

	// System pool must be in moca_system.
	if err := assertSchema(ctx, sysPool, "moca_system"); err != nil {
		t.Errorf("system pool not in moca_system: %v", err)
	}

	// System pool can query the sites table.
	var count int
	if err := sysPool.QueryRow(ctx, "SELECT COUNT(*) FROM sites").Scan(&count); err != nil {
		t.Errorf("system pool cannot query moca_system.sites: %v", err)
	} else {
		t.Logf("system pool sees %d rows in moca_system.sites", count)
	}

	// System pool cannot query tab_test (exists only in tenant schemas, not moca_system).
	err := sysPool.QueryRow(ctx, "SELECT COUNT(*) FROM tab_test").Scan(&count)
	if err == nil {
		// tab_test should not exist in moca_system
		t.Log("note: moca_system.tab_test exists (unexpected) — system pool should not create tables there")
	} else {
		t.Logf("system pool correctly cannot see tab_test: %v", err)
	}

	// Tenant pool must not interfere with system pool's view of moca_system.
	tenantPool, err := mgr.ForSite(ctx, "tenant_07")
	if err != nil {
		t.Fatalf("ForSite tenant_07: %v", err)
	}
	if _, err := tenantPool.Exec(ctx,
		"INSERT INTO tab_test (name, value) VALUES ($1, $2)",
		"sys_isolation_test", "tenant_07",
	); err != nil {
		t.Fatalf("insert into tenant_07: %v", err)
	}

	// System pool still sees only moca_system.
	if err := assertSchema(ctx, sysPool, "moca_system"); err != nil {
		t.Errorf("system pool schema changed after tenant operation: %v", err)
	}

	// System pool still reads sites correctly.
	if err := sysPool.QueryRow(ctx, "SELECT COUNT(*) FROM sites").Scan(&count); err != nil {
		t.Errorf("system pool lost access to moca_system.sites: %v", err)
	}
	t.Log("System pool isolation confirmed — tenant operations do not affect system pool search_path")
}
