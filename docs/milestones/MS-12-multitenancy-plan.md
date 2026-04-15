# MS-12 — Multitenancy Plan

## Milestone Summary

- **ID:** MS-12
- **Name:** Multitenancy — Site Resolver Middleware, Per-Site Isolation
- **Roadmap Reference:** ROADMAP.md → MS-12 section (lines 714–750)
- **Goal:** Implement server-side tenant resolution (subdomain/header/path), expanded SiteContext with full tenant metadata, Redis-cached site lookup, disabled-site handling, and background pool eviction for multi-site serving from a single process.
- **Why it matters:** Up to now the server handles a single site. Multitenancy is a core Moca requirement — every downstream milestone assumes per-tenant isolation. This milestone makes the framework usable for real multi-tenant deployments and is the gateway to the Alpha release.
- **Position in roadmap:** Order #13 of 30 milestones (Alpha phase)
- **Upstream dependencies:** MS-06 (REST API — complete), MS-02 (PostgreSQL Foundation — complete), MS-11 (CLI Operational Commands — complete)
- **Downstream dependencies:** MS-13 (CLI App Scaffolding), MS-14 (Permissions), MS-15 (Jobs/Events)

## Vision Alignment

MS-12 transforms Moca from a single-tenant framework into a true multi-tenant platform. The tenant resolution middleware enables a single `moca-server` process to serve any number of independent sites, each with its own database schema, Redis key namespace, and configuration. This is the architectural foundation that every production deployment will use.

The expanded `SiteContext` becomes the central carrier of tenant identity through the request lifecycle — from HTTP ingress through document operations to cache reads. Redis-cached site lookup ensures sub-millisecond overhead per request, while background pool eviction prevents connection exhaustion at scale.

This milestone completes the Server Stream chain at the multitenancy layer: MS-02 (DB) → MS-06 (API) → **MS-12** (Multitenancy) → MS-14 (Permissions). Without it, the permissions system (MS-14) cannot enforce per-tenant access control, and the background jobs system (MS-15) cannot partition work by site.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| ROADMAP.md | MS-12 | 714–750 | Milestone definition, scope, acceptance criteria |
| MOCA_SYSTEM_DESIGN.md | §8.1 Tenant Resolution | 1390–1421 | SiteResolver, ResolutionStrategy, SiteContext spec |
| MOCA_SYSTEM_DESIGN.md | §8.2 Per-Site Isolation | 1423–1433 | Isolation methods per resource type |
| MOCA_SYSTEM_DESIGN.md | §8.3 Site Lifecycle | 1435–1462 | 9-step site creation workflow |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.2 Site Management | 763–843 | Site command specs (already implemented in MS-11) |

## Research Notes

No web research needed. Key findings from codebase exploration:

- **SiteContext is minimal:** `pkg/tenancy/site.go` has only 2 fields (`Pool`, `Name`). Design spec requires 7 fields. Expansion is the foundation for all other tasks.
- **Tenant middleware exists but is incomplete:** `pkg/api/middleware.go` has `tenantMiddleware()` with header and subdomain resolution, but has a **localhost bug** — `acme.localhost:8000` fails because `subdomainFromHost()` requires 3+ domain parts. Path-based resolution (`/sites/{site}/...`) is not implemented.
- **DBSiteResolver is a thin wrapper:** `pkg/api/site_resolver.go` only calls `db.ForSite()` and returns `{Name, Pool}`. No Redis caching, no status checking, no metadata population.
- **Per-site pool registry is production-ready:** `pkg/orm/postgres.go` has `ForSite()` with double-checked locking, `EvictIdlePools()` with `lastUsed` tracking — all tested. Missing: periodic eviction goroutine.
- **Redis key isolation is already in place:** `internal/drivers/redis.go` defines key patterns like `meta:{site}:{doctype}`, and `pkg/meta/registry.go` uses them consistently. Rate limiter also includes site in key (`rl:{site}:{user}` at `pkg/api/ratelimit.go:136`). No Redis key changes needed.
- **Site lifecycle is complete:** `pkg/tenancy/manager.go` has `CreateSite()`, `DropSite()`, `EnableSite()`, `DisableSite()`, `GetSiteInfo()` — all implemented and tested in MS-11.
- **Server wiring exists:** `internal/serve/server.go` instantiates `DBSiteResolver` and passes it to the Gateway. Needs updated constructor signature.
- **Sentinel errors already defined:** `ErrSiteExists` and `ErrSiteNotFound` are in `pkg/tenancy/manager.go`. Need to add `ErrSiteDisabled`.
- **`SiteInfo` struct exists:** `pkg/tenancy/manager.go:50` has `SiteInfo` with `DBSchema`, `Status`, `Config`, `Apps` — can reuse the `GetSiteInfo()` SQL pattern for the resolver.
- **`.Pool` field is widely referenced:** `SiteContext.Pool` is used in ~20 files (document CRUD, validators, naming, tests). Renaming to `.DBPool` would cause unnecessary churn for zero functional gain — keep as-is and add new fields alongside.

