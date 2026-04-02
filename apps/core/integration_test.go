//go:build integration

package core

import (
	"context"
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

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/apps"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── connection defaults ──────────────────────────────────────────────────────

const (
	coreTestHost     = "localhost"
	coreTestPort     = 5433
	coreTestUser     = "moca"
	coreTestPassword = "moca_test"
	coreTestDB       = "moca_test"
	coreTestSchema   = "tenant_core_integ"
	coreTestSite     = "core_integ"
)

// ── shared infrastructure ────────────────────────────────────────────────────

var (
	coreTestPool    *pgxpool.Pool
	coreDBManager   *orm.DBManager
	coreRegistry    *meta.Registry
	coreDocManager  *document.DocManager
	coreSite        *tenancy.SiteContext
	coreUser        *auth.User
	coreControllers *document.ControllerRegistry
	coreRedisClient *redis.Client
	coreHookReg     *hooks.HookRegistry
	coreMetaTypes   []*meta.MetaType // bootstrap output
)

func TestMain(m *testing.M) {
	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			coreTestUser, coreTestPassword,
			coreTestHost, coreTestPort, coreTestDB,
		)
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create admin pool: %v\n", err)
		os.Exit(0)
	}
	defer adminPool.Close()

	if err := adminPool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to PostgreSQL: %v\n", err)
		os.Exit(0)
	}

	// Create test schema.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(
		"CREATE SCHEMA IF NOT EXISTS %s",
		pgx.Identifier{coreTestSchema}.Sanitize(),
	)); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create schema: %v\n", err)
		os.Exit(1)
	}

	// Pool with search_path bound to test schema.
	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: parse pool config: %v\n", err)
		os.Exit(1)
	}
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf(
			"SET search_path TO %s, public",
			pgx.Identifier{coreTestSchema}.Sanitize(),
		))
		return err
	}
	coreTestPool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create test pool: %v\n", err)
		os.Exit(1)
	}
	defer coreTestPool.Close()

	// moca_system schema + sites table.
	if _, err := adminPool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS moca_system;
		CREATE TABLE IF NOT EXISTS moca_system.sites (
			name        TEXT PRIMARY KEY,
			db_schema   TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'active',
			admin_email TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create moca_system: %v\n", err)
		os.Exit(1)
	}

	if _, err := adminPool.Exec(ctx, `
		INSERT INTO moca_system.sites (name, db_schema, admin_email)
		VALUES ($1, $2, 'test@test.dev')
		ON CONFLICT (name) DO UPDATE SET db_schema = EXCLUDED.db_schema
	`, coreTestSite, coreTestSchema); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: insert site row: %v\n", err)
		os.Exit(1)
	}

	// DBManager.
	logger := observe.NewLogger(slog.LevelWarn)
	host := os.Getenv("PG_HOST")
	if host == "" {
		host = coreTestHost
	}
	coreDBManager, err = orm.NewDBManager(ctx, config.DatabaseConfig{
		Host:     host,
		Port:     coreTestPort,
		User:     coreTestUser,
		Password: coreTestPassword,
		SystemDB: coreTestDB,
		PoolSize: 10,
	}, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: NewDBManager: %v\n", err)
		os.Exit(1)
	}
	defer coreDBManager.Close()

	// Redis.
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6380"
	}
	rc := redis.NewClient(&redis.Options{Addr: redisAddr, DB: 0})
	if err := rc.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: Redis unavailable at %s: %v\n", redisAddr, err)
		rc.Close()
		os.Exit(0)
	}
	coreRedisClient = rc
	defer func() {
		coreRedisClient.Close()
		coreRedisClient = nil
	}()

	// Registry + meta tables.
	coreRegistry = meta.NewRegistry(coreDBManager, coreRedisClient, logger)

	migrator := meta.NewMigrator(coreDBManager, logger)
	if err := migrator.EnsureMetaTables(ctx, coreTestSite); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: EnsureMetaTables: %v\n", err)
		os.Exit(1)
	}

	// Bootstrap core MetaTypes.
	coreMetaTypes, err = BootstrapCoreMeta()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: BootstrapCoreMeta: %v\n", err)
		os.Exit(1)
	}

	// Register all bootstrap MetaTypes (child tables first, then parents).
	// Registry.Register expects JSON bytes, so marshal each compiled MetaType.
	// Order: register child tables before parents that reference them via Table fields.
	childFirst := reorderChildrenFirst(coreMetaTypes)
	for _, mt := range childFirst {
		js, merr := json.Marshal(mt)
		if merr != nil {
			fmt.Fprintf(os.Stderr, "FATAL: marshal MetaType %q: %v\n", mt.Name, merr)
			os.Exit(1)
		}
		if _, rerr := coreRegistry.Register(ctx, coreTestSite, js); rerr != nil {
			fmt.Fprintf(os.Stderr, "FATAL: register MetaType %q: %v\n", mt.Name, rerr)
			os.Exit(1)
		}
	}

	// DocManager with controllers and hooks.
	naming := document.NewNamingEngine()
	validator := document.NewValidator()
	coreControllers = document.NewControllerRegistry()
	coreHookReg = hooks.NewHookRegistry()
	coreDocManager = document.NewDocManager(coreRegistry, coreDBManager, naming, validator, coreControllers, logger)

	dispatcher := hooks.NewDocEventDispatcher(coreHookReg)
	coreDocManager.SetHookDispatcher(dispatcher)

	// Initialize core app (registers UserController + hooks).
	Initialize(coreControllers, coreHookReg)

	// SiteContext + User.
	sitePool, err := coreDBManager.ForSite(ctx, coreTestSite)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: ForSite: %v\n", err)
		os.Exit(1)
	}
	coreSite = &tenancy.SiteContext{
		Name: coreTestSite,
		Pool: sitePool,
	}
	coreUser = &auth.User{
		Email:    "admin@moca.dev",
		FullName: "Core Admin",
		Roles:    []string{"System Manager"},
	}

	exitCode := m.Run()

	// Teardown.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(
		"DROP SCHEMA IF EXISTS %s CASCADE",
		pgx.Identifier{coreTestSchema}.Sanitize(),
	)); err != nil {
		fmt.Fprintf(os.Stderr, "teardown warning: drop schema: %v\n", err)
	}

	os.Exit(exitCode)
}

