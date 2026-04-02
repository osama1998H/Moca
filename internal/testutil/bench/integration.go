package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/tenancy"
)

const (
	defaultPGHost     = "localhost"
	defaultPGPort     = 5433
	defaultPGUser     = "moca"
	defaultPGPassword = "moca_test"
	defaultPGDB       = "moca_test"
	defaultRedisAddr  = "localhost:6380"
)

var (
	siteNameSanitizer = regexp.MustCompile(`[^a-z0-9_]+`)
	siteCounter       atomic.Uint64
)

// IntegrationEnv holds shared benchmark infrastructure backed by real
// PostgreSQL and optional Redis.
type IntegrationEnv struct {
	Ctx       context.Context
	Logger    *slog.Logger
	AdminPool *pgxpool.Pool
	DBManager *orm.DBManager
	Redis     *redis.Client
	Site      *tenancy.SiteContext
	User      *auth.User

	registry   *meta.Registry
	docManager *document.DocManager
	SiteName   string
	Schema     string
}

// GatewayBundle groups the benchmark gateway and its fully wrapped handler.
type GatewayBundle struct {
	Gateway *api.Gateway
	Handler http.Handler
}

// StaticSiteResolver always returns the benchmark site's SiteContext.
type StaticSiteResolver struct {
	Site *tenancy.SiteContext
}

// ResolveSite returns the configured benchmark site for the matching site name.
func (r StaticSiteResolver) ResolveSite(_ context.Context, siteID string) (*tenancy.SiteContext, error) {
	if r.Site != nil && r.Site.Name == siteID {
		return r.Site, nil
	}
	return nil, fmt.Errorf("unknown benchmark site %q", siteID)
}

// NewLogger returns the standard warning-level benchmark logger.
func NewLogger() *slog.Logger {
	return observe.NewLogger(slog.LevelWarn)
}

