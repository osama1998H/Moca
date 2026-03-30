# MOCA Roadmap

## Planning Summary

| Metric | Value |
|--------|-------|
| **Total milestones** | 30 (MS-00 through MS-29) |
| **Milestones to v1.0** | 27 (MS-00 through MS-26) |
| **Post-v1.0 deferred** | 3 (MS-27, MS-28, MS-29) |
| **Major streams** | Backend Core, CLI, Frontend, Operations |
| **Estimated duration to v1.0** | ~72 weeks (18 months) with 2-4 devs; ~12-14 months wall-clock with parallelism |

### Critical Blockers (Resolved)

Each blocker below has been traced to its source in the design documents and assigned a concrete solution strategy. See `docs/blocker-resolution-strategies.md` for full technical details.

1. **Go workspace (`go.work`) composition** -- validated in MS-00 Spike 3.
   - **Risk:** Go's Minimal Version Selection (MVS) picks the highest version globally across all workspace modules. Two apps requiring incompatible major versions of the same dependency cause build failure. Patch-version differences are silently resolved but may cause runtime bugs if APIs changed.
   - **Solution:** MS-00 Spike 3 must test with intentional dependency conflicts (not just a happy-path compile). MS-13 (`moca app get`) must validate dependency compatibility before adding a module to `go.work`. Use `replace` directives in `go.work` as escape hatch.
   - **Source:** `MOCA_SYSTEM_DESIGN.md` lines 1380-1384 (build composition model); `MOCA_CLI_SYSTEM_DESIGN.md` lines 2287-2296 (`moca build server`).

2. **PostgreSQL schema-per-tenant isolation** -- validated in MS-00 Spike 1.
   - **Risk:** `SET search_path` is a per-connection setting. pgxpool reuses connections across goroutines. Without explicit per-acquisition reset, goroutine B can inherit goroutine A's search_path and query the wrong tenant's data. Prepared statement caches may also leak across schemas.
   - **Solution:** Use pgxpool's `BeforeAcquire` callback to reset `search_path` to `public` before every connection acquisition. Then set the correct tenant schema explicitly before each query via a `TenantConn` wrapper. Disable or namespace prepared statement caching per-schema.
   - **Source:** `MOCA_SYSTEM_DESIGN.md` lines 830-832 (schema-per-tenant), line 1414 (`DBPool *pgxpool.Pool` in SiteContext), lines 2063-2067 (ADR-001).

3. **Config sync contract** (YAML on disk vs DB at runtime) -- implemented in MS-11.
   - **Risk:** Five data-loss scenarios: (a) server crash between YAML write and DB commit, (b) DB commit succeeds but cache invalidation event fails, (c) concurrent `moca config set` from two sessions races on filesystem, (d) `moca deploy update` partially syncs some sites but not others, (e) YAML corruption blocks all CLI config operations.
   - **Solution:** (a) Atomic YAML writes via temp-file + POSIX rename; DB write in same error scope with YAML rollback on failure. (b) Use transactional outbox for cache invalidation events (same TX as config update). (c) Redis distributed lock (`moca:config:lock:{site}`) serializes concurrent writes. (d) Single DB transaction for all sites during deploy sync. (e) `moca config verify` command to detect and repair YAML/DB divergence.
   - **Source:** `MOCA_SYSTEM_DESIGN.md` lines 1060-1072 (§5.1.1 Config Sync Contract); `MOCA_CLI_SYSTEM_DESIGN.md` lines 1659-1692 (config get/set commands).

4. **Kafka-optional fallback** (Redis pub/sub) -- implemented in MS-15.
   - **Risk:** When `kafka.enabled=false`, 3 features become completely unavailable (CDC, event replay, multi-consumer fan-out) and 6 features degrade silently (audit streaming, document events become fire-and-forget, webhook ordering lost, search sync becomes blocking, notifications lose buffering, workflow transitions not durable).
   - **Solution:** Three-layer detection: (a) Startup-time validation -- `moca-search-sync` exits with fatal error if Kafka disabled; `moca-server` prints a feature-matrix banner listing unavailable/degraded features. (b) Request-time checks -- CDC and event replay endpoints return explicit errors with documentation links. (c) Observable metrics -- `moca_kafka_enabled` Prometheus gauge (0/1) plus per-feature availability metrics for alerting.
   - **Source:** `MOCA_SYSTEM_DESIGN.md` lines 1235-1260 (§6.5 Kafka-Optional Architecture); `MOCA_CLI_SYSTEM_DESIGN.md` lines 585-586 (`--no-kafka` flag).

### Assumptions
- Team of 2-4 Go developers, 1-2 frontend (React/TypeScript) developers.
- PostgreSQL 16+, Redis 7+, Go 1.22+ available in all environments.
- Kafka and Meilisearch available for production; optional for development.
- GitHub Actions for CI; GoReleaser for release binaries.
- All design decisions from `MOCA_SYSTEM_DESIGN.md` ADR section (lines 2061-2105) are accepted as-is.
- The 30 mismatches documented in `docs/moca-cross-doc-mismatch-report.md` are resolved and their resolutions are authoritative.

---

## Milestone Dependency Graph

```
MS-00 (Spikes)
  └─► MS-01 (Project Structure & Config)
        ├─► MS-02 (PostgreSQL & Redis Foundation)
        │     ├─► MS-03 (Metadata Registry)
        │     │     ├─► MS-04 (Document Runtime)
        │     │     │     ├─► MS-05 (Query Engine)
        │     │     │     │     └─► MS-06 (REST API Layer) ◄─── MS-04
        │     │     │     │           ├─► MS-10 (Dev Server & Hot Reload) ◄─── MS-08
        │     │     │     │           ├─► MS-12 (Multitenancy) ◄─── MS-02
        │     │     │     │           │     └─► MS-15 (Jobs, Events, Search) ◄─── MS-04, MS-06
        │     │     │     │           │           ├─► MS-16 (CLI Queue/Events/Search) ◄─── MS-07
        │     │     │     │           │           ├─► MS-18 (API Keys, Webhooks) ◄─── MS-14
        │     │     │     │           │           ├─► MS-21 (Deployment) ◄─── MS-10
        │     │     │     │           │           ├─► MS-22 (Security Hardening) ◄─── MS-14
        │     │     │     │           │           └─► MS-24 (Observability) ◄─── MS-06
        │     │     │     │           ├─► MS-14 (Permission Engine) ◄─── MS-08
        │     │     │     │           │     └─► MS-17 (React Desk) ◄─── MS-06
        │     │     │     │           │           ├─► MS-19 (Desk Real-Time) ◄─── MS-15
        │     │     │     │           │           └─► MS-20 (GraphQL, Dashboard, i18n) ◄─── MS-05
        │     │     │     │           └─► MS-27 (Portal SSR) [post-v1.0]
        │     │     │     └─► MS-08 (Hook Registry & App System)
        │     │     │           └─► MS-09 (CLI Site/App Commands) ◄─── MS-07
        │     │     │                 ├─► MS-11 (CLI DB/Backup/Config)
        │     │     │                 └─► MS-13 (CLI App Scaffold, Users, Build)
        │     │     └─► MS-23 (Workflow Engine) ◄─── MS-14, MS-15, MS-17
        │     └─► MS-12
        └─► MS-07 (CLI Foundation)
              └─► MS-09, MS-16

MS-25 (Testing Framework) ◄─── MS-23
  └─► MS-26 (Documentation & Packaging) ──► v1.0 Release

Post-v1.0:
  MS-28 (VirtualDoc, CDC, Advanced) ◄─── v1.0
  MS-29 (WASM Plugin Marketplace) ◄─── v1.0
```

### Critical Path (longest dependency chain)
```
MS-00 → MS-01 → MS-02 → MS-03 → MS-04 → MS-06 → MS-12 → MS-15 → MS-23 → MS-25 → MS-26 → v1.0
```

---

## Milestones

---

### MS-00: Architecture Validation Spikes and Project Scaffold

- **Goal:** Validate the 5 highest-risk architectural assumptions and establish the Go project skeleton with CI.
- **Why now:** Every subsequent milestone depends on these assumptions. Discovering a fundamental flaw after building 10 milestones would be catastrophic.
- **Scope:**
  - IN: Go module init, `go.work` skeleton, CI pipeline (lint, test, build), 5 spike prototypes (throwaway code), ADR documentation for spike outcomes.
  - OUT: No production code, no CLI, no frontend.
- **Deliverables:**
  1. Root `go.mod` for `github.com/moca-framework/moca`
  2. `go.work` file composing framework + a stub `apps/core` module
  3. 5 spike directories under `spikes/` with self-contained validation code
  4. CI pipeline (GitHub Actions) with `go build ./...`, `go test ./...`, `golangci-lint`
  5. ADR documents for each spike decision
- **Spikes:**
  1. **PostgreSQL schema-per-tenant isolation** -- `SET search_path` via pgxpool under concurrent access
  2. **Redis Streams consumer groups** -- verify franz-go or go-redis supports needed semantics
  3. **Go workspace (`go.work`) composition** -- multiple app modules compile into single binary
  4. **Meilisearch multi-index** -- prefix-based tenant isolation, bulk indexing performance
  5. **Cobra CLI extension discovery** -- apps register commands via `init()` at build time
- **Acceptance Criteria:**
  - `go build ./...` succeeds from workspace root
  - Spike 1 (PostgreSQL): Two concurrent goroutines using different tenant schemas via pgxpool never cross-contaminate data. `BeforeAcquire` callback resets `search_path` before every acquisition. Prepared statement cache does not leak across schemas.
  - Spike 2 (Redis Streams): Enqueues 100 jobs, consumes with consumer group, acknowledges all. Dead-letter queue receives failed jobs after 3 retries.
  - Spike 3 (go.work): Two app modules with **intentionally conflicting dependency versions** produce a documented resolution (either clean build with MVS, or documented `replace` directive strategy). Happy-path: two modules compile into single binary. Conflict-path: build failure is caught and a resolution strategy is documented in ADR.
  - Spike 4 (Meilisearch): Creates index with tenant prefix, indexes 1000 docs, searches with typo tolerance, verifies tenant isolation (site A's docs invisible to site B's index).
  - Spike 5 (Cobra extension): Command registered via `init()` in stub app appears in root command tree.
- **Dependencies:** None
- **Risks:**
  - pgxpool `search_path` with prepared statements: solution is `BeforeAcquire` reset + per-query explicit `SET search_path` + schema-namespaced statement cache
  - Go workspace dependency conflicts: solution is MVS (Minimal Version Selection) for minor versions + `replace` directives in `go.work` for major conflicts + `moca app get` pre-install compatibility check
