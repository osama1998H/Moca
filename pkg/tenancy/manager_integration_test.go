//go:build integration

package tenancy_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/moca-framework/moca/apps/core"
	"github.com/moca-framework/moca/internal/config"
	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/observe"
	"github.com/moca-framework/moca/pkg/orm"
	"github.com/moca-framework/moca/pkg/tenancy"
)

// ── connection defaults ─────────────────────────────────────────────────────

const (
	tmTestHost     = "localhost"
	tmTestPort     = 5433
	tmTestUser     = "moca"
	tmTestPassword = "moca_test"
	tmTestDB       = "moca_test"
)

// ── shared infrastructure ───────────────────────────────────────────────────

var (
	tmAdminPool   *pgxpool.Pool
	tmRedisClient *redis.Client
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		host := os.Getenv("PG_HOST")
		if host == "" {
			host = tmTestHost
		}
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			tmTestUser, tmTestPassword, host, tmTestPort, tmTestDB,
		)
	}

	var err error
	tmAdminPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create admin pool: %v\n", err)
		os.Exit(0)
	}
	defer tmAdminPool.Close()

	if err := tmAdminPool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to PostgreSQL: %v\n", err)
		os.Exit(0)
	}

	// Create full system schema (sites, apps, site_apps with proper columns).
	if err := orm.EnsureSystemSchema(ctx, tmAdminPool, "moca_system"); err != nil {
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
	tmRedisClient = rc
	defer tmRedisClient.Close()

	exitCode := m.Run()

	// Safety-net teardown: drop any leftover test schemas.
	rows, _ := tmAdminPool.Query(ctx,
		"SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant_integ_%'")
	if rows != nil {
		for rows.Next() {
			var schema string
			if err := rows.Scan(&schema); err == nil {
				_, _ = tmAdminPool.Exec(ctx, fmt.Sprintf(
					"DROP SCHEMA IF EXISTS %s CASCADE",
					pgx.Identifier{schema}.Sanitize(),
				))
			}
		}
		rows.Close()
	}
	// Clean up system table entries from tests.
	_, _ = tmAdminPool.Exec(ctx,
		"DELETE FROM moca_system.site_apps WHERE site_name LIKE 'integ_%'")
	_, _ = tmAdminPool.Exec(ctx,
		"DELETE FROM moca_system.sites WHERE name LIKE 'integ_%'")

	os.Exit(exitCode)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func newTestSiteManager(t *testing.T) (*tenancy.SiteManager, *orm.DBManager) {
	t.Helper()
	ctx := context.Background()
	logger := observe.NewLogger(slog.LevelWarn)

	host := os.Getenv("PG_HOST")
	if host == "" {
		host = tmTestHost
	}
	db, err := orm.NewDBManager(ctx, config.DatabaseConfig{
		Host:     host,
		Port:     tmTestPort,
		User:     tmTestUser,
		Password: tmTestPassword,
		SystemDB: tmTestDB,
		PoolSize: 10,
	}, logger)
	if err != nil {
		t.Fatalf("NewDBManager: %v", err)
	}
	t.Cleanup(db.Close)

	migrator := meta.NewMigrator(db, logger)
	registry := meta.NewRegistry(db, tmRedisClient, logger)

	sm := tenancy.NewSiteManager(db, migrator, registry, tmRedisClient, logger, core.BootstrapCoreMeta)
	return sm, db
}

func uniqueSiteName(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return "integ_" + hex.EncodeToString(b)
}

func cleanupTestSite(t *testing.T, sm *tenancy.SiteManager, name string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		// Best-effort drop via SiteManager.
		_ = sm.DropSite(ctx, name, tenancy.SiteDropOptions{})
		// Fallback manual cleanup.
		schema := "tenant_" + name
		_, _ = tmAdminPool.Exec(ctx, fmt.Sprintf(
			"DROP SCHEMA IF EXISTS %s CASCADE",
			pgx.Identifier{schema}.Sanitize(),
		))
		_, _ = tmAdminPool.Exec(ctx,
			"DELETE FROM moca_system.site_apps WHERE site_name = $1", name)
		_, _ = tmAdminPool.Exec(ctx,
			"DELETE FROM moca_system.sites WHERE name = $1", name)
		// Redis cleanup.
		tmRedisClient.Del(ctx, fmt.Sprintf("config:%s", name))
	})
}

