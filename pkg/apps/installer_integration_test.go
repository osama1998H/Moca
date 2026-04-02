//go:build integration

package apps_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/apps/core"
	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/apps"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── connection defaults ─────────────────────────────────────────────────────

const (
	aiTestHost     = "localhost"
	aiTestPort     = 5433
	aiTestUser     = "moca"
	aiTestPassword = "moca_test"
	aiTestDB       = "moca_test"
)

// ── shared infrastructure ───────────────────────────────────────────────────

var (
	aiAdminPool   *pgxpool.Pool
	aiRedisClient *redis.Client
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		host := os.Getenv("PG_HOST")
		if host == "" {
			host = aiTestHost
		}
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			aiTestUser, aiTestPassword, host, aiTestPort, aiTestDB,
		)
	}

	var err error
	aiAdminPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create admin pool: %v\n", err)
		os.Exit(0)
	}
	defer aiAdminPool.Close()

	if err := aiAdminPool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to PostgreSQL: %v\n", err)
		os.Exit(0)
	}

	// Create full system schema.
	if err := orm.EnsureSystemSchema(ctx, aiAdminPool, "moca_system"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: EnsureSystemSchema: %v\n", err)
		os.Exit(1)
	}

	// Redis.
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisHost := os.Getenv("REDIS_HOST")
		if redisHost == "" {
			redisHost = "localhost"
		}
		redisAddr = fmt.Sprintf("%s:6380", redisHost)
	}
	rc := redis.NewClient(&redis.Options{Addr: redisAddr, DB: 0})
	if err := rc.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: Redis unavailable at %s: %v\n", redisAddr, err)
		rc.Close()
		os.Exit(0)
	}
	aiRedisClient = rc
	defer aiRedisClient.Close()

	exitCode := m.Run()

	// Safety-net teardown.
	rows, _ := aiAdminPool.Query(ctx,
		"SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant_appinteg_%'")
	if rows != nil {
		for rows.Next() {
			var schema string
			if err := rows.Scan(&schema); err == nil {
				_, _ = aiAdminPool.Exec(ctx, fmt.Sprintf(
					"DROP SCHEMA IF EXISTS %s CASCADE",
					pgx.Identifier{schema}.Sanitize(),
				))
			}
		}
		rows.Close()
	}
	_, _ = aiAdminPool.Exec(ctx,
		"DELETE FROM moca_system.site_apps WHERE site_name LIKE 'appinteg_%'")
	_, _ = aiAdminPool.Exec(ctx,
		"DELETE FROM moca_system.sites WHERE name LIKE 'appinteg_%'")

	os.Exit(exitCode)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func newTestInstallerAndSiteManager(t *testing.T) (*apps.AppInstaller, *tenancy.SiteManager, *orm.DBManager) {
	t.Helper()
	ctx := context.Background()
	logger := observe.NewLogger(slog.LevelWarn)

	host := os.Getenv("PG_HOST")
	if host == "" {
		host = aiTestHost
	}
	db, err := orm.NewDBManager(ctx, config.DatabaseConfig{
		Host:     host,
		Port:     aiTestPort,
		User:     aiTestUser,
		Password: aiTestPassword,
		SystemDB: aiTestDB,
		PoolSize: 10,
	}, logger)
	if err != nil {
		t.Fatalf("NewDBManager: %v", err)
	}
	t.Cleanup(db.Close)

	migrator := meta.NewMigrator(db, logger)
	registry := meta.NewRegistry(db, aiRedisClient, logger)
	runner := orm.NewMigrationRunner(db, logger)

	sm := tenancy.NewSiteManager(db, migrator, registry, aiRedisClient, logger, core.BootstrapCoreMeta)
	installer := apps.NewAppInstaller(db, migrator, registry, runner, aiRedisClient, logger)

	return installer, sm, db
}

func uniqueAppIntegSiteName(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return "appinteg_" + hex.EncodeToString(b)
}

func createTestSite(t *testing.T, sm *tenancy.SiteManager, name string) {
	t.Helper()
	ctx := context.Background()

	if err := sm.CreateSite(ctx, tenancy.SiteCreateConfig{
		Name:          name,
		AdminEmail:    "admin@test.dev",
		AdminPassword: "secret123",
	}); err != nil {
		t.Fatalf("CreateSite(%s): %v", name, err)
	}

	t.Cleanup(func() {
		bgCtx := context.Background()
		_ = sm.DropSite(bgCtx, name, tenancy.SiteDropOptions{})
		// Fallback cleanup.
		schema := "tenant_" + name
		_, _ = aiAdminPool.Exec(bgCtx, fmt.Sprintf(
			"DROP SCHEMA IF EXISTS %s CASCADE",
			pgx.Identifier{schema}.Sanitize(),
		))
		_, _ = aiAdminPool.Exec(bgCtx,
			"DELETE FROM moca_system.site_apps WHERE site_name = $1", name)
		_, _ = aiAdminPool.Exec(bgCtx,
			"DELETE FROM moca_system.sites WHERE name = $1", name)
	})
}

// projectAppsDir returns the absolute path to the apps/ directory at the project root.
func projectAppsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	// This file is at pkg/apps/installer_integration_test.go
	// Project root is ../../
	projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	appsDir := filepath.Join(projectRoot, "apps")
	if _, err := os.Stat(appsDir); err != nil {
		t.Fatalf("apps directory not found at %s: %v", appsDir, err)
	}
	absPath, _ := filepath.Abs(appsDir)
	return absPath
}