- **Suggested Implementation Order:**
  1. Initialize `go.mod`, `go.work`, CI pipeline
  2. Spike 1 (PostgreSQL) + Spike 2 (Redis) in parallel
  3. Spike 3 (go.work) + Spike 4 (Meilisearch) in parallel
  4. Spike 5 (Cobra extension) -- depends on Spike 3
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §4.1-4.2 Database Per Tenant & Core System Tables / lines 828-887
  - `MOCA_SYSTEM_DESIGN.md` / §5.2 Queue Layer (Redis Streams) / lines 1073-1107
  - `MOCA_SYSTEM_DESIGN.md` / §15 Framework Package Layout / lines 1924-2057
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §8 Extension System / lines 3363-3406

---

### MS-01: Project Structure, Configuration, and Go Module Layout

- **Goal:** Establish the canonical directory structure, `moca.yaml` parsing, configuration resolution, and the 5 `cmd/` entry points as empty stubs.
- **Why now:** Every file written from this point needs a home. The configuration system is read by nearly every other package.
- **Scope:**
  - IN: 5 `cmd/` stubs (`moca-server`, `moca-worker`, `moca-scheduler`, `moca`, `moca-outbox`), full `pkg/` directory tree, `moca.yaml` parser and validator, config resolution (project -> common_site -> site), env var expansion.
  - OUT: No actual functionality in any binary.
- **Deliverables:**
  1. Complete `pkg/` directory tree matching §15 with `doc.go` placeholders
  2. `internal/config/` package: YAML parser, validator, defaults, env var expansion
  3. `internal/config/moca_yaml.go`: Typed struct for full `moca.yaml` schema
  4. Unit tests for config parsing, merging, env var expansion, validation
  5. Five `cmd/` entry points that parse `moca.yaml` and print resolved config
- **Acceptance Criteria:**
  - `go build ./cmd/...` produces 5 binaries
  - Valid `moca.yaml` returns fully typed `ProjectConfig` struct
  - Missing required fields produce clear validation errors with field paths
  - `${MOCA_MEILI_KEY}` in YAML is replaced by env var value
  - Malformed YAML produces user-friendly error, not Go stack trace
  - `staging:` section with `inherits: production` correctly merges production defaults with staging overrides
- **Dependencies:** MS-00
- **Risks:** Config struct may need frequent changes; design for extensibility.
- **Suggested Implementation Order:**
  1. Directory tree with `doc.go` placeholders
  2. `moca.yaml` typed struct
  3. YAML parsing with `gopkg.in/yaml.v3`
  4. Validation layer
  5. Env var expansion
  6. Five `main.go` stubs
  7. Tests
- **Design References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §3 Project Structure / lines 155-200
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §3.1 Project Manifest (moca.yaml) / lines 202-313
  - `MOCA_SYSTEM_DESIGN.md` / §15 Framework Package Layout / lines 1924-2057

---

### MS-02: PostgreSQL Foundation and Redis Connection Layer

- **Goal:** Implement the DB connection pool manager (per-tenant schema isolation), Redis client manager (4 logical DBs), system schema DDL, and basic health checks.
- **Why now:** MetaType storage (MS-03) and document storage (MS-04) both depend on a working database layer.
- **Scope:**
  - IN: `pkg/orm/postgres.go` (pgxpool, per-site `search_path`), `pkg/orm/transaction.go`, Redis client factory for 4 DBs, `moca_system` schema DDL (3 tables), health checks, structured logging.
  - OUT: No query builder, no migration runner, no document tables.
- **Deliverables:**
  1. `pkg/orm/postgres.go`: `DBManager` with per-site connection pools and schema isolation
  2. `pkg/orm/transaction.go`: `TxManager` with `WithTransaction(ctx, fn)` pattern
  3. `internal/drivers/redis.go`: Redis client factory for 4 DBs (cache=0, queue=1, session=2, pubsub=3)
  4. `pkg/orm/schema.go`: DDL for `moca_system` schema (sites, apps, site_apps tables)
  5. `pkg/observe/logging.go`: Structured logger (slog-based) with site/user/request_id fields
  6. `pkg/observe/health.go`: `/health`, `/health/ready`, `/health/live` endpoints
  7. Integration tests against real PostgreSQL and Redis (docker-compose for CI)
- **Acceptance Criteria:**
  - `DBManager.ForSite("acme")` returns pool executing against `tenant_acme` schema
  - Two goroutines using pools for different sites do not cross-contaminate
  - `TxManager.WithTransaction` commits on success, rolls back on error/panic
  - Redis clients for DB 0 and DB 1 are distinct connections
  - System tables are created idempotently
  - `/health/ready` returns 200 when PG+Redis are up, 503 when either is down
- **Dependencies:** MS-01
- **Risks:**
  - pgxpool `AfterConnect` hook for `SET search_path` -- verify with prepared statements
  - Connection pool sizing per tenant at scale
- **Suggested Implementation Order:**
  1. Structured logging
  2. PostgreSQL pool manager with schema isolation
  3. Transaction manager
  4. System schema DDL
  5. Redis client factory
  6. Health check endpoints
  7. Integration tests
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §4.1-4.2 Database Per Tenant & Core System Tables / lines 828-887
  - `MOCA_SYSTEM_DESIGN.md` / §5.1 Caching Layer / lines 1026-1072
  - `MOCA_SYSTEM_DESIGN.md` / §11.2-11.3 Logging & Health Checks / lines 1749-1770

---

### MS-03: Metadata Registry -- MetaType, FieldDef, Compiler, Redis Cache

- **Goal:** Implement MetaType and FieldDef types, the schema compiler (JSON -> validated MetaType), the in-memory + Redis-backed metadata registry, and the schema migrator (MetaType diff -> DDL).
- **Why now:** MetaType is the foundational primitive. Document Runtime (MS-04), API layer (MS-06), and permission engine (MS-14) all depend on compiled MetaType definitions.
- **Scope:**
  - IN: `pkg/meta/metatype.go`, `pkg/meta/fielddef.go` (33 FieldTypes), `pkg/meta/compiler.go`, `pkg/meta/registry.go` (L1 in-memory / L2 Redis / L3 PostgreSQL), `pkg/meta/migrator.go` (diff -> ALTER TABLE DDL), `tab_doctype` table.
  - OUT: No hot reload (MS-10), no API route generation, no search index config.
- **Deliverables:**
  1. `MetaType` struct with all fields from §3.1.1
  2. `FieldDef` struct, all 33 FieldType constants, `NamingStrategy` enum
  3. `Compile(jsonBytes) -> (*MetaType, error)` with validation
  4. `Registry.Get(site, doctype) -> *MetaType` with 3-tier cache hierarchy
  5. `Migrator.Diff(current, desired) -> []DDLStatement` and `Apply(site, statements)`
  6. Column type mapping for all 33 FieldTypes (Data->TEXT, Int->INTEGER, Float->NUMERIC(18,6), Currency->NUMERIC(18,6), Date->DATE, Datetime->TIMESTAMPTZ, Check->BOOLEAN, JSON->JSONB, etc.)
  7. Every generated table includes standard columns (name, owner, creation, modified, modified_by, docstatus, idx, workflow_state, _extra, _user_tags, _comments, _assign, _liked_by)
- **Acceptance Criteria:**
  - Valid `SalesOrder.json` compiles without errors
  - Invalid definition (missing name, unknown field_type) produces specific validation errors
  - `Registry.Get` checks in-memory -> Redis (`meta:acme:SalesOrder`) -> PostgreSQL
  - `Migrator.Diff` generates correct DDL for: add field, remove field, change type, add index
  - Column mapping verified for all storable FieldTypes
  - `tab_audit_log` is generated with `PARTITION BY RANGE (timestamp)` for time-based partitioning
- **Dependencies:** MS-02
- **Risks:**
  - JSONB `_extra` indexing strategy (start without GIN, add when needed)
  - Geolocation column type (start with JSON, revisit for PostGIS)
- **Suggested Implementation Order:**
  1. FieldDef and FieldType constants
  2. MetaType struct
  3. NamingStrategy
  4. Compiler (JSON parsing + validation)
  5. DDL generation (column type mapping)
  6. Migrator (diff logic)
  7. Registry with cache hierarchy
  8. Tests
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §3.1.1-3.1.3 MetaType, FieldDef, Metadata Lifecycle / lines 133-296
  - `MOCA_SYSTEM_DESIGN.md` / §4.3-4.4 Per-Tenant Schema & _extra JSONB Pattern / lines 889-1022

---

### MS-04: Document Runtime -- DynamicDoc, Lifecycle Engine, Naming, Validation

- **Goal:** Implement the Document interface, DynamicDoc (map-backed), 18-event lifecycle engine, naming engine (6 strategies), and field-level validation.
- **Why now:** The Document is what users interact with. The API layer (MS-06) generates REST endpoints for Document CRUD.
- **Scope:**
  - IN: `pkg/document/document.go` (Document interface, DynamicDoc), `pkg/document/lifecycle.go` (18 events, dispatcher), `pkg/document/naming.go` (AutoIncrement, Pattern, ByField, ByHash, UUID, Custom), `pkg/document/validator.go` (required, type coercion, regex, min/max, unique, link), `pkg/document/controller.go` (controller resolution). CRUD: Insert, Update, Delete, Get, GetList.
  - OUT: No VirtualDoc (MS-28), no workflow integration, no version tracking.
- **Deliverables:**
  1. Document interface and DynamicDoc with Get/Set/IsNew/IsModified/ModifiedFields/AsMap/ToJSON, child table support
  2. `LifecycleManager.Execute(event, doc)` dispatching BeforeInsert->...->AfterSave in correct order with TX management
  3. `NamingEngine.GenerateName(site, meta, doc)` for all 6 strategies (PG sequences for pattern naming)
  4. Field-level validation for all storable FieldTypes
  5. `DocContext` struct with Site, User, Flags, TX, EventBus
  6. CRUD operations: Insert, Update, Delete, Get, GetList
  7. Integration tests: full lifecycle with hooks, naming, validation
- **Acceptance Criteria:**
  - `Insert("SalesOrder", values)` triggers all lifecycle events in order
  - Pattern naming "SO-.####" generates "SO-0001", "SO-0002" (via PG sequence), thread-safe
  - Required field validation returns `"field X is required"`
  - Type coercion converts string "42" to int 42 for Int fields
  - Link validation checks referenced document exists
  - Child table: AddChild, reorder by idx, cascade delete
- **Dependencies:** MS-03
- **Risks:**
  - PG sequences per (site, doctype) must be created dynamically
  - Custom naming functions: use `map[string]NamingFunc` registry