## Milestone Plan

### Task 1

- **Task ID:** MS-12-T1
- **Title:** Expand SiteContext and Add ErrSiteDisabled Sentinel
- **Status:** Completed
- **Description:**
  Expand the current 2-field `SiteContext` to carry all per-tenant metadata required by downstream code. Add helper methods for key/index prefixing and the `ErrSiteDisabled` sentinel error.

  **Changes to `pkg/tenancy/site.go`:**

  Current struct:
  ```go
  type SiteContext struct {
      Pool *pgxpool.Pool
      Name string
  }
  ```

  Expanded struct (add fields, keep `Pool` name unchanged):
  - `DBSchema string` — e.g. `"tenant_acme"`, populated from `SchemaNameForSite(Name)`
  - `Status string` — `"active"`, `"disabled"`, etc. Enables middleware to reject disabled sites without extra DB queries
  - `Config map[string]any` — per-site configuration from `moca_system.sites.config` JSONB column
  - `InstalledApps []string` — apps installed on this site, from `moca_system.site_apps`
  - `RedisPrefix string` — key prefix for all Redis operations, e.g. `"acme:"` (trailing colon)
  - `StorageBucket string` — S3/MinIO bucket prefix, e.g. `"acme/"` (trailing slash). Placeholder for MS-21.

  **New sentinel error** (in `site.go`, alongside existing sentinels in `manager.go`):
  - `ErrSiteDisabled = errors.New("site is disabled")` — returned by the resolver when a site's status is `"disabled"`. The HTTP layer translates this to 503 Service Unavailable.

  **Helper methods on `*SiteContext`:**
  - `IsActive() bool` — returns true if Status is `""` (backwards compat for tests) or `"active"`
  - `PrefixRedisKey(key string) string` — prepends RedisPrefix to key
  - `PrefixSearchIndex(index string) string` — returns `"{Name}_{index}"` for Meilisearch

  **New test file: `pkg/tenancy/site_test.go`:**
  - `TestIsActive` — active, disabled, empty status
  - `TestPrefixRedisKey` — verifies `"acme"` site + `"meta:SalesOrder"` → `"acme:meta:SalesOrder"`
  - `TestPrefixSearchIndex` — verifies `"acme"` site + `"SalesOrder"` → `"acme_SalesOrder"`

- **Why this task exists:** Every other task depends on the expanded SiteContext. The resolver (T2) must populate it, the middleware (T2) must check its Status, and the integration tests (T4) must verify all fields. This is the foundation.
- **Dependencies:** None
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §8.1 (lines 1409–1417) — target SiteContext spec
  - `pkg/tenancy/site.go` — current minimal struct
  - `pkg/tenancy/manager.go:32-33` — existing sentinel errors (`ErrSiteExists`, `ErrSiteNotFound`)
  - `pkg/tenancy/manager.go:50-62` — `SiteInfo` struct (field reference for what DB stores)
- **Deliverable:**
  - Modified: `pkg/tenancy/site.go`
  - New: `pkg/tenancy/site_test.go`
- **Acceptance Criteria:**
  - `SiteContext` has all 8 fields (Pool, Name, DBSchema, Status, Config, InstalledApps, RedisPrefix, StorageBucket)
  - `ErrSiteDisabled` is exported and usable with `errors.Is()`
  - All existing code compiles without modification (new fields are additive, `Pool` name unchanged)
  - Helper method unit tests pass
- **Risks / Unknowns:**
  - Existing test code constructs `SiteContext{Name: "test"}` with struct literals — additive fields won't break these since Go allows partial struct initialization.

### Task 2

