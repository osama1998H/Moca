# MS-15 — Background Jobs, Scheduler, Kafka/Redis Events, Search Sync Plan

## Milestone Summary

- **ID:** MS-15
- **Name:** Background Jobs, Scheduler, Kafka/Redis Events, Search Sync
- **Roadmap Reference:** ROADMAP.md → MS-15 section (lines 845-891)
- **Goal:** Implement background jobs (Redis Streams workers, DLQ), cron scheduler, Kafka event producer (with Redis fallback), transactional outbox, and Meilisearch sync.
- **Why it matters:** Many features produce async work (audit, search, webhooks, email). The scheduler enables recurring tasks. Events enable the integration backbone. This is the async infrastructure that 6+ downstream milestones depend on.
- **Position in roadmap:** Order #16 of 30 milestones (critical path: MS-12 → MS-15 → MS-23 → MS-25)
- **Upstream dependencies:** MS-04 (Document Runtime), MS-06 (REST API), MS-12 (Multitenancy)
- **Downstream dependencies:** MS-16 (CLI Queue/Events/Search), MS-18 (API Keys/Webhooks), MS-19 (Desk Real-Time), MS-21 (Deployment), MS-22 (Security Hardening), MS-23 (Workflow Engine), MS-24 (Observability)

## Vision Alignment

MS-15 is the **async backbone** of Moca. The framework's MetaType-driven architecture promises that a single definition drives database schema, API generation, search indexing, and event streaming. Until MS-15, all document operations are synchronous — there's no way to trigger background work, schedule recurring tasks, or keep Meilisearch indexes in sync with PostgreSQL.

This milestone transforms Moca from a synchronous CRUD framework into an event-driven platform. The transactional outbox pattern (already partially implemented — `pkg/document/crud.go:520-531` writes outbox rows in document transactions) gets its consumer. The event producer abstraction with Kafka/Redis fallback enables small deployments (Redis-only) and large deployments (Kafka) from the same codebase. Meilisearch search sync closes the loop: document save → outbox → event → search index.

The three standalone binaries (`moca-worker`, `moca-scheduler`, `moca-outbox`) move from placeholder stubs to production processes, completing the process architecture that MS-10 scaffolded with the goroutine supervisor.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `MOCA_SYSTEM_DESIGN.md` | §5.2 Queue Layer (Redis Streams) | 1073-1115 | Stream naming, Job struct, WorkerPool design |
| `MOCA_SYSTEM_DESIGN.md` | §6.1 Topic Design | 1089-1160 | Kafka topic definitions and partitioning |
| `MOCA_SYSTEM_DESIGN.md` | §6.2 Event Schema | 1162-1190 | DocumentEvent struct definition |
| `MOCA_SYSTEM_DESIGN.md` | §6.3 Event Consumers | 1192-1203 | Consumer topology (webhook, search, ERP) |
| `MOCA_SYSTEM_DESIGN.md` | §6.4 Transactional Outbox | 1205-1233 | Outbox table DDL, poller pattern |
| `MOCA_SYSTEM_DESIGN.md` | §6.5 Kafka-Optional Architecture | 1235-1260 | Feature matrix with/without Kafka |
| `ROADMAP.md` | MS-15 | 845-891 | Milestone definition, acceptance criteria |
| `spikes/redis-streams/ADR-002-redis-streams-queue.md` | Full | — | Validated: consumer groups, DLQ, delayed jobs |
| `spikes/meilisearch/ADR-006-meilisearch-tenant-isolation.md` | Full | — | Validated: index-per-tenant, bulk indexing |
| `docs/blocker-resolution-strategies.md` | Blocker 4 | 271-387 | Kafka-optional detection layers |
| `docs/moca-cross-doc-mismatch-report.md` | MISMATCH-004, -006, -019 | — | Kafka fallback, search process, Redis key collision |

## Research Notes

No external web research was needed. All implementation patterns are fully validated in the MS-00 spikes:

- **Redis Streams** (ADR-002): Consumer groups with XAutoClaim, DLQ via XPendingExt → XClaim → XAdd, delayed jobs via ZADD sorted set — all 7 spike tests pass with `-race`.
- **Meilisearch** (ADR-006): Index-per-tenant naming `{site}_{doctype}`, bulk indexing via `AddDocumentsInBatches(250)`, typo tolerance zero-config — all 7 spike tests pass with `-race`.
- **Kafka client**: ROADMAP OQ-3 recommends `franz-go` (pure Go, no CGo). This is the right choice for Moca's deployment simplicity goals.
- **Cron parsing**: `github.com/robfig/cron/v3` is the standard Go cron library.

