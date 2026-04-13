package testutils

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/builtin/core"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/tenancy"
)

const (
	DefaultPGHost     = "localhost"
	DefaultPGPort     = 5433
	DefaultPGUser     = "moca"
	DefaultPGPassword = "moca_test"
	DefaultPGDB       = "moca_test"
	DefaultRedisAddr  = "localhost:6380"
)

var (
	siteNameSanitizer = regexp.MustCompile(`[^a-z0-9_]+`)
	siteCounter       atomic.Uint64
)

// TestEnv holds shared test infrastructure backed by real PostgreSQL and
// optional Redis. Each TestEnv provisions a unique PostgreSQL schema and
// registers t.Cleanup for automatic teardown.
type TestEnv struct {
	Ctx       context.Context
	Logger    *slog.Logger
	AdminPool *pgxpool.Pool
	DBManager *orm.DBManager
	Redis     *redis.Client
	Site      *tenancy.SiteContext
	User      *auth.User
	SiteName  string
	Schema    string

	registry    *meta.Registry
	migrator    *meta.Migrator
	docManager  *document.DocManager
	siteManager *tenancy.SiteManager
	cfg         *envConfig
}

// NewTestEnv provisions a unique tenant schema, DB manager, optional Redis
// client, and default site/user context for integration tests. Cleanup is
// automatic via t.Cleanup.
func NewTestEnv(t testing.TB, opts ...EnvOption) *TestEnv {
	t.Helper()

	cfg := defaultEnvConfig(t)
	for _, opt := range opts {
		opt(cfg)
	}

	ctx := context.Background()
	siteName := uniqueSiteName(cfg.prefix)
	schema := tenancy.SchemaNameForSite(siteName)

	adminPool := mustOpenAdminPool(t, ctx)
	env := &TestEnv{
		Ctx:       ctx,
		AdminPool: adminPool,
		SiteName:  siteName,
		Schema:    schema,
		cfg:       cfg,
	}

	t.Cleanup(func() {
		if env.Redis != nil {
			env.FlushRedis(t,
				fmt.Sprintf("meta:%s:*", env.SiteName),
				fmt.Sprintf("schema:%s:*", env.SiteName),
				fmt.Sprintf("rl:%s:*", env.SiteName),
			)
			_ = env.Redis.Close()
		}
		if env.DBManager != nil {
			env.DBManager.Close()
		}
		if env.AdminPool != nil {
			if env.Schema != "" {
				_, _ = env.AdminPool.Exec(context.Background(),
					fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, quoteIdent(env.Schema)))
			}
			if env.SiteName != "" {
				_, _ = env.AdminPool.Exec(context.Background(),
					`DELETE FROM moca_system.sites WHERE name = $1`, env.SiteName)
			}
			env.AdminPool.Close()
		}
	})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	env.Logger = logger

	mustExec(t, adminPool, `CREATE SCHEMA IF NOT EXISTS moca_system`)
	mustExec(t, adminPool, `
		CREATE TABLE IF NOT EXISTS moca_system.sites (
			name        TEXT PRIMARY KEY,
			db_schema   TEXT NOT NULL UNIQUE,
			status      TEXT NOT NULL DEFAULT 'active',
			plan        TEXT,
			config      JSONB NOT NULL DEFAULT '{}',
			admin_email TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	mustExec(t, adminPool, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema)))
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO moca_system.sites (name, db_schema, admin_email)
		VALUES ($1, $2, $3)
		ON CONFLICT (name) DO UPDATE SET
			db_schema = EXCLUDED.db_schema,
			admin_email = EXCLUDED.admin_email
	`, siteName, schema, cfg.userEmail); err != nil {
		t.Fatalf("insert test site %q: %v", siteName, err)
	}

	dbManager, err := orm.NewDBManager(ctx, testDatabaseConfig(t), logger)
	if err != nil {
		t.Fatalf("new test DB manager: %v", err)
	}
	env.DBManager = dbManager

	sitePool, err := dbManager.ForSite(ctx, siteName)
	if err != nil {
		dbManager.Close()
		t.Fatalf("create tenant pool for %q: %v", siteName, err)
	}

	env.Redis = maybeOpenRedis(t, ctx)
	env.Site = &tenancy.SiteContext{
		Name:     siteName,
		DBSchema: schema,
		Status:   "active",
		Pool:     sitePool,
	}
	env.User = &auth.User{
		Email:    cfg.userEmail,
		FullName: cfg.userFullName,
		Roles:    cfg.userRoles,
	}

	if cfg.bootstrap {
		env.EnsureMetaTables(t)
		env.bootstrapCoreMeta(t)
	}

	return env
}