- **Task ID:** MS-12-T2
- **Title:** Enhance SiteResolver and Tenant Middleware
- **Status:** Completed
- **Description:**
  This is the core task of MS-12. It has 5 sub-parts that together deliver full tenant resolution with three strategies, Redis-cached site lookup, and disabled-site handling.

  **2A: Fix `subdomainFromHost` for `*.localhost` (Bug Fix)**

  File: `pkg/api/middleware.go` (line 151)

  Bug: `acme.localhost:8000` splits to `["acme", "localhost"]` — only 2 parts, so `subdomainFromHost()` returns `""`. The ROADMAP acceptance criterion explicitly requires `acme.localhost:8000` to resolve to site `"acme"`.

  Fix: Add special case after port stripping:
  ```go
  if len(parts) == 2 && parts[1] == "localhost" {
      return parts[0]
  }
  ```

  **2B: Add Path-Based Resolution**

  File: `pkg/api/middleware.go`

  Add helper `siteFromPath(path string) (siteID, strippedPath string)` that matches the pattern `/sites/{site}/...` and returns both the site identifier and the remaining path with the `/sites/{site}` prefix stripped.

  Update `tenantMiddleware` resolution priority:
  1. `X-Moca-Site` header (explicit, highest priority)
  2. Path prefix `/sites/{site}/...` (rewrite `r.URL.Path` to stripped path so downstream handlers see `/api/v1/...`)
  3. Subdomain (lowest priority)

  Path rewriting must happen before the request reaches the version router, which expects paths like `/api/v{n}/resource/...`.

  **2C: Expand DBSiteResolver with Redis Caching**

  File: `pkg/api/site_resolver.go`

  Currently this resolver only calls `db.ForSite()` and returns `{Name, Pool}`. The expanded version must:

  1. Accept `*redis.Client` and `*slog.Logger` in the constructor (signature change from `NewDBSiteResolver(db)` to `NewDBSiteResolver(db, redis, logger)`)
  2. On `ResolveSite(ctx, siteID)`:
     - Check Redis cache at key `site_meta:{siteID}` (TTL 5 min) for JSON-serialized site metadata
     - On cache miss: query `moca_system.sites` joined with `moca_system.site_apps` via system pool. Reuse the SQL pattern from `SiteManager.GetSiteInfo()` at `pkg/tenancy/manager.go:288`
     - Check `status` column: if `"disabled"`, return `tenancy.ErrSiteDisabled`. If not found, return `tenancy.ErrSiteNotFound`
     - Call `db.ForSite(ctx, siteID)` for the pool (existing lazy creation logic, unchanged)
     - Construct the full `SiteContext` with all fields: `DBSchema` from `SchemaNameForSite()`, `RedisPrefix` as `siteName + ":"`, `StorageBucket` as `siteName + "/"`
     - Cache the metadata (not the pool pointer) in Redis with 5-min TTL
  3. If Redis is unavailable, fall through to DB lookup (fail-open pattern, consistent with existing rate limiter behavior)

  **2D: Disabled Site Handling in Middleware**

  File: `pkg/api/middleware.go`

  After `resolver.ResolveSite()` returns an error, check for specific sentinels:
  - `errors.Is(err, tenancy.ErrSiteDisabled)` → HTTP 503 with JSON `{"error":{"code":"SITE_DISABLED","message":"site is under maintenance"}}`
  - `errors.Is(err, tenancy.ErrSiteNotFound)` → HTTP 404 with JSON `{"error":{"code":"TENANT_NOT_FOUND","message":"site not found"}}` (existing behavior, made explicit)
  - Other errors → HTTP 404 (generic, existing behavior)

  **2E: Update Server Wiring**

  File: `internal/serve/server.go` (line 83)

  Change:
  ```go
  siteResolver := api.NewDBSiteResolver(dbManager)
  ```
  To:
  ```go
  siteResolver := api.NewDBSiteResolver(dbManager, redisClients.Cache, logger)
  ```

  **Tests:**

  Extend `pkg/api/middleware_test.go`:
  - `TestSubdomainFromHost_Localhost` — `acme.localhost` and `acme.localhost:8000` both resolve to `"acme"`
  - `TestSiteFromPath` — unit test for path extraction: `/sites/acme/api/v1/resource/X` → `("acme", "/api/v1/resource/X")`
  - `TestTenantMiddleware_PathBased` — full middleware test with path-based URL
  - `TestTenantMiddleware_DisabledSite503` — mock resolver returns `ErrSiteDisabled`, verify 503 response
  - `TestTenantMiddleware_ResolutionPriority` — header beats path beats subdomain

  New file `pkg/api/site_resolver_test.go`:
  - `TestDBSiteResolver_PopulatesFullContext` — mock DB query, verify all SiteContext fields populated
  - `TestDBSiteResolver_RedisCacheHit` — verify Redis read path skips DB
  - `TestDBSiteResolver_RedisCacheMiss` — verify DB query + Redis write
  - `TestDBSiteResolver_DisabledSite` — verify `ErrSiteDisabled` returned
  - `TestDBSiteResolver_NotFound` — verify `ErrSiteNotFound` returned
  - `TestDBSiteResolver_RedisUnavailable` — verify fallback to DB (fail-open)