- **Suggested Implementation Order:**
  1. Document interface and DynamicDoc
  2. DocContext
  3. Naming engine (UUID first, then Pattern, then others)
  4. Field-level validator
  5. Lifecycle event dispatcher
  6. CRUD operations
  7. Controller resolution
  8. Integration tests
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §3.2.1-3.2.3 Document Runtime / lines 300-462
  - `MOCA_SYSTEM_DESIGN.md` / §3.5 Hook Registry (DocLifecycle events) / lines 714-781

---

### MS-05: Query Engine and Report Foundation

- **Goal:** Implement the dynamic query builder with 15 filter operators, `_extra` JSONB transparency, Link field auto-joins, pagination, and the ReportDef structure.
- **Why now:** GetList from MS-04 uses hardcoded SQL. The query builder enables the API layer (MS-06) to translate URL parameters into safe, parameterized SQL.
- **Scope:**
  - IN: `pkg/orm/query.go` (QueryBuilder with 15 operators), transparent `_extra` JSONB access, Link field auto-joins, ORDER BY, GROUP BY, LIMIT/OFFSET, `pkg/orm/report.go` (ReportDef, QueryReport execution).
  - OUT: No ScriptReport, no full-text search operator (depends on MS-15).
- **Deliverables:**
  1. `QueryBuilder.For(doctype).Fields(...).Filters(...).OrderBy(...).Limit(...).Build()` returning `(sql, args)`
  2. All 15 operators: `=`, `!=`, `>`, `<`, `>=`, `<=`, `like`, `not like`, `in`, `not in`, `between`, `is null`, `is not null`, `@>` (JSONB), `@@` (full-text)
  3. Transparent `_extra` field access with type casting
  4. Link field auto-join (filtering on `customer_name.territory` generates JOIN)
  5. Pagination: offset-based with total count
  6. `ReportDef` struct and QueryReport execution
  7. SQL injection protection: all values parameterized, field names validated against MetaType
- **Acceptance Criteria:**
  - Simple filter generates correct parameterized SQL
  - Filtering on `_extra` field generates `_extra->>'custom_field' = $1`
  - Link field filter generates correct JOIN
  - `in` operator with list generates `status IN ($1, $2)`
  - Filtering on non-existent field returns error
  - QueryReport executes safely with parameter binding
- **Dependencies:** MS-03, MS-04
- **Risks:**
  - JSONB type casting for numeric comparisons
  - Deeply nested joins (limit to depth 2)
- **Suggested Implementation Order:**
  1. Basic QueryBuilder structure
  2. Simple operators
  3. Parameterized SQL generation
  4. Field validation against MetaType
  5. `_extra` JSONB transparency
  6. Link field auto-joins
  7. Pagination
  8. ReportDef and QueryReport
  9. Tests
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §10.1-10.2 Query Engine & Report Builder / lines 1654-1725

---

### MS-06: REST API Layer -- Auto-Generated CRUD, Middleware, Rate Limiting

- **Goal:** Implement auto-generated REST API for any MetaType, middleware chain, request/response transformers, API versioning, and rate limiting. First externally-usable surface.
- **Why now:** After this milestone, a developer defines a MetaType JSON and the framework generates a full REST API. Unblocks the React frontend (MS-17).
- **Scope:**
  - IN: `pkg/api/gateway.go` (router, middleware chain), `pkg/api/rest.go` (CRUD endpoints), `pkg/api/transformer.go` (field mapping, exclusion, computed fields), `pkg/api/version.go` (versioning), `pkg/api/ratelimit.go` (Redis sliding window), CORS, request ID, audit log.
  - OUT: No GraphQL (MS-20), no API keys (MS-18), no OAuth2 (MS-22), no webhooks (MS-15).
- **Deliverables:**
  1. HTTP router with middleware chain (request ID -> CORS -> rate limit -> [auth placeholder] -> version router -> handler)
  2. Auto-generated endpoints: `GET/POST/PUT/DELETE /api/v1/resource/{doctype}/{name}`, `GET /api/v1/meta/{doctype}`
  3. Request/response transformers: APIAlias mapping, field exclusion, read-only enforcement
  4. API versioning: `/api/v1/`, `/api/v2/` with per-version transformers
  5. Redis-backed sliding window rate limiter (per-user, per-tenant)
  6. Audit log writes to `tab_audit_log` on every mutation
  7. `cmd/moca-server/main.go` wired to start HTTP server
  8. Integration tests: full CRUD via HTTP
- **Acceptance Criteria:**
  - `POST /api/v1/resource/SalesOrder` with valid JSON returns 201
  - `GET /api/v1/resource/SalesOrder?filters=[["status","=","Draft"]]&limit=10` returns paginated results
  - Fields with `in_api: false` excluded from responses
  - Rate limiting returns 429 after exceeding limit
  - Every mutation writes to `tab_audit_log`
  - `moca-server` binary starts and serves the API
- **Dependencies:** MS-04, MS-05
- **Risks:**
  - Auth is placeholder until MS-14; need clean interface for swapping
  - API versioning design: copy-on-write transformers vs explicit handlers
- **Suggested Implementation Order:**
  1. HTTP router skeleton with middleware chain
  2. Request ID and CORS middleware
  3. Auto-generated CRUD endpoints (create, get, list first)
  4. Update and delete endpoints
  5. Meta endpoint
  6. Request/response transformers
  7. API versioning
  8. Rate limiter
  9. Audit log
  10. Wire up `moca-server`
  11. Integration tests
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §3.3 Customizable API Layer / lines 466-679
  - `MOCA_SYSTEM_DESIGN.md` / §14 Complete Request Lifecycle / lines 1884-1920

---

### MS-07: CLI Foundation -- Context Resolver, Output Layer, Cobra Scaffold

- **Goal:** Build CLI infrastructure: project/site/env detection, output formatting (TTY/JSON/Table/Progress), rich errors, and full Cobra command tree scaffold.
- **Why now:** Can start in parallel with MS-04/MS-05. CLI scaffold must exist before real commands (MS-09).
- **Scope:**
  - IN: `internal/context/` (project/site/env resolvers), `internal/output/` (TTY, JSON, Table, Progress, rich errors), Cobra root command with all 24 groups registered as placeholders, `moca version`, `moca completion`, `moca doctor` skeleton.
  - OUT: No real command implementations, no driver connections.
- **Deliverables:**
  1. `internal/context/resolver.go`: `Resolve() -> *CLIContext` with Project, Site, Environment
  2. `internal/context/project.go`: Find `moca.yaml` by walking up directories
  3. `internal/context/site.go`: Resolve from `--site` flag > `MOCA_SITE` env > `.moca/current_site`
  4. Output formatters: `table.go`, `json.go`, `progress.go`, `color.go`
  5. `internal/output/error.go`: Rich error format (Context/Cause/Fix/Reference)
  6. All 24 command group files registered with placeholder `RunE`
  7. `moca version`: CLI version, Go version, infrastructure versions
  8. `moca completion bash/zsh/fish/powershell`
  9. `moca doctor` skeleton (PG, Redis connectivity)
- **Acceptance Criteria:**
  - Running `moca` in a dir with `moca.yaml` detects project; outside prints "Not inside a Moca project"
  - `moca --site acme.localhost site info` resolves site from flag
  - `MOCA_SITE=acme.localhost moca site info` resolves from env
  - `moca site create --json` outputs JSON; `moca site list --table` outputs formatted table
  - Failed command shows Context/Cause/Fix error format
  - `moca help` shows all 24 command groups
- **Dependencies:** MS-01
- **Risks:**
  - Go REPL for `moca dev console` is non-trivial (defer actual REPL to MS-28)
  - TUI library choice: consider `charmbracelet/bubbletea`
- **Suggested Implementation Order:**
  1. Cobra root command
  2. Context resolver (project, site, environment)
  3. Output layer (TTY and JSON first)
  4. Rich error formatting
  5. Register all 24 command groups
  6. `moca version`
  7. `moca completion`
  8. `moca doctor` skeleton
  9. Table output and progress bars
- **Design References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §2.3 CLI Internal Architecture / lines 92-150
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.1 Command Tree Overview / lines 349-559
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §6 Context Detection & Resolution / lines 3268-3293
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §7 Error Handling / lines 3297-3359
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §9 CLI Internal Package Layout / lines 3410-3516

---

### MS-08: Hook Registry and App System Foundation

- **Goal:** Implement HookRegistry (priority-ordered, dependency-aware), AppManifest parser, app directory scanner, and the `apps/core` framework app with core DocTypes (User, Role, DocType, Module, SystemSettings).
- **Why now:** Hooks wire app-provided lifecycle behavior into Document Runtime. Core app provides the minimum MetaTypes for the system to function.
- **Scope:**
  - IN: `pkg/hooks/registry.go` (PrioritizedHandler, dependency resolution, topological sort), `pkg/hooks/docevents.go`, `pkg/apps/manifest.go`, `pkg/apps/loader.go`, `apps/core/` (5 DocType definitions + Go controllers).
  - OUT: No app installation lifecycle (MS-09), no migration runner (MS-09), no fixtures (MS-09).
- **Deliverables:**
  1. `HookRegistry` with `Register(event, handler)`, `Resolve(event) -> []handler` sorted by priority
  2. DocEvent dispatcher integrated with lifecycle engine
  3. `AppManifest` parser and validator
  4. App directory scanner/loader
  5. `apps/core/manifest.yaml`
  6. Core DocType definitions: User, Role, DocType, Module, SystemSettings (JSON + Go controllers)
  7. User controller with bcrypt password hashing on BeforeSave
- **Acceptance Criteria:**
  - `Register("SalesOrder", "after_save", handler, priority=100)` followed by `priority=200` dispatches in order
  - Hook with `DependsOn: ["crm"]` sorted after all "crm" hooks
  - Circular dependencies produce clear error
  - `apps/core/manifest.yaml` validates successfully
  - User MetaType compiles and generates correct DDL
  - SystemSettings uses `tab_singles` (not regular table)
- **Dependencies:** MS-04
- **Risks:**
  - DocType MetaType is self-referential -- bootstrap ordering needs care
  - Auth system incomplete (MS-14); bcrypt hashing is placeholder
- **Suggested Implementation Order:**
  1. HookRegistry with priority sorting
  2. Dependency resolution (topological sort)
  3. DocEvent dispatcher integration
  4. AppManifest parser
  5. App directory scanner
  6. Core DocType JSON definitions
  7. User controller (password hashing)
  8. Bootstrap sequence for self-referential DocType
  9. Tests
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §3.5 Hook Registry / lines 714-781
  - `MOCA_SYSTEM_DESIGN.md` / §7.1-7.3 App System / lines 1263-1383

---

### MS-09: CLI Project Init, Site, and App Commands (Init, Create, Drop, Install, Migrate)

