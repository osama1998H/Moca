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

	sm := tenancy.NewSiteManager(db, migrator, registry, tmRedisClient, tmRedisClient, logger, core.BootstrapCoreMeta)
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

func TestSiteManager_EnableDisableSite(t *testing.T) {
	sm, _ := newTestSiteManager(t)
	ctx := context.Background()
	name := uniqueSiteName(t)
	cleanupTestSite(t, sm, name)

	// Create site.
	if err := sm.CreateSite(ctx, tenancy.SiteCreateConfig{
		Name:          name,
		AdminEmail:    "admin@test.dev",
		AdminPassword: "secret123",
	}); err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	// Verify site starts as active.
	info, err := sm.GetSiteInfo(ctx, name)
	if err != nil {
		t.Fatalf("GetSiteInfo: %v", err)
	}
	if info.Status != "active" {
		t.Fatalf("expected active status, got %q", info.Status)
	}

	// Disable the site.
	if err := sm.DisableSite(ctx, name, "Scheduled maintenance", []string{"127.0.0.1"}); err != nil {
		t.Fatalf("DisableSite: %v", err)
	}

	// Verify disabled status.
	info, err = sm.GetSiteInfo(ctx, name)
	if err != nil {
		t.Fatalf("GetSiteInfo after disable: %v", err)
	}
	if info.Status != "disabled" {
		t.Errorf("expected disabled status, got %q", info.Status)
	}
	if msg, ok := info.Config["maintenance_message"].(string); !ok || msg != "Scheduled maintenance" {
		t.Errorf("expected maintenance_message='Scheduled maintenance', got %v", info.Config["maintenance_message"])
	}

	// Verify maintenance Redis key.
	maintenanceKey := fmt.Sprintf("maintenance:%s", name)
	val, redisErr := tmRedisClient.Get(ctx, maintenanceKey).Result()
	if redisErr != nil {
		t.Errorf("expected maintenance redis key, got err: %v", redisErr)
	} else if !strings.Contains(val, "Scheduled maintenance") {
		t.Errorf("maintenance redis key doesn't contain message: %s", val)
	}

	// Re-enable the site.
	if err := sm.EnableSite(ctx, name); err != nil {
		t.Fatalf("EnableSite: %v", err)
	}

	// Verify active status and maintenance metadata removed.
	info, err = sm.GetSiteInfo(ctx, name)
	if err != nil {
		t.Fatalf("GetSiteInfo after enable: %v", err)
	}
	if info.Status != "active" {
		t.Errorf("expected active status after enable, got %q", info.Status)
	}
	if _, hasMsg := info.Config["maintenance_message"]; hasMsg {
		t.Error("maintenance_message should be removed after enable")
	}
	if _, hasIPs := info.Config["maintenance_allow_ips"]; hasIPs {
		t.Error("maintenance_allow_ips should be removed after enable")
	}

	// Verify maintenance Redis key removed.
	_, redisErr = tmRedisClient.Get(ctx, maintenanceKey).Result()
	if redisErr != redis.Nil {
		t.Errorf("expected maintenance redis key to be deleted, got err: %v", redisErr)
	}
}

