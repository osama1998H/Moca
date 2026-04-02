//go:build integration

package hooks_test

import (
	"context"
	"errors"
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
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── connection defaults ──────────────────────────────────────────────────────

const (
	hooksTestHost     = "localhost"
	hooksTestPort     = 5433
	hooksTestUser     = "moca"
	hooksTestPassword = "moca_test"
	hooksTestDB       = "moca_test"
	hooksTestSchema   = "tenant_hooks_integ"
	hooksTestSite     = "hooks_integ"
)

// ── shared infrastructure ────────────────────────────────────────────────────

var (
	hooksTestPool    *pgxpool.Pool
	hooksDBManager   *orm.DBManager
	hooksRegistry    *meta.Registry
	hooksDocManager  *document.DocManager
	hooksSite        *tenancy.SiteContext
	hooksUser        *auth.User
	hooksControllers *document.ControllerRegistry
	hooksRedisClient *redis.Client
	hookRegistry     *hooks.HookRegistry
)

const hookTestDocJSON = `{
	"name": "HookTestDoc",
	"module": "test",
	"naming_rule": {"rule": "uuid"},
	"fields": [
		{"name": "title", "field_type": "Data", "label": "Title"},
		{"name": "marker", "field_type": "Data", "label": "Marker"}
	]
}`

func TestMain(m *testing.M) {
	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			hooksTestUser, hooksTestPassword,
			hooksTestHost, hooksTestPort, hooksTestDB,
		)
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create admin pool: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Start PostgreSQL: docker compose up -d\n")
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
		pgx.Identifier{hooksTestSchema}.Sanitize(),
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
			pgx.Identifier{hooksTestSchema}.Sanitize(),
		))
		return err
	}
	hooksTestPool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create test pool: %v\n", err)
		os.Exit(1)
	}
	defer hooksTestPool.Close()

	// moca_system schema + sites table.
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

	if _, err := adminPool.Exec(ctx, `
		INSERT INTO moca_system.sites (name, db_schema)
		VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET db_schema = EXCLUDED.db_schema
	`, hooksTestSite, hooksTestSchema); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: insert site row: %v\n", err)
		os.Exit(1)
	}

	// DBManager.
	logger := observe.NewLogger(slog.LevelWarn)
	host := os.Getenv("PG_HOST")
	if host == "" {
		host = hooksTestHost
	}
	hooksDBManager, err = orm.NewDBManager(ctx, config.DatabaseConfig{
		Host:     host,
		Port:     hooksTestPort,
		User:     hooksTestUser,
		Password: hooksTestPassword,
		SystemDB: hooksTestDB,
		PoolSize: 10,
	}, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: NewDBManager: %v\n", err)
		os.Exit(1)
	}
	defer hooksDBManager.Close()

	// Redis (optional for hooks tests, but needed for Registry).
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
	hooksRedisClient = rc
	defer func() {
		hooksRedisClient.Close()
		hooksRedisClient = nil
	}()

	// Registry, meta tables, fixture MetaType.
	hooksRegistry = meta.NewRegistry(hooksDBManager, hooksRedisClient, logger)

	migrator := meta.NewMigrator(hooksDBManager, logger)
	if err := migrator.EnsureMetaTables(ctx, hooksTestSite); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: EnsureMetaTables: %v\n", err)
		os.Exit(1)
	}

	if _, err := hooksRegistry.Register(ctx, hooksTestSite, []byte(hookTestDocJSON)); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: register HookTestDoc: %v\n", err)
		os.Exit(1)
	}

	// DocManager with HookRegistry wired in.
	naming := document.NewNamingEngine()
	validator := document.NewValidator()
	hooksControllers = document.NewControllerRegistry()
	hooksDocManager = document.NewDocManager(hooksRegistry, hooksDBManager, naming, validator, hooksControllers, logger)

	hookRegistry = hooks.NewHookRegistry()
	dispatcher := hooks.NewDocEventDispatcher(hookRegistry)
	hooksDocManager.SetHookDispatcher(dispatcher)

	// SiteContext + User.
	sitePool, err := hooksDBManager.ForSite(ctx, hooksTestSite)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: ForSite: %v\n", err)
		os.Exit(1)
	}
	hooksSite = &tenancy.SiteContext{
		Name: hooksTestSite,
		Pool: sitePool,
	}
	hooksUser = &auth.User{
		Email:    "hooks-test@moca.dev",
		FullName: "Hooks Tester",
		Roles:    []string{"System Manager"},
	}

	exitCode := m.Run()

	// Teardown.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(
		"DROP SCHEMA IF EXISTS %s CASCADE",
		pgx.Identifier{hooksTestSchema}.Sanitize(),
	)); err != nil {
		fmt.Fprintf(os.Stderr, "teardown warning: drop schema: %v\n", err)
	}

	os.Exit(exitCode)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func newHooksCtx(t *testing.T) *document.DocContext {
	t.Helper()
	return document.NewDocContext(context.Background(), hooksSite, hooksUser)
}

