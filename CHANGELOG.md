# Changelog

All notable changes to the Moca framework will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - Unreleased

### Added

#### Documentation & Packaging (MS-26)
- Apache-2.0 `LICENSE` file
- `CONTRIBUTING.md` with links to wiki guides (Development Setup, Code Conventions, Testing Guide, CI/CD Pipeline)
- `.goreleaser.yml` for cross-compilation of all 5 binaries (linux/darwin/windows, amd64/arm64)
- `Makefile` `release-local` target for local GoReleaser snapshot builds
- Multi-arch Docker images for `moca-server`, `moca-worker`, `moca-scheduler`, `moca-outbox`
- Auto-generated CLI reference docs (`moca docs generate`)
- Deployment guides (bare-metal, Docker Compose, Kubernetes)
- Wiki updates covering all milestones MS-00 through MS-26

## [0.4.0-rc] - 2026-04-13

### Added

#### Observability (MS-24)
- Prometheus metrics exporter with 13 custom metrics (request latency, queue depth, cache hit rate, search index lag, tenant count, and more)
- OpenTelemetry OTLP trace exporter with span propagation across HTTP, queue, and scheduler boundaries
- `moca doctor` extended with live infra health checks (PG replication lag, Redis memory, Meilisearch index health)
- `moca monitor metrics` command with real-time Prometheus scrape output
- `moca dev bench` command for local benchmark execution against live services
- `moca dev profile` command for CPU/memory profiling with pprof HTTP endpoint
- Jaeger-compatible trace export via OTLP HTTP

#### Testing Framework (MS-25)
- `moca test run` command with parallel test execution, build-tag filtering, and structured JSON output
- Fixture generation (`moca test fixture`) producing deterministic seed data from MetaType definitions
- Coverage report aggregation and HTML export (`moca test coverage`)
- Test data factory system with relationship-aware generation for all 35 FieldTypes
- Integration test helpers: `IntegrationSuite`, `WithTenant`, `WithFixtures` composable test builders
- Benchmarking helpers in `internal/testutil/bench/` with warm-up, cooldown, and stats reporting

## [0.3.0-beta] - 2026-04-07

### Added

#### React Desk Foundation (MS-17)
- React 19 + TypeScript app shell with Vite build pipeline served at `/desk/`
- `MetaProvider` context supplying live MetaType definitions to all child components
- `FormView` auto-rendered from MetaType field definitions with full validation
- `ListView` with server-side filtering, sorting, and pagination
- 29 field type components covering all storable FieldTypes (Text, Int, Float, Date, DateTime, Link, Select, MultiSelect, Table, Attach, etc.)
- `@moca/desk` npm package for embedding the Desk UI in external React apps

#### API Extensions (MS-18)
- API key management: create, rotate, revoke with per-key permission scopes
- Webhook system with HMAC-SHA256 signatures, retry with exponential backoff, and dead-letter queue
- Custom endpoint registration via `AppManifest` (`endpoints:` section)
- `APIConfig` per-tenant rate limit and CORS overrides

#### Realtime & Customization (MS-19)
- WebSocket event bus (`/desk/ws`) broadcasting document lifecycle events to subscribed clients
- Custom field overlay system: add/remove fields on any DocType without modifying the base MetaType
- Document version tracking with full diff storage and `moca doc versions` CLI command

#### Platform Capabilities (MS-20)
- GraphQL schema auto-generated from MetaType definitions with DataLoader batching
- Dashboard engine with configurable card/chart widgets persisted as `DashboardDef` documents
- Report builder UI and `ReportDef` execution API with CSV/XLSX export
- i18n translation layer with per-tenant locale overrides stored in Redis
- File storage adapter (S3/MinIO) with presigned URL generation and virus-scan hook

#### Developer Tooling (MS-21)
- `moca generate` with 7 generators: `doctype`, `controller`, `hook`, `migration`, `report`, `dashboard`, `api-endpoint`
- `moca deploy` with 6 sub-commands: `push`, `migrate`, `seed`, `rollback`, `status`, `diff`
- Backup automation: scheduled site backups to S3 with retention policy and `moca backup restore`

#### Auth & Notifications (MS-22)
- OAuth2 provider (Authorization Code + PKCE) with per-tenant client registry
- SAML 2.0 and OIDC identity provider connectors
- Field-level encryption for sensitive data at rest (AES-256-GCM, key rotation)
- Notification engine with Email, SMS, and in-app channels; template rendering via Go `text/template`

