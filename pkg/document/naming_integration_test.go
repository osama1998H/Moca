//go:build integration

package document_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/tenancy"
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

// ── integration infrastructure (shared by naming + CRUD integration tests) ───

var (
	integDBManager   *orm.DBManager
	integRegistry    *meta.Registry
	integDocManager  *document.DocManager
	integSite        *tenancy.SiteContext
	integUser        *auth.User
	integControllers *document.ControllerRegistry
	integRedisClient *redis.Client
)

const integSiteName = "doc_integ"

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

	// ── Full DocManager infrastructure for CRUD integration tests ────────

	// 1. Create moca_system schema + sites table (required by orm.DBManager).
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
		fmt.Fprintf(os.Stderr, "FATAL: create moca_system: %v\n", err)
		os.Exit(1)
	}

	// 2. Insert a site row pointing to the existing tenant schema.
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO moca_system.sites (name, db_schema, admin_email)
		VALUES ($1, $2, $3)
		ON CONFLICT (name) DO UPDATE
		SET db_schema = EXCLUDED.db_schema,
		    admin_email = EXCLUDED.admin_email
	`, integSiteName, namingTestSchema, "test@moca.dev"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: insert site row: %v\n", err)
		os.Exit(1)
	}

	// 3. Create orm.DBManager.
	logger := observe.NewLogger(slog.LevelWarn)
	host := os.Getenv("PG_HOST")
	if host == "" {
		host = namingTestHost
	}
	integDBManager, err = orm.NewDBManager(ctx, config.DatabaseConfig{
		Host:     host,
		Port:     namingTestPort,
		User:     namingTestUser,
		Password: namingTestPassword,
		SystemDB: namingTestDB,
		PoolSize: 10,
	}, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: NewDBManager: %v\n", err)
		os.Exit(1)
	}
	defer integDBManager.Close()

	// 4. Probe Redis at localhost:6380 (docker-compose external port).
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6380"
	}
	rc := redis.NewClient(&redis.Options{Addr: redisAddr, DB: 0})
	if err := rc.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "INFO: Redis unavailable at %s: %v — CRUD integration tests will be skipped\n",
			redisAddr, err)
		rc.Close()
	} else {
		integRedisClient = rc
		defer func() {
			integRedisClient.Close()
			integRedisClient = nil
		}()
	}

	// 5. Create meta.Registry.
	integRegistry = meta.NewRegistry(integDBManager, integRedisClient, logger)

	// 6. EnsureMetaTables (tab_doctype, tab_singles, tab_outbox, tab_audit_log, tab_version).
	migrator := meta.NewMigrator(integDBManager, logger)
	if err := migrator.EnsureMetaTables(ctx, integSiteName); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: EnsureMetaTables: %v\n", err)
		os.Exit(1)
	}

	// 7. Register fixture MetaTypes.
	fixtureJSONs := []string{
		integTestOrderItemJSON,
		integLinkedDocJSON,
		integTestOrderJSON,
		integValidationJSON,
		integSingleJSON,
		integConcurrentOrderJSON,
	}
	for _, js := range fixtureJSONs {
		if _, err := integRegistry.Register(ctx, integSiteName, []byte(js)); err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: register fixture MetaType: %v\n", err)
			os.Exit(1)
		}
	}

	// 8. Create NamingEngine, Validator, ControllerRegistry, DocManager.
	naming := document.NewNamingEngine()
	validator := document.NewValidator()
	integControllers = document.NewControllerRegistry()
	integDocManager = document.NewDocManager(integRegistry, integDBManager, naming, validator, integControllers, logger)

	// 9. Build SiteContext with the tenant pool.
	sitePool, err := integDBManager.ForSite(ctx, integSiteName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: ForSite(%q): %v\n", integSiteName, err)
		os.Exit(1)
	}
	integSite = &tenancy.SiteContext{
		Name: integSiteName,
		Pool: sitePool,
	}
	integUser = &auth.User{
		Email:    "test@moca.dev",
		FullName: "Test User",
		Roles:    []string{"System Manager"},
	}

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

// ── fixture MetaType JSONs for CRUD integration tests ────────────────────────

const integTestOrderJSON = `{
	"name": "IntegTestOrder",
	"module": "test",
	"naming_rule": {"rule": "pattern", "pattern": "TO-.####"},
	"fields": [
		{"name": "customer",  "field_type": "Data",  "label": "Customer"},
		{"name": "amount",    "field_type": "Float", "label": "Amount"},
		{"name": "notes",     "field_type": "Text",  "label": "Notes"},
		{"name": "items",     "field_type": "Table", "label": "Items", "options": "IntegTestOrderItem"}
	]
}`

const integTestOrderItemJSON = `{
	"name": "IntegTestOrderItem",
	"module": "test",
	"is_child_table": true,
	"naming_rule": {"rule": "uuid"},
	"fields": [
		{"name": "item_name", "field_type": "Data", "label": "Item Name"},
		{"name": "qty",       "field_type": "Int",  "label": "Quantity"}
	]
}`

const integValidationJSON = `{
	"name": "IntegValidation",
	"module": "test",
	"naming_rule": {"rule": "uuid"},
	"fields": [
		{"name": "title",      "field_type": "Data",  "label": "Title",      "required": true},
		{"name": "code",       "field_type": "Data",  "label": "Code",       "validation_regex": "^[A-Z]{3}-[0-9]+$"},
		{"name": "unique_key", "field_type": "Data",  "label": "Unique Key", "unique": true},
		{"name": "linked_doc", "field_type": "Link",  "label": "Linked Doc", "options": "IntegLinkedDoc"},
		{"name": "count",      "field_type": "Int",   "label": "Count"},
		{"name": "active",     "field_type": "Check", "label": "Active"}
	]
}`

const integLinkedDocJSON = `{
	"name": "IntegLinkedDoc",
	"module": "test",
	"naming_rule": {"rule": "uuid"},
	"fields": [
		{"name": "label", "field_type": "Data", "label": "Label"}
	]
}`

const integSingleJSON = `{
	"name": "IntegSingle",
	"module": "test",
	"is_single": true,
	"naming_rule": {"rule": "uuid"},
	"fields": [
		{"name": "site_name",  "field_type": "Data",  "label": "Site Name"},
		{"name": "max_users",  "field_type": "Int",   "label": "Max Users"},
		{"name": "is_enabled", "field_type": "Check", "label": "Is Enabled"}
	]
}`

const integConcurrentOrderJSON = `{
	"name": "IntegConcurrentOrder",
	"module": "test",
	"naming_rule": {"rule": "pattern", "pattern": "CO-.####"},
	"fields": [
		{"name": "title", "field_type": "Data", "label": "Title"}
	]
}`

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
