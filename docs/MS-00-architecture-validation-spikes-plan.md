# MS-00 - Architecture Validation Spikes and Project Scaffold Plan

## Milestone Summary

- **ID:** MS-00
- **Name:** Architecture Validation Spikes and Project Scaffold
- **Roadmap Reference:** `ROADMAP.md` lines 98-137
- **Goal:** Validate the 5 highest-risk architectural assumptions with throwaway spike code and establish the Go project skeleton with CI.
- **Why it matters:** Every subsequent milestone depends on these assumptions. Discovering a fundamental flaw in schema-per-tenant isolation, Redis Streams consumer groups, Go workspace composition, Meilisearch tenant isolation, or Cobra extension discovery after building 10+ milestones would require expensive rework. This milestone burns down risk early and produces ADR documentation for each decision.
- **Position in roadmap:** First milestone. Start of the critical path (MS-00 -> MS-01 -> MS-02 -> ... -> v1.0). No predecessors.
- **Upstream dependencies:** None
- **Downstream dependencies:** MS-01 (Project Structure, Configuration, Go Module Layout) depends on the validated project scaffold, `go.work` structure, and spike outcomes from this milestone.

---

## Vision Alignment

MOCA is a metadata-driven, multitenant business application framework in Go. Its architecture makes five load-bearing bets that cannot be validated by reading documentation alone:

1. **PostgreSQL schema-per-tenant** is the multitenancy foundation (ADR-001). If pgxpool cannot safely isolate schemas under concurrent access, the entire data architecture needs redesign.
2. **Redis Streams** replaces a dedicated message broker (ADR-002). If consumer group semantics are insufficient, a new infrastructure dependency (RabbitMQ/SQS) enters the stack.
3. **Go workspace (`go.work`)** is the build composition model. If multi-app modules cannot reliably compile into a single binary, the app system architecture changes.
4. **Meilisearch** provides full-text search with tenant isolation (ADR-006). If prefix-based index isolation is not viable, Elasticsearch re-enters the discussion.
5. **Cobra CLI extension** via `init()` allows apps to register custom commands at build time. If this pattern fails across Go workspace modules, the CLI extension system needs redesign.

This milestone produces five independent spike validations and a working CI pipeline, giving the team a proven Go project structure and high confidence in the core infrastructure choices before writing any production code.

---

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `ROADMAP.md` | MS-00: Architecture Validation Spikes | 98-137 | Milestone definition, scope, deliverables, acceptance criteria |
| `MOCA_SYSTEM_DESIGN.md` | §4.1 Database Per Tenant | 828-850 | Schema-per-tenant architecture diagram and rationale |
| `MOCA_SYSTEM_DESIGN.md` | §4.2 Core System Tables | 852-887 | `moca_system` schema DDL (sites, apps, site_apps) |
| `MOCA_SYSTEM_DESIGN.md` | §5.2 Queue Layer (Redis Streams) | 1073-1107 | Job struct, WorkerPool struct, stream naming, consumer groups |
| `MOCA_SYSTEM_DESIGN.md` | §15 Framework Package Layout | 1924-2057 | Full directory tree, `go.work`, cmd/, pkg/, apps/, desk/ |
| `MOCA_SYSTEM_DESIGN.md` | §16 ADR Summary | 2061-2105 | ADR-001 through ADR-007 |
| `MOCA_SYSTEM_DESIGN.md` | Build Composition Model | 1380-1384 | go.work composes framework + apps into single binary |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §8 Extension System | 3363-3406 | Apps register Cobra commands via init() |
| `MOCA_CLI_SYSTEM_DESIGN.md` | `moca build server` | 2281-2296 | CLI command that compiles the workspace |
| `docs/blocker-resolution-strategies.md` | Blocker 1: Go Workspace | 7-63 | go.work MVS behavior, conflict resolution, replace directives |
| `docs/blocker-resolution-strategies.md` | Blocker 2: PostgreSQL Isolation | 66-178 | DBManager, per-site pool, AfterConnect, assertSchema, idle eviction |

---

## Research Notes

