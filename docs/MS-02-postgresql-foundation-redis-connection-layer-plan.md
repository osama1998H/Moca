# MS-02 - PostgreSQL Foundation and Redis Connection Layer Plan

## Milestone Summary
- **ID:** MS-02
- **Name:** PostgreSQL Foundation and Redis Connection Layer
- **Roadmap Reference:** `ROADMAP.md` lines 257-295
- **Goal:** Implement the DB connection pool manager (per-tenant schema isolation), Redis client manager (4 logical DBs), system schema DDL, and basic health checks.
- **Why it matters:** MS-02 is the 3rd milestone on the critical path. Every subsequent backend milestone (MS-03 Metadata Registry, MS-04 Document Runtime, MS-06 REST API, MS-12 Multitenancy, etc.) depends on the database and Redis infrastructure established here.
- **Position in roadmap:** Implementation Order #3 (first milestone where parallelism begins — runs alongside MS-07 CLI Foundation). Critical path: MS-00 → MS-01 → **MS-02** → MS-03 → MS-04 → ... → v1.0
- **Upstream dependencies:** MS-01 (Project Structure & Config) — provides `internal/config/` with `DatabaseConfig` and `RedisConfig` structs, `config.LoadAndResolve()` pipeline, 5 cmd/ binary stubs
- **Downstream dependencies:** MS-03 (Metadata Registry), MS-12 (Multitenancy)
- **Estimated duration:** 3 weeks

## Vision Alignment

MS-02 establishes the two foundational infrastructure layers that the entire Moca framework builds upon:

1. **Schema-per-tenant PostgreSQL isolation** — the core multitenancy primitive. Every tenant gets its own `pgxpool.Pool` with a fixed `search_path`, ensuring zero cross-tenant data leakage. This is the architectural decision (ADR-001) that differentiates Moca's approach.
2. **Redis 4-DB client factory** — separates cache, queue, session, and pub/sub concerns into distinct logical databases, enabling clean resource management.
3. **Structured observability** — slog-based logging with tenant context fields, plus health check endpoints for deployment readiness.

Without MS-02, no data can be stored, no tenant can be isolated, and no downstream milestone can function.

## Source References

| Source File | Section | Lines | Relevance |
|---|---|---|---|
| `ROADMAP.md` | MS-02 definition | 257-295 | Scope, deliverables, acceptance criteria, risks |
| `MOCA_SYSTEM_DESIGN.md` | §4.1 Database Per Tenant | 828-850 | Schema isolation architecture |
| `MOCA_SYSTEM_DESIGN.md` | §4.2 Core System Tables | 852-887 | DDL for `moca_system` schema (sites, apps, site_apps) |
| `MOCA_SYSTEM_DESIGN.md` | §5.1 Caching Layer | 1026-1072 | Redis 4-DB layout, key patterns, config sync contract |
| `MOCA_SYSTEM_DESIGN.md` | §11.2 Structured Logging | 1749-1761 | slog fields: site, doctype, docname, user, request_id, duration_ms |
| `MOCA_SYSTEM_DESIGN.md` | §11.3 Health Checks | 1763-1770 | /health, /health/ready, /health/live endpoints |
| `MOCA_SYSTEM_DESIGN.md` | §15 Package Layout | 1924-2057 | Canonical file locations for pkg/orm/ and pkg/observe/ |
| `MOCA_SYSTEM_DESIGN.md` | ADR-001 | 2063-2067 | Schema-per-tenant decision rationale |
| `docs/blocker-resolution-strategies.md` | Blocker 2 | 66-177 | Detailed DBManager implementation: per-site pool registry, ForSite double-checked locking, assertSchema, EvictIdlePools |
| `docs/moca-database-decision-report.md` | PG validation | 109-151 | Confirms MS-02 design is PG-specific |
| `docs/MS-01-project-structure-configuration-plan.md` | Context | 9-193 | Upstream: config system, explicit "no DB connections" in MS-01 scope |

## Research Notes

No web research was needed. All implementation patterns are well-defined in the design documents and validated by MS-00 spikes:

- **`AfterConnect` hook** validated in `spikes/pg-tenant/` — confirmed as the correct pgxpool hook (not deprecated `BeforeAcquire`). Separate pools naturally isolate prepared statement caches.
- **go-redis v9** validated in `spikes/redis-streams/` — client construction patterns carry over to the 4-DB factory.
- **slog** is in Go stdlib (Go 1.21+, project uses Go 1.26) — no external logging dependency needed.
- **`pgx.Identifier.Sanitize()`** used in spike for SQL injection prevention on schema names.

## Milestone Plan

### Task 1
- **Task ID:** MS-02-T1
- **Title:** Structured Logger and CI Docker Infrastructure
- **Description:** Implement slog-based structured logging with tenant context fields, create a unified root-level docker-compose for CI integration tests, and add `pgx/v5` and `go-redis/v9` dependencies to `go.mod`.
- **Why this task exists:** Every subsequent component (DBManager, Redis factory, health checks) needs a logger. Docker-compose and module dependencies are one-time prerequisites that unblock all other tasks.
- **Dependencies:** None (first task)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §11.2 lines 1749-1761 — required log fields (site, doctype, docname, user, request_id, duration_ms)
  - `spikes/pg-tenant/docker-compose.yml` — PostgreSQL compose config to merge
  - `spikes/redis-streams/docker-compose.yml` — Redis compose config to merge