- **Goal:** Implement `moca init` (project bootstrapping), `moca site create/drop/list/use`, `moca app install/uninstall`, and `moca db migrate` -- making the framework usable from the command line for the first time.
- **Why now:** Developer can define MetaTypes and has a working API but no CLI to bootstrap projects, create sites, or install apps. `moca init` is the very first command any user runs.
- **Scope:**
  - IN: `moca init` (project bootstrapping: create directory, generate moca.yaml, connect PG/Redis, create moca_system schema, install core app, generate moca.lock, init git), `pkg/tenancy/manager.go` (9-step site creation), `pkg/orm/migrate.go` (migration runner), `pkg/apps/installer.go` (install: deps, migrate, fixtures, register), CLI PostgreSQL/Redis drivers. Commands: `moca init`, `moca site create/drop/list/use/info`, `moca app install/uninstall/list`, `moca db migrate/rollback/diff`.
  - OUT: No `moca app new` (MS-13), no `moca app get` (MS-13), no `moca site clone/reinstall/enable/disable/rename/browse` (MS-11).
- **Deliverables:**
  1. `moca init`: Project bootstrapping with flags `--name`, `--template`, `--minimal`, `--apps`, `--db-host`, `--db-port`, `--redis-host`, `--redis-port`, `--kafka`/`--no-kafka`, `--skip-assets`, `--json`
  2. `pkg/tenancy/manager.go`: `CreateSite(config)` -- 9-step lifecycle
  3. `pkg/orm/migrate.go`: Migration runner with version tracking, `DependsOn` ordering, dry-run
  4. `pkg/apps/installer.go`: `InstallApp(site, app)` -- dep resolution, migration, fixtures
  5. Site CLI: create (with interactive password prompt), drop (with confirmation), list, use, info
  6. App CLI: install, uninstall, list
  7. DB CLI: migrate (with --dry-run, --skip), rollback, diff
- **Acceptance Criteria:**
  - `moca init my-erp` creates project directory, generates moca.yaml, connects to PG/Redis, creates moca_system schema, installs core app, generates moca.lock, initializes git
  - `moca init my-erp --template minimal --no-kafka` creates minimal project without Kafka config
  - `moca site create acme.localhost --admin-password secret123` runs full 9-step lifecycle
  - `moca site list` shows sites with status, apps, DB size
  - `moca site use acme.localhost` writes to `.moca/current_site`
  - `moca app install crm --site acme.localhost` resolves deps, runs migrations, seeds fixtures
  - `moca db migrate --dry-run` shows pending migrations without executing
  - `moca site drop acme.localhost --force` drops PG schema, removes Redis keys
- **Dependencies:** MS-07, MS-08
- **Risks:**
  - Migration rollback strategy: full SQL DOWN vs snapshot-based
  - Fixture loading order when fixtures reference each other
- **Suggested Implementation Order:**
  1. CLI PostgreSQL and Redis drivers
  2. Migration runner
  3. Site creation lifecycle
  4. `moca site create` command
  5. `moca site list/use/info`
  6. App installer
  7. `moca app install/uninstall/list`
  8. `moca db migrate/rollback/diff`
  9. `moca site drop`
  10. Integration tests (full CLI workflow)
- **Design References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.1 moca init / lines 565-640
  - `MOCA_SYSTEM_DESIGN.md` / §8.3 Site Lifecycle / lines 1371-1396
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.2 Site Management / lines 644-865
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.3 App Management / lines 869-1091
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.5 Database Operations / lines 1327-1495

---

### MS-10: Dev Server, Process Management, and Hot Reload

- **Goal:** Implement `moca serve` (single-process dev server), MetaType filesystem hot reload, and `moca stop/restart`.
- **Why now:** Developer experience milestone. After this: `moca init` -> `moca site create` -> `moca serve` -> edit MetaType JSON -> see changes immediately.
- **Scope:**
  - IN: Dev mode all-in-one process (HTTP + workers + scheduler + outbox), `pkg/meta/watcher.go` (fsnotify on `*/doctypes/*.json`), PID management, `moca serve/stop/restart`, `--no-watch` flag, static file serving.
  - OUT: No production process separation (MS-21), no systemd generation (MS-21).
- **Deliverables:**
  1. `internal/process/supervisor.go`: Goroutine supervisor with graceful shutdown
  2. `pkg/meta/watcher.go`: fsnotify watcher, 500ms debounce, triggers validate -> migrate -> cache flush -> route update
  3. `moca serve`: Starts dev server, writes PID, prints URL
  4. `moca stop`: SIGTERM via PID file, waits for graceful shutdown
  5. `moca restart`: Stop + Serve
  6. WebSocket stub: placeholder for real-time (MS-19)
  7. Static file serving: `desk/dist/`
- **Acceptance Criteria:**
  - `moca serve` starts HTTP server, prints URL, serves API endpoints
  - Worker goroutines process background jobs
  - Modifying `*.json` MetaType triggers hot reload within 2 seconds
  - `--no-watch` disables filesystem watching
  - `moca stop` gracefully shuts down (drains in-flight requests)
  - PID file cleaned up on shutdown
- **Dependencies:** MS-06, MS-08
- **Risks:**
  - fsnotify on macOS with vim (temp file -> rename); debounce mitigates
  - Graceful shutdown ordering: stop HTTP -> drain workers -> close DB
- **Suggested Implementation Order:**
  1. Goroutine supervisor with graceful shutdown
  2. Dev mode wiring (HTTP + workers + scheduler + outbox in one process)
  3. PID file management
  4. `moca serve/stop/restart`
  5. Filesystem watcher
  6. Hot reload pipeline
  7. Static file serving
  8. Integration test: edit MetaType -> verify API reflects changes
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §3.1.3 Metadata Lifecycle & Hot Reload / lines 271-296
  - `MOCA_SYSTEM_DESIGN.md` / §12.1 Single-Instance Development / lines 1773-1791
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.4 Server/Process Management / lines 1094-1217

---

### MS-11: CLI Operational Commands -- Site Ops, Database, Backup, Config, Cache

- **Goal:** Implement secondary site operations, backup/restore, configuration management, cache operations, and database utilities.
- **Why now:** Essential operational commands for real development. Includes site management commands deferred from MS-09. Can run in parallel with frontend work (MS-17).
- **Scope:**
  - IN: `moca site clone/reinstall/enable/disable/rename/browse`, `moca backup create/restore/list/verify`, `moca config get/set/remove/list/diff/export/import/edit`, `moca cache clear/warm`, `moca db console/seed/reset/snapshot/export-fixtures/trim-tables/trim-database`, `internal/config/sync.go` (YAML-to-DB sync per config sync contract).
  - OUT: No `moca backup schedule` (MS-21), no S3 upload/download (MS-21), no encryption (MS-22).
- **Deliverables:**
  1. `moca site clone`: Clone site schema + data with `--anonymize` for staging
  2. `moca site reinstall`: Reset site to fresh state
  3. `moca site enable/disable`: Maintenance mode with `--message` and `--allow` IP list
  4. `moca site rename`: Rename site with `--no-proxy-reload` option
  5. `moca site browse`: Open site in browser with `--user` impersonation
  6. `pkg/backup/create.go`: pg_dump wrapper, file archival, `.sql.gz` output
  7. `pkg/backup/restore.go`: Drop + recreate schema, restore SQL + files
  8. `pkg/backup/verify.go`: Integrity check
  9. Config sync: `moca config set` writes YAML AND updates DB + publishes cache invalidation
  10. `moca config get --resolved` (merged YAML) vs `--runtime` (from DB)
  11. `moca config edit`: Open config in `$EDITOR`
  12. `moca cache clear --site X --type meta|doc|session|all`: flush Redis keys
  13. `moca db console`: exec `psql` with correct connection string
  14. `moca db seed/reset/snapshot/export-fixtures`
  15. `moca db trim-tables`: Remove orphaned columns not in MetaType definitions
  16. `moca db trim-database`: Remove orphaned tables not corresponding to any MetaType
- **Acceptance Criteria:**
  - `moca site clone acme.localhost staging.localhost --anonymize` creates anonymized copy
  - `moca site disable acme.localhost --message "Maintenance"` returns 503 to visitors
  - `moca site enable acme.localhost` restores site serving
  - `moca backup create` produces timestamped backup in `sites/{site}/backups/`
  - `moca backup restore BACKUP_FILE --site X` restores to backup state
  - `moca config set` updates YAML AND running server's config (via event)
  - `moca config get --resolved` shows merged config
  - `moca cache clear` removes all Redis keys for site
  - `moca db console` opens psql connected to correct schema
  - `moca db trim-tables --dry-run` shows orphaned columns without removing them
- **Dependencies:** MS-09
- **Risks:**
  - pg_dump dependency: handle missing binary
  - Config sync event: if server not running, DB update queued until next start
- **Suggested Implementation Order:**
  1. Backup create (pg_dump wrapper)
  2. Backup restore
  3. Backup list and verify
  4. Config get/set/remove/list
  5. Config sync (YAML <-> DB)
  6. Cache clear/stats
  7. DB console/seed/reset
  8. Tests
- **Design References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.2 Site Management (clone, reinstall, enable, disable, rename, browse) / lines 763-843
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.5 Database Operations (trim-tables, trim-database) / lines 1443-1466
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.6 Backup Operations / lines 1498-1653
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.7 Configuration Management / lines 1627-1758
  - `MOCA_SYSTEM_DESIGN.md` / §5.1.1 Configuration Sync Contract / lines 1040-1072

---

### MS-12: Multitenancy -- Site Resolver Middleware, Per-Site Isolation

- **Goal:** Implement server-side tenant resolution (subdomain/header/path), per-site DB pool management, per-site Redis key prefixing, and multi-site serving from a single process.
- **Why now:** Up to now the server handles a single site. Multitenancy is a core requirement before permissions (per-tenant) and Beta.
- **Scope:**
  - IN: `pkg/tenancy/resolver.go` (3 strategies), `pkg/tenancy/context.go` (SiteContext per request), per-site pool registry (lazy init), Redis key prefixing, Meilisearch index prefix, S3 prefix.
  - OUT: No RLS policies (MS-14), no Kafka partition key isolation (MS-15).
- **Deliverables:**
  1. `SiteResolver.Middleware()`: Extracts site from request, looks up in `moca_system.sites` (cached), injects `SiteContext`
  2. `SiteContext`: Name, DBSchema, Config, InstalledApps, DBPool, RedisPrefix, StorageBucket
  3. Per-site pool management: lazy creation, connection limit per site
  4. All existing code refactored to use `SiteContext` from request context
  5. Redis operations prefixed with site name
  6. Integration test: two sites, concurrent requests, data isolation verified
