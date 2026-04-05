# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Moca** is a metadata-driven, multitenant, full-stack business application framework built in Go. It is a spiritual successor to the [Frappe](https://frappeframework.com/) framework (behind ERPNext), redesigned from scratch. A single `MetaType` definition drives database schema, validation, document lifecycle, permissions, API generation, search indexing, and React UI rendering.

**Current state:** MS-00 through MS-16 are fully implemented and tested. MS-17 (React Desk Foundation) is in progress. Completed milestones: Architecture Validation, Project Structure, PostgreSQL/Redis, Metadata Registry, Document Runtime, Query Engine, REST API, CLI Foundation, Hook Registry & App System, CLI Site/App Commands, Dev Server & Hot Reload, CLI Operational Commands, Multitenancy, CLI App Scaffolding & User Management, Permission Engine (RBAC/FLS/RLS), Background Jobs/Scheduler/Kafka/Redis Events/Search Sync, CLI Queue/Events/Search/Monitor Commands.

## Build & Development Commands

```bash
# Building
make build              # Build all 5 binaries to bin/
make build-server       # Build moca-server only
make build-worker       # Build moca-worker only
make build-scheduler    # Build moca-scheduler only
make build-moca         # Build moca CLI only
make build-outbox       # Build moca-outbox only

# Testing
make test               # Run all tests with race detector
make test-integration   # Run integration tests (requires Docker — starts PG + Redis + Meilisearch via docker-compose)
make test-api-integration # Run API integration tests only
go test -race -run TestFunctionName ./pkg/meta/...  # Run a single test

# Benchmarking
make bench              # Run Tier 1 benchmarks (pkg/meta, pkg/document, pkg/orm, pkg/api, pkg/hooks)
make bench-integration  # Run Docker-backed benchmarks (10 iterations, 20m timeout)
make bench-compare      # Compare current run against saved baseline (uses benchstat)
make bench-save-baseline # Save latest run as baseline
make bench-profile      # Capture CPU/memory profiles for a specific benchmark

# Linting & Cleanup
make lint               # Run golangci-lint (v2, 5m timeout)
make clean              # Remove build artifacts and Go caches

# Spike tests (validation prototypes from MS-00)
make spike-pg           # PostgreSQL tenant isolation spike
make spike-redis        # Redis Streams spike (uses GOWORK=off)
make spike-meili        # Meilisearch spike (uses GOWORK=off)
make spike-gowork       # Go workspace composition spike
make spike-cobra        # Cobra CLI extension spike
```

Integration tests use the `integration` build tag and require Docker. `docker-compose.yml` provides:
- PostgreSQL 16 on port 5433 (user: `moca`, password: `moca_test`, db: `moca_test`)
- Redis 7 on port 6380
- Meilisearch v1.12 on port 7700

All services use tmpfs and health checks.

## Key Design Documents