// NewIntegrationEnv provisions a unique tenant schema, DB manager, optional
// Redis client, and default site/user context for integration benchmarks.
func NewIntegrationEnv(tb testing.TB, prefix string) *IntegrationEnv {
	tb.Helper()

	ctx := context.Background()
	siteName := uniqueSiteName(prefix)
	schema := tenancy.SchemaNameForSite(siteName)

	adminPool := mustOpenAdminPool(tb, ctx)
	env := &IntegrationEnv{
		Ctx:       ctx,
		AdminPool: adminPool,
		SiteName:  siteName,
		Schema:    schema,
	}
	tb.Cleanup(func() {
		if env.Redis != nil {
			env.FlushRedis(tb,
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

	logger := NewLogger()
	env.Logger = logger

	mustExec(tb, adminPool, `CREATE SCHEMA IF NOT EXISTS moca_system`)
	mustExec(tb, adminPool, `
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
	mustExec(tb, adminPool, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema)))
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO moca_system.sites (name, db_schema, admin_email)
		VALUES ($1, $2, $3)
		ON CONFLICT (name) DO UPDATE SET
			db_schema = EXCLUDED.db_schema,
			admin_email = EXCLUDED.admin_email
	`, siteName, schema, "bench@moca.dev"); err != nil {
		tb.Fatalf("insert benchmark site %q: %v", siteName, err)
	}

	dbManager, err := orm.NewDBManager(ctx, benchmarkDatabaseConfig(tb), logger)
	if err != nil {
		tb.Fatalf("new benchmark DB manager: %v", err)
	}
	env.DBManager = dbManager

	sitePool, err := dbManager.ForSite(ctx, siteName)
	if err != nil {
		dbManager.Close()
		tb.Fatalf("create tenant pool for %q: %v", siteName, err)
	}

	env.Redis = maybeOpenRedis(tb, ctx)
	env.Site = &tenancy.SiteContext{
		Name:     siteName,
		DBSchema: schema,
		Status:   "active",
		Pool:     sitePool,
	}
	env.User = &auth.User{
		Email:    "bench@moca.dev",
		FullName: "Benchmark Runner",
		Roles:    []string{"System Manager"},
	}

	return env
}

// RequireRedis returns the integration Redis client or skips the benchmark if
// Redis is unavailable.
func (e *IntegrationEnv) RequireRedis(tb testing.TB) *redis.Client {
	tb.Helper()
	if e.Redis == nil {
		tb.Skip("Redis unavailable; start Docker services with `docker compose up -d --wait`")
	}
	return e.Redis
}

// Registry returns the lazily constructed benchmark registry.
func (e *IntegrationEnv) Registry() *meta.Registry {
	if e.registry == nil {
		e.registry = meta.NewRegistry(e.DBManager, e.Redis, e.Logger)
	}
	return e.registry
}

// DocManager returns the lazily constructed benchmark document manager.
func (e *IntegrationEnv) DocManager() *document.DocManager {
	if e.docManager == nil {
		e.docManager = document.NewDocManager(
			e.Registry(),
			e.DBManager,
			document.NewNamingEngine(),
			document.NewValidator(),
			document.NewControllerRegistry(),
			e.Logger,
		)
	}
	return e.docManager
}

// DocContext returns a fresh document context for the benchmark site and user.
func (e *IntegrationEnv) DocContext() *document.DocContext {
	return document.NewDocContext(context.Background(), e.Site, e.User)
}

// EnsureMetaTables creates the per-tenant system tables required by registry
// and document benchmarks.
func (e *IntegrationEnv) EnsureMetaTables(tb testing.TB) {
	tb.Helper()
	migrator := meta.NewMigrator(e.DBManager, e.Logger)
	if err := migrator.EnsureMetaTables(e.Ctx, e.SiteName); err != nil {
		tb.Fatalf("EnsureMetaTables(%q): %v", e.SiteName, err)
	}
}

// RegisterMetaType registers a compiled MetaType into the benchmark registry.
func (e *IntegrationEnv) RegisterMetaType(tb testing.TB, mt *meta.MetaType) *meta.MetaType {
	tb.Helper()
	e.EnsureMetaTables(tb)

	data, err := json.Marshal(mt)
	if err != nil {
		tb.Fatalf("marshal MetaType %q: %v", mt.Name, err)
	}
	registered, err := e.Registry().Register(e.Ctx, e.SiteName, data)
	if err != nil {
		tb.Fatalf("register MetaType %q: %v", mt.Name, err)
	}
	return registered
}

// FlushRedis deletes every key matching the provided patterns.
func (e *IntegrationEnv) FlushRedis(tb testing.TB, patterns ...string) {
	tb.Helper()
	if e.Redis == nil {
		return
	}
	for _, pattern := range patterns {
		keys, err := e.Redis.Keys(e.Ctx, pattern).Result()
		if err != nil {
			tb.Fatalf("redis KEYS %q: %v", pattern, err)
		}
		if len(keys) == 0 {
			continue
		}
		if err := e.Redis.Del(e.Ctx, keys...).Err(); err != nil {
			tb.Fatalf("redis DEL %q: %v", pattern, err)
		}
	}
}

// MetaRedisKey returns the registry L2 cache key for the given doctype.
func (e *IntegrationEnv) MetaRedisKey(doctype string) string {
	return fmt.Sprintf("meta:%s:%s", e.SiteName, doctype)
}

// NewGatewayBundle builds a real resource gateway and returns its fully wrapped
// handler for HTTP benchmarks.
func (e *IntegrationEnv) NewGatewayBundle(tb testing.TB, defaultRate *meta.RateLimitConfig) *GatewayBundle {
	tb.Helper()

	opts := []api.GatewayOption{
		api.WithDocManager(e.DocManager()),
		api.WithRegistry(e.Registry()),
		api.WithLogger(e.Logger),
		api.WithSiteResolver(StaticSiteResolver{Site: e.Site}),
	}
	if defaultRate != nil && e.Redis != nil {
		opts = append(opts, api.WithRateLimiter(api.NewRateLimiter(e.Redis, e.Logger), defaultRate))
	}

	gw := api.NewGateway(opts...)
	resource := api.NewResourceHandler(gw)
	resource.RegisterRoutes(gw.Mux(), "v1")
	gw.SetVersionRouter(api.NewVersionRouter(resource, e.Logger))

	return &GatewayBundle{
		Gateway: gw,
		Handler: gw.Handler(),
	}
}

func defaultEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func benchmarkConnString() string {
	if connStr := os.Getenv("PG_CONN_STRING"); connStr != "" {
		return connStr
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		defaultPGUser,
		defaultPGPassword,
		defaultEnv("PG_HOST", defaultPGHost),
		defaultPGPort,
		defaultPGDB,
	)
}

func benchmarkDatabaseConfig(tb testing.TB) config.DatabaseConfig {
	tb.Helper()

	cfg := config.DatabaseConfig{
		Host:     defaultEnv("PG_HOST", defaultPGHost),
		Port:     defaultPGPort,
		User:     defaultPGUser,
		Password: defaultPGPassword,
		SystemDB: defaultPGDB,
		PoolSize: 20,
	}
	if os.Getenv("PG_CONN_STRING") == "" {
		return cfg
	}

	poolCfg, err := pgxpool.ParseConfig(benchmarkConnString())
	if err != nil {
		tb.Fatalf("parse PG_CONN_STRING: %v", err)
	}

	cfg.Host = poolCfg.ConnConfig.Host
	cfg.Port = int(poolCfg.ConnConfig.Port)
	cfg.User = poolCfg.ConnConfig.User
	cfg.Password = poolCfg.ConnConfig.Password
	cfg.SystemDB = poolCfg.ConnConfig.Database
	return cfg
}

func mustOpenAdminPool(tb testing.TB, ctx context.Context) *pgxpool.Pool {
	tb.Helper()

	connStr := benchmarkConnString()

	adminPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		tb.Skipf("PostgreSQL unavailable: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		tb.Skipf("PostgreSQL unavailable: %v", err)
	}
	return adminPool
}

func maybeOpenRedis(tb testing.TB, ctx context.Context) *redis.Client {
	tb.Helper()

	addr := defaultEnv("REDIS_ADDR", defaultRedisAddr)
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 0})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil
	}
	return client
}

func mustExec(tb testing.TB, pool *pgxpool.Pool, sql string, args ...any) {
	tb.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		tb.Fatalf("exec %q: %v", oneLine(sql), err)
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
		base = "bench"
	}
	if len(base) > 24 {
		base = base[:24]
	}
	return fmt.Sprintf("%s_%x", base, siteCounter.Add(1)+uint64(time.Now().UnixNano()))
}