// reorderChildrenFirst returns a copy with IsChildTable MetaTypes before others.
func reorderChildrenFirst(mts []*meta.MetaType) []*meta.MetaType {
	var children, parents []*meta.MetaType
	for _, mt := range mts {
		if mt.IsChildTable {
			children = append(children, mt)
		} else {
			parents = append(parents, mt)
		}
	}
	return append(children, parents...)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func newCoreCtx(t *testing.T) *document.DocContext {
	t.Helper()
	return document.NewDocContext(context.Background(), coreSite, coreUser)
}

func columnExists(t *testing.T, table, column string) bool {
	t.Helper()
	var count int
	err := coreTestPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = $2 AND column_name = $3`,
		coreTestSchema, table, column,
	).Scan(&count)
	if err != nil {
		t.Fatalf("columnExists(%q, %q): %v", table, column, err)
	}
	return count > 0
}

func tableExists(t *testing.T, table string) bool {
	t.Helper()
	var count int
	err := coreTestPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = $1 AND table_name = $2`,
		coreTestSchema, table,
	).Scan(&count)
	if err != nil {
		t.Fatalf("tableExists(%q): %v", table, err)
	}
	return count > 0
}

// ── integration tests ────────────────────────────────────────────────────────

func TestInteg_ManifestParsesAndValidates(t *testing.T) {
	// manifest.yaml is in the same directory as this test file.
	manifestPath := filepath.Join(".", "manifest.yaml")

	m, err := apps.ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	if err := apps.ValidateManifest(m); err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}

	if m.Name != "core" {
		t.Errorf("name: got %q, want %q", m.Name, "core")
	}
	if m.Version != "0.1.0" {
		t.Errorf("version: got %q, want %q", m.Version, "0.1.0")
	}
	if len(m.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(m.Modules))
	}
	if m.Modules[0].Name != "Core" {
		t.Errorf("module name: got %q, want %q", m.Modules[0].Name, "Core")
	}
	if len(m.Modules[0].DocTypes) != 8 {
		t.Errorf("expected 8 doctypes in Core module, got %d", len(m.Modules[0].DocTypes))
	}
}

func TestInteg_AllCoreDoctypesCompile(t *testing.T) {
	if len(coreMetaTypes) != 8 {
		t.Fatalf("expected 8 MetaTypes from BootstrapCoreMeta, got %d", len(coreMetaTypes))
	}

	for _, mt := range coreMetaTypes {
		if mt.Name == "" {
			t.Error("MetaType has empty Name")
		}
		if mt.Module == "" {
			t.Errorf("MetaType %q has empty Module", mt.Name)
		}
		if len(mt.Fields) == 0 {
			t.Errorf("MetaType %q has no fields", mt.Name)
		}
		t.Logf("compiled: %s (%d fields)", mt.Name, len(mt.Fields))
	}
}

func TestInteg_UserDDLCorrectness(t *testing.T) {
	if !tableExists(t, "tab_user") {
		t.Fatal("tab_user table does not exist")
	}

	expectedColumns := []string{"name", "email", "full_name", "password", "enabled",
		"language", "time_zone", "user_type", "creation", "modified", "owner"}
	for _, col := range expectedColumns {
		if !columnExists(t, "tab_user", col) {
			t.Errorf("missing column: tab_user.%s", col)
		}
	}
}

func TestInteg_RoleDDLCorrectness(t *testing.T) {
	if !tableExists(t, "tab_role") {
		t.Fatal("tab_role table does not exist")
	}

	for _, col := range []string{"name", "role_name", "disabled"} {
		if !columnExists(t, "tab_role", col) {
			t.Errorf("missing column: tab_role.%s", col)
		}
	}
}