- **Why this task exists:** This is the core of MS-12. Without tenant resolution, the framework cannot serve multiple sites. The Redis caching ensures per-request overhead is sub-millisecond. The disabled site handling enables the maintenance mode workflow already built in MS-11.
- **Dependencies:** MS-12-T1 (SiteContext expansion, ErrSiteDisabled)
- **Inputs / References:**
  - `pkg/api/middleware.go` — existing `tenantMiddleware()`, `subdomainFromHost()` (lines 111–161)
  - `pkg/api/site_resolver.go` — existing `DBSiteResolver` (34 lines)
  - `pkg/tenancy/manager.go:288-330` — `GetSiteInfo()` SQL pattern to reuse
  - `pkg/tenancy/manager.go:717` — `SchemaNameForSite()` for DBSchema derivation
  - `internal/serve/server.go:83` — current resolver wiring
  - `MOCA_SYSTEM_DESIGN.md` §8.1 (lines 1390–1421) — SiteResolver architecture
- **Deliverable:**
  - Modified: `pkg/api/middleware.go`, `pkg/api/site_resolver.go`, `internal/serve/server.go`
  - New: `pkg/api/site_resolver_test.go`
  - Extended: `pkg/api/middleware_test.go`
- **Acceptance Criteria:**
  - `acme.localhost:8000` resolves to site `"acme"` via subdomain
  - `X-Moca-Site: globex` header resolves to site `"globex"`
  - `/sites/acme/api/v1/resource/SalesOrder` resolves to site `"acme"` with path stripped to `/api/v1/resource/SalesOrder`
  - Header takes priority over path, path takes priority over subdomain
  - Nonexistent site returns 404 with `TENANT_NOT_FOUND` code
  - Disabled site returns 503 with `SITE_DISABLED` code
  - Repeated requests for the same site hit Redis cache (no DB query)
  - Redis unavailability falls back to DB lookup gracefully
- **Risks / Unknowns:**
  - Path-based URL rewriting must not break the version router which expects `/api/v{n}/resource/...` — rewriting happens in middleware before the request reaches downstream handlers
  - Redis cache invalidation on `site disable`/`site enable`: the TTL (5 min) provides eventual consistency. For immediate invalidation, the resolver could subscribe to `pubsub:config:{site}` events — defer to future optimization if needed
  - Constructor signature change for `NewDBSiteResolver` will require updating `internal/serve/server.go` and any test code that constructs the resolver

### Task 3

- **Task ID:** MS-12-T3
- **Title:** Background Pool Eviction Goroutine
- **Status:** Completed
- **Description:**
  Start a periodic background goroutine in the server that calls the already-implemented `DBManager.EvictIdlePools()` to close idle tenant database pools, preventing connection exhaustion at scale.

  **File: `internal/serve/server.go`** — in `Run()` method

  `EvictIdlePools(maxIdle time.Duration) int` already exists at `pkg/orm/postgres.go:160` and is fully tested in `pkg/orm/postgres_test.go:312`. It scans `lastUsed` timestamps, closes idle pools, and removes them from the pool map. What's missing is calling it periodically from the server.

  Add before the `select` block in `Run()`:
  ```go
  evictTicker := time.NewTicker(5 * time.Minute)
  defer evictTicker.Stop()
  go func() {
      for {
          select {
          case <-evictTicker.C:
              n := s.dbManager.EvictIdlePools(30 * time.Minute)
              if n > 0 {
                  s.logger.Info("evicted idle tenant pools", slog.Int("count", n))
              }
          case <-ctx.Done():
              return
          }
      }
  }()
  ```

  Hardcoded defaults (5-min tick, 30-min idle threshold) are appropriate for MS-12. Configurability can be added in a future milestone if needed.