// orderRecorder records app names in order of hook execution.
type orderRecorder struct {
	mu    sync.Mutex
	order []string
}

func (r *orderRecorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, name)
}

func (r *orderRecorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]string, len(r.order))
	copy(cp, r.order)
	return cp
}

// freshHookRegistry creates a new HookRegistry and wires it into the DocManager.
// This ensures test isolation — each test gets its own empty registry.
func freshHookRegistry(t *testing.T) *hooks.HookRegistry {
	t.Helper()
	hr := hooks.NewHookRegistry()
	dispatcher := hooks.NewDocEventDispatcher(hr)
	hooksDocManager.SetHookDispatcher(dispatcher)
	t.Cleanup(func() {
		// Restore the global registry after the test.
		d := hooks.NewDocEventDispatcher(hookRegistry)
		hooksDocManager.SetHookDispatcher(d)
	})
	return hr
}

// ── integration tests ────────────────────────────────────────────────────────

func TestInteg_HookPriorityOrdering(t *testing.T) {
	hr := freshHookRegistry(t)
	rec := &orderRecorder{}

	hr.Register("HookTestDoc", document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler:  func(_ *document.DocContext, _ document.Document) error { rec.record("p200"); return nil },
		AppName:  "app_high",
		Priority: 200,
	})
	hr.Register("HookTestDoc", document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler:  func(_ *document.DocContext, _ document.Document) error { rec.record("p100"); return nil },
		AppName:  "app_low",
		Priority: 100,
	})

	ctx := newHooksCtx(t)
	doc, err := hooksDocManager.Insert(ctx, "HookTestDoc", map[string]any{"title": "priority test"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	t.Logf("inserted doc: %s", doc.Name())

	order := rec.get()
	if len(order) != 2 {
		t.Fatalf("expected 2 hook calls, got %d: %v", len(order), order)
	}
	if order[0] != "p100" || order[1] != "p200" {
		t.Errorf("expected [p100, p200], got %v", order)
	}
}

func TestInteg_HookDependencyOrdering(t *testing.T) {
	hr := freshHookRegistry(t)
	rec := &orderRecorder{}

	// app_b depends on app_a — app_a must fire first even though app_b has lower priority number.
	hr.Register("HookTestDoc", document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler:   func(_ *document.DocContext, _ document.Document) error { rec.record("app_b"); return nil },
		AppName:   "app_b",
		Priority:  100,
		DependsOn: []string{"app_a"},
	})
	hr.Register("HookTestDoc", document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler:  func(_ *document.DocContext, _ document.Document) error { rec.record("app_a"); return nil },
		AppName:  "app_a",
		Priority: 500,
	})

	ctx := newHooksCtx(t)
	doc, err := hooksDocManager.Insert(ctx, "HookTestDoc", map[string]any{"title": "dep test"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	t.Logf("inserted doc: %s", doc.Name())

	order := rec.get()
	if len(order) != 2 {
		t.Fatalf("expected 2 hook calls, got %d: %v", len(order), order)
	}
	if order[0] != "app_a" || order[1] != "app_b" {
		t.Errorf("expected [app_a, app_b], got %v", order)
	}
}

func TestInteg_CircularDependencyError(t *testing.T) {
	hr := freshHookRegistry(t)

	hr.Register("HookTestDoc", document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler:   func(_ *document.DocContext, _ document.Document) error { return nil },
		AppName:   "alpha",
		DependsOn: []string{"beta"},
	})
	hr.Register("HookTestDoc", document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler:   func(_ *document.DocContext, _ document.Document) error { return nil },
		AppName:   "beta",
		DependsOn: []string{"alpha"},
	})

	ctx := newHooksCtx(t)
	_, err := hooksDocManager.Insert(ctx, "HookTestDoc", map[string]any{"title": "cycle test"})
	if err == nil {
		t.Fatal("expected error from circular dependency, got nil")
	}

	var cycleErr *hooks.CircularDependencyError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("expected CircularDependencyError, got: %T: %v", err, err)
	}
	t.Logf("cycle error: %v", cycleErr)
}