#### Workflow Engine (MS-23)
- State machine core: `WorkflowDef`, `StateTransition`, guard conditions, and side-effect hooks
- SLA timer management with Redis-backed deadlines and escalation callbacks
- Approval chain support: sequential/parallel approvers, delegate, and bulk-approve APIs
- `moca workflow` CLI: `list`, `advance`, `history`, `retry`, `cancel` sub-commands

## [0.2.0-alpha] - 2026-04-01

### Added

#### CLI Operational Commands (MS-11)
- `moca db migrate/rollback/diff` with `DependsOn` ordering, `--dry-run`, and `tab_migration_log` version tracking
- `moca backup create/restore/list` with gzip compression and S3-compatible upload
- `moca config get/set/list/reset` with live config reload without server restart

#### Multitenancy (MS-12)
- Schema-per-tenant isolation enforced via `AfterConnect` callback setting `search_path` per pool
- Per-tenant connection pool registry with idle eviction and health probing
- `SiteManager`: create, drop, list, and context-switch sites
- `moca site create/drop/list/use/info` commands with full 9-step site creation lifecycle

#### App Scaffolding & User Management (MS-13)
- `moca app new` scaffolding: generates `AppManifest`, module skeleton, and example DocType JSON
- `moca user create/list/set-password/add-role/remove-role` commands
- `moca dev shell` interactive REPL with pre-loaded site context and ORM helpers
- `moca dev routes` command listing all registered HTTP routes with method and middleware chain

#### Permission Engine (MS-14)
- Role-Based Access Control (RBAC): `DocPerm` rules evaluated per DocType per role
- Field-Level Security (FLS): per-field read/write restrictions enforced in API transformers
- Row-Level Security (RLS): `match_conditions` filter appended to all queries for restricted roles
- `moca perm check/explain` CLI commands for permission debugging

#### Background Jobs, Events & Search (MS-15)
- Redis Streams job queue with XAutoClaim at-least-once delivery and DLQ after max retries
- Cron scheduler with leader election (Redis SETNX) and missed-run catch-up
- Kafka event backend with transactional outbox poller (`moca-outbox`)
- Redis pub/sub fallback for low-volume deployments
- Meilisearch sync daemon with per-tenant index (`{site}_{doctype}`) and `waitForTask` write guarantees

#### CLI Queue/Events/Search/Monitor Commands (MS-16)
- `moca queue list/purge/retry/dlq` — inspect and manage Redis Streams jobs
- `moca events emit/listen/replay` — trigger and subscribe to platform events
- `moca search index/query/sync/reset` — manage Meilisearch indexes per site
- `moca monitor log/tail/status` — structured log streaming and process status

## [0.1.0-mvp] - 2026-04-02

### Added

#### Architecture & Project Foundation
- 5 architecture validation spikes (PostgreSQL schema-per-tenant, Redis Streams, Go workspace, Meilisearch multi-index, Cobra CLI extensions)
- 5 ADR documents (ADR-001 through ADR-006) capturing proven patterns from spike results
- Go workspace (`go.work`) with multi-module composition
- CI pipeline with build, test, and lint stages
- 15 `pkg/` packages with canonical directory structure
- 5 binary entry points: `moca`, `moca-server`, `moca-worker`, `moca-scheduler`, `moca-outbox`

#### Configuration
- `moca.yaml` configuration parser with environment variable expansion (`${VAR}`)
- Configuration validation with field-path error messages
- Config inheritance (staging inherits production)

#### PostgreSQL & Multitenancy
- `DBManager` with per-tenant connection pools and schema isolation via `AfterConnect` callback
- `TxManager` with `WithTransaction(ctx, pool, fn)` pattern and panic recovery
- System schema DDL (`moca_system`: sites, apps, site_apps tables)
- Schema-per-tenant isolation enforced at connection level

#### Redis
- Redis client factory for 4 logical databases (cache, queue, session, pubsub)

#### Observability
- Structured logging via `slog` with site, user, and request_id context
- Health check endpoints (`/health`, `/health/ready`, `/health/live`)

#### Metadata Registry
- `MetaType` struct -- the central abstraction driving schema, API, and UI generation
- 35 `FieldType` constants (29 storage types + 6 layout-only types)
- Schema compiler (`JSON -> validated MetaType`) with 12 validation rules
- 3-tier metadata cache (L1 `sync.Map`, L2 Redis, L3 PostgreSQL)
- DDL generator with column type mapping for all 29 storable FieldTypes
- Schema migrator with diff-to-DDL (add/remove columns, type changes, indexes)
- 13 standard columns per document table (name, owner, creation, modified, etc.)