- **Acceptance Criteria:**
  - `acme.localhost:8000/api/v1/resource/SalesOrder` resolves to "acme.localhost"
  - `X-Moca-Site: globex` header resolves to "globex"
  - Creating SalesOrder on "acme" does not appear on "globex"
  - Redis keys prefixed: `acme:meta:SalesOrder`, `globex:meta:SalesOrder`
  - Nonexistent site returns 404; disabled site returns 503
- **Dependencies:** MS-06, MS-02
- **Risks:**
  - Pool exhaustion with many tenants: implement idle tenant eviction
  - SiteContext must propagate through all spawned goroutines
- **Suggested Implementation Order:**
  1. SiteContext struct
  2. SiteResolver middleware (subdomain first)
  3. Site lookup with Redis caching
  4. Per-site pool registry
  5. Refactor existing code to use SiteContext
  6. Redis key prefixing
  7. Disabled site handling
  8. Integration tests with 2+ sites
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §8.1-8.2 Tenant Resolution & Isolation / lines 1326-1462

---

### MS-13: CLI App Scaffolding, User Management, and Developer Tools

- **Goal:** Implement `moca app new` (scaffold), `moca app get` (download from git), `moca user` commands, `moca dev execute/request`, and `moca build server/app`.
- **Why now:** Developers need to create apps and manage users. Build commands needed for Go workspace model.
- **Scope:**
  - IN: `moca app new` (scaffold), `moca app get` (git clone + go.work update), `moca app update/resolve/diff`, `moca user add/remove/set-password/add-role/remove-role/list/disable/enable/impersonate`, `moca dev execute/request`, `moca build server/app`, `internal/scaffold/`, `internal/lockfile/`.
  - OUT: No `moca app publish` (MS-28), no `moca dev console` REPL (MS-28).
- **Deliverables:**
  1. `internal/scaffold/app.go`: Templates for new app structure
  2. `moca app new crm`: Creates `apps/crm/` with full scaffold
  3. `moca app get github.com/moca-apps/crm --version ~1.2.0`: Git clone, validate manifest, update go.work
  4. `moca app resolve`: Dependency resolution, lockfile generation
  5. `moca user add/remove/set-password/add-role/remove-role/list/disable/enable/impersonate`
  6. `moca build server`: `go build -o bin/moca-server ./cmd/moca-server` via go.work
  7. `moca build app crm`: Verify app compiles within workspace
  8. `moca dev request GET /api/v1/resource/SalesOrder --user Administrator`
- **Acceptance Criteria:**
  - `moca app new my-app` creates valid scaffold that compiles cleanly
  - `moca app get` clones repo, validates manifest, adds to go.work
  - `moca app resolve` writes valid moca.lock
  - `moca user add --email admin@test.com --password secret` creates User document
  - `moca build server` produces working binary serving all installed apps
  - `moca dev request` makes authenticated HTTP request, displays response
- **Dependencies:** MS-09, MS-08
- **Risks:**
  - Git operations: auth (SSH keys, HTTPS tokens) for private repos
  - Semver resolution algorithm complexity
- **Suggested Implementation Order:**
  1. App scaffold templates
  2. `moca app new`
  3. `moca app get` (git clone + go.work)
  4. Lockfile and dependency resolver
  5. `moca app resolve/update/diff`
  6. User management commands
  7. `moca build server/app`
  8. `moca dev execute/request`
- **Design References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.3 App Management / lines 869-1091
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.16 User Management (incl. impersonate) / lines 2779-2974
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §3.2 Lockfile / lines 315-345
  - `MOCA_SYSTEM_DESIGN.md` / §7.3 App Directory Structure / lines 1279-1383

---

### MS-14: Permission Engine -- Role-Based, Field-Level, Row-Level

- **Goal:** Implement complete permission resolution: role-based DocType perms, field-level read/write, row-level matching, custom rules, PostgreSQL RLS, and session/JWT auth.
- **Why now:** API is functional but unprotected. Before Beta, every endpoint must enforce permissions.
- **Scope:**
  - IN: `pkg/auth/permission.go` (5-step resolution), `pkg/auth/session.go` (Redis-backed), `pkg/auth/jwt.go`, field-level filtering, row-level matching, RLS policies, login/logout endpoints.
  - OUT: No OAuth2/SAML/OIDC (MS-22), no API keys (MS-18).
- **Deliverables:**
  1. `ResolvePermissions(user, doctype) -> EffectivePerms` with Redis cache (`perm:{site}:{user}:{doctype}`, TTL 2min)
  2. Session management stored in Redis DB 2
  3. JWT issuing (login), validation middleware, refresh token rotation
  4. Auth middleware (session cookie, JWT bearer) replacing placeholder from MS-06
  5. Field-level filtering: exclude fields not in `field_level_read`; reject writes not in `field_level_write`
  6. Row-level matching: QueryBuilder adds WHERE clauses from `match_field`/`match_value`
  7. Custom rule support via registered Go functions
  8. RLS policy generation: `CREATE POLICY` from PermRule definitions
  9. Login/logout: `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`
- **Acceptance Criteria:**
  - "Sales User" with `read|create` perm can GET and POST but not DELETE
  - `field_level_read: ["customer_name", "grand_total"]` returns only those fields
  - `match_field: "company"` filters documents to user's company only
  - JWT login returns token; subsequent requests with Bearer token authenticated
  - RLS policy enforces restrictions even on direct SQL
- **Dependencies:** MS-06, MS-08 (User/Role DocTypes)
- **Risks:**
  - RLS performance with many tenants and complex match conditions
  - Custom rule sandbox: Go function registry, not arbitrary execution
- **Suggested Implementation Order:**
  1. PermRule evaluation engine
  2. Permission caching
  3. Session management
  4. JWT issuing and validation
  5. Auth middleware (replace placeholder)
  6. Login/logout endpoints
  7. Field-level filtering
  8. Row-level matching
  9. Custom rule registry
  10. RLS policy generation
  11. Integration tests
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §3.4 Permission Engine / lines 682-711
  - `MOCA_SYSTEM_DESIGN.md` / §13.1 Authentication Methods / lines 1746-1754
  - `MOCA_SYSTEM_DESIGN.md` / §13.2 API Key System / lines 1853-1868

---

### MS-15: Background Jobs, Scheduler, Kafka/Redis Events, Search Sync

- **Goal:** Implement background jobs (Redis Streams workers, DLQ), cron scheduler, Kafka event producer (with Redis fallback), transactional outbox, and Meilisearch sync.
- **Why now:** Many features produce async work (audit, search, webhooks, email). Scheduler enables recurring tasks. Events enable integration backbone.
- **Scope:**
  - IN: `pkg/queue/` (producer, consumer, DLQ, scheduler), `pkg/events/` (Kafka/Redis producer, consumer, outbox), `pkg/search/` (indexer, sync, query API), `cmd/moca-worker/`, `cmd/moca-scheduler/`, `cmd/moca-outbox/`.
  - OUT: No `moca-search-sync` as separate process (in-process for now), no CDC, no webhooks (MS-18).
- **Deliverables:**
  1. Job enqueue, worker pool, consumer groups, DLQ, retry with exponential backoff
  2. Cron scheduler with Redis distributed lock (single-leader)
  3. Abstracted event producer (Kafka when enabled, Redis pub/sub fallback)
  4. Outbox poller (100ms interval): reads `tab_outbox`, publishes, marks published
  5. Meilisearch indexer: create/update/delete indexes per DocType per site
  6. Search sync: document events -> Meilisearch
  7. Search API: `GET /api/v1/search?q=...&doctype=...`
  8. Standalone binaries: `moca-worker`, `moca-scheduler`, `moca-outbox`
- **Acceptance Criteria:**
  - `queue.Enqueue(site, job)` adds to correct Redis Stream; worker consumes and acknowledges
  - Failed job (3 retries) moves to DLQ
  - Scheduler fires cron jobs within 1-second accuracy; single leader across replicas
  - Document insert -> outbox -> event publish -> Meilisearch sync
  - With `kafka.enabled: false`, events go to Redis pub/sub
  - Search API returns matching documents from Meilisearch
  - Standalone process binaries start correctly
- **Dependencies:** MS-04, MS-06, MS-12
- **Risks:**
  - Kafka client: franz-go (pure Go, no CGo) vs confluent-kafka-go
  - Outbox batching for high throughput
  - Meilisearch eventual consistency lag
- **Suggested Implementation Order:**
  1. Redis Streams producer/consumer
  2. Worker pool with consumer groups
  3. Dead-letter queue
  4. Cron scheduler with leader election
  5. Event producer abstraction
  6. Transactional outbox poller
  7. Meilisearch indexer
  8. Search sync consumer
  9. Search API endpoint
  10. Standalone binaries
  11. Integration tests
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §5.2 Queue Layer (Redis Streams) / lines 1073-1115
  - `MOCA_SYSTEM_DESIGN.md` / §6 Kafka Event Streaming / lines 1089-1260
  - `MOCA_SYSTEM_DESIGN.md` / §6.5 Kafka-Optional Architecture / lines 1235-1260

---

### MS-16: CLI Queue, Events, Search, Monitor, and Log Commands

- **Goal:** CLI commands for managing queues, events, search, monitoring, and logs.
- **Why now:** MS-15 adds async systems. Operators need CLI visibility.
- **Scope:**
  - IN: `moca queue status/list/retry/purge/inspect/dead-letter`, `moca events list-topics/tail/publish/consumer-status/replay`, `moca search rebuild/status/query`, `moca monitor live/metrics/audit`, `moca log tail/search/export`, `moca cache warm/stats`, `moca worker start/stop/status/scale`, `moca scheduler` management.
  - OUT: No `moca monitor live` TUI (defer); use simple streaming output.
- **Deliverables:**
  1. Queue commands: inspect Redis Streams, list jobs, retry, purge, manage DLQ
  2. Events commands: list topics, tail events, publish test event, consumer lag, replay
  3. Search commands: rebuild, status, query from CLI
  4. Monitor commands: dump Prometheus metrics, query audit log
  5. Log commands: tail with streaming, search with regex, export date range
  6. Worker management: `moca worker start/stop/status/scale`
  7. Scheduler management (all 8 commands): `moca scheduler start/stop/status/enable/disable/trigger/list-jobs/purge-jobs`
  8. Cache commands: warm metadata, hit/miss stats
- **Acceptance Criteria:**
  - `moca queue status` shows queue depths
  - `moca events tail moca.doc.events --site acme.localhost` streams in real-time
  - `moca search rebuild --doctype SalesOrder` rebuilds index
  - `moca monitor audit --user admin --last 1h` shows recent audit entries
  - `moca log tail --follow --filter "level=error"` streams filtered logs
  - `moca worker scale default 4` adjusts worker count for default queue
  - `moca scheduler list-jobs --site acme.localhost` shows all registered cron jobs
  - `moca scheduler enable/disable` toggles scheduler for a site
