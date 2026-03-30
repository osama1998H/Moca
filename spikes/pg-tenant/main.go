// Package main implements a spike validating the per-site pgxpool registry
// pattern for PostgreSQL schema-per-tenant isolation.
//
// Spike: MS-00-T2
// Design ref: docs/blocker-resolution-strategies.md (Blocker 2, lines 66-178)
//
// Key architectural bet being validated:
//   Each tenant (site) gets its own pgxpool.Pool whose AfterConnect callback
//   permanently sets search_path to that tenant's schema. This prevents any
//   cross-tenant data leakage and naturally isolates prepared statement caches
//   (one cache per pool, not per connection).
//
// This is throwaway spike code. Do not promote to pkg/.
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBManager holds the system pool (for moca_system queries) and a registry
// of per-tenant pools. Each tenant pool uses AfterConnect to permanently set
// search_path, so every connection in that pool always queries the correct schema.
type DBManager struct {
	systemPool *pgxpool.Pool
	sitePools  map[string]*pgxpool.Pool
	mu         sync.RWMutex
	connStr    string
	maxConns   int32
	lastUsed   sync.Map // map[string]time.Time — concurrent-safe, avoids RLock+write hazard
}

// NewDBManager creates a DBManager with a system pool permanently bound to
// the moca_system schema. The system pool uses AfterConnect to set search_path.
func NewDBManager(ctx context.Context, connStr string, maxConns int32) (*DBManager, error) {
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	config.MaxConns = maxConns
	config.AfterConnect = makeSearchPathHook("moca_system")

	systemPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create system pool: %w", err)
	}
	if err := systemPool.Ping(ctx); err != nil {
		systemPool.Close()
		return nil, fmt.Errorf("ping system pool: %w", err)
	}

	return &DBManager{
		systemPool: systemPool,
		sitePools:  make(map[string]*pgxpool.Pool),
		connStr:    connStr,
		maxConns:   maxConns,
	}, nil
}

// SystemPool returns the pool bound to the moca_system schema.
func (m *DBManager) SystemPool() *pgxpool.Pool {
	return m.systemPool
}

// ForSite returns the pool for the given tenant schema, creating it lazily on
// first access. Uses double-check locking: fast read-lock path, slow write-lock
// path for creation.
func (m *DBManager) ForSite(ctx context.Context, schema string) (*pgxpool.Pool, error) {
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

	pool, err := m.createSitePool(ctx, schema)
	if err != nil {
		return nil, err
	}
	m.sitePools[schema] = pool
	m.lastUsed.Store(schema, time.Now())
	return pool, nil
}

// createSitePool creates a new pgxpool.Pool whose every connection is permanently
// bound to the given tenant schema via AfterConnect. Called under write lock.
func (m *DBManager) createSitePool(ctx context.Context, schema string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(m.connStr)
	if err != nil {
		return nil, fmt.Errorf("parse config for %q: %w", schema, err)
	}
	config.MaxConns = m.maxConns
	config.AfterConnect = makeSearchPathHook(schema)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool for %q: %w", schema, err)
	}
	return pool, nil
}

// makeSearchPathHook returns an AfterConnect callback that permanently sets
// search_path for a connection. AfterConnect runs once when a physical
// connection is created, before it enters the pool. The setting persists for
// the connection's lifetime in the pool — no per-acquire overhead.
//
// pgx.Identifier.Sanitize() quotes and escapes the identifier, preventing
// SQL injection if an attacker could control schema names.
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

// assertSchema is a defense-in-depth check that queries current_schema() on an
// acquired connection and returns an error if it does not match expected.
// This catches any hypothetical misconfiguration at the cost of one extra query.
func assertSchema(ctx context.Context, pool *pgxpool.Pool, expected string) error {
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
		return fmt.Errorf("schema mismatch: expected %q, got %q", expected, current)
	}
	return nil
}

// EvictIdlePools closes and removes pools that have not been used for longer
// than maxIdle. Returns the number of pools evicted.
// After eviction, ForSite will lazily re-create the pool on next access.
func (m *DBManager) EvictIdlePools(maxIdle time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	evicted := 0
	for schema, pool := range m.sitePools {
		if v, ok := m.lastUsed.Load(schema); ok {
			if time.Since(v.(time.Time)) > maxIdle {
				pool.Close()
				delete(m.sitePools, schema)
				m.lastUsed.Delete(schema)
				evicted++
			}
		}
	}
	return evicted
}

// SitePoolCount returns the number of active (non-evicted) site pools.
// Used in tests to verify lazy creation and eviction behavior.
func (m *DBManager) SitePoolCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sitePools)
}

// SetLastUsed overrides the last-used time for a schema. Exposed for testing
// idle eviction without real time delays.
func (m *DBManager) SetLastUsed(schema string, t time.Time) {
	m.lastUsed.Store(schema, t)
}

// Close shuts down all pools: system pool and all site pools.
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
}

func main() {
	// The spike is test-driven. Run: go test -v -count=1 -race ./...
	// See main_test.go for all validation scenarios.
	fmt.Println("MS-00-T2: PostgreSQL Schema-Per-Tenant Isolation Spike")
	fmt.Println("Run: go test -v -count=1 -race ./...")
}