func TestInteg_SystemSettingsSingleNoDedicatedTable(t *testing.T) {
	// IsSingle doctypes use tab_singles (key-value), NOT a dedicated table.
	// GenerateTableDDL returns nil for IsSingle, so no tab_system_settings table.
	if tableExists(t, "tab_system_settings") {
		t.Error("tab_system_settings should NOT exist — SystemSettings is a Single doctype using tab_singles")
	}

	// Verify tab_singles exists (created by EnsureMetaTables).
	if !tableExists(t, "tab_singles") {
		t.Fatal("tab_singles table does not exist")
	}
}

func TestInteg_ChildTablesDDL(t *testing.T) {
	childTables := map[string][]string{
		"tab_has_role":  {"name", "parent", "parenttype", "parentfield", "role"},
		"tab_doc_field": {"name", "parent", "parenttype", "parentfield", "field_name", "field_type"},
		"tab_doc_perm":  {"name", "parent", "parenttype", "parentfield", "role", "doctype_perm"},
	}

	for table, cols := range childTables {
		if !tableExists(t, table) {
			t.Errorf("child table %s does not exist", table)
			continue
		}
		for _, col := range cols {
			if !columnExists(t, table, col) {
				t.Errorf("missing column: %s.%s", table, col)
			}
		}
	}
}

func TestInteg_UserControllerBcryptOnInsert(t *testing.T) {
	ctx := newCoreCtx(t)

	doc, err := coreDocManager.Insert(ctx, "User", map[string]any{
		"email":     "bcrypt-test@moca.dev",
		"full_name": "Bcrypt Tester",
		"password":  "plaintext123",
		"enabled":   1,
		"user_type": "System",
	})
	if err != nil {
		t.Fatalf("Insert User: %v", err)
	}

	// Read back from DB.
	fetched, err := coreDocManager.Get(ctx, "User", doc.Name())
	if err != nil {
		t.Fatalf("Get User: %v", err)
	}

	password, ok := fetched.Get("password").(string)
	if !ok || password == "" {
		t.Fatal("password is empty or not a string")
	}
	if !strings.HasPrefix(password, "$2a$") && !strings.HasPrefix(password, "$2b$") {
		t.Errorf("expected bcrypt hash, got: %s", password)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(password), []byte("plaintext123")); err != nil {
		t.Errorf("bcrypt verification failed: %v", err)
	}
}

func TestInteg_AppDirectoryScanDiscoversCore(t *testing.T) {
	// From apps/core/, the apps directory is "..".
	appsDir := filepath.Join(".", "..")
	absAppsDir, err := filepath.Abs(appsDir)
	if err != nil {
		t.Fatalf("resolve apps dir: %v", err)
	}

	discovered, err := apps.ScanApps(absAppsDir)
	if err != nil {
		t.Fatalf("ScanApps: %v", err)
	}

	found := false
	for _, app := range discovered {
		if app.Name == "core" {
			found = true
			if app.Manifest.Version != "0.1.0" {
				t.Errorf("core app version: got %q, want %q", app.Manifest.Version, "0.1.0")
			}
			break
		}
	}
	if !found {
		t.Errorf("core app not found in scan results: %v", discovered)
	}
}

func TestInteg_FullLifecycleSmoke(t *testing.T) {
	ctx := newCoreCtx(t)

	// 1. Insert a User with a plain password.
	doc, err := coreDocManager.Insert(ctx, "User", map[string]any{
		"email":     "smoke-test@moca.dev",
		"full_name": "Smoke Tester",
		"password":  "s3cret!",
		"enabled":   1,
		"user_type": "System",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	name := doc.Name()
	t.Logf("created User: %s", name)

	// 2. Verify password was hashed (controller fired during Insert).
	password, _ := doc.Get("password").(string)
	if !strings.HasPrefix(password, "$2a$") && !strings.HasPrefix(password, "$2b$") {
		t.Fatalf("password not hashed after Insert: %s", password)
	}

	// 3. Read back from DB.
	fetched, err := coreDocManager.Get(ctx, "User", name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// 4. Verify fields round-trip.
	if got := fetched.Get("email"); got != "smoke-test@moca.dev" {
		t.Errorf("email: got %v, want %q", got, "smoke-test@moca.dev")
	}
	if got := fetched.Get("full_name"); got != "Smoke Tester" {
		t.Errorf("full_name: got %v, want %q", got, "Smoke Tester")
	}

	// 5. Verify hashed password in DB matches original.
	dbPassword, _ := fetched.Get("password").(string)
	if err := bcrypt.CompareHashAndPassword([]byte(dbPassword), []byte("s3cret!")); err != nil {
		t.Errorf("bcrypt verification on fetched doc failed: %v", err)
	}
}