## Milestone Plan

### Task 1

- **Task ID:** MS-15-T1
- **Title:** Redis Streams Job Queue — Producer, Worker Pool, DLQ, Delayed Jobs
- **Status:** Completed
- **Description:**
  Implement the full job queue system in `pkg/queue/`. This is the foundational async layer that everything else builds upon.

  **Producer** (`pkg/queue/producer.go`):
  - `Enqueue(ctx, site, job) (string, error)` — XADD to `moca:queue:{site}:{queueType}` with MAXLEN trimming (~10,000 for default/critical, ~1,000 for long/scheduler)
  - `EnqueueDelayed(ctx, site, job, runAfter) error` — ZADD to `moca:delayed:{site}` sorted set with score = `runAfter.UnixNano()`

  **Job struct** (`pkg/queue/job.go`):
  - Promote from spike: `Job{ID, Site, Type, Payload, Priority, MaxRetries, Retries, CreatedAt, RunAfter, Timeout}`
  - Queue type constants: `default`, `long`, `critical`, `scheduler`
  - Stream key builders: `StreamKey(site, queueType)`, `DLQKey(site)`, `DelayedKey(site)`

  **Consumer** (`pkg/queue/consumer.go`):
  - XReadGroup with configurable block duration, per-message XAck
  - Consumer group auto-creation (XGROUP CREATE with MKSTREAM)
  - Graceful shutdown via context cancellation

  **Worker Pool** (`pkg/queue/worker.go`):
  - `WorkerPool` manages N goroutine consumers per stream
  - Handler registry: `map[string]JobHandler` where `JobHandler = func(ctx, Job) error`
  - `Start(ctx)` / `Stop()` lifecycle

  **DLQ** (`pkg/queue/dlq.go`):
  - Monitor pending messages via XPendingExt
  - Messages exceeding MaxRetries (default 3): XClaim → XAdd to `moca:deadletter:{site}` → XAck original
  - Exponential backoff: `min(baseDelay * 2^retries, maxDelay)`

  **Delayed Job Promoter** (`pkg/queue/delayed.go`):
  - Goroutine polls ZRANGEBYSCORE on `moca:delayed:{site}` every 500ms
  - Moves due jobs to target stream via XAdd, removes from sorted set via ZREM

  **moca-worker binary** (`cmd/moca-worker/main.go`):
  - Replace placeholder with: load config → connect Redis (Queue client, db1) → register handlers → start WorkerPool → graceful shutdown

- **Why this task exists:** The job queue is the foundation. The scheduler (T2) enqueues to it. Search sync (T4) uses it as fallback when Kafka is disabled. The worker binary consumes from it.
- **Dependencies:** None (first task)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §5.2 (lines 1073-1115) — stream naming, Job struct, WorkerPool
  - `spikes/redis-streams/main.go` — validated producer/consumer/DLQ patterns
  - `spikes/redis-streams/ADR-002-redis-streams-queue.md` — architectural decisions
  - `internal/drivers/redis.go` — Queue client (db1) ready to use
  - `pkg/queue/doc.go` — existing stub to build upon
  - `internal/serve/stubs.go:10` — `WorkerStub` to replace
  - `cmd/moca/serve.go:96` — supervisor wiring point
- **Deliverable:**
  - `pkg/queue/job.go`, `producer.go`, `consumer.go`, `worker.go`, `dlq.go`, `delayed.go`, `options.go`
  - `pkg/queue/*_test.go` (unit tests with miniredis)
  - `cmd/moca-worker/main.go` (rewritten)
- **Acceptance Criteria:**
  - `queue.Enqueue(site, job)` adds to correct Redis Stream; worker consumes and acknowledges
  - Failed job (3 retries) moves to DLQ stream `moca:deadletter:{site}`
  - Delayed job with `RunAfter` in the future is promoted to target stream when due
  - `moca-worker` binary starts, consumes jobs, shuts down gracefully on SIGTERM
  - All tests pass with `-race`
- **Risks / Unknowns:**
  - Memory management: MAXLEN trimming values may need tuning under load
  - Consumer group rebalancing on worker restart — XAutoClaim with MinIdle 30s handles this per spike validation

---

### Task 2

