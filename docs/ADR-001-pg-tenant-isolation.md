# ADR-001: PostgreSQL Schema-Per-Tenant Isolation via Per-Pool Registry

**Status:** Accepted
**Spike:** MS-00-T2
**Date:** 2026-03-30
**Validated by:** `go test -v -count=1 -race ./...` — all 7 tests pass

---

## Context

MOCA is a multitenant framework where each tenant (site) has its own PostgreSQL
schema (e.g. `tenant_acme`). A shared `moca_system` schema holds global tables
(sites, apps, site_apps).

The fundamental risk: `SET search_path` is a per-connection setting in PostgreSQL.
`pgxpool` reuses physical connections across goroutines. Without a proven isolation
strategy, goroutine B can inherit goroutine A's `search_path` and read or write into
the wrong tenant's tables — a critical security defect.

Three failure modes were identified before the spike:
1. **Cross-tenant data leak** — connection released by goroutine A retains its
   `search_path` when acquired by goroutine B for a different tenant.
2. **Prepared statement cache poisoning** — pgx caches prepared statements per
   connection. A statement prepared in schema A could be reused in schema B if the
   SQL text matches, returning wrong data.
3. **Race condition** — `search_path` state is global to the session, not per-query.

---

## Decision

Use a **per-site `pgxpool.Pool` registry** (`DBManager`) where each tenant gets
its own dedicated pool. The pool's `AfterConnect` callback permanently sets
`search_path` for every physical connection created in that pool.

```go
type DBManager struct {
    systemPool *pgxpool.Pool
    sitePools  map[string]*pgxpool.Pool
    mu         sync.RWMutex
    connStr    string
    maxConns   int32
    lastUsed   sync.Map // schema -> time.Time
}

// AfterConnect hook — runs once when a physical connection is created.
config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
    _, err := conn.Exec(ctx,
        fmt.Sprintf("SET search_path TO %s, public",
            pgx.Identifier{schema}.Sanitize()))
    return err
}
```

Key design properties:
- **Permanent setting**: `AfterConnect` runs when the physical connection is
  established. The `search_path` persists for the connection's lifetime.
  No per-acquire overhead.
- **Natural statement cache isolation**: Each pool maintains its own prepared
  statement cache (one cache per pool, not per connection). Separate pools =
  separate caches = zero leakage.
- **SQL injection prevention**: `pgx.Identifier{schema}.Sanitize()` properly
  quotes and escapes the schema name.
- **Defense-in-depth**: `assertSchema()` can query `current_schema()` to
  verify correctness; useful in tests and middleware.

---

## Alternatives Considered

### Option A: Shared pool with `BeforeAcquire` reset (Rejected)

Reset `search_path` on every connection acquisition via the `BeforeAcquire`
callback (now `PrepareConn` in pgx v5).

**Why rejected:**
- `BeforeAcquire` is **deprecated** in pgx v5.7.6. Replaced by `PrepareConn`.
- Adds overhead on every `Acquire()` call, not just on new connection creation.
- All tenants share a statement cache, making prepared-statement isolation
  impossible without expensive per-query cache invalidation.
- The ROADMAP referenced `BeforeAcquire` — the spike confirms `AfterConnect`
  is the correct hook for pool-wide schema binding.

### Option B: Single shared pool with per-query `SET search_path` (Rejected)

Prepend `SET search_path TO <schema>;` before every SQL statement.

**Why rejected:**
- Error-prone: every query path must remember the SET, with no enforcement
  mechanism.
- Shared statement cache: impossible to isolate prepared statements per tenant.
- Higher latency: extra RTT per query.
- No defense against accidental omission of the SET prefix.

### Option C: Database-per-tenant (Rejected as default)

Each tenant gets a dedicated PostgreSQL database.

**Why rejected as default:**
- Extreme operational overhead at 10,000+ tenants.
- Cannot use cross-tenant reporting queries.
- Complicates migrations and schema evolution.
- **Retained as opt-in** for enterprise tenants requiring stronger isolation
  guarantees (documented in ADR-001 of `MOCA_SYSTEM_DESIGN.md`).

---

## Validation Results

All tests passed with `-race` flag enabled.
Connection string: `postgres://moca:moca_test@localhost:5433/moca_test`
PostgreSQL: 16-alpine, `max_connections=200`

| Test | Result | Observation |
|------|--------|-------------|
| `TestBasicTenantIsolation` | PASS | Two tenant pools see only their own data |
| `TestConcurrentAccess` | PASS | 100 goroutines × 10 schemas, zero cross-contamination |
| `TestAssertSchema` | PASS | Mismatch detected: expected `tenant_02`, got `tenant_01` |
| `TestPreparedStatementIsolation` | PASS | Each pool has independent statement cache |
| `TestConnectionReuse` | PASS | `search_path` persists across acquire/release cycles |
| `TestPoolLifecycle` | PASS | Lazy creation → eviction → re-creation all correct |
| `TestSystemPoolIsolation` | PASS | System pool permanently in `moca_system`; `tab_test` not visible |