#### Document Runtime
- `Document` interface and `DynamicDoc` (map-backed) with dirty tracking
- 16-event lifecycle engine (14 `DocEvent` constants + 2 rename methods)
- 6 naming strategies: UUID, AutoIncrement, ByField, ByHash, Pattern, Custom
- Pattern naming via PostgreSQL sequences (thread-safe: `"SO-.####"` -> `"SO-0001"`)
- Field-level validation with 9 rules (required, max_length, regex, select, unique, link, type coercion, custom, depends_on)
- Type coercion (string->int, string->bool, ISO 8601->time.Time)
- CRUD operations: Insert (16-step lifecycle), Update (partial), Delete, Get, GetList
- Controller registry with override and extension composition
- Singles support (`is_single: true` MetaTypes use `tab_singles`)
- Child table support with cascade delete

#### Query Engine
- `QueryBuilder` with fluent API (`For`, `Fields`, `Where`, `OrderBy`, `GroupBy`, `Limit`, `Offset`)
- 15 filter operators: `=`, `!=`, `>`, `<`, `>=`, `<=`, `like`, `not like`, `in`, `not in`, `between`, `is null`, `is not null`, `@>` (JSONB containment), `@@` (full-text search)
- Transparent `_extra` JSONB field access with type casting
- Link field auto-joins with dot notation (depth <= 2)
- Offset-based pagination with total count
- `ReportDef` and QueryReport execution with DDL rejection

#### REST API
- Auto-generated REST CRUD endpoints for any MetaType (POST/GET/PUT/DELETE)
- Meta endpoint (`GET /api/v1/meta/{doctype}`)
- Middleware chain: Request ID -> CORS -> Tenant Resolution -> Auth -> Rate Limit -> Version Router
- Redis-backed sliding window rate limiter (per-user, per-tenant)
- Request/response transformers (field filtering, alias remapping, read-only enforcement)
- API versioning (`/api/v1/`, `/api/v2/`)
- Audit log on every mutation (`tab_audit_log`)
- Response envelopes (`{"data": ...}`, `{"error": ...}`)

#### CLI
- Cobra CLI with 24+ command groups and global flags (`--site`, `--env`, `--json`, `--table`, `--no-color`, `--verbose`)
- Context resolver with 6-level priority (flags > env > state files > config > auto-detect > defaults)
- Output formatters: TTY, JSON, Table, Progress spinner
- Rich error format with Context, Cause, Fix, and Reference fields
- `moca version`, `moca completion`, `moca doctor` commands
- Thread-safe command registry with collision detection
- `moca init` -- project bootstrapping (create directory, `moca.yaml`, connect PG/Redis, create system schema, install core app)
- `moca site create` -- 9-step site creation lifecycle (schema, tables, bootstrap, admin user, Redis, register)
- `moca site drop/list/use/info` -- full site management
- `moca app install/uninstall/list` -- 6-step app installation lifecycle
- `moca db migrate/rollback/diff` -- migration runner with `DependsOn` ordering and `--dry-run`
- `MigrationRunner` with version tracking via `tab_migration_log`

#### Hook Registry & App System
- `HookRegistry` with priority-ordered handlers and dependency resolution (Kahn's algorithm)
- Circular dependency detection with clear error messages
- `DocEventDispatcher` integrated with document lifecycle
- `AppManifest` YAML parser and validator
- App directory scanner and loader with dependency validation
- `pkg/builtin/core` with 8 core DocTypes: DocType, User, Role, Module, SystemSettings, DocField, DocPerm, HasRole
- User controller with bcrypt password hashing
- Bootstrap sequence for self-referential DocType MetaType

#### Dev Server
- `moca serve` -- single-process dev server (HTTP + worker/scheduler/outbox stubs)
- `moca stop` -- graceful shutdown via PID file (`--force` for SIGKILL)
- `moca restart` -- stop + serve
- Goroutine supervisor with critical/non-critical subsystem management
- `fsnotify` MetaType watcher with 500ms debounce (edit JSON -> auto-reload)
- PID file management with stale PID detection
- Static file serving at `/desk/`

[1.0.0]: https://github.com/osama1998H/moca/releases/tag/v1.0.0
[0.4.0-rc]: https://github.com/osama1998H/moca/releases/tag/v0.4.0-rc
[0.3.0-beta]: https://github.com/osama1998H/moca/releases/tag/v0.3.0-beta
[0.2.0-alpha]: https://github.com/osama1998H/moca/releases/tag/v0.2.0-alpha
[0.1.0-mvp]: https://github.com/osama1998H/moca/releases/tag/v0.1.0-mvp
