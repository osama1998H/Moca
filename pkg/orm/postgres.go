package orm

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/observe"
)

// schemaNameForSite returns the PostgreSQL schema name for a site. It mirrors
// tenancy.SchemaNameForSite to avoid a circular import between orm ↔ tenancy.
// Both must produce identical results — see pkg/tenancy/manager.go:722.
func schemaNameForSite(siteName string) string {
	s := strings.ToLower(siteName)
	s = strings.NewReplacer(".", "_", "-", "_", " ", "_").Replace(s)
	s = regexp.MustCompile(`[^a-z0-9_]`).ReplaceAllString(s, "")
	if len(s) > 0 && s[0] >= '0' && s[0] <= '9' {
		s = "s" + s
	}
	if s == "" {
		s = "site"
	}
	return "tenant_" + s
}

// perTenantDivisor is the default divisor used to calculate per-tenant pool
// size from the global pool size. A future config field can override this in
// MS-12 (Multitenancy).
const perTenantDivisor = 10

// DBManager holds the system pool and the per-tenant pool registry.
// Each tenant site gets its own *pgxpool.Pool whose AfterConnect callback
// permanently sets search_path to that tenant's schema, preventing any
// cross-tenant data leakage and naturally isolating prepared statement caches.
type DBManager struct {
	systemPool        *pgxpool.Pool
	sitePools         map[string]*pgxpool.Pool
	logger            *slog.Logger
	connStr           string
	lastUsed          sync.Map // map[string]time.Time, keyed by schema name
	mu                sync.RWMutex
	systemMaxConns    int32
	perTenantMaxConns int32
}

// NewDBManager creates a DBManager from the given DatabaseConfig. It builds the
// DSN, creates the system pool (bound to moca_system), pings it to verify
// connectivity, and returns a ready-to-use manager.
//
// The pool sizes are derived as follows:
//   - System pool: cfg.PoolSize (defaults to 10 when zero)
//   - Per-tenant pool: max(cfg.PoolSize/perTenantDivisor, 2)
func NewDBManager(ctx context.Context, cfg config.DatabaseConfig, logger *slog.Logger) (*DBManager, error) {
	connStr := buildDSN(cfg)

	systemMaxConns := int32(cfg.PoolSize)
	if systemMaxConns <= 0 {
		systemMaxConns = 10
	}

	perTenantMaxConns := int32(cfg.PoolSize / perTenantDivisor)
	if perTenantMaxConns < 2 {
		perTenantMaxConns = 2
	}

	systemPool, err := newPool(ctx, connStr, systemMaxConns, "moca_system")
	if err != nil {
		return nil, fmt.Errorf("create system pool: %w", err)
	}
	if err := systemPool.Ping(ctx); err != nil {
		systemPool.Close()
		return nil, fmt.Errorf("ping system pool: %w", err)
	}

	logger.Info("db system pool ready",
		slog.String("schema", "moca_system"),
		slog.Int("max_conns", int(systemMaxConns)),
	)

	return &DBManager{
		systemPool:        systemPool,
		sitePools:         make(map[string]*pgxpool.Pool),
		logger:            logger,
		connStr:           connStr,
		systemMaxConns:    systemMaxConns,
		perTenantMaxConns: perTenantMaxConns,
	}, nil
}

// SystemPool returns the pool bound to the moca_system schema.
func (m *DBManager) SystemPool() *pgxpool.Pool {
	return m.systemPool
}

// ForSite returns the *pgxpool.Pool for the given site name. The pool's
// search_path is permanently set to tenant_{siteName} via AfterConnect.
//
// Pool creation is lazy: the first call for a site creates and caches the pool.
// Subsequent calls use the fast RLock path. Double-checked locking ensures safe
// concurrent pool creation.
func (m *DBManager) ForSite(ctx context.Context, siteName string) (*pgxpool.Pool, error) {
	schema := schemaNameForSite(siteName)

	// Fast path: pool already exists.
	m.mu.RLock()
	pool, ok := m.sitePools[schema]
	m.mu.RUnlock()
	if ok {
		m.lastUsed.Store(schema, time.Now())
		return pool, nil
	}

	// Slow path: create the pool under write lock.
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check: another goroutine may have created the pool while we waited.
	if pool, ok = m.sitePools[schema]; ok {
		m.lastUsed.Store(schema, time.Now())
		return pool, nil
	}

	pool, err := newPool(ctx, m.connStr, m.perTenantMaxConns, schema)
	if err != nil {
		return nil, fmt.Errorf("create pool for site %q (schema %q): %w", siteName, schema, err)
	}

	m.sitePools[schema] = pool
	m.lastUsed.Store(schema, time.Now())

	observe.WithSite(m.logger, siteName).Info("db tenant pool created",
		slog.String("schema", schema),
		slog.Int("max_conns", int(m.perTenantMaxConns)),
	)

	return pool, nil
}