| File | Purpose |
|------|---------|
| `MOCA_SYSTEM_DESIGN.md` | Full framework architecture — MetaType, Document Runtime, API layer, permissions, hooks, workflows, database, caching, queuing, Kafka, React frontend, multitenancy, observability |
| `MOCA_CLI_SYSTEM_DESIGN.md` | CLI tool architecture — 152 commands across 23 command groups, context detection, internal packages |
| `ROADMAP.md` | 30-milestone roadmap (MS-00 to MS-29), dependency graph, critical path, ~72 weeks to v1.0 |
| `docs/MS-00-architecture-validation-spikes-plan.md` | MS-00: Architecture validation spikes (complete) |
| `docs/MS-01-project-structure-configuration-plan.md` | MS-01: Project structure & config (complete) |
| `docs/MS-02-postgresql-foundation-redis-connection-layer-plan.md` | MS-02: PostgreSQL & Redis foundation (complete) |
| `docs/MS-03-metadata-registry-plan.md` | MS-03: Metadata registry (complete) |
| `docs/MS-04-document-runtime-plan.md` | MS-04: Document runtime (complete) |
| `docs/MS-05-query-engine-and-report-foundation-plan.md` | MS-05: Query engine & reports (complete) |
| `docs/MS-06-rest-api-layer-plan.md` | MS-06: REST API layer (complete) |
| `docs/MS-07-cli-foundation-plan.md` | MS-07: CLI foundation (complete) |
| `docs/MS-08-hook-registry-and-app-system-foundation-plan.md` | MS-08: Hook registry & app system (complete) |
| `docs/MS-09-cli-project-init-site-and-app-commands-plan.md` | MS-09: CLI init, site, app commands (complete) |
| `docs/MS-10-dev-server-process-management-hot-reload-plan.md` | MS-10: Dev server & hot reload (complete) |
| `docs/MS-11-cli-operational-commands-plan.md` | MS-11: CLI operational commands — DB, backup, config (complete) |
| `docs/MS-12-multitenancy-plan.md` | MS-12: Multitenancy — tenant isolation, schema management (complete) |
| `docs/MS-13-cli-app-scaffolding-user-management-developer-tools-plan.md` | MS-13: App scaffolding, user management, dev tools (complete) |
| `docs/MS-14-permission-engine-role-based-field-level-row-level-plan.md` | MS-14: Permission engine — RBAC, FLS, RLS (complete) |
| `docs/MS-15-background-jobs-scheduler-kafka-redis-events-search-sync-plan.md` | MS-15: Background jobs, scheduler, Kafka/Redis events, search sync (complete) |
| `docs/MS-16-cli-queue-events-search-monitor-log-commands-plan.md` | MS-16: CLI queue/events/search/monitor commands (complete) |
| `docs/MS-17-react-desk-foundation-plan.md` | MS-17: React Desk foundation (in progress) |
| `docs/MVP-VALIDATION-REPORT.md` | MVP release validation audit (MS-00 through MS-10) |
| `docs/moca-database-decision-report.md` | ADR: PostgreSQL 16+ with schema-per-tenant over CockroachDB |
| `docs/blocker-resolution-strategies.md` | Solutions for 4 critical architectural blockers |
| `docs/moca-cross-doc-mismatch-report.md` | Cross-document inconsistency resolution report |
| `docs/roadmap-gap-fix-summary.md` | Roadmap validation and gap fixes |

## Technology Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26+ (module: `github.com/osama1998H/moca`) |
| Frontend | React 19+ with TypeScript (MS-17, in progress) |
| Database | PostgreSQL 16+ (schema-per-tenant, JSONB, RLS) — pgx v5 / pgxpool |
| Cache / Queue | Redis 7+ (go-redis v9; cache + Redis Streams for jobs) |
| Event streaming | Apache Kafka (optional; Redis pub/sub fallback) |
| Search | Meilisearch v1.12 |
| Object storage | S3-compatible (MinIO) |
| CLI framework | Cobra |
| Linting | golangci-lint v2 (govet, errcheck, staticcheck, unused) — `spikes/` excluded |
| CI | GitHub Actions (build, test, integration test, lint, benchmark, release) |

## Project Structure

```
cmd/
  moca/              # CLI binary (Cobra)
  moca-server/       # HTTP + WebSocket server
  moca-worker/       # Background job consumer (Redis Streams)
  moca-scheduler/    # Cron scheduler
  moca-outbox/       # Transactional outbox -> Kafka poller
pkg/
  meta/              # MetaType registry, schema compiler, DDL generator, migrator
  document/          # Document interface, lifecycle, naming, validation, CRUD
  orm/               # PostgreSQL adapter, dynamic query builder, transactions, schema DDL
  observe/           # Structured logging (slog), health checks
  api/               # REST API gateway, middleware, rate limiting, transformers
  auth/              # Auth stubs (NoopAuthenticator placeholder — full auth in MS-14)
  hooks/             # HookRegistry, priority sorting, dependency resolution, DocEventDispatcher
  apps/              # AppManifest parser, app loader, installer
  tenancy/           # SiteManager (create/drop/list sites), SiteContext
  cli/               # Cobra command registry, thread-safe registration
  queue/             # Redis Streams producer/consumer, DLQ, scheduling, leader election, worker pool
  events/            # Event emitter with Kafka + Redis backends, transactional outbox
  search/            # Meilisearch indexer, query execution, sync daemon, multi-tenant indexing
  backup/            # Backup utilities
  notify/            # Notification stubs
  ui/                # UI rendering stubs
  workflow/          # State machine, SLA timers, approval chains (planned — MS-23)
  storage/           # S3/MinIO adapter (planned — MS-21)
internal/
  config/            # YAML config parser, validation, merge, env expansion
  drivers/           # Redis driver wrappers (4-DB client factory)
  context/           # CLI context resolver (project/site/env detection)
  output/            # CLI output formatters (TTY, JSON, Table, Progress, rich errors)
  process/           # Goroutine supervisor, PID file management
  serve/             # HTTP server extraction (composes DB, Redis, Registry, Gateway)
  lockfile/          # Distributed lock support (Redis-backed), PID file management
  scaffold/          # Project scaffolding templates
  testutil/          # Test utilities, benchmarking helpers (internal/testutil/bench/)
apps/core/           # Core framework doctypes (User, Role, DocType, Module, SystemSettings)
  modules/core/      # Modular doctype definitions (JSON schemas + controllers)
    doctypes/
      doctype/       # DocType meta-definition
      user/          # User entity
      role/          # Role entity
      module_def/    # Module definition
      doc_field/     # Document field definition
      doc_perm/      # Document permissions
      has_role/      # Has-Role join table
      system_settings/ # System settings
desk/                # React frontend (MS-17, in progress)
spikes/              # MS-00 validation prototypes (5 spikes, all passing)
```