- **Task ID:** MS-15-T2
- **Title:** Cron Scheduler with Leader Election
- **Status:** Completed
- **Description:**
  Implement the cron scheduler in `pkg/queue/` and the standalone `moca-scheduler` binary. The scheduler runs on a single leader (elected via Redis distributed lock) and enqueues jobs to the queue system from T1 on cron schedules.

  **Scheduler** (`pkg/queue/scheduler.go`):
  - Cron expression parsing via `github.com/robfig/cron/v3` (new dependency)
  - `Scheduler` struct holds registered cron entries: `{CronExpr, JobType, Payload, Site, QueueType}`
  - `Register(cronExpr, site, jobType, payload)` adds a cron entry
  - `Run(ctx)` ticks every second, checks all entries against current time, enqueues matching jobs via the Producer from T1
  - Configurable tick interval from `SchedulerConfig.TickInterval` (`internal/config/types.go:172-175`)

  **Leader Election** (`pkg/queue/leader.go`):
  - Redis distributed lock: `SET moca:scheduler:leader {instanceID} NX EX 30`
  - Heartbeat renewal every 10s (extends TTL to 30s)
  - Automatic release on context cancellation / shutdown
  - Non-leader instances poll every 5s to attempt acquisition
  - Uses the Queue Redis client (db1)

  **moca-scheduler binary** (`cmd/moca-scheduler/main.go`):
  - Replace placeholder with: load config → connect Redis → acquire leader lock → register cron entries from config/hooks → run scheduler loop → graceful shutdown
  - Non-leader mode: log "waiting for leader", poll until acquired

- **Why this task exists:** The scheduler enables recurring tasks (report generation, cleanup, email digests). Leader election ensures exactly-once execution across replicas, which is critical for production deployments.
- **Dependencies:** MS-15-T1 (scheduler enqueues jobs via Producer)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §5.2 (lines 1073-1115) — scheduler stream `moca:queue:{site}:scheduler`
  - `MOCA_SYSTEM_DESIGN.md` §5.3 — distributed locks pattern `moca:lock:{site}:{resource}`
  - `internal/config/types.go:171-175` — `SchedulerConfig{TickInterval, Enabled}`
  - `internal/serve/stubs.go:20` — `SchedulerStub` to replace
  - `cmd/moca/serve.go:99` — supervisor wiring point
- **Deliverable:**
  - `pkg/queue/scheduler.go`, `pkg/queue/leader.go`
  - `pkg/queue/scheduler_test.go`, `pkg/queue/leader_test.go`
  - `cmd/moca-scheduler/main.go` (rewritten)
- **Acceptance Criteria:**
  - Scheduler fires cron jobs within 1-second accuracy
  - Single leader across replicas: two scheduler instances, only one fires jobs
  - Leader failover: if leader crashes, standby acquires lock within 30s
  - `moca-scheduler` binary starts, acquires lock, runs scheduler, shuts down gracefully
  - All tests pass with `-race`
- **Risks / Unknowns:**
  - Cron expression parsing edge cases (timezone handling, DST transitions)
  - Clock skew between replicas could cause duplicate firings — mitigated by leader election (only one instance fires)

---

### Task 3

- **Task ID:** MS-15-T3
- **Title:** Event Producer Abstraction — Kafka + Redis Pub/Sub Fallback
- **Status:** Not Started
- **Description:**
  Implement the event producer abstraction in `pkg/events/` with dual backends: Kafka (franz-go) when `kafka.enabled: true`, Redis pub/sub when `kafka.enabled: false`.

  **Event Schema** (`pkg/events/event.go`):
  - `DocumentEvent` struct from design doc §6.2: `{EventID, EventType, Timestamp, Source, Site, DocType, DocName, Action, User, Data, PrevData, RequestID}`
  - Event type constants: `doc.created`, `doc.updated`, `doc.submitted`, `doc.cancelled`, `doc.deleted`
  - Topic constants from §6.1: `moca.doc.events`, `moca.audit.log`, `moca.meta.changes`, `moca.integration.outbox`, `moca.search.indexing`, etc.

  **Producer Interface** (`pkg/events/producer.go`):
  ```go
  type Producer interface {
      Publish(ctx context.Context, topic string, event DocumentEvent) error
      Close() error
  }
  ```

  **Kafka Backend** (`pkg/events/kafka.go`):
  - New dependency: `github.com/twmb/franz-go` (pure Go, no CGo — per ROADMAP OQ-3)
  - Connects to brokers from `KafkaConfig.Brokers` (`internal/config/types.go:85-91`)
  - Partition key: `{site}:{doctype}` for ordering guarantees per tenant+doctype
  - JSON serialization of DocumentEvent

  **Redis Fallback** (`pkg/events/redis.go`):
  - Uses PubSub Redis client (db3) from `internal/drivers/redis.go`
  - `PUBLISH` to channel matching topic name
  - Fire-and-forget semantics (no persistence, no replay)

  **Factory** (`pkg/events/factory.go`):
  - `NewProducer(cfg KafkaConfig, redisClients RedisClients) (Producer, error)`
  - Reads `KafkaConfig.Enabled` — returns Kafka or Redis backend accordingly
  - Logs feature matrix warning when Kafka disabled (per blocker-resolution-strategies.md)

  **Replace no-op Emitter** (`pkg/events/emitter.go`):
  - Replace current `Emitter.Emit()` no-op with delegation to Producer interface
  - Maintain backward compatibility for any existing callers