Total test run: 1.966s (including Docker startup). Race detector found no data races.

### Key Observations

**`AfterConnect` hook behavior:**
- Runs exactly once per physical connection, at connection establishment time.
- The `search_path` setting persists for the entire connection lifetime in the pool.
- `TestConnectionReuse` (MaxConns=1) confirmed: after release and re-acquire of the
  same physical connection, `current_schema()` still returns the correct tenant schema.

**Prepared statement cache isolation:**
- `TestPreparedStatementIsolation` ran the same query text on two different tenant
  pools. Each pool returned only its own data, even on the second call (cache hit path).
- pgxpool's statement cache is per-pool, not per-connection. With separate pools per
  tenant, no additional cache namespacing is required.

**System pool isolation:**
- System pool permanently bound to `moca_system`.
- `moca_system.tab_test` does not exist — the system pool correctly raises
  `ERROR: relation "tab_test" does not exist` when queried without schema qualification.
- Tenant operations did not affect the system pool's `current_schema()`.

**Pool lifecycle:**
- Lazy creation on first `ForSite()` call — no startup overhead for unused tenants.
- Idle eviction correctly closes pools and removes them from the registry.
- After eviction, the next `ForSite()` call re-creates the pool with a fresh
  `AfterConnect`, confirming the pattern is fully self-healing.

---

## Performance Observations

- **Pool creation latency:** Negligible for the spike (10 tenants). The
  `pgxpool.NewWithConfig()` call does not open connections eagerly; connections
  are created on first use.
- **`AfterConnect` cost:** One `SET search_path` statement per physical connection
  creation. With `MaxConns=5` per tenant and connections being long-lived, this
  cost is amortized over thousands of queries.
- **Connection count:** 10 tenants × 5 MaxConns + 5 system pool = 55 max connections.
  Well within PostgreSQL's configured `max_connections=200`.

---

## Consequences for Production

### Connection Sizing
At 10,000 tenants with `MaxConns=5`, total possible connections = 50,005.
PostgreSQL's default `max_connections=100` cannot accommodate this.

Resolution strategies (to be evaluated in MS-02+):
1. **Idle eviction** (implemented in this spike): Close pools for tenants that have
   not been active for N minutes. With typical SaaS usage patterns, most tenants are
   idle at any given time.
2. **PgBouncer** (transaction-mode pooling): Insert a connection pooler between the
   application and PostgreSQL. PgBouncer can multiplex thousands of application-side
   pools onto a limited number of server-side connections. Note: transaction-mode
   PgBouncer makes `search_path` setting more complex — must use `SET LOCAL` within
   transactions or use `startup_parameters` in the PgBouncer config.
3. **Reduced `MaxConns`**: Set `MaxConns=1` or `MaxConns=2` for inactive tenants.

Open Question OQ-1 (ROADMAP line 1268) remains: the optimal strategy for 10,000+
tenants depends on traffic patterns and latency requirements.

### Idle Eviction Tuning
The `EvictIdlePools(maxIdle)` method should be called periodically (e.g., every 5
minutes) from a background goroutine in the production `SiteManager`. The
`lastUsed` timestamp is updated on every `ForSite()` call. The `sync.Map` avoids
the read-lock + map-write hazard that would arise from using a plain `map` with
`sync.RWMutex`.

### `pgxpool.Stat().IdleDuration()` — Correction
The `blocker-resolution-strategies.md` document (line 168) referenced
`pool.Stat().IdleDuration()` as a way to detect idle pools. This method does not
exist in the pgxpool API. The correct approach is application-level timestamp
tracking (implemented as `lastUsed sync.Map` in `DBManager`).

### RLS as Defense-in-Depth
The schema-per-tenant model eliminates the need for Row-Level Security (RLS) for
cross-tenant isolation, but adding RLS policies to tenant tables provides defense-
in-depth against any future misconfiguration (e.g., a pool somehow pointing at the
wrong schema). This is noted in `MOCA_SYSTEM_DESIGN.md` ADR-001.

---

## References

- `docs/blocker-resolution-strategies.md` — Blocker 2, lines 66-178
- `MOCA_SYSTEM_DESIGN.md` — §4.1 lines 828-850, §4.2 lines 852-887, ADR-001 lines 2063-2067
- `ROADMAP.md` — MS-00 lines 98-137, acceptance criteria line 119
- pgx v5 documentation: `pgxpool.Config.AfterConnect`, `pgx.Identifier.Sanitize()`