- **Dependencies:** MS-15, MS-07
- **Risks:**
  - `moca events replay` with Kafka requires timestamp offset seeking
  - Log aggregation: centralized vs per-process
- **Design References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.4a Scheduler Management (all 8 commands) / lines 1220-1336
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.15 Queue & Events / lines 2611-2839
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.14 Monitoring & Diagnostics / lines 2484-2607
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.17 Search Management / lines 2978-3033
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.19 Log Management / lines 3065-3122

---

### MS-17: React Desk Foundation -- App Shell, MetaProvider, FormView, ListView

- **Goal:** Build the React Desk app shell, providers (Meta, Doc, Auth), metadata-driven FormView and ListView, and the field type component library. First time users see the Desk UI.
- **Why now:** REST API (MS-06) and auth (MS-14) are ready. Frontend can now consume metadata and render dynamic forms.
- **Scope:**
  - IN: `desk/` React 19 + TypeScript + Vite, App shell (sidebar, breadcrumbs, search), providers (Meta, Doc, Auth), FormView (metadata-driven), ListView (filters, pagination), FieldRenderer for 20+ field types, `moca build desk`, dev HMR proxy.
  - OUT: No WebSocket real-time (MS-19), no custom field registry (MS-19), no Dashboard/Report views (MS-20), no Portal/SSR (MS-27), no i18n (MS-20).
- **Deliverables:**
  1. Vite project: React 19, TypeScript, TailwindCSS
  2. App shell: sidebar navigation (module-based), breadcrumbs, command-K search, user menu
  3. `MetaProvider`: Fetches and caches MetaType from `/api/v1/meta/{doctype}`
  4. `DocProvider`: Document CRUD operations
  5. `AuthProvider`: Login form, session persistence, permission context
  6. FormView: Renders from MetaType with SectionBreak/ColumnBreak/TabBreak layout, dirty tracking, save/cancel
  7. ListView: Columns from `in_list_view`, filters from `in_filter`, pagination
  8. Field components: Data, Text, Int, Float, Currency, Date, Datetime, Select, Link (autocomplete), Checkbox, Attach, CodeEditor, Markdown, JSON, ChildTable (inline editable)
  9. `moca build desk`: Runs Vite production build
  10. Dev mode: `moca serve` proxies `/desk` to Vite dev server for HMR
- **Acceptance Criteria:**
  - `moca build desk` produces optimized bundle in `desk/dist/`
  - `http://localhost:8000/desk` shows login page
  - After login, sidebar shows modules with DocTypes
  - ListView shows data from API; clicking document opens FormView
  - Editing, saving triggers PUT; creating new document triggers POST
  - LinkField shows autocomplete; ChildTableField renders inline rows
- **Dependencies:** MS-06, MS-14
- **Risks:**
  - Field component library is large; prioritize 15 most common types first
  - ChildTableField is complex (inline add/delete/reorder)
- **Suggested Implementation Order:**
  1. Vite project setup
  2. AuthProvider and login page
  3. API client layer
  4. MetaProvider
  5. App shell (sidebar, breadcrumbs)
  6. FieldRenderer + basic components (Data, Int, Select, Date, Check)
  7. FormView with layout
  8. ListView with filters + pagination
  9. Advanced components (Link, ChildTable, Attach, Code, Markdown, JSON)
  10. `moca build desk`
  11. Dev mode HMR proxy
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §9 React Frontend Architecture / lines 1466-1568
  - `MOCA_SYSTEM_DESIGN.md` / §17.2 Desk Composition Model / lines 2134-2143

---

### MS-18: API Keys, Webhooks, Custom Endpoints, APIConfig per DocType

- **Goal:** API key management, webhook dispatch, per-DocType APIConfig (custom middleware, custom endpoints), whitelisted API methods.
- **Why now:** External integrations need API keys and webhooks. Per-DocType customization is a key differentiator.
- **Scope:**
  - IN: `pkg/api/apikey.go` (CRUD, bcrypt, scopes, rate limit per key, IP allowlist), `pkg/api/webhook.go` (registration, dispatch via background job, retry, logging), per-DocType APIConfig, whitelisted API methods, `moca api` CLI commands.
  - OUT: No OAuth2 provider (MS-22), no GraphQL (MS-20).
- **Deliverables:**
  1. API key CRUD, scope-based permission (APIScopePerm), rate limit per key
  2. Webhook dispatch on document events via background job, retry, delivery logging
  3. Per-DocType APIConfig: custom middleware, disabled endpoints, custom endpoints
  4. Whitelisted API methods: `/api/method/{name}`
  5. CLI: `moca api keys create/revoke/list/rotate`, `moca api webhooks list/test/logs`
  6. `moca api list` (all registered endpoints), `moca api docs` (OpenAPI 3.0)
  7. `moca api test` (test request from CLI)
- **Acceptance Criteria:**
  - `Authorization: token KEY:SECRET` authenticates with scope restrictions
  - API key with `orders:read` scope can GET but not POST
  - Webhook fires HTTP POST on document creation; retries 3x on failure
  - DocType with `DisabledEndpoints: ["delete"]` has no DELETE endpoint
  - `moca api list` shows all registered endpoints with method, path, source, and auth type
  - `moca api test /api/v1/resource/SalesOrder --user admin` returns valid response with timing
  - `moca api docs` generates valid OpenAPI 3.0 spec
- **Dependencies:** MS-14, MS-15
- **Risks:**
  - OpenAPI generation from MetaType: FieldType -> OpenAPI type mapping
  - Webhook HMAC signing for payload verification
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §3.3 Customizable API Layer / lines 466-679
  - `MOCA_SYSTEM_DESIGN.md` / §13.2 API Key System / lines 1853-1868
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.13 API Management / lines 2303-2481

---

### MS-19: Desk Real-Time, Custom Field Types, Version Tracking

- **Goal:** WebSocket real-time updates, custom field type registry for app extensions, and document version tracking.
- **Why now:** Desk is functional but static. Real-time prevents stale-data conflicts. Custom fields enable app UI extensions.
- **Scope:**
  - IN: `pkg/ui/websocket.go` (WebSocket hub, Redis pub/sub bridge), Desk WebSocketProvider, custom field type registry (`registerFieldType()`), `tab_version` tracking on save, version history UI.
  - OUT: No typing indicators, no collaborative editing.
- **Deliverables:**
  1. WebSocket hub: connection management, per-site subscriptions, Redis pub/sub bridge
  2. Desk `WebSocketProvider`: auto-reconnecting client, event dispatch
  3. Real-time: other user saves -> open FormViews auto-refresh with diff notification
  4. `registerFieldType()` API for app extensions
  5. `@moca/desk` workspace-local npm package structure
  6. Version tracking: field-level diff on save -> `tab_version` entry
  7. FormView version history sidebar
- **Acceptance Criteria:**
  - User A saves SalesOrder; User B sees real-time refresh notification
  - WebSocket reconnects after network interruption
  - `registerFieldType('TreeSelect', Component)` makes TreeSelect available in FormView
  - Save with `track_changes: true` creates version entry with diff
  - Version history shows timeline of field-level changes
- **Dependencies:** MS-17, MS-15
- **Risks:**
  - WebSocket scaling: Redis pub/sub fan-out latency at 50k connections
  - Custom field type discovery: apps must ship `.tsx` files found at build time
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §9.4 Real-Time Updates via WebSocket / lines 1516-1534
  - `MOCA_SYSTEM_DESIGN.md` / §9.3 Custom Field Type Registry / lines 1504-1514
  - `MOCA_SYSTEM_DESIGN.md` / §17.2 Desk Composition Model / lines 2134-2143

---

### MS-20: GraphQL, Dashboard, Report, Translation, File Storage

- **Goal:** GraphQL auto-generation, Dashboard/Report views, translation system, S3 file storage.
- **Why now:** Rounds out the platform for Beta. GraphQL is a differentiator. Reports make it useful for business users.
- **Scope:**
  - IN: `pkg/api/graphql.go` (auto-generated schema), `pkg/storage/s3.go` (MinIO), file upload API, Desk Dashboard/Report views, `pkg/i18n/` (translation loading, extraction, .mo compilation), `moca translate` CLI commands.
  - OUT: No ScriptReport (security concern), no Portal SSR translations (MS-27).
- **Deliverables:**
  1. GraphQL schema: queries + mutations per DocType; playground at `/api/graphql/playground`
  2. S3 adapter (minio-go): upload, download, delete, signed URLs
  3. File metadata management (`tab_file`), access control, thumbnail generation
  4. File upload API: `POST /api/v1/file/upload`
  5. Desk DashboardView: configurable widgets (number cards, charts, lists)
  6. Desk ReportView: filters, data table, chart visualization
  7. `pkg/i18n/`: Translation loading from `tab_translation` and `.mo` files
  8. `pkg/i18n/extractor.go`: Extract strings from MetaType labels, .tsx, templates
  9. `moca translate export/import/compile/status`
  10. Desk I18nProvider with `t("string")` helper
- **Acceptance Criteria:**
  - GraphQL query returns correct data; mutation creates document with lifecycle
  - File upload stores in S3, creates `tab_file` entry, returns URL
  - Dashboard widget shows filtered document count
  - Report renders data table + chart
  - `moca translate export --app crm --lang ar` extracts strings to PO file
  - `moca translate compile` produces binary `.mo` files
  - Desk UI displays translated labels for user's language
- **Dependencies:** MS-06, MS-17, MS-05
- **Risks:**
  - GraphQL nested types (Link fields, child tables) need careful mapping
  - Translation `.mo` compilation: need go-gettext or custom format
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §3.3 Customizable API Layer (GraphQL) / lines 500-510
  - `MOCA_SYSTEM_DESIGN.md` / §9.6 Translation Architecture / lines 1622-1651
  - `MOCA_SYSTEM_DESIGN.md` / §10.2 Report Builder / lines 1710-1725
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.20 Translation Management / lines 3062-3122

---

### MS-21: Deployment, Infrastructure Generation, and Production Processes

- **Goal:** `moca deploy setup/update/rollback`, infrastructure config generation (Caddy, NGINX, systemd, Docker, K8s), and 5-process production architecture.
- **Why now:** System is functionally complete. Needs to be deployable to production.
- **Scope:**
  - IN: `moca deploy setup/update/rollback/promote/status/history`, `moca generate caddy/nginx/systemd/docker/k8s/env`, 5-process systemd units, Docker Compose for dev/staging/prod.
  - OUT: No blue/green deployment, no canary, no CI/CD generation.