Web research was conducted for four library decisions. Key findings:

### pgx v5 / pgxpool (v5.9.1)
- **`AfterConnect`** is the correct callback for `SET search_path`. It runs when a new physical connection is created, before it enters the pool. The `search_path` persists for the connection's lifetime within that pool.
- **`BeforeAcquire` is deprecated** as of pgx v5.7.6. Replaced by `PrepareConn` (runs before a connection is handed to application code). The blocker-resolution-strategies document references `BeforeAcquire` -- this should use `AfterConnect` for per-pool schema setting (as the document's own code example does), or `PrepareConn` for per-acquire assertions.
- **Statement caching** is per-pool via `DefaultQueryExecMode` (default: `QueryExecModeCacheStatement`) and `StatementCacheCapacity` (default: 512). Since each tenant gets its own pool, statement caches are naturally isolated.
- **Implication for Spike 1:** The per-site pool registry design in `blocker-resolution-strategies.md` is correct. Each pool's `AfterConnect` permanently sets `search_path` for all connections in that pool. No cross-contamination is possible between pools.

### Redis Streams (go-redis v9.18.0)
- **go-redis v9** is the recommended library. It has full typed methods for all stream commands: `XReadGroup`, `XAck`, `XClaim`, `XAutoClaim`, `XPending`, `XPendingExt`.
- **Correction:** The ROADMAP mentions "franz-go or go-redis" for Redis Streams validation (line 112). **franz-go is a Kafka client, not a Redis client.** The spike should use go-redis v9 exclusively for Redis Streams.
- **Dead-letter queue** is an application-level pattern in Redis Streams: use `XPendingExt` to find messages exceeding retry count, `XClaim` to reassign, and `XAdd` to a DLQ stream after max retries.
- **Consumer groups** provide at-least-once delivery. Messages stay in the Pending Entries List (PEL) until acknowledged.

### Meilisearch Go Client (v0.36.1)
- Official SDK, pre-1.0 but actively maintained. Compatible with Meilisearch server v1.x.
- **Multi-index support:** `CreateIndex()`, `DeleteIndex()`, `MultiSearch()` for cross-index search.
- **Tenant isolation has two patterns:**
  - **Tenant tokens** (recommended by Meilisearch): Single index with `tenant_id` filterable attribute. JWTs embed filter rules. Server enforces isolation.
  - **Index-per-tenant** with prefix naming (e.g., `tenant_acme_products`): Simpler to reason about, aligns with MOCA's schema-per-tenant model.
- **Bulk indexing:** `AddDocumentsInBatches(docs, batchSize)` auto-splits into batches, returns task UIDs for async tracking.
- **Implication for Spike 4:** The ROADMAP specifies "prefix-based tenant isolation" which aligns with the index-per-tenant pattern. The spike should validate both patterns and document trade-offs in the ADR.

### Cobra CLI (v1.10.2)
- **init() registration works** across Go workspace modules. Blank imports (`import _ "app/cmd/foo"`) trigger init() which can call `rootCmd.AddCommand()`.
- **Recommended pattern for larger projects:** Explicit `NewCommand()` constructors rather than init() auto-registration, for clearer dependency graphs and testability.
- **Implication for Spike 5:** The design doc specifies init()-based registration. The spike should validate both patterns (init() auto-register vs explicit constructor) and document which is safer for MOCA's multi-app model.

---

## Milestone Plan

### Task 1: Project Scaffold and CI Pipeline

- **Task ID:** MS-00-T1
- **Status:** Completed
- **Title:** Initialize Go Module, Workspace, and CI Pipeline
- **Description:**
  Create the root Go module (`github.com/osama1998H/moca`), the `go.work` file composing the framework root and installable app modules, the `spikes/` directory structure, and the GitHub Actions CI pipeline.

  Specific deliverables:
  1. `go.mod` at project root for `github.com/osama1998H/moca` (Go 1.26+)
  2. `pkg/builtin/core/` skeleton for the builtin core package in the root module
  3. `go.work` composing the root module and validating installable app-module composition through the spike workspace
  4. `.github/workflows/ci.yml` with three jobs: `go build ./...`, `go test ./...`, `golangci-lint run`
  5. `spikes/` directory with subdirectories: `pg-tenant/`, `redis-streams/`, `go-workspace/`, `meilisearch/`, `cobra-ext/`
  6. `.golangci.yml` with baseline linter config (govet, errcheck, staticcheck, unused, gosimple)
  7. A trivial `main.go` in `cmd/moca-server/` that prints version info, to verify `go build` works end-to-end
  8. `Makefile` with targets: `build`, `test`, `lint`, `spike-pg`, `spike-redis`, `spike-gowork`, `spike-meili`, `spike-cobra`

- **Why this task exists:** Every spike and all subsequent milestones need a compilable Go workspace and a passing CI pipeline. Without this, there is no feedback loop on whether code compiles and tests pass.
- **Dependencies:** None. This is the first task.
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §15 Framework Package Layout (lines 1924-2057) -- canonical directory structure
  - `MOCA_SYSTEM_DESIGN.md` line 2056 -- `go.work` in package layout
  - `ROADMAP.md` lines 106-109 -- deliverables 1-4
- **Deliverable:** A compilable Go workspace with passing CI (build + test + lint) on GitHub Actions. The `go build ./...` command succeeds from workspace root. All spike directories exist with placeholder `README.md` files.
- **Risks / Unknowns:**
  - Go version availability in GitHub Actions runners (Go 1.26+ should be available via `actions/setup-go`)
  - golangci-lint version pinning -- use a specific version in CI, not `latest`, to avoid flaky builds

---

### Task 2: PostgreSQL Schema-Per-Tenant Isolation Spike

- **Task ID:** MS-00-T2
- **Status:** Completed
- **Title:** Spike 1 -- Validate pgxpool Per-Tenant Pool Registry Under Concurrent Access
- **Description:**
  Build a self-contained spike in `spikes/pg-tenant/` that validates the per-site pool registry pattern from `blocker-resolution-strategies.md`. This is the highest-risk spike because the entire multitenancy model depends on it.

  The spike must implement and validate:
  1. **DBManager** with `systemPool` (for `moca_system` schema) and `sitePools map[string]*pgxpool.Pool` (per-tenant pools)
  2. **AfterConnect callback** that permanently sets `search_path` for every connection in a tenant's pool
  3. **assertSchema() defense-in-depth** function that queries `current_schema()` and panics on mismatch
  4. **Concurrent access test**: 100 goroutines, 10 tenant schemas, each performing INSERT + SELECT. Zero cross-contamination.
  5. **Prepared statement isolation**: Verify that statements cached in tenant_acme's pool cannot be reused by tenant_globex's pool (separate pools = separate caches)
  6. **Pool lifecycle**: Create pool lazily on first access, verify idle pool eviction works, verify re-creation after eviction
  7. **ADR document**: Record the validated pattern, rejected alternatives (shared pool with BeforeAcquire reset, single pool with per-query SET), and performance observations

  Test setup:
  - Docker Compose with PostgreSQL 16
  - Create `moca_system` schema with `sites` table
  - Create 10 tenant schemas (`tenant_01` through `tenant_10`) with a simple `tab_test` table
  - Insert/read test data concurrently across all tenants

- **Why this task exists:** Cross-tenant data leakage is a security-critical defect. The per-site pool registry with `AfterConnect` is the designed solution, but it has never been tested under real concurrent load. This spike proves the pattern works and documents it as an ADR.
- **Dependencies:** MS-00-T1 (needs compilable Go workspace and spikes/ directory)
- **Inputs / References:**
  - `docs/blocker-resolution-strategies.md` lines 66-178 -- full solution strategy with code examples
  - `MOCA_SYSTEM_DESIGN.md` §4.1 lines 828-850 -- schema-per-tenant architecture
  - `MOCA_SYSTEM_DESIGN.md` line 1414 -- `DBPool *pgxpool.Pool` in SiteContext
  - `MOCA_SYSTEM_DESIGN.md` ADR-001 lines 2063-2067
  - pgxpool v5.9.1 API: `AfterConnect`, `PrepareConn`, `StatementCacheCapacity`
- **Deliverable:**
  - `spikes/pg-tenant/main.go` -- spike implementation
  - `spikes/pg-tenant/main_test.go` -- concurrent access test (100 goroutines x 10 schemas)
  - `spikes/pg-tenant/docker-compose.yml` -- PostgreSQL 16 container
  - `docs/ADR-001-pg-tenant-isolation.md` -- architecture decision record
  - All acceptance criteria from ROADMAP lines 119 pass: zero cross-contamination, no statement cache leaks, correct search_path after connection reuse, idle eviction works
- **Risks / Unknowns:**
  - **pgxpool `PrepareConn` vs `AfterConnect`**: The blocker doc references `BeforeAcquire` which is deprecated. Spike must validate `AfterConnect` (for pool-wide schema setting) and optionally `PrepareConn` (for per-acquire assertion). Both should be tested.
  - **Pool memory at scale**: Each pool has its own connection set. With `MaxConns=5` per tenant and 10,000 tenants, that is 50,000 potential connections. The spike should measure pool creation overhead and validate that idle eviction keeps the active count manageable.
  - **PostgreSQL `max_connections` limit**: Default is 100. Spike must configure PostgreSQL with a higher limit or document the relationship between per-tenant `MaxConns` and total server `max_connections`.

---

### Task 3: Redis Streams Consumer Group Spike

- **Task ID:** MS-00-T3
- **Status:** Completed
- **Title:** Spike 2 -- Validate Redis Streams Consumer Groups with Dead-Letter Queue Pattern
- **Description:**
  Build a self-contained spike in `spikes/redis-streams/` that validates Redis Streams as a job queue replacement for dedicated brokers (ADR-002).

  The spike must implement and validate:
  1. **Job producer**: Enqueue 100 jobs to `moca:queue:{site}:default` stream using `XAdd`
  2. **Consumer group**: Create consumer group, consume with `XReadGroup`, acknowledge with `XAck`
  3. **At-least-once delivery**: Kill a consumer mid-processing, verify unacknowledged messages are re-deliverable via `XAutoClaim` or `XClaim`
  4. **Dead-letter queue**: After 3 failed delivery attempts (tracked via `XPendingExt` delivery count), move the job to `moca:deadletter:{site}` stream
  5. **Multiple consumers**: 3 consumers in the same group, verify messages are load-balanced (each message delivered to exactly one consumer)
  6. **Delayed execution**: Validate the `RunAfter` pattern -- enqueue with a future timestamp, consumer skips jobs where `time.Now() < RunAfter` and re-adds them
  7. **Stream naming**: Use the site-scoped naming convention from the design: `moca:queue:{site}:default`, `moca:queue:{site}:long`, `moca:queue:{site}:critical`
  8. **ADR document**: Record validated semantics, DLQ implementation pattern, and any limitations vs dedicated brokers

  Test setup:
  - Docker Compose with Redis 7
  - Go test suite exercising all scenarios

- **Why this task exists:** Redis Streams is chosen over RabbitMQ/SQS to avoid adding another infrastructure dependency. The queue system underpins background jobs, scheduled tasks, and webhook delivery. This spike validates that Redis Streams' consumer group semantics are sufficient for MOCA's workload patterns.
- **Dependencies:** MS-00-T1 (needs compilable Go workspace)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §5.2 lines 1073-1107 -- Job struct, WorkerPool struct, stream naming
  - `MOCA_SYSTEM_DESIGN.md` ADR-002 lines 2069-2073 -- Redis Streams over RabbitMQ/SQS
  - `ROADMAP.md` line 121 -- acceptance criteria for Spike 2
  - go-redis v9.18.0 API: `XAdd`, `XReadGroup`, `XAck`, `XClaim`, `XAutoClaim`, `XPendingExt`
- **Deliverable:**
  - `spikes/redis-streams/main.go` -- producer/consumer implementation
  - `spikes/redis-streams/main_test.go` -- test suite covering all 7 scenarios
  - `spikes/redis-streams/docker-compose.yml` -- Redis 7 container
  - `docs/ADR-002-redis-streams-queue.md` -- architecture decision record
  - All acceptance criteria from ROADMAP line 121 pass: 100 jobs enqueued, consumed with consumer group, all acknowledged, DLQ receives failed jobs after 3 retries
- **Risks / Unknowns:**
  - **ROADMAP mentions franz-go**: The ROADMAP line 112 says "verify franz-go or go-redis supports needed semantics." Franz-go is a **Kafka** client, not Redis. The spike should use **go-redis v9** exclusively. This should be noted as a documentation correction.
  - **Delayed execution pattern**: Redis Streams do not natively support delayed messages. The `RunAfter` pattern requires consumer-side filtering, which means the consumer must read the message, check the timestamp, and re-add it if not ready. This creates reprocessing overhead for heavily delayed workloads. The ADR should document this limitation and whether a sorted set (`ZADD` by timestamp) would be better for scheduled jobs.
  - **Stream memory growth**: Redis Streams grow unboundedly unless trimmed. The spike should test `MAXLEN` or `MINID` trimming and document the recommended retention strategy.

---

### Task 4: Go Workspace Composition and Cobra CLI Extension Spike

- **Task ID:** MS-00-T4
- **Status:** Completed
- **Title:** Spike 3 + Spike 5 -- Validate go.work Multi-Module Composition and Cobra Command Extension
- **Description:**
  Build a combined spike in `spikes/go-workspace/` and `spikes/cobra-ext/` that validates two related concerns: (1) multiple app modules compiling into a single binary via `go.work`, and (2) apps registering Cobra CLI commands that appear in the root command tree.

  These spikes are combined into one task because Spike 5 directly depends on Spike 3 (command extension requires multi-module workspace), and they share the same concern: multi-module composition at build time.

  **Spike 3 -- Go Workspace Composition:**
  1. Create `spikes/go-workspace/framework/go.mod` (the framework module)
  2. Create `spikes/go-workspace/apps/stub-a/go.mod` requiring `github.com/stretchr/testify v1.8.0`
  3. Create `spikes/go-workspace/apps/stub-b/go.mod` requiring `github.com/stretchr/testify v1.9.0` (intentional minor conflict)
  4. Create `spikes/go-workspace/go.work` composing all three modules
  5. Verify `go build ./...` resolves to testify v1.9.0 via MVS
  6. Create a major version conflict test case and document the failure mode
  7. Test `go build -race ./...` with all modules

  **Spike 5 -- Cobra CLI Extension:**
  1. Framework module defines a root Cobra command and a `cli.RegisterCommand()` function
  2. `stub-a` registers `stub-a:hello` command via `init()` in its `hooks.go`
  3. `stub-b` registers `stub-b:greet` command via `init()` in its `hooks.go`
  4. Main binary imports both apps (blank imports trigger init())
  5. Verify both commands appear in root command's `help` output
  6. Test both init()-based and explicit `NewCommand()` constructor patterns
  7. Document which pattern is recommended for MOCA

  **ADR documents:**
  - ADR for go.work MVS behavior, conflict resolution policy, `replace` directive strategy
  - ADR for Cobra extension pattern (init() vs explicit constructor)

- **Why this task exists:** The build composition model is foundational to MOCA's app system. Every app is a separate Go module that must compile into a single binary. If dependency conflicts between apps are unresolvable, or if apps cannot extend the CLI, the app architecture needs redesign. These two spikes validate that the build-time composition model works.
- **Dependencies:** MS-00-T1 (needs Go workspace structure)
- **Inputs / References:**
  - `docs/blocker-resolution-strategies.md` lines 7-63 -- go.work MVS, conflict resolution, replace directives
  - `MOCA_SYSTEM_DESIGN.md` lines 1380-1384 -- build composition model
  - `MOCA_SYSTEM_DESIGN.md` line 2056 -- go.work in package layout
  - `MOCA_CLI_SYSTEM_DESIGN.md` §8 lines 3363-3406 -- extension system, init() registration, command discovery
  - `MOCA_CLI_SYSTEM_DESIGN.md` lines 2287-2296 -- `moca build server` command
  - `ROADMAP.md` line 121-122 -- acceptance criteria for Spikes 3 and 5
  - Cobra v1.10.2 API: `cobra.Command`, `AddCommand`
- **Deliverable:**
  - `spikes/go-workspace/` -- multi-module workspace with intentional dependency conflicts
  - `spikes/cobra-ext/` -- Cobra command registration from multiple app modules
  - `docs/ADR-003-go-workspace-composition.md` -- documents MVS behavior, conflict policy, replace strategy
  - `docs/ADR-005-cobra-cli-extension.md` -- documents init() vs constructor pattern, recommendation
  - All acceptance criteria pass: compatible deps compile into one binary, intentional conflict documented, Cobra commands from both apps appear in help
- **Risks / Unknowns:**
  - **Major version conflicts**: Go treats `pkg` and `pkg/v2` as distinct module paths, which is handled correctly by Go modules. The spike should clarify that "major version conflict" in the traditional sense doesn't exist in Go -- it's a separate import path. The ADR should document this clearly for the team.
  - **init() ordering**: When multiple app modules register commands via `init()`, the execution order depends on import order in the main package. If two apps try to register the same command name, the second registration will silently overwrite the first. The spike must test this collision scenario and document the safeguard (namespace prefix: `app:command`).
  - **Build time**: With 10+ app modules in a workspace, build times may increase. The spike should measure incremental build time and report whether Go's build cache handles workspace modules efficiently.

---

### Task 5: Meilisearch Multi-Index Tenant Isolation Spike

- **Task ID:** MS-00-T5
- **Status:** Completed
- **Title:** Spike 4 -- Validate Meilisearch Prefix-Based Tenant Isolation and Bulk Indexing
- **Description:**
  Build a self-contained spike in `spikes/meilisearch/` that validates Meilisearch as the full-text search engine (ADR-006) with tenant isolation.

  The spike must implement and validate:
  1. **Index-per-tenant with prefix naming**: Create indexes `tenant_acme_products` and `tenant_globex_products`
  2. **Bulk indexing**: Index 1,000 documents into each tenant's index using `AddDocumentsInBatches`
  3. **Search with typo tolerance**: Query "prodct" (typo) and verify "product" results are returned
  4. **Tenant isolation**: Search on `tenant_acme_products` returns zero results from `tenant_globex_products`
  5. **Filterable attributes**: Configure `tenant_id`, `status`, `doctype` as filterable. Verify faceted filtering works.
  6. **Multi-search**: Use `MultiSearch` API to query across specific tenant indexes in a single request (for cross-tenant admin scenarios)
  7. **Alternative pattern evaluation**: Also test the tenant-token pattern (single index with JWT-enforced filtering) and compare with prefix-based isolation
  8. **ADR document**: Record the recommended tenant isolation strategy, bulk indexing performance numbers, and typo tolerance behavior

  Test setup:
  - Docker Compose with Meilisearch server (latest v1.x)
  - Go test suite exercising all scenarios

- **Why this task exists:** MOCA uses Meilisearch instead of Elasticsearch (ADR-006) for simpler operations and better typo tolerance. The multitenancy model requires tenant-isolated search. This spike validates that prefix-based index isolation works correctly and that bulk indexing performance is acceptable.
- **Dependencies:** MS-00-T1 (needs compilable Go workspace)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` ADR-006 lines 2093-2097 -- Meilisearch over Elasticsearch
  - `ROADMAP.md` line 122 -- acceptance criteria for Spike 4
  - Meilisearch Go SDK v0.36.1: `CreateIndex`, `AddDocumentsInBatches`, `Search`, `MultiSearch`, `GenerateTenantToken`
- **Deliverable:**
  - `spikes/meilisearch/main.go` -- spike implementation
  - `spikes/meilisearch/main_test.go` -- test suite for all 7 scenarios
  - `spikes/meilisearch/docker-compose.yml` -- Meilisearch container
  - `docs/ADR-006-meilisearch-tenant-isolation.md` -- architecture decision record with recommended isolation pattern
  - All acceptance criteria from ROADMAP line 122 pass: index created with tenant prefix, 1000 docs indexed, typo-tolerant search works, tenant isolation verified
- **Risks / Unknowns:**
  - **Meilisearch SDK is pre-1.0** (v0.36.1): API may change between minor versions. Pin the SDK version in `go.mod` and document the version dependency.
  - **Index count limits**: With 10,000 tenants and multiple doctypes, the number of Meilisearch indexes could reach 50,000+. The spike should test index creation/deletion at scale (create 100 indexes) and measure memory overhead. If this is problematic, the tenant-token pattern (single shared index) becomes the recommendation.
  - **Prefix naming convention**: The design says "prefix-based tenant isolation" but Meilisearch recommends the tenant-token approach. The spike should evaluate both and the ADR should document the trade-offs clearly. The tenant-token approach may be better at scale, while prefix-based is simpler to reason about.

---

## Recommended Execution Order

```
1. MS-00-T1  Project Scaffold and CI Pipeline
   |
   ├── 2a. MS-00-T2  PostgreSQL Schema-Per-Tenant Spike  ──┐
   │                                                        │  (parallel)
   ├── 2b. MS-00-T3  Redis Streams Consumer Group Spike   ──┤
   │                                                        │  (parallel)
   └── 2c. MS-00-T5  Meilisearch Multi-Index Spike        ──┘
   |
3. MS-00-T4  Go Workspace + Cobra CLI Extension Spike
```

**Rationale:**
- T1 must be first (creates the workspace and CI).
- T2, T3, T5 are independent infrastructure spikes that can run in parallel (different external dependencies: PostgreSQL, Redis, Meilisearch).
- T4 should run after T1 (needs the workspace), and benefits from running after T2/T3/T5 because the workspace composition spike tests multi-module builds that may include the other spike dependencies.
- Total estimated wall-clock: 2 weeks with 2 developers working in parallel.

---

## Open Questions

1. **franz-go reference in ROADMAP**: Line 112 says "verify franz-go or go-redis." Franz-go is a Kafka client. Should this be corrected to "go-redis" only? (Research confirms go-redis v9 is the correct choice for Redis Streams.)

2. **Meilisearch isolation strategy**: The ROADMAP specifies "prefix-based tenant isolation" but Meilisearch recommends tenant tokens. The spike should evaluate both, but which should be the default recommendation if both work?

3. **Per-tenant pool MaxConns**: The blocker resolution doc shows a configurable `perTenantMaxConns`. What should the default be? With 10,000 tenants and PostgreSQL default `max_connections=100`, the math doesn't work without PgBouncer. This is called out as ROADMAP OQ-1 (line 1268) but should be explored during the spike.

4. **ADR numbering**: The spikes produce ADR documents. Should these follow the numbering from `MOCA_SYSTEM_DESIGN.md` §16 (ADR-001 through ADR-007), or start a new spike-specific series? The existing ADR-001 is "Schema-Per-Tenant" which directly corresponds to Spike 1.

---

## Out of Scope for This Milestone

- **Production code**: All spike code is throwaway. No code from `spikes/` should be promoted to `pkg/` or `cmd/`.
- **CLI implementation**: No Cobra command tree beyond the spike validation in T4.
- **Frontend**: No React, TypeScript, or desk/ directory content.
- **Configuration system**: `moca.yaml` parsing is MS-01.
- **Actual multitenancy**: The PostgreSQL spike validates the pool pattern, not the full tenant lifecycle (site creation, migration, etc.). That is MS-02 and MS-12.
- **Kafka**: Kafka validation is not part of MS-00. Kafka integration is MS-15, with Blocker 4 (Kafka-optional fallback) implemented there.
- **PgBouncer/external pooling**: OQ-1 asks about external poolers for 10,000+ tenants. This spike validates the per-tenant pool pattern at small scale (10 tenants). External pooler evaluation is deferred.
