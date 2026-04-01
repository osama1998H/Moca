//go:build integration

package orm_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/observe"
	"github.com/moca-framework/moca/pkg/orm"
)

// ── helpers ─────────────────────────────────────────────────────────────────

// setupMigrateTestSchema creates an isolated schema for a migrate test.
// It creates the schema, registers it in moca_system.sites, creates system
// tables (including tab_migration_log), and returns the site name.
// Cleanup is registered via t.Cleanup.
func setupMigrateTestSchema(t *testing.T, suffix string) string {
	t.Helper()

	schema := "tenant_migrate_" + suffix
	site := "migrate_" + suffix
	ctx := context.Background()

	// Create schema.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(
		"CREATE SCHEMA IF NOT EXISTS %s",
		pgx.Identifier{schema}.Sanitize(),
	)); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}

	// Register in moca_system.sites so ForSite can find it.
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO moca_system.sites (name, db_schema, admin_email)
		VALUES ($1, $2, 'test@test.dev')
		ON CONFLICT (name) DO UPDATE SET db_schema = EXCLUDED.db_schema`,
		site, schema,
	); err != nil {
		t.Fatalf("insert site row: %v", err)
	}

	// Create system tables including tab_migration_log.
	logger := observe.NewLogger(slog.LevelWarn)
	mgr := newTestManager(t)
	migrator := meta.NewMigrator(mgr, logger)
	if err := migrator.EnsureMetaTables(ctx, site); err != nil {
		t.Fatalf("EnsureMetaTables(%s): %v", site, err)
	}

	t.Cleanup(func() {
		bgCtx := context.Background()
		_, _ = adminPool.Exec(bgCtx, fmt.Sprintf(
			"DROP SCHEMA IF EXISTS %s CASCADE",
			pgx.Identifier{schema}.Sanitize(),
		))
		_, _ = adminPool.Exec(bgCtx,
			"DELETE FROM moca_system.sites WHERE name = $1", site)
	})

	return site
}

// sampleMigrations returns two migrations: A creates tab_widget, B depends on A
// and creates tab_gadget with a foreign key to tab_widget.
func sampleMigrations() []orm.AppMigration {
	return []orm.AppMigration{
		{
			AppName: "testapp",
			Version: "001",
			UpSQL:   `CREATE TABLE tab_widget (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`,
			DownSQL: `DROP TABLE IF EXISTS tab_widget`,
		},
		{
			AppName:   "testapp",
			Version:   "002",
			UpSQL:     `CREATE TABLE tab_gadget (id SERIAL PRIMARY KEY, widget_id INT REFERENCES tab_widget(id))`,
			DownSQL:   `DROP TABLE IF EXISTS tab_gadget`,
			DependsOn: []string{"testapp:001"},
		},
	}
}

type logEntry struct {
	App     string
	Version string
	Batch   int
}

// migrationLogEntries returns all entries from tab_migration_log ordered by id.
func migrationLogEntries(t *testing.T, mgr *orm.DBManager, site string) []logEntry {
	t.Helper()
	ctx := context.Background()
	pool, err := mgr.ForSite(ctx, site)
	if err != nil {
		t.Fatalf("ForSite(%s): %v", site, err)
	}

	rows, err := pool.Query(ctx, "SELECT app, version, batch FROM tab_migration_log ORDER BY id")
	if err != nil {
		t.Fatalf("query tab_migration_log: %v", err)
	}
	defer rows.Close()

	var entries []logEntry
	for rows.Next() {
		var e logEntry
		if err := rows.Scan(&e.App, &e.Version, &e.Batch); err != nil {
			t.Fatalf("scan migration log: %v", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migration log: %v", err)
	}
	return entries
}

// tableExistsInSchema checks if a table exists in a specific schema.
func tableExistsInSchema(t *testing.T, schema, table string) bool {
	t.Helper()
	var count int
	err := adminPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = $1 AND table_name = $2`,
		schema, table,
	).Scan(&count)
	if err != nil {
		t.Fatalf("tableExistsInSchema(%s, %s): %v", schema, table, err)
	}
	return count > 0
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestMigrateRunner_ApplyAndVerify(t *testing.T) {
	site := setupMigrateTestSchema(t, "apply")
	schema := "tenant_migrate_apply"
	ctx := context.Background()

	logger := observe.NewLogger(slog.LevelWarn)
	mgr := newTestManager(t)
	runner := orm.NewMigrationRunner(mgr, logger)

	migrations := sampleMigrations()
	result, err := runner.Apply(ctx, site, migrations, orm.MigrateOptions{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Verify result fields.
	if len(result.Applied) != 2 {
		t.Errorf("expected 2 applied, got %d", len(result.Applied))
	}
	if result.Batch != 1 {
		t.Errorf("expected batch 1, got %d", result.Batch)
	}
	if result.DryRun {
		t.Error("expected DryRun=false")
	}

	// Verify tables exist.
	if !tableExistsInSchema(t, schema, "tab_widget") {
		t.Error("tab_widget not created")
	}
	if !tableExistsInSchema(t, schema, "tab_gadget") {
		t.Error("tab_gadget not created")
	}

	// Verify migration log entries.
	entries := migrationLogEntries(t, mgr, site)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}

	// A (001) must appear before B (002) in log by insertion order.
	if entries[0].Version != "001" {
		t.Errorf("first entry: expected version 001, got %s", entries[0].Version)
	}
	if entries[1].Version != "002" {
		t.Errorf("second entry: expected version 002, got %s", entries[1].Version)
	}
	if entries[0].Batch != 1 || entries[1].Batch != 1 {
		t.Errorf("expected both entries in batch 1, got batches %d and %d",
			entries[0].Batch, entries[1].Batch)
	}
}

func TestMigrateRunner_DependsOnOrdering(t *testing.T) {
	site := setupMigrateTestSchema(t, "deporder")
	ctx := context.Background()

	logger := observe.NewLogger(slog.LevelWarn)
	mgr := newTestManager(t)
	runner := orm.NewMigrationRunner(mgr, logger)

	// Pass migrations in reverse order: B first, A second.
	migrations := []orm.AppMigration{
		{
			AppName:   "testapp",
			Version:   "002",
			UpSQL:     `CREATE TABLE tab_gadget (id SERIAL PRIMARY KEY, widget_id INT)`,
			DownSQL:   `DROP TABLE IF EXISTS tab_gadget`,
			DependsOn: []string{"testapp:001"},
		},
		{
			AppName: "testapp",
			Version: "001",
			UpSQL:   `CREATE TABLE tab_widget (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`,
			DownSQL: `DROP TABLE IF EXISTS tab_widget`,
		},
	}

	result, err := runner.Apply(ctx, site, migrations, orm.MigrateOptions{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(result.Applied) != 2 {
		t.Fatalf("expected 2 applied, got %d", len(result.Applied))
	}

	// Verify insertion order: 001 must come before 002.
	entries := migrationLogEntries(t, mgr, site)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if entries[0].Version != "001" {
		t.Errorf("expected 001 first (DependsOn ordering), got %s", entries[0].Version)
	}
	if entries[1].Version != "002" {
		t.Errorf("expected 002 second, got %s", entries[1].Version)
	}
}

func TestMigrateRunner_Rollback(t *testing.T) {
	site := setupMigrateTestSchema(t, "rollback")
	schema := "tenant_migrate_rollback"
	ctx := context.Background()

	logger := observe.NewLogger(slog.LevelWarn)
	mgr := newTestManager(t)
	runner := orm.NewMigrationRunner(mgr, logger)

	// Apply migrations.
	migrations := sampleMigrations()
	if _, err := runner.Apply(ctx, site, migrations, orm.MigrateOptions{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Verify tables exist before rollback.
	if !tableExistsInSchema(t, schema, "tab_widget") {
		t.Fatal("tab_widget not created before rollback")
	}

	// Rollback.
	result, err := runner.Rollback(ctx, site, orm.RollbackOptions{Step: 1})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if len(result.Applied) != 2 {
		t.Errorf("expected 2 rolled back, got %d", len(result.Applied))
	}

	// Verify tables dropped.
	if tableExistsInSchema(t, schema, "tab_gadget") {
		t.Error("tab_gadget still exists after rollback")
	}
	if tableExistsInSchema(t, schema, "tab_widget") {
		t.Error("tab_widget still exists after rollback")
	}

	// Verify migration log is empty.
	entries := migrationLogEntries(t, mgr, site)
	if len(entries) != 0 {
		t.Errorf("expected 0 log entries after rollback, got %d", len(entries))
	}
}

func TestMigrateRunner_DryRun(t *testing.T) {
	site := setupMigrateTestSchema(t, "dryrun")
	schema := "tenant_migrate_dryrun"
	ctx := context.Background()

	logger := observe.NewLogger(slog.LevelWarn)
	mgr := newTestManager(t)
	runner := orm.NewMigrationRunner(mgr, logger)

	migrations := sampleMigrations()
	previews, err := runner.DryRun(ctx, site, migrations)
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}

	// Verify previews returned.
	if len(previews) != 2 {
		t.Fatalf("expected 2 previews, got %d", len(previews))
	}
	if previews[0].SQL == "" {
		t.Error("preview[0] has empty SQL")
	}

	// Verify NO tables were created (no side effects).
	if tableExistsInSchema(t, schema, "tab_widget") {
		t.Error("tab_widget should not exist after dry-run")
	}
	if tableExistsInSchema(t, schema, "tab_gadget") {
		t.Error("tab_gadget should not exist after dry-run")
	}

	// Verify migration log is empty.
	entries := migrationLogEntries(t, mgr, site)
	if len(entries) != 0 {
		t.Errorf("expected 0 log entries after dry-run, got %d", len(entries))
	}
}

func TestMigrateRunner_PendingFiltersApplied(t *testing.T) {
	site := setupMigrateTestSchema(t, "pending")
	ctx := context.Background()

	logger := observe.NewLogger(slog.LevelWarn)
	mgr := newTestManager(t)
	runner := orm.NewMigrationRunner(mgr, logger)

	// Apply only the first migration.
	migrationA := orm.AppMigration{
		AppName: "testapp",
		Version: "001",
		UpSQL:   `CREATE TABLE tab_widget (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`,
		DownSQL: `DROP TABLE IF EXISTS tab_widget`,
	}
	if _, err := runner.Apply(ctx, site, []orm.AppMigration{migrationA}, orm.MigrateOptions{}); err != nil {
		t.Fatalf("Apply A: %v", err)
	}

	// Now check Pending with both A and B.
	allMigrations := sampleMigrations()
	pending, err := runner.Pending(ctx, site, allMigrations)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}

	if len(pending) != 1 {
		t.Fatalf("expected 1 pending migration, got %d", len(pending))
	}
	if pending[0].Version != "002" {
		t.Errorf("expected pending migration version 002, got %s", pending[0].Version)
	}
}