func appIntegTableExists(t *testing.T, schema, table string) bool {
	t.Helper()
	var count int
	err := aiAdminPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = $1 AND table_name = $2`,
		schema, table,
	).Scan(&count)
	if err != nil {
		t.Fatalf("tableExists(%s, %s): %v", schema, table, err)
	}
	return count > 0
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestAppInstaller_CoreInstalledViaSiteCreate(t *testing.T) {
	installer, sm, _ := newTestInstallerAndSiteManager(t)
	ctx := context.Background()
	name := uniqueAppIntegSiteName(t)

	createTestSite(t, sm, name)

	// Core should already be installed via CreateSite.
	installed, err := installer.ListInstalled(ctx, name)
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(installed) != 1 {
		t.Fatalf("expected 1 installed app, got %d", len(installed))
	}
	if installed[0].AppName != "core" {
		t.Errorf("installed app: got %q, want core", installed[0].AppName)
	}
	if installed[0].AppVersion != "0.1.0" {
		t.Errorf("installed version: got %q, want 0.1.0", installed[0].AppVersion)
	}

	// Verify core tables exist.
	schema := "tenant_" + name
	for _, tbl := range []string{"tab_user", "tab_role", "tab_has_role", "tab_doc_field", "tab_doc_perm"} {
		if !appIntegTableExists(t, schema, tbl) {
			t.Errorf("core table %s missing in schema %s", tbl, schema)
		}
	}
}

func TestAppInstaller_InstallMissingDep_Error(t *testing.T) {
	installer, sm, _ := newTestInstallerAndSiteManager(t)
	ctx := context.Background()
	name := uniqueAppIntegSiteName(t)

	createTestSite(t, sm, name)

	// Create a fake app with a dependency on a nonexistent app.
	tmpDir := t.TempDir()
	fakeAppDir := filepath.Join(tmpDir, "fake_app")
	if err := os.MkdirAll(filepath.Join(fakeAppDir, "modules"), 0o755); err != nil {
		t.Fatalf("create fake app dir: %v", err)
	}

	manifest := `name: fake_app
title: Fake App
version: "0.1.0"
publisher: test
moca_version: ">=0.1.0"
modules: []
dependencies:
  - app: nonexistent_app
    min_version: ">=0.1.0"
`
	if err := os.WriteFile(filepath.Join(fakeAppDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	err := installer.Install(ctx, name, "fake_app", tmpDir)
	if err == nil {
		t.Fatal("expected error for missing dependency, got nil")
	}

	// Verify error is a DependencyError.
	var depErr *apps.DependencyError
	if !isDepError(err) {
		t.Logf("error type: %T, message: %v", err, err)
		// The error may be wrapped; check the message.
		if depErr != nil || containsStr(err.Error(), "not installed") {
			// Acceptable — the error message indicates the dependency issue.
		} else {
			t.Errorf("expected DependencyError or 'not installed' message, got: %v", err)
		}
	}
}

func TestAppInstaller_Uninstall(t *testing.T) {
	installer, sm, _ := newTestInstallerAndSiteManager(t)
	ctx := context.Background()
	name := uniqueAppIntegSiteName(t)

	createTestSite(t, sm, name)

	// Uninstall core with Force (skip reverse-dep check since there are no other apps).
	err := installer.Uninstall(ctx, name, "core", apps.UninstallOptions{Force: true})
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	// Verify ListInstalled returns empty.
	installed, err := installer.ListInstalled(ctx, name)
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(installed) != 0 {
		t.Errorf("expected 0 installed apps after uninstall, got %d", len(installed))
	}

	// Verify site_apps entry removed.
	var count int
	err = aiAdminPool.QueryRow(ctx,
		"SELECT COUNT(*) FROM moca_system.site_apps WHERE site_name = $1 AND app_name = 'core'",
		name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query site_apps: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 site_apps entries for core, got %d", count)
	}
}

func TestAppInstaller_ListInstalled(t *testing.T) {
	installer, sm, _ := newTestInstallerAndSiteManager(t)
	ctx := context.Background()
	name := uniqueAppIntegSiteName(t)

	createTestSite(t, sm, name)

	installed, err := installer.ListInstalled(ctx, name)
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(installed) != 1 {
		t.Fatalf("expected 1 installed app, got %d", len(installed))
	}

	app := installed[0]
	if app.AppName != "core" {
		t.Errorf("app name: got %q, want core", app.AppName)
	}
	if app.AppVersion != "0.1.0" {
		t.Errorf("app version: got %q, want 0.1.0", app.AppVersion)
	}
	if app.InstalledAt.IsZero() {
		t.Error("installed_at is zero")
	}
}

// ── small helpers ───────────────────────────────────────────────────────────

func isDepError(err error) bool {
	if err == nil {
		return false
	}
	var depErr *apps.DependencyError
	// Use errors.As via type assertion chain.
	for e := err; e != nil; {
		if _, ok := e.(*apps.DependencyError); ok {
			return true
		}
		_ = depErr // satisfy linter
		unwrapper, ok := e.(interface{ Unwrap() error })
		if !ok {
			break
		}
		e = unwrapper.Unwrap()
	}
	return false
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
