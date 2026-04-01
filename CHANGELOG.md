# Changelog

All notable changes to the Moca framework will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
- `apps/core` with 8 core DocTypes: DocType, User, Role, Module, SystemSettings, DocField, DocPerm, HasRole
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

[0.1.0-mvp]: https://github.com/moca-framework/moca/releases/tag/v0.1.0-mvp
