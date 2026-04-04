//go:build integration

package meta_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
)

// ── connection defaults (mirror pkg/orm/postgres_test.go) ────────────────────

const (
	migratorTestHost      = "localhost"
	migratorTestPort      = 5433
	migratorTestUser      = "moca"
	migratorTestPassword  = "moca_test"
	migratorTestDB        = "moca_test"
	migratorTestSite      = "meta_test"
	migratorTestSchema    = "tenant_meta_test"
	registryTestRedisHost = "localhost"
	registryTestRedisPort = 6379
)

// migratorAdminPool is a raw pool used only for fixture setup/teardown.
var migratorAdminPool *pgxpool.Pool

// testRedisClient is a Redis client shared by registry integration tests.
// It is nil when Redis is unavailable; registry tests must skip in that case.
var testRedisClient *redis.Client

// TestMain sets up shared fixtures for all pkg/meta integration tests:
//   - PostgreSQL: moca_system schema + sites table + tenant schema
//   - Redis: optional client for registry tests (nil when unavailable)
func TestMain(m *testing.M) {
	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			migratorTestUser, migratorTestPassword,
			migratorTestHost, migratorTestPort, migratorTestDB,
		)
	}

	ctx := context.Background()

	var err error
	migratorAdminPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create admin pool: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Start PostgreSQL: docker compose up -d\n")
		os.Exit(0) // skip rather than fail
	}
	defer migratorAdminPool.Close()

	if err := migratorAdminPool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to PostgreSQL at %s: %v\n", connStr, err)
		fmt.Fprintf(os.Stderr, "  Start PostgreSQL: docker compose up -d\n")
		os.Exit(0)
	}

	// Set up: create moca_system schema+sites table and the tenant schema.
	if err := migratorSetupFixtures(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "migrator fixture setup failed: %v\n", err)
		os.Exit(1)
	}

	// Probe Redis — optional; registry tests skip when unavailable.
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = fmt.Sprintf("%s:%d", registryTestRedisHost, registryTestRedisPort)
	}
	rc := redis.NewClient(&redis.Options{Addr: redisAddr, DB: 0})
	if err := rc.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "INFO: Redis unavailable at %s: %v — registry tests will be skipped\n",
			redisAddr, err)
		rc.Close()
		// testRedisClient remains nil; registry tests check for this.
	} else {
		testRedisClient = rc
		defer func() {
			testRedisClient.Close()
			testRedisClient = nil
		}()
	}

	exitCode := m.Run()

	// Tear down: drop the tenant schema.
	if err := migratorTeardownFixtures(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "migrator fixture teardown failed: %v\n", err)
	}

	os.Exit(exitCode)
}

func migratorSetupFixtures(ctx context.Context) error {
	// moca_system is required by DBManager.
	if _, err := migratorAdminPool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS moca_system;
		CREATE TABLE IF NOT EXISTS moca_system.sites (
			name        TEXT PRIMARY KEY,
			db_schema   TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'active',
			admin_email TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		return fmt.Errorf("create moca_system: %w", err)
	}

	// Tenant schema used by all migrator integration tests.
	if _, err := migratorAdminPool.Exec(ctx, fmt.Sprintf(
		"CREATE SCHEMA IF NOT EXISTS %s", migratorTestSchema,
	)); err != nil {
		return fmt.Errorf("create tenant schema %s: %w", migratorTestSchema, err)
	}
	return nil
}

func migratorTeardownFixtures(ctx context.Context) error {
	_, err := migratorAdminPool.Exec(ctx, fmt.Sprintf(
		"DROP SCHEMA IF EXISTS %s CASCADE", migratorTestSchema,
	))
	return err
}

// migratorTestConfig returns a DatabaseConfig for the integration test instance.
func migratorTestConfig() config.DatabaseConfig {
	host := os.Getenv("PG_HOST")
	if host == "" {
		host = migratorTestHost
	}
	return config.DatabaseConfig{
		Host:     host,
		Port:     migratorTestPort,
		User:     migratorTestUser,
		Password: migratorTestPassword,
		SystemDB: migratorTestDB,
		PoolSize: 10,
	}
}

// newMigratorTestMgr creates a DBManager for migrator tests.
func newMigratorTestMgr(t *testing.T) *orm.DBManager {
	t.Helper()
	logger := observe.NewLogger(slog.LevelWarn)
	mgr, err := orm.NewDBManager(context.Background(), migratorTestConfig(), logger)
	if err != nil {
		t.Fatalf("NewDBManager: %v", err)
	}
	t.Cleanup(func() { mgr.Close() })
	return mgr
}