// Registry returns the lazily constructed test registry.
func (e *TestEnv) Registry() *meta.Registry {
	if e.registry == nil {
		e.registry = meta.NewRegistry(e.DBManager, e.Redis, e.Logger)
	}
	return e.registry
}

// Migrator returns the lazily constructed test migrator.
func (e *TestEnv) Migrator() *meta.Migrator {
	if e.migrator == nil {
		e.migrator = meta.NewMigrator(e.DBManager, e.Logger)
	}
	return e.migrator
}

// DocManager returns the lazily constructed test document manager.
func (e *TestEnv) DocManager() *document.DocManager {
	if e.docManager == nil {
		e.docManager = document.NewDocManager(
			e.Registry(),
			e.DBManager,
			document.NewNamingEngine(),
			document.NewValidator(),
			document.NewControllerRegistry(),
			e.Logger,
		)
		e.docManager.SetHookDispatcher(hooks.NewDocEventDispatcher(hooks.NewHookRegistry()))
	}
	return e.docManager
}

// SiteManager returns the lazily constructed test site manager.
func (e *TestEnv) SiteManager() *tenancy.SiteManager {
	if e.siteManager == nil {
		var redisCache, redisPubSub *redis.Client
		if e.Redis != nil {
			redisCache = e.Redis
			redisPubSub = e.Redis
		}
		e.siteManager = tenancy.NewSiteManager(
			e.DBManager, e.Migrator(), e.Registry(),
			redisCache, redisPubSub, e.Logger, nil,
		)
	}
	return e.siteManager
}

// DocContext returns a fresh document context for the test site and user.
func (e *TestEnv) DocContext() *document.DocContext {
	return document.NewDocContext(context.Background(), e.Site, e.User)
}

// GetDocManager implements factory.InsertEnv.
func (e *TestEnv) GetDocManager() *document.DocManager { return e.DocManager() }

// GetDocContext implements factory.InsertEnv.
func (e *TestEnv) GetDocContext() *document.DocContext { return e.DocContext() }

// GetSiteName implements factory.InsertEnv.
func (e *TestEnv) GetSiteName() string { return e.SiteName }

// EnsureMetaTables creates the per-tenant system tables required by registry
// and document operations.
func (e *TestEnv) EnsureMetaTables(t testing.TB) {
	t.Helper()
	migrator := e.Migrator()
	if err := migrator.EnsureMetaTables(e.Ctx, e.SiteName); err != nil {
		t.Fatalf("EnsureMetaTables(%q): %v", e.SiteName, err)
	}
}

// RegisterMetaType compiles and registers a MetaType into the test registry.
func (e *TestEnv) RegisterMetaType(t testing.TB, mt *meta.MetaType) *meta.MetaType {
	t.Helper()
	e.EnsureMetaTables(t)

	data, err := json.Marshal(mt)
	if err != nil {
		t.Fatalf("marshal MetaType %q: %v", mt.Name, err)
	}
	registered, err := e.Registry().Register(e.Ctx, e.SiteName, data)
	if err != nil {
		t.Fatalf("register MetaType %q: %v", mt.Name, err)
	}
	return registered
}

// RequireRedis returns the Redis client or skips the test if unavailable.
func (e *TestEnv) RequireRedis(t testing.TB) *redis.Client {
	t.Helper()
	if e.Redis == nil {
		t.Skip("Redis unavailable; start Docker services with `docker compose up -d --wait`")
	}
	return e.Redis
}