- **Why this task exists:** Without periodic eviction, a server handling many tenants will accumulate database connection pools that are no longer in use, eventually exhausting `max_connections`. The eviction logic is implemented and tested — this task just wires it into the server lifecycle.
- **Dependencies:** None (can run in parallel with MS-12-T2)
- **Inputs / References:**
  - `pkg/orm/postgres.go:155-180` — `EvictIdlePools()` implementation
  - `pkg/orm/postgres_test.go:312-350` — existing eviction tests
  - `internal/serve/server.go:142-171` — `Run()` method where goroutine should be added
  - `docs/adr/ADR-001-pg-tenant-isolation.md:189` — ADR recommending periodic eviction every 5 min
- **Deliverable:**
  - Modified: `internal/serve/server.go`
- **Acceptance Criteria:**
  - Server starts eviction goroutine alongside HTTP listener
  - Goroutine stops cleanly on context cancellation (graceful shutdown)
  - Idle pools (>30 min unused) are closed and logged
  - No new tests needed — eviction logic is already unit-tested; goroutine lifecycle is validated by existing server shutdown tests
- **Risks / Unknowns:**
  - Race condition: `EvictIdlePools` acquires write lock on `sitePools` — already safe via `sync.RWMutex` in DBManager
  - Eviction during active request: `ForSite()` marks `lastUsed` on every access, so actively-used pools won't be evicted

### Task 4

- **Task ID:** MS-12-T4
- **Title:** Multi-Site Integration Tests
- **Status:** Completed
- **Description:**
  End-to-end acceptance criteria validation with two fully isolated tenants. These tests verify every ROADMAP acceptance criterion and ensure multi-site data isolation works through the full HTTP stack.

  **New file: `pkg/api/multitenancy_integration_test.go`**

  Build tag: `//go:build integration`

  **Test setup (TestMain or shared helper):**
  1. Start PostgreSQL + Redis via test containers (same pattern as existing integration tests in `pkg/api/api_integration_test.go`)
  2. Create system schema (`moca_system`) with `sites` and `site_apps` tables
  3. Register two sites: `acme` (status: `"active"`) and `globex` (status: `"active"`)
  4. Create tenant schemas `tenant_acme` and `tenant_globex` with bootstrap MetaTypes (at minimum a `SalesOrder` table)
  5. Instantiate full server stack: `DBManager`, `RedisClients`, `Registry`, `DocManager`, `Gateway` with enhanced `DBSiteResolver`
  6. Start `httptest.Server`

  **Test cases (map directly to ROADMAP acceptance criteria):**

  | Test | Acceptance Criterion | Method |
  |------|---------------------|--------|
  | `TestMultitenancy_SubdomainResolution` | AC: `acme.localhost:8000/api/v1/resource/SalesOrder` resolves to "acme" | Set `Host: acme.localhost:8000`, verify 200 and correct site |
  | `TestMultitenancy_HeaderResolution` | AC: `X-Moca-Site: globex` resolves to "globex" | Set header, verify 200 and correct site |
  | `TestMultitenancy_PathResolution` | `/sites/acme/api/v1/resource/SalesOrder` resolves + path stripped | Verify 200, response shows acme data |
  | `TestMultitenancy_DataIsolation` | AC: SalesOrder on "acme" does not appear on "globex" | POST SalesOrder on acme, GET list on globex → empty |
  | `TestMultitenancy_RedisPrefixIsolation` | AC: Redis keys prefixed with site name | Trigger cache write on acme, verify Redis key starts with `acme:` |
  | `TestMultitenancy_NonexistentSite404` | AC: Nonexistent site returns 404 | `X-Moca-Site: noexist` → 404 + `TENANT_NOT_FOUND` JSON error |
  | `TestMultitenancy_DisabledSite503` | AC: Disabled site returns 503 | Update globex status to `"disabled"` in DB, clear Redis cache, request → 503 + `SITE_DISABLED` |
  | `TestMultitenancy_ResolutionPriority` | Header beats subdomain | Set both `X-Moca-Site: globex` and `Host: acme.localhost`, verify globex is used |

- **Why this task exists:** Integration tests are the definitive proof that multitenancy works end-to-end. Without them, subtle isolation bugs (data leaking between tenants) could ship undetected. Each test maps to a specific ROADMAP acceptance criterion.
- **Dependencies:** MS-12-T1, MS-12-T2, MS-12-T3 (requires all prior tasks)
- **Inputs / References:**
  - `pkg/api/api_integration_test.go` — existing integration test setup pattern, `staticSiteResolver`, test helpers
  - `pkg/tenancy/manager_integration_test.go` — site creation/deletion test setup
  - `ROADMAP.md` MS-12 acceptance criteria (lines 729–734)
  - `docker-compose.yml` — PostgreSQL (port 5433) and Redis for integration tests