- **Deliverable:**
  - `pkg/observe/logging.go` — `NewLogger(level slog.Level) *slog.Logger` (JSON handler to stdout), `WithSite(logger, site)`, `WithRequest(logger, requestID, user)`, `ContextWithLogger(ctx, logger)`, `LoggerFromContext(ctx) *slog.Logger`
  - `pkg/observe/logging_test.go` — Unit tests: JSON output structure, attribute attachment, context round-trip (uses `bytes.Buffer` as handler target)
  - `docker-compose.yml` (root) — PostgreSQL 16-alpine on port 5433 + Redis 7-alpine on port 6380, both with healthchecks
  - `go.mod` / `go.sum` updates — `github.com/jackc/pgx/v5` and `github.com/redis/go-redis/v9`
- **Risks / Unknowns:** None — slog is Go stdlib, docker-compose patterns are proven in the spikes.

### Task 2
- **Task ID:** MS-02-T2
- **Title:** PostgreSQL DBManager and Transaction Manager
- **Description:** Promote the validated spike's `DBManager` to production code in `pkg/orm/`, integrating with the config layer and structured logger. Add `User` and `Password` fields to `DatabaseConfig`. Implement `TxManager` with `WithTransaction(ctx, pool, fn)` including panic recovery and nested transaction detection.
- **Why this task exists:** The per-tenant pool registry is the core multitenancy primitive. The transaction manager is required by system schema DDL (T3) and every downstream data operation.
- **Dependencies:** MS-02-T1 (logger)
- **Inputs / References:**
  - `spikes/pg-tenant/main.go` lines 29-206 — validated DBManager blueprint (ForSite, makeSearchPathHook, assertSchema, EvictIdlePools, Close)
  - `docs/blocker-resolution-strategies.md` Blocker 2 lines 66-177 — production implementation spec with double-checked locking detail
  - `MOCA_SYSTEM_DESIGN.md` §4.1 lines 828-850 — schema isolation architecture
  - `internal/config/types.go` — `DatabaseConfig` struct to extend
- **Deliverable:**
  - `internal/config/types.go` update — add `User string` and `Password string` fields to `DatabaseConfig` (env-expanded via `${DB_USER}` / `${DB_PASSWORD}` in moca.yaml)
  - `pkg/orm/postgres.go` — `DBManager` with `systemPool *pgxpool.Pool`, `sitePools map[string]*pgxpool.Pool`, `sync.RWMutex`, `lastUsed sync.Map`. `NewDBManager(ctx, cfg config.DatabaseConfig, logger *slog.Logger)` builds DSN from config fields. `ForSite(ctx, siteName)` with double-checked locking, schema name `tenant_{siteName}`. `SystemPool()`, `makeSearchPathHook(schema)`, `assertSchema(ctx, pool, expected)`, `EvictIdlePools(maxIdle)`, `Close()`
  - `pkg/orm/transaction.go` — `type TxFunc func(ctx context.Context, tx pgx.Tx) error`, `WithTransaction(ctx, pool, fn)` with commit/rollback/panic-recovery, `TxFromContext(ctx)` for nested detection
  - `pkg/orm/postgres_test.go` — Integration tests (`//go:build integration`): concurrent isolation (100 goroutines × 10 tenants), assertSchema verification
  - `pkg/orm/transaction_test.go` — Integration tests: commit-on-success, rollback-on-error, rollback-on-panic
- **Risks / Unknowns:**
  - Per-tenant pool sizing: derive `perTenantMaxConns` from `cfg.PoolSize` with a floor of 2. The divisor (expected tenant count) should default to a reasonable constant (e.g., 10) with a future config field.

### Task 3
- **Task ID:** MS-02-T3
- **Title:** Redis Client Factory and System Schema DDL
- **Description:** Implement the Redis 4-DB client factory in `internal/drivers/` and the idempotent `EnsureSystemSchema` function that creates the 3 `moca_system` tables.
- **Why this task exists:** Redis clients are required by health checks and all downstream caching/queue work. The system schema DDL creates the global tables (`sites`, `apps`, `site_apps`) that tenant management and app registration depend on from MS-03 onward.
- **Dependencies:** MS-02-T1 (logger), MS-02-T2 (TxManager and SystemPool needed for DDL execution)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §5.1 lines 1026-1072 — Redis 4-DB layout, key name patterns
  - `MOCA_SYSTEM_DESIGN.md` §4.2 lines 852-887 — exact DDL for moca_system tables
  - `internal/config/types.go` — `RedisConfig` struct to extend with `Password`
  - `spikes/redis-streams/main.go` — go-redis v9 client construction patterns