- **Why this task exists:** The event producer is the abstraction layer that the outbox poller (T4) publishes through. It also enables the Kafka-optional architecture that's a key Moca design principle — small deployments use Redis, large ones use Kafka, same codebase.
- **Dependencies:** None (independent of T1/T2; uses Redis PubSub client which already exists)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §6.1 (lines 1089-1160) — topic design
  - `MOCA_SYSTEM_DESIGN.md` §6.2 (lines 1162-1190) — DocumentEvent schema
  - `MOCA_SYSTEM_DESIGN.md` §6.5 (lines 1235-1260) — Kafka-optional feature matrix
  - `docs/blocker-resolution-strategies.md` (lines 271-387) — detection layers for Kafka mode
  - `internal/config/types.go:85-91` — `KafkaConfig{Enabled *bool, Brokers []string}`
  - `internal/drivers/redis.go` — PubSub client (db3)
  - `pkg/events/emitter.go` — existing no-op to replace
  - `pkg/events/doc.go` — existing stub
- **Deliverable:**
  - `pkg/events/event.go`, `producer.go`, `kafka.go`, `redis.go`, `factory.go`
  - `pkg/events/emitter.go` (rewritten)
  - `pkg/events/*_test.go`
- **Acceptance Criteria:**
  - With `kafka.enabled: true`, events publish to Kafka topic with correct partition key
  - With `kafka.enabled: false`, events publish to Redis pub/sub channel
  - Factory correctly reads config and returns appropriate backend
  - Startup logs feature matrix when Kafka disabled
  - All tests pass with `-race`
- **Risks / Unknowns:**
  - franz-go learning curve (spike validated Redis Streams, not Kafka) — mitigated by clean Producer interface that isolates Kafka-specific code
  - Kafka topic auto-creation vs. pre-creation: decide whether the producer creates topics on first publish or requires them to exist

---

### Task 4

- **Task ID:** MS-15-T4
- **Title:** Transactional Outbox Poller + Meilisearch Search Sync + Search API
- **Status:** Not Started
- **Description:**
  Implement the outbox poller that reads `tab_outbox` and publishes events, the Meilisearch indexer and search sync pipeline, and the Search API endpoint.

  **Outbox Poller** (`pkg/events/outbox.go`):
  - Polls `tab_outbox` every 100ms per the design spec
  - Query: `SELECT id, event_type, topic, partition_key, payload FROM tab_outbox WHERE status = 'pending' ORDER BY id LIMIT 100`
  - For each row: call `Producer.Publish()` (from T3) with the topic and payload
  - On success: `UPDATE tab_outbox SET status = 'published', published_at = NOW() WHERE id = $1`
  - Multi-tenant: iterate active sites via `pkg/tenancy/Manager`, set search_path per site
  - Batch processing: commit status updates in batches for throughput
  - Error handling: mark individual rows as `failed` after N retries, continue processing others

  **moca-outbox binary** (`cmd/moca-outbox/main.go`):
  - Replace placeholder with: load config → connect PG + Redis → create event Producer → run outbox poll loop → graceful shutdown

  **Meilisearch Client** (`pkg/search/client.go`):
  - Wrapper around `github.com/meilisearch/meilisearch-go` (new dependency, validated in spike)
  - Constructor reads `SearchConfig{Engine, Host, APIKey, Port}` from `internal/config/types.go:94-99`

  **Indexer** (`pkg/search/indexer.go`):
  - `EnsureIndex(site, doctype, filterableAttrs)` — creates index `{site}_{doctype}` via `SiteContext.PrefixSearchIndex()` (`pkg/tenancy/site.go:37-38`), configures filterable attributes (`tenant_id`, `status`, `doctype`, `category`), waits for task completion
  - `DeleteIndex(site, doctype)` — removes index
  - `IndexDocuments(site, doctype, docs)` — bulk index via `AddDocumentsInBatches(docs, 250)`
  - `RemoveDocument(site, doctype, docName)` — delete single document from index

  **Search Sync** (`pkg/search/sync.go`):
  - Subscribes to document events (from Kafka consumer or Redis pub/sub depending on mode)
  - Maps event actions to Meilisearch operations: `insert`/`update` → IndexDocuments, `delete` → RemoveDocument
  - When `kafka.enabled: false`: registers as a background job handler in the queue system (T1), processes search sync jobs enqueued by document lifecycle hooks

  **Search Query** (`pkg/search/query.go`):
  - `Search(site, doctype, query, filters, page, limit) ([]SearchResult, int, error)`
  - Calls Meilisearch Search() on the correct index
  - Maps results back to document references (doctype + name)

  **Search API** (`pkg/api/search.go`):
  - `GET /api/v1/search?q=...&doctype=...&page=...&limit=...`
  - Uses tenant context from middleware to scope to correct site
  - Delegates to `pkg/search/query.go`
  - Wire into `pkg/api/gateway.go` route registration

