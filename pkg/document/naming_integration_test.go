//go:build integration

package document_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moca-framework/moca/pkg/document"
)

// ── connection defaults ────────────────────────────────────────────────────────

const (
	namingTestHost     = "localhost"
	namingTestPort     = 5433
	namingTestUser     = "moca"
	namingTestPassword = "moca_test"
	namingTestDB       = "moca_test"
	namingTestSchema   = "tenant_naming_test"
)

// namingTestPool is a pool with search_path bound to namingTestSchema.
// Sequences created through this pool land in the test schema and are cleaned
// up when the schema is dropped in TestMain teardown.
var namingTestPool *pgxpool.Pool

// TestMain sets up shared fixtures for all pkg/document integration tests:
//   - PostgreSQL: creates the tenant_naming_test schema
//   - namingTestPool: a pool with its search_path set to that schema
//
// All test infrastructure is torn down after m.Run() exits.
func TestMain(m *testing.M) {
	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			namingTestUser, namingTestPassword,
			namingTestHost, namingTestPort, namingTestDB,
		)
	}

	ctx := context.Background()

	// Admin pool for schema setup and teardown; uses the default search_path.
	adminPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create admin pool: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Start PostgreSQL: docker compose up -d\n")
		os.Exit(0) // skip rather than fail
	}
	defer adminPool.Close()

	if err := adminPool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to PostgreSQL at %s: %v\n", connStr, err)
		fmt.Fprintf(os.Stderr, "  Start PostgreSQL: docker compose up -d\n")
		os.Exit(0)
	}

	// Create the test schema.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(
		"CREATE SCHEMA IF NOT EXISTS %s",
		pgx.Identifier{namingTestSchema}.Sanitize(),
	)); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create schema %q: %v\n", namingTestSchema, err)
		os.Exit(1)
	}

	// Build a pool whose connections have search_path permanently set to the
	// test schema so that sequences are created in the right place.
	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: parse pool config: %v\n", err)
		os.Exit(1)
	}
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf(
			"SET search_path TO %s, public",
			pgx.Identifier{namingTestSchema}.Sanitize(),
		))
		return err
	}
	namingTestPool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create test pool: %v\n", err)
		os.Exit(1)
	}
	defer namingTestPool.Close()

	exitCode := m.Run()

	// Tear down: drop the schema and everything in it (sequences, tables).
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(
		"DROP SCHEMA IF EXISTS %s CASCADE",
		pgx.Identifier{namingTestSchema}.Sanitize(),
	)); err != nil {
		fmt.Fprintf(os.Stderr, "teardown warning: drop schema %q: %v\n", namingTestSchema, err)
	}

	os.Exit(exitCode)
}

// ── integration tests ─────────────────────────────────────────────────────────

func TestNamingEngine_AutoIncrement(t *testing.T) {
	const autoJSON = `{
		"name": "AutoIncrTestDoc",
		"module": "test",
		"naming_rule": {"rule": "autoincrement"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`
	mt := mustCompile(t, autoJSON)
	engine := document.NewNamingEngine()

	var names []string
	for i := 0; i < 3; i++ {
		doc := document.NewDynamicDoc(mt, nil, true)
		name, err := engine.GenerateName(context.Background(), doc, namingTestPool)
		if err != nil {
			t.Fatalf("GenerateName(autoincrement) call %d: %v", i+1, err)
		}
		names = append(names, name)
		t.Logf("autoincrement call %d → %q", i+1, name)
	}

	// Autoincrement must produce sequential integers starting at 1.
	for i, want := range []string{"1", "2", "3"} {
		if names[i] != want {
			t.Errorf("name[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestNamingEngine_Pattern(t *testing.T) {
	const patternJSON = `{
		"name": "PatternTestOrder",
		"module": "test",
		"naming_rule": {"rule": "pattern", "pattern": "SO-.####"},
		"fields": [{"name": "customer", "field_type": "Data", "label": "Customer"}]
	}`
	mt := mustCompile(t, patternJSON)
	engine := document.NewNamingEngine()

	for i, want := range []string{"SO-0001", "SO-0002", "SO-0003"} {
		doc := document.NewDynamicDoc(mt, nil, true)
		name, err := engine.GenerateName(context.Background(), doc, namingTestPool)
		if err != nil {
			t.Fatalf("GenerateName(pattern) call %d: %v", i+1, err)
		}
		if name != want {
			t.Errorf("call %d: name = %q, want %q", i+1, name, want)
		}
		t.Logf("pattern call %d → %q", i+1, name)
	}
}

// TestNamingEngine_PatternConcurrency verifies that 10 concurrent goroutines each
// receive a unique, correctly-formatted name from the pattern "TO-.####".
// The test is run under the race detector (-race) during CI.
func TestNamingEngine_PatternConcurrency(t *testing.T) {
	const concurrentJSON = `{
		"name": "ConcurrentTestOrder",
		"module": "test",
		"naming_rule": {"rule": "pattern", "pattern": "TO-.####"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`
	mt := mustCompile(t, concurrentJSON)
	engine := document.NewNamingEngine()

	const n = 10
	results := make([]string, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			doc := document.NewDynamicDoc(mt, nil, true)
			name, err := engine.GenerateName(context.Background(), doc, namingTestPool)
			results[i] = name
			errs[i] = err
		}()
	}
	wg.Wait()

	// Check no errors.
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d error: %v", i, err)
		}
	}

	// All names must be unique.
	seen := make(map[string]int, n)
	for i, name := range results {
		if prev, dup := seen[name]; dup {
			t.Errorf("duplicate name %q produced by goroutines %d and %d", name, prev, i)
		}
		seen[name] = i
	}

	// The complete set TO-0001..TO-0010 must be present.
	for i := 1; i <= n; i++ {
		expected := fmt.Sprintf("TO-%04d", i)
		if _, ok := seen[expected]; !ok {
			t.Errorf("expected name %q not in results: %v", expected, results)
		}
	}

	t.Logf("concurrent results: %v", results)
}

// TestNamingEngine_Pattern_NoHash_Integration verifies that an invalid pattern
// with no '#' character is rejected even in an integration context (pool available).
func TestNamingEngine_Pattern_InvalidAtRuntime(t *testing.T) {
	const badPatternJSON = `{
		"name": "BadPatternDoc",
		"module": "test",
		"naming_rule": {"rule": "pattern", "pattern": "NO-HASH-HERE"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`
	mt := mustCompile(t, badPatternJSON)
	engine := document.NewNamingEngine()
	doc := document.NewDynamicDoc(mt, nil, true)

	_, err := engine.GenerateName(context.Background(), doc, namingTestPool)
	if err == nil {
		t.Fatal("expected error for pattern with no '#' character")
	}
	t.Logf("invalid pattern with real pool returns: %v", err)
}