func TestSiteManager_EnableSite_NotFound(t *testing.T) {
	sm, _ := newTestSiteManager(t)
	ctx := context.Background()

	err := sm.EnableSite(ctx, "nonexistent_site_xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent site")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestSiteManager_DisableSite_NotFound(t *testing.T) {
	sm, _ := newTestSiteManager(t)
	ctx := context.Background()

	err := sm.DisableSite(ctx, "nonexistent_site_xyz", "test", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent site")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestSiteManager_RenameSite(t *testing.T) {
	sm, _ := newTestSiteManager(t)
	ctx := context.Background()
	oldName := uniqueSiteName(t)
	newName := uniqueSiteName(t)
	cleanupTestSite(t, sm, oldName)
	cleanupTestSite(t, sm, newName)

	tmpDir := t.TempDir()

	// Create site.
	if err := sm.CreateSite(ctx, tenancy.SiteCreateConfig{
		Name:          oldName,
		AdminEmail:    "admin@test.dev",
		AdminPassword: "secret123",
		Config:        map[string]any{"timezone": "UTC"},
	}); err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	// Create sites directory for filesystem rename test.
	oldDir := filepath.Join(tmpDir, "sites", oldName)
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Set active site to old name.
	if err := sm.SetActiveSite(tmpDir, oldName); err != nil {
		t.Fatalf("SetActiveSite: %v", err)
	}

	// Rename.
	if err := sm.RenameSite(ctx, oldName, newName, tmpDir); err != nil {
		t.Fatalf("RenameSite: %v", err)
	}

	// Old schema should be gone.
	oldSchema := "tenant_" + oldName
	if schemaExists(t, oldSchema) {
		t.Error("old schema still exists after rename")
	}

	// New schema should exist.
	newSchema := "tenant_" + newName
	if !schemaExists(t, newSchema) {
		t.Error("new schema does not exist after rename")
	}

	// Old site row should be gone, new one should exist.
	if siteRowExists(t, oldName) {
		t.Error("old site row still exists after rename")
	}
	if !siteRowExists(t, newName) {
		t.Error("new site row does not exist after rename")
	}

	// Redis config should use new name.
	val, redisErr := tmRedisClient.Get(ctx, fmt.Sprintf("config:%s", newName)).Result()
	if redisErr != nil {
		t.Errorf("expected config key for new name, got err: %v", redisErr)
	} else if !strings.Contains(val, "timezone") {
		t.Errorf("config key doesn't contain expected data: %s", val)
	}

	// Old Redis config should be gone.
	_, redisErr = tmRedisClient.Get(ctx, fmt.Sprintf("config:%s", oldName)).Result()
	if redisErr != redis.Nil {
		t.Errorf("expected old config key to be deleted, got: %v", redisErr)
	}

	// Filesystem: old dir gone, new dir exists.
	if _, statErr := os.Stat(oldDir); !os.IsNotExist(statErr) {
		t.Error("old site directory still exists after rename")
	}
	newDir := filepath.Join(tmpDir, "sites", newName)
	if _, statErr := os.Stat(newDir); os.IsNotExist(statErr) {
		t.Error("new site directory does not exist after rename")
	}

	// .moca/current_site should be updated.
	currentSite, readErr := os.ReadFile(filepath.Join(tmpDir, ".moca", "current_site"))
	if readErr != nil {
		t.Fatalf("read current_site: %v", readErr)
	}
	if string(currentSite) != newName {
		t.Errorf("current_site: got %q, want %q", string(currentSite), newName)
	}
}

func TestSiteManager_RenameSite_TargetExists(t *testing.T) {
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

	err := sm.RenameSite(ctx, name1, name2, "")
	if err == nil {
		t.Fatal("expected error when renaming to existing site")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestSiteManager_CloneSite(t *testing.T) {
	sm, db := newTestSiteManager(t)
	ctx := context.Background()
	source := uniqueSiteName(t)
	target := uniqueSiteName(t)
	cleanupTestSite(t, sm, source)
	cleanupTestSite(t, sm, target)

	// Create source site.
	if err := sm.CreateSite(ctx, tenancy.SiteCreateConfig{
		Name:          source,
		AdminEmail:    "admin@test.dev",
		AdminPassword: "secret123",
		Config:        map[string]any{"timezone": "US/Eastern"},
	}); err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	// Clone without anonymization.
	if err := sm.CloneSite(ctx, source, target, tenancy.CloneOptions{}); err != nil {
		t.Fatalf("CloneSite: %v", err)
	}

	// Verify target schema exists.
	targetSchema := "tenant_" + target
	if !schemaExists(t, targetSchema) {
		t.Fatal("target schema does not exist after clone")
	}

	// Verify target registered in system.
	if !siteRowExists(t, target) {
		t.Fatal("target site row does not exist after clone")
	}

	// Verify target has core tables.
	coreTables := []string{"tab_user", "tab_role", "tab_has_role"}
	for _, tbl := range coreTables {
		if !tableExistsInTenantSchema(t, targetSchema, tbl) {
			t.Errorf("target missing table %s", tbl)
		}
	}

	// Verify data was cloned (admin user exists in target).
	targetPool, err := db.ForSite(ctx, target)
	if err != nil {
		t.Fatalf("ForSite target: %v", err)
	}
	var email string
	err = targetPool.QueryRow(ctx, "SELECT email FROM tab_user WHERE email = 'admin@test.dev'").Scan(&email)
	if err != nil {
		t.Fatalf("admin user not found in clone: %v", err)
	}

	// Verify source is unchanged.
	sourceSchema := "tenant_" + source
	if !schemaExists(t, sourceSchema) {
		t.Fatal("source schema should still exist after clone")
	}

	// Verify Redis config for target.
	val, redisErr := tmRedisClient.Get(ctx, fmt.Sprintf("config:%s", target)).Result()
	if redisErr != nil {
		t.Errorf("expected config key for target, got err: %v", redisErr)
	} else if !strings.Contains(val, "US/Eastern") {
		t.Errorf("target config should match source, got: %s", val)
	}
}

func TestSiteManager_CloneSite_WithAnonymize(t *testing.T) {
	sm, db := newTestSiteManager(t)
	ctx := context.Background()
	source := uniqueSiteName(t)
	target := uniqueSiteName(t)
	cleanupTestSite(t, sm, source)
	cleanupTestSite(t, sm, target)

	// Create source site.
	if err := sm.CreateSite(ctx, tenancy.SiteCreateConfig{
		Name:          source,
		AdminEmail:    "admin@test.dev",
		AdminPassword: "secret123",
	}); err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	// Clone with anonymization.
	if err := sm.CloneSite(ctx, source, target, tenancy.CloneOptions{Anonymize: true}); err != nil {
		t.Fatalf("CloneSite with anonymize: %v", err)
	}

	// Admin user should be preserved (not anonymized).
	targetPool, err := db.ForSite(ctx, target)
	if err != nil {
		t.Fatalf("ForSite target: %v", err)
	}
	var email string
	err = targetPool.QueryRow(ctx, "SELECT email FROM tab_user WHERE email = 'admin@test.dev'").Scan(&email)
	if err != nil {
		t.Fatalf("admin user should be preserved in anonymized clone: %v", err)
	}
}

func TestSiteManager_CloneSite_TargetExists(t *testing.T) {
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

	err := sm.CloneSite(ctx, name1, name2, tenancy.CloneOptions{})
	if err == nil {
		t.Fatal("expected error when cloning to existing site")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestSiteManager_ReinstallSite(t *testing.T) {
	sm, db := newTestSiteManager(t)
	ctx := context.Background()
	name := uniqueSiteName(t)
	cleanupTestSite(t, sm, name)

	// Create site.
	if err := sm.CreateSite(ctx, tenancy.SiteCreateConfig{
		Name:          name,
		AdminEmail:    "admin@test.dev",
		AdminPassword: "secret123",
		Plan:          "business",
		Config:        map[string]any{"timezone": "US/Eastern"},
	}); err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	// Reinstall.
	prevApps, err := sm.ReinstallSite(ctx, name, "newpassword")
	if err != nil {
		t.Fatalf("ReinstallSite: %v", err)
	}

	// No non-core apps should be in previous list.
	if len(prevApps) != 0 {
		t.Errorf("expected 0 previous apps, got %v", prevApps)
	}

	// Verify site still exists with preserved config.
	info, err := sm.GetSiteInfo(ctx, name)
	if err != nil {
		t.Fatalf("GetSiteInfo after reinstall: %v", err)
	}
	if info.Status != "active" {
		t.Errorf("expected active status after reinstall, got %q", info.Status)
	}
	if info.Plan != "business" {
		t.Errorf("expected plan 'business' preserved, got %q", info.Plan)
	}
	if info.AdminEmail != "admin@test.dev" {
		t.Errorf("expected admin email preserved, got %q", info.AdminEmail)
	}

	// Verify new password works.
	pool, err := db.ForSite(ctx, name)
	if err != nil {
		t.Fatalf("ForSite: %v", err)
	}
	var password string
	err = pool.QueryRow(ctx, "SELECT password FROM tab_user WHERE email = 'admin@test.dev'").Scan(&password)
	if err != nil {
		t.Fatalf("query admin password: %v", err)
	}
	if bcryptErr := bcrypt.CompareHashAndPassword([]byte(password), []byte("newpassword")); bcryptErr != nil {
		t.Errorf("new password doesn't match: %v", bcryptErr)
	}
}
