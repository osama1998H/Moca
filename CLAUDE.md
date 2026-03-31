# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Moca** is a metadata-driven, multitenant, full-stack business application framework built in Go. It is a spiritual successor to the [Frappe](https://frappeframework.com/) framework (behind ERPNext), redesigned from scratch. A single `MetaType` definition drives database schema, validation, document lifecycle, permissions, API generation, search indexing, and React UI rendering.

**Current state:** MS-00 (architecture validation spikes) is complete. Active development is underway on core packages (`pkg/meta`, `pkg/document`, `pkg/orm`, `pkg/observe`, `internal/config`, `internal/drivers`).

## Build & Development Commands

```bash
make build              # Build all 5 binaries to bin/
make test               # Run all tests with race detector
make test-integration   # Run integration tests (requires Docker — starts PG + Redis via docker-compose)
make lint               # Run golangci-lint
make clean              # Remove build artifacts

# Run a single test
go test -race -run TestFunctionName ./pkg/meta/...

# Spike tests (validation prototypes from MS-00)
make spike-pg           # PostgreSQL tenant isolation spike
make spike-redis        # Redis Streams spike (uses GOWORK=off)
make spike-meili        # Meilisearch spike (uses GOWORK=off)
make spike-gowork       # Go workspace composition spike
make spike-cobra        # Cobra CLI extension spike
```

Integration tests use the `integration` build tag and require Docker (`docker-compose.yml` provides PostgreSQL on port 5433 and Redis).

## Key Design Documents

| File | Purpose |
|------|---------|
| `MOCA_SYSTEM_DESIGN.md` | Full framework architecture — MetaType, Document Runtime, API layer, permissions, hooks, workflows, database, caching, queuing, Kafka, React frontend, multitenancy, observability |
| `MOCA_CLI_SYSTEM_DESIGN.md` | CLI tool architecture — 152 commands across 23 command groups, context detection, internal packages |
| `ROADMAP.md` | 30-milestone roadmap (MS-00 to MS-29), dependency graph, critical path, ~72 weeks to v1.0 |
| `docs/MS-00-architecture-validation-spikes-plan.md` | MS-00: 5 architecture validation spikes (complete) |
| `docs/MS-01-project-structure-configuration-plan.md` | MS-01: Project structure & config |
| `docs/MS-02-postgresql-foundation-redis-connection-layer-plan.md` | MS-02: PostgreSQL & Redis foundation |
| `docs/MS-03-metadata-registry-plan.md` | MS-03: Metadata registry |
| `docs/MS-04-document-runtime-plan.md` | MS-04: Document runtime |
| `docs/moca-database-decision-report.md` | ADR: PostgreSQL 16+ with schema-per-tenant over CockroachDB |
| `docs/blocker-resolution-strategies.md` | Solutions for 4 critical architectural blockers |

## Technology Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26+ (module: `github.com/moca-framework/moca`) |
| Frontend | React 19+ with TypeScript (planned) |
| Database | PostgreSQL 16+ (schema-per-tenant, JSONB, RLS) — pgx v5 / pgxpool |
| Cache / Queue | Redis 7+ (go-redis v9; cache + Redis Streams for jobs) |
| Event streaming | Apache Kafka (optional; Redis pub/sub fallback) |
| Search | Meilisearch |
| Object storage | S3-compatible (MinIO) |
| CLI framework | Cobra |
| Linting | golangci-lint v2 (govet, errcheck, staticcheck, unused) — `spikes/` excluded |

## Project Structure

```
cmd/
  moca/              # CLI binary (Cobra)
  moca-server/       # HTTP + WebSocket server
  moca-worker/       # Background job consumer (Redis Streams)
  moca-scheduler/    # Cron scheduler
  moca-outbox/       # Transactional outbox → Kafka poller
pkg/
  meta/              # MetaType registry, schema compiler, DDL generator, migrator
  document/          # Document interface, lifecycle, naming, validation, CRUD
  orm/               # PostgreSQL adapter, dynamic query builder, transactions, schema DDL
  observe/           # Structured logging (slog), health checks
  api/               # REST + GraphQL gateway (planned)
  auth/              # Session, JWT, OAuth2, SSO, permissions (planned)
  hooks/             # HookRegistry, doc events, API middleware hooks (planned)
  workflow/          # State machine, SLA timers, approval chains (planned)
  tenancy/           # Site resolver middleware, SiteContext (planned)
  queue/             # Redis Streams producer/consumer, DLQ (planned)
  events/            # Kafka producer/consumer, transactional outbox (planned)
  search/            # Meilisearch indexer and query (planned)
  storage/           # S3/MinIO adapter (planned)
internal/
  config/            # YAML config parser, validation, merge, env expansion
  drivers/           # Redis driver wrappers
apps/core/           # Core framework doctypes (own go.mod, part of go.work)
spikes/              # MS-00 validation prototypes (5 spikes, all passing)
```

Go multi-module workspace (`go.work`) composes the root module with `apps/core`.

## Key Architectural Decisions

- **Schema-per-tenant**: Each tenant gets its own PostgreSQL schema. Enforced via `AfterConnect` callback (not deprecated `BeforeAcquire`) setting `search_path` per pool. Separate pools per tenant naturally isolate prepared statement caches.
- **MetaType-driven**: Every `MetaType` definition auto-generates table DDL, CRUD API routes, GraphQL schema, Meilisearch index config, and React form/list views.
- **`_extra JSONB` column**: Every document table includes a `_extra JSONB` column for dynamic/custom fields, avoiding schema migrations for customizations.
- **Transactional outbox**: DB writes and event publishing are kept consistent via an outbox table inside the same transaction, polled by `moca-outbox`.
- **Hook registry with explicit priorities**: App hooks declare numeric priority and dependency order; no implicit ordering.

## Validated Spike Findings (MS-00)

These ADRs in `spikes/*/` document proven patterns — reference them when implementing production code:
- **ADR-001** (pg-tenant): `AfterConnect` for search_path, per-site pool registry, app-level idle eviction
- **ADR-002** (redis-streams): go-redis v9, XAutoClaim for at-least-once delivery, ZADD for scheduled jobs
- **ADR-003** (go-workspace): MVS resolves correctly, `replace` directives as escape hatch
- **ADR-005** (cobra-ext): `init()` + blank imports for app commands, `app:command` namespace convention
- **ADR-006** (meilisearch): Index-per-tenant (`{site}_{doctype}`), tenant-token for high-tenant-count, `waitForTask` required for all writes