- **Deliverables:**
  1. `moca deploy setup --proxy caddy --process-manager systemd`: Full production setup
  2. `moca deploy update`: Atomic backup -> pull -> migrate -> build -> restart with auto-rollback
  3. `moca deploy rollback`: Restore previous state
  4. `moca deploy promote SOURCE_ENV TARGET_ENV`: Promote staging -> production with `--dry-run` and `--skip-backup`
  5. `moca deploy status/history`: Show deployment state and log
  6. `moca generate caddy/nginx`: Per-site routing, TLS
  7. `moca generate systemd`: 5 unit files (server, worker, scheduler, outbox, search-sync)
  8. `moca generate docker`: Full stack Compose file
  9. `moca generate k8s`: Deployment, Service, ConfigMap, PVC, HPA manifests
  10. `moca generate env`: Generate .env file from moca.yaml (`--format dotenv|docker|systemd`)
  11. `moca backup schedule`: Configure automated backups with cron expressions (`--cron`, `--show`, `--enable`, `--disable`)
  12. `moca backup upload/download`: S3/MinIO backup storage integration
  13. `moca backup prune`: Delete old backups per retention policy (`--dry-run`, `--force`)
- **Acceptance Criteria:**
  - `moca deploy setup` on fresh Ubuntu creates working production deployment
  - `moca generate systemd` produces 5 manageable unit files
  - `moca deploy update` with migration failure auto-rolls back
  - `moca deploy promote staging production` promotes code with backup
  - `moca generate docker` produces working docker-compose.yml
  - `moca generate k8s` produces valid K8s manifests (`kubectl --dry-run`)
  - `moca backup schedule --cron "0 2 * * *"` configures daily backups
  - `moca backup upload --destination s3://moca-backups` uploads to S3
- **Dependencies:** MS-10, MS-15
- **Risks:**
  - Systemd socket activation for zero-downtime: may defer to post-v1.0
  - K8s HPA depends on Prometheus metrics (MS-24)
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §12 Deployment Architecture / lines 1676-1838
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.8 Deployment Operations / lines 1742-1928
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.9 Infrastructure Generation / lines 1897-2032

---

### MS-22: Security Hardening -- OAuth2, SAML/OIDC, Encryption, Notifications

- **Goal:** Enterprise SSO (OAuth2, SAML, OIDC), sensitive field encryption, email/in-app notifications, backup encryption.
- **Why now:** Beta-blocking security requirements. Enterprise customers need SSO. Encryption is compliance-required.
- **Scope:**
  - IN: `pkg/auth/oauth2.go` (auth code flow), `pkg/auth/sso.go` (SAML 2.0, OIDC), field encryption (AES-256-GCM), `pkg/notify/email.go` (SMTP/SES), `pkg/notify/inapp.go`, backup encryption, `moca notify test-email/config`.
  - OUT: No push notifications (deferred), no SMS (deferred).
- **Deliverables:**
  1. OAuth2 authorization code flow (provider + consumer)
  2. SAML 2.0 SP implementation, OIDC discovery + auth
  3. Transparent field encryption for Password and sensitive fields
  4. Email sending (SMTP/SES) with template rendering
  5. In-app notification model + API endpoints
  6. Backup encryption: `moca backup create --encrypt`
  7. `moca notify test-email/config`
- **Acceptance Criteria:**
  - OAuth2 flow: redirect -> authorize -> code -> token -> session
  - SAML: configure IdP metadata, SSO login flow completes
  - OIDC: configure Google/Okta, login flow creates session
  - Password fields encrypted at rest, decrypted only with proper permissions
  - `moca backup create --encrypt` -> `moca backup restore --decrypt` works
  - Email notifications fire on configurable document events
  - In-app notifications appear in Desk notification bell
  - `moca notify test-email --to admin@test.com` sends test email and reports success/failure
  - `moca notify config --set smtp.host=smtp.gmail.com` updates notification provider settings
- **Dependencies:** MS-14, MS-15
- **Risks:**
  - SAML 2.0 complexity: consider crewjam/saml library
  - Encryption key rotation strategy
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §13 Security Architecture / lines 1746-1880
  - `MOCA_SYSTEM_DESIGN.md` / §15 Package Layout (notify/) / lines 2019-2023
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.21 Notification Configuration / lines 3192-3220

---

### MS-23: Workflow Engine

- **Goal:** State machine, transitions with role-based access, approval chains, SLA timers, and Desk workflow UI.
- **Why now:** Core business requirement for submittable documents. Ties permissions, lifecycle, and notifications together.
- **Scope:**
  - IN: `pkg/workflow/engine.go` (state machine, transition validation, auto-actions), `pkg/workflow/approval.go` (multi-level, quorum), `pkg/workflow/sla.go` (timer + scheduler + escalation), Desk workflow bar and timeline.
  - OUT: No visual workflow builder (post-v1.0).
- **Deliverables:**
  1. `WorkflowEngine.Transition(doc, action, user)`: validates role, evaluates condition, updates state, fires auto-action
  2. Approval chain: multiple approvers, quorum logic, delegation
  3. SLA timer with scheduler integration, escalation on breach
  4. Workflow events published to `moca.workflow.transitions`
  5. Desk workflow bar: current state, action buttons, transition confirmation
  6. Desk workflow timeline: history of transitions
- **Acceptance Criteria:**
  - Document in "Draft" -> "Approve" by Approver role -> "Approved"
  - Transition blocked without required role
  - Transition blocked if condition expression fails
  - SLA: "Pending Approval" > 24h triggers escalation notification
  - Quorum: 3 approvers, 2 of 3 needed
  - Transitions published to event stream
- **Dependencies:** MS-04, MS-14, MS-15, MS-17
- **Risks:**
  - Safe expression evaluator for conditions (not `eval`)
  - SLA accuracy depends on scheduler tick interval
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §3.6 Workflow Engine / lines 785-824

---

### MS-24: Observability, Metrics, and Profiling

- **Goal:** Prometheus metrics, OpenTelemetry tracing, comprehensive `moca doctor`, and `moca dev bench/profile`.
- **Why now:** Before v1.0, the system must be observable in production.
- **Scope:**
  - IN: `pkg/observe/metrics.go` (all Prometheus metrics), `pkg/observe/tracing.go` (OpenTelemetry spans), `moca doctor` comprehensive diagnostic, `moca dev bench/profile`.
  - OUT: No Grafana dashboard provisioning (provide as docs).
- **Deliverables:**
  1. All Prometheus metrics from §11.1, exposed at `/metrics`
  2. OpenTelemetry spans: HTTP requests, DB queries, Redis ops, hook execution
  3. `moca doctor`: PG, Redis, Kafka, Meilisearch connectivity, migration status, disk, config, versions
  4. `moca dev bench`: Query/operation microbenchmarks with p50/p95/p99
  5. `moca dev profile`: pprof-based profiling with SVG flamegraph
- **Acceptance Criteria:**
  - `curl localhost:8000/metrics` returns Prometheus metrics
  - `moca_http_requests_total` increments with correct labels
  - OpenTelemetry spans propagate through full request lifecycle
  - `moca doctor` produces pass/fail/warn report with suggested fixes
  - `moca dev bench` outputs latency percentiles
  - `moca dev profile` produces SVG flamegraph
- **Dependencies:** MS-06, MS-15
- **Risks:**
  - Metrics cardinality with many tenants/doctypes: may need label aggregation
  - OpenTelemetry Go SDK maturity
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §11 Observability & Operations / lines 1729-1770
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.10 Developer Tools (bench, profile) / lines 2126-2153
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.14 Monitoring & Diagnostics (doctor) / lines 2484-2607

---

### MS-25: Testing Framework, Coverage, and Test Data Generation

- **Goal:** Testing infrastructure: `moca test run`, `moca test factory`, coverage, and comprehensive integration tests.
- **Why now:** Before v1.0, critical paths must be tested. Test factory enables app developers to generate realistic data.
- **Scope:**
  - IN: `moca test run` (Go tests with test site), `moca test factory` (random valid documents from MetaType), `moca test coverage`, test helpers package, comprehensive integration test suite.
  - OUT: No `moca test run-ui` (Playwright, defer to MS-28).
- **Deliverables:**
  1. `pkg/testutils/`: CreateTestSite, CreateTestUser, LoginAs, NewTestDoc, CleanupTestSite
  2. `moca test run`: Ephemeral test site, discover and run Go tests
  3. `moca test factory --doctype SalesOrder --count 100`: Generate valid documents
  4. `moca test coverage`: Aggregate coverage (target: 70% of pkg/)
  5. Integration tests: document lifecycle, permissions, API, multitenancy, search, events, workflow
- **Acceptance Criteria:**
  - `moca test run` creates test site, runs tests, cleans up
  - Tests run in isolation (clean state per test)
  - Factory generates valid documents respecting validation constraints
  - Coverage output per package
  - Integration tests pass for all critical paths
- **Dependencies:** MS-23 (all features implemented)
- **Risks:**
  - Test isolation: separate PG schema per test vs truncate (schema-per-test safer but slower)
  - Factory for Link fields: must create referenced docs first
- **Design References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` / §4.2.11 Testing / lines 2143-2233

---

### MS-26: Documentation, Packaging, and v1.0 Polish

- **Goal:** Developer docs, API reference, deployment guide, release packaging (binaries, Docker, install script), and final polish.
- **Why now:** Final milestone before v1.0. All features implemented and tested.
- **Scope:**
  - IN: Getting-started guide, architecture docs, API reference (OpenAPI + manual), CLI reference (auto-generated from Cobra), deployment guides, app development guide, GoReleaser, Dockerfile, install script, CHANGELOG, LICENSE.
  - OUT: No video tutorials, no hosted docs platform.
- **Deliverables:**
  1. `docs/getting-started.md`: Install -> init -> create site -> create app -> serve
  2. `docs/architecture.md`: High-level subsystem overview
  3. `docs/api-reference/`: Auto-generated from OpenAPI
  4. `docs/cli-reference/`: Auto-generated from `moca --help`
  5. `docs/deployment/`: Single-server, Docker, K8s guides
  6. `docs/app-development.md`: Creating apps, defining DocTypes, hooks, tests
  7. GoReleaser: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
  8. Dockerfile and published Docker image
  9. Install script: `curl -sSL https://get.moca.dev | sh`
  10. CHANGELOG.md, LICENSE, CONTRIBUTING.md
- **Acceptance Criteria:**
  - New developer follows getting-started guide and has working project in <15 minutes
  - Install script works on Linux and macOS
  - Docker image runs with `docker compose up`
  - CLI reference matches actual behavior
  - All tests pass on Linux and macOS
- **Dependencies:** MS-25
- **Risks:**
  - Documentation is time-consuming; maximize auto-generation
  - Cross-compilation for ARM may need special handling
- **Design References:**
  - All sections of both design documents

---

### MS-27: Portal SSR Layer (Post-v1.0)