// FlushRedis deletes every key matching the provided patterns.
func (e *TestEnv) FlushRedis(t testing.TB, patterns ...string) {
	t.Helper()
	if e.Redis == nil {
		return
	}
	for _, pattern := range patterns {
		keys, err := e.Redis.Keys(e.Ctx, pattern).Result()
		if err != nil {
			t.Fatalf("redis KEYS %q: %v", pattern, err)
		}
		if len(keys) == 0 {
			continue
		}
		if err := e.Redis.Del(e.Ctx, keys...).Err(); err != nil {
			t.Fatalf("redis DEL %q: %v", pattern, err)
		}
	}
}

// MetaRedisKey returns the registry L2 cache key for the given doctype.
func (e *TestEnv) MetaRedisKey(doctype string) string {
	return fmt.Sprintf("meta:%s:%s", e.SiteName, doctype)
}

// bootstrapCoreMeta registers the builtin core MetaTypes in the test schema.
func (e *TestEnv) bootstrapCoreMeta(t testing.TB) {
	t.Helper()

	// Import core bootstrap dynamically to avoid circular dependency issues.
	// The bootstrap function is expected to be available from pkg/builtin/core.
	// We register each MetaType returned by BootstrapCoreMeta.
	coreMeta, err := core.BootstrapCoreMeta()
	if err != nil {
		t.Fatalf("bootstrap core meta: %v", err)
	}
	for _, mt := range coreMeta {
		data, err := json.Marshal(mt)
		if err != nil {
			t.Fatalf("marshal core MetaType %q: %v", mt.Name, err)
		}
		if _, err := e.Registry().Register(e.Ctx, e.SiteName, data); err != nil {
			t.Fatalf("register core MetaType %q: %v", mt.Name, err)
		}
	}
}

// ServicesAvailable checks if PostgreSQL and Redis are reachable at the default
// test addresses. Returns true if at least PostgreSQL is available.
func ServicesAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	connStr := pgConnString()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return false
	}
	defer pool.Close()
	return pool.Ping(ctx) == nil
}

// ─── Internal helpers ───────────────────────────────────────────────────────

func defaultEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func pgConnString() string {
	if connStr := os.Getenv("PG_CONN_STRING"); connStr != "" {
		return connStr
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		DefaultPGUser,
		DefaultPGPassword,
		defaultEnv("PG_HOST", DefaultPGHost),
		DefaultPGPort,
		DefaultPGDB,
	)
}

func testDatabaseConfig(t testing.TB) config.DatabaseConfig {
	t.Helper()

	cfg := config.DatabaseConfig{
		Host:     defaultEnv("PG_HOST", DefaultPGHost),
		Port:     DefaultPGPort,
		User:     DefaultPGUser,
		Password: DefaultPGPassword,
		SystemDB: DefaultPGDB,
		PoolSize: 20,
	}
	if os.Getenv("PG_CONN_STRING") == "" {
		return cfg
	}

	poolCfg, err := pgxpool.ParseConfig(pgConnString())
	if err != nil {
		t.Fatalf("parse PG_CONN_STRING: %v", err)
	}

	cfg.Host = poolCfg.ConnConfig.Host
	cfg.Port = int(poolCfg.ConnConfig.Port)
	cfg.User = poolCfg.ConnConfig.User
	cfg.Password = poolCfg.ConnConfig.Password
	cfg.SystemDB = poolCfg.ConnConfig.Database
	return cfg
}

func mustOpenAdminPool(t testing.TB, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	connStr := pgConnString()
	adminPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	return adminPool
}

func maybeOpenRedis(t testing.TB, ctx context.Context) *redis.Client {
	t.Helper()

	addr := defaultEnv("REDIS_ADDR", DefaultRedisAddr)
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 0})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil
	}
	return client
}

func mustExec(t testing.TB, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", oneLine(sql), err)
	}
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func oneLine(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

func uniqueSiteName(prefix string) string {
	base := strings.ToLower(prefix)
	base = siteNameSanitizer.ReplaceAllString(base, "_")
	base = strings.Trim(base, "_")
	if base == "" {
		base = "test"
	}
	if len(base) > 24 {
		base = base[:24]
	}
	return fmt.Sprintf("%s_%x", base, siteCounter.Add(1)+uint64(time.Now().UnixNano()))
}