Go multi-module workspace (`go.work`) composes the root module with `apps/core`.

## Key Architectural Decisions

- **Schema-per-tenant**: Each tenant gets its own PostgreSQL schema. Enforced via `AfterConnect` callback (not deprecated `BeforeAcquire`) setting `search_path` per pool. Separate pools per tenant naturally isolate prepared statement caches.
- **MetaType-driven**: Every `MetaType` definition auto-generates table DDL, CRUD API routes, GraphQL schema, Meilisearch index config, and React form/list views.
- **`_extra JSONB` column**: Every document table includes a `_extra JSONB` column for dynamic/custom fields, avoiding schema migrations for customizations.
- **Transactional outbox**: DB writes and event publishing are kept consistent via an outbox table inside the same transaction, polled by `moca-outbox`.
- **Hook registry with explicit priorities**: App hooks declare numeric priority and dependency order; no implicit ordering.
- **At-least-once delivery**: Redis Streams with XAutoClaim for job processing; DLQ for failed messages after max retries.
- **Index-per-tenant search**: Meilisearch indexes follow `{site}_{doctype}` naming; `waitForTask` required for all writes.

## CI/CD Pipeline

GitHub Actions workflows in `.github/workflows/`:

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `ci.yml` | Push/PR to main | Build, test (race), integration test (PG + Redis + Meilisearch), lint |
| `benchmark.yml` | PR to main (when pkg/internal/cmd changed) | Benchmark regression detection against baseline |
| `release.yml` | Tag `v*` | Cross-compile (linux/darwin x amd64/arm64), create GitHub release with binaries |
| `nightly.yml` | Scheduled | Nightly build and test |

CI uses Go 1.26.1 with golangci-lint v2.11.4.

## Testing Conventions

- **Unit tests**: Co-located with source files, run via `make test` (includes race detector).
- **Integration tests**: Use `//go:build integration` build tag. Require Docker services (PG, Redis, Meilisearch).
- **Benchmarks**: Tier 1 packages: `pkg/meta`, `pkg/document`, `pkg/orm`, `pkg/api`, `pkg/hooks`. Results tracked in `bench-latest.txt` / `bench-baseline.txt`.
- **142+ test files** across the codebase with comprehensive coverage.

## Validated Spike Findings (MS-00)

These ADRs in `spikes/*/` document proven patterns — reference them when implementing production code:
- **ADR-001** (pg-tenant): `AfterConnect` for search_path, per-site pool registry, app-level idle eviction
- **ADR-002** (redis-streams): go-redis v9, XAutoClaim for at-least-once delivery, ZADD for scheduled jobs
- **ADR-003** (go-workspace): MVS resolves correctly, `replace` directives as escape hatch
- **ADR-005** (cobra-ext): `init()` + blank imports for app commands, `app:command` namespace convention
- **ADR-006** (meilisearch): Index-per-tenant (`{site}_{doctype}`), tenant-token for high-tenant-count, `waitForTask` required for all writes