- **Goal:** Server-side rendered Portal for public-facing pages (customer portals, websites) with Go template rendering.
- **Why deferred:** Desk serves internal users first. Most initial deployments prioritize internal tools.
- **Deliverables:** `pkg/ui/portal.go`, PortalPage/PortalController, SSR templates, portal translations.
- **Dependencies:** MS-06
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §9.5 Portal / SSR Layer / lines 1602-1619

---

### MS-28: Advanced Features -- VirtualDoc, CDC, Dev Console, Playwright (Post-v1.0)

- **Goal:** VirtualDoc (external data sources), CDC topics, opt-in event sourcing, `moca dev console` (Go REPL), `moca dev playground`, `moca app publish`, `moca test run-ui` (Playwright).
- **Why deferred:** Valuable but not essential for v1.0. VirtualDoc needs stable Document interface. CDC needs Kafka maturity.
- **Dependencies:** v1.0
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §3.2.1 VirtualDoc / lines 334-348
  - `MOCA_SYSTEM_DESIGN.md` / §18 What to Revisit / lines 2026-2040

---

### MS-29: Plugin Marketplace and WASM Sandboxing (Post-v1.0)

- **Goal:** WASM-based sandboxed plugin execution for untrusted code, app marketplace with discovery/ratings/scanning.
- **Why deferred:** Requires v1.0 ecosystem maturity. Architecturally complex.
- **Dependencies:** v1.0
- **Design References:**
  - `MOCA_SYSTEM_DESIGN.md` / §18 item 4 "Plugin sandboxing" / line 2040

---

## Parallel Workstreams

After MS-02 completes, three workstreams can proceed in parallel:

| Stream | Milestones | Focus | Staffing |
|--------|-----------|-------|----------|
| **A: Backend Core** | MS-03 -> MS-04 -> MS-05 -> MS-06 -> MS-08 -> MS-10 -> MS-12 -> MS-14 -> MS-15 | Framework runtime, API, multitenancy, permissions, events | 2 Go developers |
| **B: CLI** | MS-07 -> MS-09 -> MS-11 -> MS-13 -> MS-16 | CLI foundation and commands | 1 Go developer |
| **C: Frontend** | MS-17 -> MS-19 -> MS-20 | React Desk UI, real-time, GraphQL, reports | 1-2 frontend developers (starts after MS-06 + MS-14) |

Stream B (CLI) runs independently after MS-07, syncing with Stream A at MS-09 (needs MS-08) and MS-11 (needs MS-09).

Stream C (Frontend) starts later but has fewer milestones; frontend developer can assist with CLI or docs before MS-17 begins.

**Beta and v1.0 milestones (MS-18 through MS-26)** are largely sequential but have internal parallelism (e.g., MS-18 API Keys + MS-19 WebSocket can overlap; MS-21 Deployment + MS-22 Security can overlap).

---

## Deferred / Later Phase Items

| Item | Reason | Target |
|------|--------|--------|
| Portal SSR (`PortalPage`, `PortalController`, Go templates) | Internal tools prioritized over public pages | MS-27 (post-v1.0) |
| VirtualDoc (external data source adapter) | Needs stable Document interface | MS-28 |
| CDC topics (`moca.cdc.{tenant}.{doctype}`) | Requires Kafka production maturity | MS-28 |
| Event sourcing (opt-in per MetaType) | Advanced pattern, not core | MS-28 |
| `moca dev console` (yaegi Go REPL) | Non-trivial; `moca dev execute` is sufficient | MS-28 |
| `moca dev playground` (Swagger/GraphiQL) | Nice-to-have; `moca api docs --serve` is sufficient | MS-28 |
| `moca app publish` (registry) | No registry infrastructure yet | MS-28 |
| `moca test run-ui` (Playwright) | Frontend testing deferred | MS-28 |
| WASM plugin sandboxing | Requires ecosystem maturity | MS-29 |
| App marketplace | Requires WASM + registry | MS-29 |
| gRPC internal communication | Only if microservice decomposition | Post-v1.0 |
| Multi-region (CockroachDB/Citus) | Only for geo-distributed deployments | Post-v1.0 |
| AI/ML integration (pgvector, embeddings) | Future enhancement | Post-v1.0 |
| Visual workflow builder in Desk | Complex UI; JSON editing sufficient for v1.0 | Post-v1.0 |
| `moca monitor live` TUI dashboard | Nice-to-have; simple output sufficient | Post-v1.0 |
| `moca generate supervisor` | Legacy compat only; no detailed spec in CLI design (tree-level entry only) | Post-v1.0 |
| `moca app pin` | Tree-level entry only; no detailed spec section in CLI design | Post-v1.0 |
| Push notifications / SMS | Email and in-app sufficient for v1.0 | Post-v1.0 |
| ScriptReport (server-side Go execution) | Security sandboxing concern | Post-v1.0 |
| Blue/green and canary deployments | Advanced deployment patterns | Post-v1.0 |

---

## Open Questions / Blockers

| ID | Question | Impact | Blocking |
|----|----------|--------|----------|
| OQ-1 | What PostgreSQL connection pooler (PgBouncer, Odyssey) is required for 10,000+ tenants? | MS-12 scaling | No (design pool interface now, add external pooler later) |
| OQ-2 | Will `pgxpool.AfterConnect` with `SET search_path` work correctly with prepared statements? | MS-02 foundation | Yes -- must be resolved in MS-00 spike |
| OQ-3 | Kafka client library: franz-go (pure Go) vs confluent-kafka-go (CGo)? | MS-15 events | No (decide during MS-00 spike, prefer franz-go for no CGo) |
| OQ-4 | Expression evaluator for workflow conditions and computed fields? | MS-23 workflow | No (evaluate options: `expr-lang/expr`, `google/cel-go`, or custom) |
| OQ-5 | GraphQL library: gqlgen (code-gen) vs graphql-go (runtime)? | MS-20 GraphQL | No (decide at MS-20 start; gqlgen recommended for type safety) |
| OQ-6 | Frontend CSS framework: TailwindCSS vs custom design tokens? | MS-17 Desk | No (decide at MS-17 start; Tailwind recommended for speed) |
| OQ-7 | License model: MIT, Apache-2.0, or AGPL-3.0? | MS-26 packaging | Yes -- must decide before v1.0 |
| OQ-8 | Hosting for Docker images and install script? | MS-26 packaging | Yes -- need registry and CDN before release |

---

## Suggested Release Grouping

### MVP (MS-00 through MS-10)
**Single-tenant CRUD with CLI and dev server.**
- A developer can: init project, create site, install apps, define DocTypes, auto-generate REST API, hot-reload MetaType changes, manage via CLI.
- No multitenancy, no permissions, no frontend, no events.

### Alpha (MS-11 through MS-17)
**Feature-complete for internal testing.**
- Adds: multitenancy, permissions with JWT auth, background jobs, Kafka events, Meilisearch search, React Desk UI with FormView/ListView, backup/restore, full CLI operational commands.
- Suitable for internal dogfooding.

### Beta (MS-18 through MS-23)
**Feature-complete for external testing.**
- Adds: API keys, webhooks, custom endpoints, WebSocket real-time, GraphQL, Dashboard/Report views, translation system, file storage, deployment tooling, security hardening (OAuth2, SAML, encryption), workflow engine.
- Suitable for early adopter testing.

### v1.0 (MS-24 through MS-26)
**Production-ready release.**
- Adds: Prometheus metrics, OpenTelemetry tracing, comprehensive `moca doctor`, testing framework, developer documentation, release packaging, Docker images, install script.
- Suitable for production deployments.

### Post-v1.0 (MS-27 through MS-29)
**Advanced features and ecosystem.**
- Portal SSR, VirtualDoc, CDC, Go REPL, Playwright tests, app marketplace, WASM sandboxing.

---

## Summary Table

| ID | Title | Est. Weeks | Dependencies | Release |
|----|-------|-----------|-------------|---------|
| MS-00 | Architecture Validation Spikes | 2 | None | Phase 0 |
| MS-01 | Project Structure & Config | 2 | MS-00 | MVP |
| MS-02 | PostgreSQL & Redis Foundation | 3 | MS-01 | MVP |
| MS-03 | Metadata Registry | 3 | MS-02 | MVP |
| MS-04 | Document Runtime | 3 | MS-03 | MVP |
| MS-05 | Query Engine | 2 | MS-03, MS-04 | MVP |
| MS-06 | REST API Layer | 3 | MS-04, MS-05 | MVP |
| MS-07 | CLI Foundation | 3 | MS-01 | MVP |
| MS-08 | Hook Registry & App System | 3 | MS-04 | MVP |
| MS-09 | CLI Init, Site & App Commands | 3 | MS-07, MS-08 | MVP |
| MS-10 | Dev Server & Hot Reload | 3 | MS-06, MS-08 | MVP |
| MS-11 | CLI Operational: Site Ops, DB, Backup, Config | 3 | MS-09 | Alpha |
| MS-12 | Multitenancy | 3 | MS-06, MS-02 | Alpha |
| MS-13 | CLI App Scaffold, Users, Build | 3 | MS-09, MS-08 | Alpha |
| MS-14 | Permission Engine | 4 | MS-06, MS-08 | Alpha |
| MS-15 | Jobs, Events, Search | 4 | MS-04, MS-06, MS-12 | Alpha |
| MS-16 | CLI Queue/Events/Search/Monitor | 3 | MS-15, MS-07 | Alpha |
| MS-17 | React Desk Foundation | 4 | MS-06, MS-14 | Alpha |
| MS-18 | API Keys, Webhooks, Custom Endpoints | 3 | MS-14, MS-15 | Beta |
| MS-19 | Desk Real-Time, Custom Fields | 3 | MS-17, MS-15 | Beta |
| MS-20 | GraphQL, Dashboard, Report, i18n | 4 | MS-06, MS-17, MS-05 | Beta |
| MS-21 | Deployment & Infrastructure Gen | 3 | MS-10, MS-15 | Beta |
| MS-22 | Security Hardening | 3 | MS-14, MS-15 | Beta |
| MS-23 | Workflow Engine | 3 | MS-04, MS-14, MS-15, MS-17 | Beta |
| MS-24 | Observability & Profiling | 3 | MS-06, MS-15 | v1.0 |
| MS-25 | Testing Framework | 3 | MS-23 | v1.0 |
| MS-26 | Documentation & Packaging | 4 | MS-25 | v1.0 |
| MS-27 | Portal SSR Layer | 3 | MS-06 | Post-v1.0 |
| MS-28 | VirtualDoc, CDC, Advanced | 4 | v1.0 | Post-v1.0 |
| MS-29 | WASM Plugin Marketplace | 6 | v1.0 | Post-v1.0 |