- **Why this task exists:** This task closes the full pipeline: document save → outbox → event → Meilisearch → search API. It's the end-to-end proof that the async backbone works. The outbox poller is what makes the transactional outbox pattern (already half-implemented in `pkg/document/crud.go:520-531`) actually publish events.
- **Dependencies:** MS-15-T1 (search sync as background job when Kafka disabled), MS-15-T3 (outbox publishes via Producer)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §6.4 (lines 1205-1233) — outbox table DDL, poller pattern
  - `MOCA_SYSTEM_DESIGN.md` §6.5 (lines 1235-1260) — search sync behavior with/without Kafka
  - `spikes/meilisearch/main.go` — validated indexer, bulk indexing, search patterns
  - `spikes/meilisearch/ADR-006-meilisearch-tenant-isolation.md` — index-per-tenant decisions
  - `pkg/document/crud.go:520-531` — `insertOutbox()` already writes to `tab_outbox`
  - `pkg/tenancy/site.go:37-38` — `PrefixSearchIndex()` for index naming
  - `internal/config/types.go:94-99` — `SearchConfig`
  - `internal/serve/stubs.go:30` — `OutboxStub` to replace
  - `cmd/moca/serve.go:101` — supervisor wiring point
  - `pkg/api/gateway.go` — route registration for search endpoint
- **Deliverable:**
  - `pkg/events/outbox.go`, `pkg/events/outbox_test.go`
  - `pkg/search/client.go`, `indexer.go`, `sync.go`, `query.go`
  - `pkg/search/*_test.go`
  - `pkg/api/search.go`, `pkg/api/search_test.go`
  - `cmd/moca-outbox/main.go` (rewritten)
- **Acceptance Criteria:**
  - Document insert → outbox row → outbox poller publishes event → Meilisearch indexed
  - Search API returns matching documents from Meilisearch
  - With `kafka.enabled: false`, search sync runs via Redis Streams background job
  - `moca-outbox` binary starts, polls, publishes, shuts down gracefully
  - Bulk indexing handles 250-document batches correctly
  - All tests pass with `-race`
- **Risks / Unknowns:**
  - Meilisearch eventual consistency lag: document may not be searchable immediately after save (~50-200ms)
  - Multi-tenant outbox polling: iterating all site schemas every 100ms could be expensive with many tenants — may need per-site polling intervals or change notification
  - Outbox table growth: need VACUUM/cleanup strategy for published rows

---

### Task 5