func schemaExists(t *testing.T, schemaName string) bool {
	t.Helper()
	var count int
	err := tmAdminPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = $1`,
		schemaName,
	).Scan(&count)
	if err != nil {
		t.Fatalf("schemaExists(%s): %v", schemaName, err)
	}
	return count > 0
}

func siteRowExists(t *testing.T, siteName string) bool {
	t.Helper()
	var exists bool
	err := tmAdminPool.QueryRow(
		context.Background(),
		"SELECT EXISTS(SELECT 1 FROM moca_system.sites WHERE name = $1)",
		siteName,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("siteRowExists(%s): %v", siteName, err)
	}
	return exists
}

func tableExistsInTenantSchema(t *testing.T, schema, table string) bool {
	t.Helper()
	var count int
	err := tmAdminPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = $1 AND table_name = $2`,
		schema, table,
	).Scan(&count)
	if err != nil {
		t.Fatalf("tableExistsInTenantSchema(%s, %s): %v", schema, table, err)
	}
	return count > 0
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestSiteManager_CreateSite_FullLifecycle(t *testing.T) {
	sm, db := newTestSiteManager(t)
	ctx := context.Background()
	name := uniqueSiteName(t)
	cleanupTestSite(t, sm, name)

	cfg := tenancy.SiteCreateConfig{
		Name:          name,
		AdminEmail:    "admin@test.dev",
		AdminPassword: "secret123",
		Plan:          "free",
		Config:        map[string]any{"timezone": "US/Eastern"},
	}

	if err := sm.CreateSite(ctx, cfg); err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	schema := "tenant_" + name

	// Step 1: Schema exists.
	if !schemaExists(t, schema) {
		t.Fatal("schema does not exist after CreateSite")
	}

	// Step 2: System tables exist in tenant schema.
	systemTables := []string{"tab_doctype", "tab_singles", "tab_version", "tab_audit_log", "tab_migration_log"}
	for _, tbl := range systemTables {
		if !tableExistsInTenantSchema(t, schema, tbl) {
			t.Errorf("system table %s missing in schema %s", tbl, schema)
		}
	}

	// Step 3: Core MetaType tables exist.
	coreTables := []string{"tab_user", "tab_role", "tab_has_role", "tab_doc_field", "tab_doc_perm"}
	for _, tbl := range coreTables {
		if !tableExistsInTenantSchema(t, schema, tbl) {
			t.Errorf("core table %s missing in schema %s", tbl, schema)
		}
	}

	// Step 4: Admin user exists with bcrypt password.
	pool, err := db.ForSite(ctx, name)
	if err != nil {
		t.Fatalf("ForSite: %v", err)
	}

	var email string
	var password string
	var enabled bool
	err = pool.QueryRow(ctx,
		"SELECT email, password, enabled FROM tab_user WHERE email = $1",
		"admin@test.dev",
	).Scan(&email, &password, &enabled)
	if err != nil {
		t.Fatalf("query admin user: %v", err)
	}
	if !enabled {
		t.Error("admin user not enabled")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(password), []byte("secret123")); err != nil {
		t.Errorf("admin password bcrypt mismatch: %v", err)
	}

	// Step 8: Site registered in moca_system.
	var status, plan, adminEmail string
	var configJSON []byte
	err = tmAdminPool.QueryRow(ctx,
		"SELECT status, plan, admin_email, config FROM moca_system.sites WHERE name = $1",
		name,
	).Scan(&status, &plan, &adminEmail, &configJSON)
	if err != nil {
		t.Fatalf("query moca_system.sites: %v", err)
	}
	if status != "active" {
		t.Errorf("site status: got %q, want active", status)
	}
	if plan != "free" {
		t.Errorf("site plan: got %q, want free", plan)
	}
	if adminEmail != "admin@test.dev" {
		t.Errorf("admin_email: got %q, want admin@test.dev", adminEmail)
	}

	var cfgMap map[string]any
	if err := json.Unmarshal(configJSON, &cfgMap); err == nil {
		if tz, ok := cfgMap["timezone"].(string); !ok || tz != "US/Eastern" {
			t.Errorf("config timezone: got %v, want US/Eastern", cfgMap["timezone"])
		}
	}

	// Core app linked via site_apps.
	var appName string
	err = tmAdminPool.QueryRow(ctx,
		"SELECT app_name FROM moca_system.site_apps WHERE site_name = $1",
		name,
	).Scan(&appName)
	if err != nil {
		t.Fatalf("query site_apps: %v", err)
	}
	if appName != "core" {
		t.Errorf("site_apps app_name: got %q, want core", appName)
	}

	// Step 5: Redis config key exists.
	configVal, err := tmRedisClient.Get(ctx, fmt.Sprintf("config:%s", name)).Result()
	if err != nil {
		t.Fatalf("redis get config: %v", err)
	}
	if !strings.Contains(configVal, "timezone") {
		t.Errorf("redis config missing timezone, got: %s", configVal)
	}
}