func TestInteg_HookFiresDuringInsert(t *testing.T) {
	hr := freshHookRegistry(t)

	// Hook modifies the "marker" field during BeforeSave.
	hr.Register("HookTestDoc", document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler: func(_ *document.DocContext, doc document.Document) error {
			return doc.Set("marker", "hook-was-here")
		},
		AppName:  "marker_app",
		Priority: 100,
	})

	ctx := newHooksCtx(t)
	doc, err := hooksDocManager.Insert(ctx, "HookTestDoc", map[string]any{"title": "marker test"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Read back from DB to verify the hook's modification persisted.
	fetched, err := hooksDocManager.Get(ctx, "HookTestDoc", doc.Name())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	marker, _ := fetched.Get("marker").(string)
	if marker != "hook-was-here" {
		t.Errorf("expected marker=%q, got %q", "hook-was-here", marker)
	}
}

func TestInteg_GlobalAndLocalHooksMerge(t *testing.T) {
	hr := freshHookRegistry(t)
	rec := &orderRecorder{}

	hr.Register("HookTestDoc", document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler:  func(_ *document.DocContext, _ document.Document) error { rec.record("local"); return nil },
		AppName:  "local_app",
		Priority: 200,
	})
	hr.RegisterGlobal(document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler:  func(_ *document.DocContext, _ document.Document) error { rec.record("global"); return nil },
		AppName:  "global_app",
		Priority: 100,
	})

	ctx := newHooksCtx(t)
	_, err := hooksDocManager.Insert(ctx, "HookTestDoc", map[string]any{"title": "merge test"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	order := rec.get()
	if len(order) != 2 {
		t.Fatalf("expected 2 hook calls, got %d: %v", len(order), order)
	}
	// Global has priority 100 (lower = first), local has 200.
	if order[0] != "global" || order[1] != "local" {
		t.Errorf("expected [global, local], got %v", order)
	}
}

func TestInteg_HookErrorAbortsInsert(t *testing.T) {
	hr := freshHookRegistry(t)

	sentinel := errors.New("hook: abort insert")
	hr.Register("HookTestDoc", document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler:  func(_ *document.DocContext, _ document.Document) error { return sentinel },
		AppName:  "failing_app",
		Priority: 100,
	})

	ctx := newHooksCtx(t)
	_, err := hooksDocManager.Insert(ctx, "HookTestDoc", map[string]any{"title": "error test"})
	if err == nil {
		t.Fatal("expected Insert to fail, got nil error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error in chain, got: %v", err)
	}

	// Verify no document was written — list all HookTestDoc rows with this title.
	var count int
	qerr := hooksTestPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM tab_hook_test_doc WHERE "title" = $1`,
		"error test",
	).Scan(&count)
	if qerr != nil {
		t.Fatalf("query count: %v", qerr)
	}
	if count != 0 {
		t.Errorf("expected 0 rows after aborted insert, got %d", count)
	}
}