- **Task ID:** MS-15-T5
- **Title:** Integration Wiring, Dev Server Composition, and End-to-End Tests
- **Status:** Not Started
- **Description:**
  Wire all subsystems into the dev server supervisor, finalize standalone binaries, update build infrastructure, and write end-to-end integration tests.

  **Dev Server Wiring** (`cmd/moca/serve.go`):
  - Replace stub references (lines 96-101) with real subsystems:
    - `serve.WorkerStub(logger)` → real WorkerPool from T1
    - `serve.SchedulerStub(logger)` → real Scheduler from T2
    - `serve.OutboxStub(logger)` → real Outbox Poller from T4
  - Pass `RedisClients`, `KafkaConfig`, `SearchConfig` through to subsystem constructors

  **Server Composition** (`internal/serve/server.go`):
  - Extend `Server` struct to hold event `Producer` and search `Indexer`
  - Inject into Gateway for search API endpoint access
  - Remove or deprecate stubs in `internal/serve/stubs.go`

  **Build Infrastructure**:
  - `docker-compose.yml`: add Meilisearch service for integration tests
  - `Makefile`: ensure `make build` builds all 5 binaries including updated worker/scheduler/outbox
  - `go.mod` / `go.sum`: add `franz-go`, `meilisearch-go`, `robfig/cron/v3` dependencies

  **Integration Tests** (build tag `integration`):
  - `pkg/queue/integration_test.go`: enqueue 100 jobs across 3 queues, verify all consumed and acknowledged; fail jobs to verify DLQ; test delayed promotion
  - `pkg/events/integration_test.go`: publish events via Kafka backend (if available) and Redis fallback; verify consumer receives
  - `pkg/search/integration_test.go`: create index, index documents, search, verify results (requires Meilisearch container)
  - End-to-end pipeline test: HTTP POST create document → verify `tab_outbox` row → run outbox poller → verify event published → verify Meilisearch indexed → HTTP GET `/api/v1/search` returns document

- **Why this task exists:** Individual subsystems from T1-T4 are tested in isolation. This task proves they compose correctly under the dev server supervisor and validates the full pipeline end-to-end. It also finalizes build infrastructure so CI can run the full test suite.
- **Dependencies:** MS-15-T1, MS-15-T2, MS-15-T3, MS-15-T4 (all must be complete)
- **Inputs / References:**
  - `cmd/moca/serve.go:96-101` — stub wiring to replace
  - `internal/serve/stubs.go` — stubs to remove
  - `internal/serve/server.go` — server composition to extend
  - `docker-compose.yml` — add Meilisearch service
  - `Makefile` — build targets
  - All acceptance criteria from ROADMAP.md MS-15 (lines 862-869)
- **Deliverable:**
  - `cmd/moca/serve.go` (updated wiring)
  - `internal/serve/server.go` (extended)
  - `internal/serve/stubs.go` (removed or deprecated)
  - `docker-compose.yml` (Meilisearch added)
  - `pkg/queue/integration_test.go`
  - `pkg/events/integration_test.go`
  - `pkg/search/integration_test.go`
  - End-to-end test file
- **Acceptance Criteria:**
  - `moca serve` starts all subsystems (HTTP, worker, scheduler, outbox) without errors
  - Full pipeline: document insert → outbox → event → Meilisearch → search API (verified in integration test)
  - `kafka.enabled: false` mode: full pipeline works with Redis fallback
  - All three standalone binaries (`moca-worker`, `moca-scheduler`, `moca-outbox`) start correctly
  - `make test-integration` passes with all new tests
  - `make build` produces all 5 binaries
- **Risks / Unknowns:**
  - CI environment needs Meilisearch container — add to docker-compose.yml
  - Integration test timing: eventual consistency in search sync means tests may need short polling/retry loops

## Recommended Execution Order

1. **MS-15-T1** (Queue) — foundation; no dependencies; enables T2 and T4
2. **MS-15-T3** (Event Producer) — independent of T1; can be developed in parallel; enables T4
3. **MS-15-T2** (Scheduler) — depends on T1 Producer; smaller scope
4. **MS-15-T4** (Outbox + Search) — depends on T1 and T3; largest task; closes the pipeline
5. **MS-15-T5** (Integration) — depends on all; proves composition

**Parallelization opportunity:** T1 and T3 can be developed simultaneously by two engineers.

## Open Questions

1. **Kafka topic auto-creation**: Should the event producer auto-create topics on first publish, or require pre-creation via a CLI command (`moca kafka setup-topics`)? Auto-creation is simpler for dev; pre-creation is safer for production.
2. **Outbox cleanup**: Published outbox rows accumulate. Need a retention policy — delete after 7 days? Archive to audit table? This could be a scheduler cron job.
3. **Search sync on site creation**: Should `EnsureIndex` be called automatically when a new site is created via `pkg/tenancy/Manager`, or only when the first document of a doctype is saved?
4. **franz-go version**: Pin to latest stable. Verify compatibility with Kafka versions commonly available (2.8+).

## Out of Scope for This Milestone

- `moca-search-sync` as a separate standalone process (in-process for now, per ROADMAP)
- Change Data Capture (CDC) — requires Kafka, deferred
- Webhooks — MS-18
- CLI commands for queue/events/search management — MS-16
- GraphQL subscriptions for real-time events — MS-19/MS-20
- Kafka consumer groups for external apps — future milestone
- Prometheus metrics for queue/events/search — MS-24