- **Deliverable:**
  - `internal/config/types.go` update — add `Password string` field to `RedisConfig` (env-expanded via `${REDIS_PASSWORD}`)
  - `internal/drivers/redis.go` — `RedisClients` struct with `Cache, Queue, Session, PubSub *redis.Client`. `NewRedisClients(cfg config.RedisConfig, logger *slog.Logger) (*RedisClients, error)`. `Ping(ctx) error`. `Close() error`. Key pattern constants (`MetaKey`, `DocKey`, `PermKey`, `SessionKey`, `ConfigKey`)
  - `internal/drivers/redis_test.go` — Integration tests (`//go:build integration`): 4 clients connect to distinct DB indices; write to DB 0, verify invisible on DB 1
  - `pkg/orm/schema.go` — `EnsureSystemSchema(ctx context.Context, pool *pgxpool.Pool, systemSchema string) error`. Uses `CREATE SCHEMA IF NOT EXISTS` + `CREATE TABLE IF NOT EXISTS` for all 3 tables. All DDL in a single `WithTransaction` call. Schema name quoted via `pgx.Identifier.Sanitize()`
  - `pkg/orm/schema_test.go` — Integration tests: idempotency (two calls, no error), column existence via `information_schema`, FK constraints on `site_apps`
- **Risks / Unknowns:**
  - Redis password: defaults to empty string (no auth) for development. Required for production — `RedisConfig.Password` covers this.

### Task 4
- **Task ID:** MS-02-T4
- **Title:** Health Check Endpoints and End-to-End Integration Tests
- **Description:** Implement the three health check HTTP handlers using stdlib `net/http`, then write the comprehensive integration test suite that validates all MS-02 acceptance criteria end-to-end and adds the `test-integration` Makefile target.
- **Why this task exists:** Health checks are the final milestone deliverable and require both PG and Redis. The integration test suite proves all components work together and establishes the CI testing pattern for future milestones.
- **Dependencies:** MS-02-T1, MS-02-T2, MS-02-T3
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §11.3 lines 1763-1770 — health endpoint specs
  - `ROADMAP.md` lines 257-295 — MS-02 acceptance criteria (the complete checklist to satisfy)
- **Deliverable:**
  - `pkg/observe/health.go` — `type Pinger interface { Ping(ctx context.Context) error }`. `HealthChecker` accepting `db Pinger`, `redis Pinger`, `version string`, `logger *slog.Logger`. `NewHealthChecker(...)`. `RegisterRoutes(mux *http.ServeMux)`. Three handlers: `GET /health` (200, `{"status":"ok","version":"..."}` always), `GET /health/live` (200 always), `GET /health/ready` (200 with checks map when healthy; 503 with failed check names when degraded)
  - `pkg/observe/health_test.go` — Unit tests via `httptest.NewRecorder`: /health always 200, /health/live always 200, /health/ready 200 when Pinger.Ping returns nil / 503 when Pinger.Ping returns error
  - `pkg/orm/integration_test.go` — Full end-to-end suite (`//go:build integration`): `TestDBManagerConcurrentIsolation` (100 goroutines × 10 tenants), `TestTransactionCommit`, `TestTransactionRollbackOnError`, `TestTransactionRollbackOnPanic`, `TestSystemSchemaIdempotency`, `TestRedisClientIsolation`, `TestHealthReadyEndToEnd`
  - `Makefile` update — `test-integration` target: `docker compose up -d --wait && go test -race -count=1 -tags=integration ./... ; docker compose down`
- **Risks / Unknowns:**
  - The `Pinger` interface must be satisfied by both `*pgxpool.Pool` (has `Ping(ctx) error`) and `*redis.Client` (has `Ping(ctx) *redis.StatusCmd` — needs a thin wrapper). Wrap Redis `Ping` in a `func(ctx) error` adapter inside `internal/drivers/redis.go`.

## Recommended Execution Order
1. **MS-02-T1** — Structured Logger + CI Docker Infrastructure (no dependencies; unblocks everything)
2. **MS-02-T2** — PostgreSQL DBManager + Transaction Manager (depends on T1)
3. **MS-02-T3** — Redis Client Factory + System Schema DDL (depends on T1, T2)
4. **MS-02-T4** — Health Check Endpoints + End-to-End Integration Tests (depends on T1, T2, T3)

## Open Questions
1. **DB and Redis credentials: RESOLVED** — Add `User` and `Password` to `DatabaseConfig`, `Password` to `RedisConfig`, using `${DB_USER}` / `${DB_PASSWORD}` / `${REDIS_PASSWORD}` env var expansion in `moca.yaml`. Consistent with existing `envexpand.go` pattern. Done in T2/T3 as minor `types.go` additions.
2. **Per-tenant pool sizing** — Recommendation: `perTenantMaxConns = max(cfg.PoolSize/10, 2)` as a sensible default, with the divisor being a named constant. A future config field (`database.per_tenant_max_conns`) can override this in MS-12.

## Out of Scope for This Milestone
- Query builder (MS-03+)
- Migration runner (MS-03+)
- Per-tenant document tables / DDL generation (MS-04)
- Row-Level Security (RLS) policies (MS-12)
- Kafka / transactional outbox (MS-15)
- API router or middleware (MS-06)
- Any frontend or React work