- **Deliverable:**
  - New: `pkg/api/multitenancy_integration_test.go`
- **Acceptance Criteria:**
  - All 8 test cases pass with `make test-integration`
  - Tests use real PostgreSQL and Redis (via docker-compose)
  - Tests clean up created schemas and sites in teardown
  - No flaky tests from concurrent pool creation
- **Risks / Unknowns:**
  - Test setup complexity: creating two fully bootstrapped sites with MetaTypes requires either using `SiteManager.CreateSite()` (which does the full 9-step lifecycle) or manually creating schemas + tables (simpler but less realistic). Recommend: use `SiteManager.CreateSite()` for realism.
  - Redis cache TTL (5 min) in disabled-site test: either bypass cache by direct DB update + cache delete, or use a short TTL for tests
  - `httptest.Server` uses `127.0.0.1` as host — subdomain test must override `Host` header manually

## Recommended Execution Order

1. **MS-12-T1** (SiteContext Expansion) — Foundation for everything else. No dependencies, small scope. Start here.
2. **MS-12-T2** (Resolver + Middleware) — Core of the milestone. Depends on T1. Largest task.
3. **MS-12-T3** (Pool Eviction) — Independent of T2. Can run in parallel with T2 after T1 completes.
4. **MS-12-T4** (Integration Tests) — Depends on all prior tasks. Must run last.

```
T1 (SiteContext)
  └──→ T2 (Resolver + Middleware)  ──┐
  └──→ T3 (Pool Eviction)         ──┤
                                     └──→ T4 (Integration Tests)
```

**Estimated scope:** ~800-1000 lines of new/modified Go code across 8 files, plus ~400 lines of tests.

## Existing Code to Reuse

| What | Where | How |
|------|-------|-----|
| Schema name derivation | `pkg/tenancy/manager.go:717` `SchemaNameForSite()` | Populate `SiteContext.DBSchema` |
| Site metadata SQL | `pkg/tenancy/manager.go:288` `GetSiteInfo()` | Reuse query pattern in expanded resolver |
| Lazy pool creation | `pkg/orm/postgres.go:93` `ForSite()` | Called by resolver, unchanged |
| Pool eviction | `pkg/orm/postgres.go:160` `EvictIdlePools()` | Called periodically by new goroutine |
| Context helpers | `pkg/api/context.go` `WithSite()`/`SiteFromContext()` | Unchanged, already propagate `*SiteContext` |
| Rate limiter site key | `pkg/api/ratelimit.go:136` `rateLimitKey()` | Already includes site — no changes needed |
| Redis meta cache keys | `pkg/meta/registry.go` / `internal/drivers/redis.go` | Already include `{site}` — no changes needed |
| Integration test setup | `pkg/api/api_integration_test.go` | Reuse DI pattern and test helpers |

## Open Questions

1. **Redis cache invalidation on `site disable`/`site enable`:** The 5-min TTL provides eventual consistency. Should the resolver also delete the cache key in `DisableSite()`/`EnableSite()` for immediate effect? Or is the TTL acceptable for v1?

2. **SiteContext.Config type:** The plan uses `map[string]any` (matching the existing `SiteInfo.Config` field). Should this be a typed struct (`SiteConfig` with `Timezone`, `Language`, `Currency` fields) for better ergonomics, or keep it generic for flexibility?

3. **Path-based resolution path rewriting:** When path `/sites/acme/api/v1/resource/X` is stripped to `/api/v1/resource/X`, should the original path be preserved in a header (e.g. `X-Original-Path`) for logging/debugging purposes?

## Out of Scope for This Milestone

- Row-Level Security (RLS) policies — deferred to MS-14 (Permissions)
- Kafka partition key isolation — deferred to MS-15 (Jobs/Events)
- S3/MinIO bucket isolation — placeholder field in SiteContext, actual implementation in MS-21
- Meilisearch index isolation — placeholder helper method, actual implementation in MS-15
- Per-site pool size configuration — hardcoded `systemPoolSize / 10` (min 2), configurable in future
- SiteContext propagation through background goroutines — handled by job queue in MS-15