// newMigratorForTest creates a Migrator backed by a real DBManager.
func newMigratorForTest(t *testing.T) *meta.Migrator {
	t.Helper()
	mgr := newMigratorTestMgr(t)
	logger := observe.NewLogger(slog.LevelWarn)
	return meta.NewMigrator(mgr, logger)
}

// columnExists queries information_schema.columns to check whether a column
// exists in the given table within the tenant schema.
func columnExists(ctx context.Context, t *testing.T, tableName, columnName string) bool {
	t.Helper()
	var count int
	err := migratorAdminPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
	`, migratorTestSchema, tableName, columnName).Scan(&count)
	if err != nil {
		t.Fatalf("columnExists query failed: %v", err)
	}
	return count > 0
}

// tableExists queries information_schema.tables.
func tableExists(ctx context.Context, t *testing.T, tableName string) bool {
	t.Helper()
	var count int
	err := migratorAdminPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = $2
	`, migratorTestSchema, tableName).Scan(&count)
	if err != nil {
		t.Fatalf("tableExists query failed: %v", err)
	}
	return count > 0
}

// indexExists queries pg_indexes.
func indexExists(ctx context.Context, t *testing.T, indexName string) bool {
	t.Helper()
	var count int
	err := migratorAdminPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = $1 AND indexname = $2
	`, migratorTestSchema, indexName).Scan(&count)
	if err != nil {
		t.Fatalf("indexExists query failed: %v", err)
	}
	return count > 0
}

// ── Integration tests ─────────────────────────────────────────────────────────

// TestApply_CreatesTable verifies that Apply creates a table from GenerateTableDDL output.
func TestApply_CreatesTable(t *testing.T) {
	ctx := context.Background()
	m := newMigratorForTest(t)

	mt := &meta.MetaType{
		Name:   "ApplyCreateTest",
		Module: "test",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
			{Name: "score", FieldType: meta.FieldTypeFloat},
		},
	}

	stmts := meta.GenerateTableDDL(mt)
	if err := m.Apply(ctx, migratorTestSite, stmts); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	t.Cleanup(func() {
		migratorAdminPool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s.tab_apply_create_test CASCADE", migratorTestSchema))
	})

	if !tableExists(ctx, t, "tab_apply_create_test") {
		t.Error("table tab_apply_create_test was not created")
	}

	// Verify user columns exist.
	for _, col := range []string{"title", "score"} {
		if !columnExists(ctx, t, "tab_apply_create_test", col) {
			t.Errorf("column %q not found in tab_apply_create_test", col)
		}
	}

	// Verify standard columns exist.
	for _, col := range []string{"name", "owner", "creation", "_extra"} {
		if !columnExists(ctx, t, "tab_apply_create_test", col) {
			t.Errorf("standard column %q not found in tab_apply_create_test", col)
		}
	}

	t.Logf("Apply created tab_apply_create_test successfully")
}

// TestApply_AddFieldMigration verifies that a Diff-generated ADD COLUMN is applied correctly.
func TestApply_AddFieldMigration(t *testing.T) {
	ctx := context.Background()
	m := newMigratorForTest(t)

	current := &meta.MetaType{
		Name:   "MigrateAddField",
		Module: "test",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
		},
	}
	desired := &meta.MetaType{
		Name:   "MigrateAddField",
		Module: "test",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
			{Name: "due_date", FieldType: meta.FieldTypeDate},
		},
	}

	// Create initial table.
	if err := m.Apply(ctx, migratorTestSite, meta.GenerateTableDDL(current)); err != nil {
		t.Fatalf("Apply initial table: %v", err)
	}
	t.Cleanup(func() {
		migratorAdminPool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s.tab_migrate_add_field CASCADE", migratorTestSchema))
	})

	// Apply migration.
	diffStmts := m.Diff(current, desired)
	if len(diffStmts) == 0 {
		t.Fatal("Diff produced no statements for add-field migration")
	}
	if err := m.Apply(ctx, migratorTestSite, diffStmts); err != nil {
		t.Fatalf("Apply migration: %v", err)
	}

	// Verify new column exists.
	if !columnExists(ctx, t, "tab_migrate_add_field", "due_date") {
		t.Error("column 'due_date' was not added by migration")
	}
	t.Logf("Add-field migration applied successfully")
}

// TestApply_Idempotent verifies that applying the same DDL twice does not error.
func TestApply_Idempotent(t *testing.T) {
	ctx := context.Background()
	m := newMigratorForTest(t)

	mt := &meta.MetaType{
		Name:   "IdempotentTest",
		Module: "test",
		Fields: []meta.FieldDef{
			{Name: "value", FieldType: meta.FieldTypeData},
		},
	}

	stmts := meta.GenerateTableDDL(mt)
	t.Cleanup(func() {
		migratorAdminPool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s.tab_idempotent_test CASCADE", migratorTestSchema))
	})

	if err := m.Apply(ctx, migratorTestSite, stmts); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := m.Apply(ctx, migratorTestSite, stmts); err != nil {
		t.Fatalf("second Apply (idempotency check): %v", err)
	}
	t.Logf("Apply is idempotent")
}

// TestEnsureMetaTables_AllSystemTables verifies all per-tenant system tables are created.
func TestEnsureMetaTables_AllSystemTables(t *testing.T) {
	ctx := context.Background()
	m := newMigratorForTest(t)

	if err := m.EnsureMetaTables(ctx, migratorTestSite); err != nil {
		t.Fatalf("EnsureMetaTables: %v", err)
	}

	for _, tbl := range []string{"tab_doctype", "tab_singles", "tab_version", "tab_audit_log", "tab_outbox", "tab_migration_log"} {
		if !tableExists(ctx, t, tbl) {
			t.Errorf("system table %q was not created", tbl)
		}
	}

	t.Logf("All system tables created by EnsureMetaTables")
}

// TestEnsureMetaTables_AuditLogIsPartitioned verifies tab_audit_log is a partitioned table.
func TestEnsureMetaTables_AuditLogIsPartitioned(t *testing.T) {
	ctx := context.Background()
	m := newMigratorForTest(t)

	if err := m.EnsureMetaTables(ctx, migratorTestSite); err != nil {
		t.Fatalf("EnsureMetaTables: %v", err)
	}

	// pg_class.relkind = 'p' means partitioned table.
	var relkind string
	err := migratorAdminPool.QueryRow(ctx, `
		SELECT c.relkind::text FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = 'tab_audit_log'
	`, migratorTestSchema).Scan(&relkind)
	if err != nil {
		t.Fatalf("query pg_class for tab_audit_log: %v", err)
	}
	if relkind != "p" {
		t.Errorf("tab_audit_log relkind = %q; want 'p' (partitioned)", relkind)
	}
	t.Logf("tab_audit_log is correctly partitioned (relkind='p')")
}

// TestEnsureMetaTables_VersionRefIndex verifies idx_version_ref exists on tab_version.
func TestEnsureMetaTables_VersionRefIndex(t *testing.T) {
	ctx := context.Background()
	m := newMigratorForTest(t)

	if err := m.EnsureMetaTables(ctx, migratorTestSite); err != nil {
		t.Fatalf("EnsureMetaTables: %v", err)
	}

	if !indexExists(ctx, t, "idx_version_ref") {
		t.Error("idx_version_ref index was not created on tab_version")
	}
	t.Logf("idx_version_ref index created successfully")
}

func TestEnsureMetaTables_OutboxColumnsAndPendingIndex(t *testing.T) {
	ctx := context.Background()
	m := newMigratorForTest(t)

	if err := m.EnsureMetaTables(ctx, migratorTestSite); err != nil {
		t.Fatalf("EnsureMetaTables: %v", err)
	}

	for _, col := range []string{"status", "retry_count", "published_at", "processed"} {
		if !columnExists(ctx, t, "tab_outbox", col) {
			t.Errorf("outbox column %q was not created", col)
		}
	}
	if !indexExists(ctx, t, "idx_outbox_pending") {
		t.Error("idx_outbox_pending was not created")
	}
}

// TestEnsureMetaTables_Idempotent verifies calling EnsureMetaTables twice does not error.
func TestEnsureMetaTables_Idempotent(t *testing.T) {
	ctx := context.Background()
	m := newMigratorForTest(t)

	if err := m.EnsureMetaTables(ctx, migratorTestSite); err != nil {
		t.Fatalf("first EnsureMetaTables: %v", err)
	}
	if err := m.EnsureMetaTables(ctx, migratorTestSite); err != nil {
		t.Fatalf("second EnsureMetaTables (idempotency check): %v", err)
	}
	t.Logf("EnsureMetaTables is idempotent")
}

// TestEnsureMetaTables_AuditLogAcceptsInserts verifies the default partition allows inserts.
func TestEnsureMetaTables_AuditLogAcceptsInserts(t *testing.T) {
	ctx := context.Background()
	m := newMigratorForTest(t)

	if err := m.EnsureMetaTables(ctx, migratorTestSite); err != nil {
		t.Fatalf("EnsureMetaTables: %v", err)
	}

	// Insert should succeed because the default partition exists.
	_, err := migratorAdminPool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.tab_audit_log (doctype, docname, action, user_id)
		VALUES ('TestDoc', 'TD-001', 'Create', 'test_user')
	`, migratorTestSchema))
	if err != nil {
		t.Errorf("insert into tab_audit_log failed (no default partition?): %v", err)
	} else {
		t.Logf("Insert into tab_audit_log succeeded via default partition")
	}
}