// AssertSchema is a defense-in-depth check that queries current_schema() on the
// given pool and verifies it matches expected. Returns an error on mismatch.
// This should be used in tests and high-assurance code paths.
func (m *DBManager) AssertSchema(ctx context.Context, pool *pgxpool.Pool, expected string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	var current string
	if err := conn.QueryRow(ctx, "SELECT current_schema()").Scan(&current); err != nil {
		return fmt.Errorf("query current_schema: %w", err)
	}
	if current != expected {
		m.logger.Error("CRITICAL: schema mismatch detected",
			slog.String("expected", expected),
			slog.String("actual", current),
		)
		return fmt.Errorf("schema mismatch: expected %q, got %q", expected, current)
	}
	return nil
}

// EvictIdlePools closes and removes tenant pools that have been idle for longer
// than maxIdle. It returns the number of pools evicted.
//
// Designed for 10,000+ tenant scale: call periodically from a background
// goroutine to reclaim connections from dormant tenants.
func (m *DBManager) EvictIdlePools(maxIdle time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	evicted := 0
	for schema, pool := range m.sitePools {
		v, ok := m.lastUsed.Load(schema)
		if !ok {
			continue
		}
		lastUsed, ok := v.(time.Time)
		if !ok {
			continue
		}
		if time.Since(lastUsed) > maxIdle {
			pool.Close()
			delete(m.sitePools, schema)
			m.lastUsed.Delete(schema)
			evicted++

			m.logger.Info("db tenant pool evicted",
				slog.String("schema", schema),
				slog.Duration("idle_for", maxIdle),
			)
		}
	}
	return evicted
}

// SitePoolCount returns the number of active tenant pools. Intended for tests.
func (m *DBManager) SitePoolCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sitePools)
}

// SetLastUsed overrides the last-used timestamp for a schema. Intended for tests.
func (m *DBManager) SetLastUsed(schema string, t time.Time) {
	m.lastUsed.Store(schema, t)
}

// Close shuts down all tenant pools and then the system pool.
func (m *DBManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for schema, pool := range m.sitePools {
		pool.Close()
		delete(m.sitePools, schema)
	}
	if m.systemPool != nil {
		m.systemPool.Close()
		m.systemPool = nil
	}
	m.logger.Info("db manager closed")
}

// buildDSN constructs a PostgreSQL DSN from the given DatabaseConfig using
// net/url so that special characters in User or Password are correctly encoded.
func buildDSN(cfg config.DatabaseConfig) string {
	systemDB := cfg.SystemDB
	if systemDB == "" {
		systemDB = "postgres"
	}
	u := &url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Path:   "/" + systemDB,
	}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

// newPool creates a pgxpool.Pool with the given connStr, max connections, and
// an AfterConnect hook that permanently sets search_path to schema.
func newPool(ctx context.Context, connStr string, maxConns int32, schema string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse config for schema %q: %w", schema, err)
	}
	cfg.MaxConns = maxConns
	cfg.AfterConnect = makeSearchPathHook(schema)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool for schema %q: %w", schema, err)
	}
	return pool, nil
}

// makeSearchPathHook returns an AfterConnect callback that permanently sets the
// connection's search_path to schema using pgx.Identifier.Sanitize() for SQL
// injection prevention.
func makeSearchPathHook(schema string) func(context.Context, *pgx.Conn) error {
	return func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx,
			fmt.Sprintf("SET search_path TO %s, public", pgx.Identifier{schema}.Sanitize()),
		)
		if err != nil {
			return fmt.Errorf("set search_path to %q: %w", schema, err)
		}
		return nil
	}
}