func TestSiteManager_DropSite(t *testing.T) {
	sm, _ := newTestSiteManager(t)
	ctx := context.Background()
	name := uniqueSiteName(t)

	cfg := tenancy.SiteCreateConfig{
		Name:          name,
		AdminEmail:    "admin@test.dev",
		AdminPassword: "secret123",
	}

	if err := sm.CreateSite(ctx, cfg); err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	schema := "tenant_" + name

	// Verify site exists before drop.
	if !schemaExists(t, schema) {
		t.Fatal("schema does not exist after CreateSite")
	}

	// Drop the site.
	if err := sm.DropSite(ctx, name, tenancy.SiteDropOptions{}); err != nil {
		t.Fatalf("DropSite: %v", err)
	}

	// Schema gone.
	if schemaExists(t, schema) {
		t.Error("schema still exists after DropSite")
	}

	// System row gone.
	if siteRowExists(t, name) {
		t.Error("site row still exists after DropSite")
	}

	// Redis config key gone.
	_, err := tmRedisClient.Get(ctx, fmt.Sprintf("config:%s", name)).Result()
	if err != redis.Nil {
		t.Errorf("expected redis.Nil for deleted config key, got: %v", err)
	}
}

func TestSiteManager_ListSites(t *testing.T) {
	sm, _ := newTestSiteManager(t)
	ctx := context.Background()

	name1 := uniqueSiteName(t)
	name2 := uniqueSiteName(t)
	cleanupTestSite(t, sm, name1)
	cleanupTestSite(t, sm, name2)

	for _, name := range []string{name1, name2} {
		if err := sm.CreateSite(ctx, tenancy.SiteCreateConfig{
			Name:          name,
			AdminEmail:    "admin@test.dev",
			AdminPassword: "secret123",
		}); err != nil {
			t.Fatalf("CreateSite(%s): %v", name, err)
		}
	}

	sites, err := sm.ListSites(ctx)
	if err != nil {
		t.Fatalf("ListSites: %v", err)
	}

	// Find our test sites in the results.
	found := make(map[string]tenancy.SiteInfo)
	for _, si := range sites {
		if si.Name == name1 || si.Name == name2 {
			found[si.Name] = si
		}
	}

	if len(found) != 2 {
		t.Fatalf("expected 2 test sites in list, found %d (total sites: %d)",
			len(found), len(sites))
	}

	for name, si := range found {
		if si.Status != "active" {
			t.Errorf("site %s: status=%q, want active", name, si.Status)
		}
		if si.AdminEmail != "admin@test.dev" {
			t.Errorf("site %s: admin_email=%q, want admin@test.dev", name, si.AdminEmail)
		}
		// Core app should be in the apps list.
		hasCore := false
		for _, app := range si.Apps {
			if app == "core" {
				hasCore = true
			}
		}
		if !hasCore {
			t.Errorf("site %s: apps=%v, missing 'core'", name, si.Apps)
		}
	}
}

func TestSiteManager_DuplicateName(t *testing.T) {
	sm, _ := newTestSiteManager(t)
	ctx := context.Background()
	name := uniqueSiteName(t)
	cleanupTestSite(t, sm, name)

	cfg := tenancy.SiteCreateConfig{
		Name:          name,
		AdminEmail:    "admin@test.dev",
		AdminPassword: "secret123",
	}

	if err := sm.CreateSite(ctx, cfg); err != nil {
		t.Fatalf("first CreateSite: %v", err)
	}

	// Second create with same name should fail.
	err := sm.CreateSite(ctx, cfg)
	if err == nil {
		t.Fatal("expected error for duplicate site name, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestSiteManager_SetActiveSite(t *testing.T) {
	sm, _ := newTestSiteManager(t)
	tmpDir := t.TempDir()

	if err := sm.SetActiveSite(tmpDir, "my_test_site"); err != nil {
		t.Fatalf("SetActiveSite: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, ".moca", "current_site"))
	if err != nil {
		t.Fatalf("read current_site: %v", err)
	}

	if string(data) != "my_test_site" {
		t.Errorf("current_site: got %q, want %q", string(data), "my_test_site")
	}
}
