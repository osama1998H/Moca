# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Moca** is a metadata-driven, multitenant, full-stack business application framework built in Go. It is a spiritual successor to the [Frappe](https://frappeframework.com/) framework (behind ERPNext), redesigned from scratch. A single `MetaType` definition drives database schema, validation, document lifecycle, permissions, API generation, search indexing, and React UI rendering.

**Current state:** v1.0 feature-complete. 28 of 30 milestones are fully implemented and tested (MS-00 through MS-26 plus MS-28). Only 2 post-v1.0 milestones remain: MS-27 (Portal SSR) and MS-29 (WASM Plugin Marketplace). The codebase includes 264 Go production files, 234 test files, 273 React component files, and a full React SPA with FormView, ListView, DocType Builder, DashboardView, ReportView, and real-time WebSocket support.

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

# Documentation generation (writes into wiki/ submodule)
make docs-generate      # Generate CLI + API reference via `moca docgen all`
make docs-generate-cli  # CLI reference only
make docs-generate-api  # API reference only

# Release
make release-local      # Build release archives locally via GoReleaser (snapshot mode)

# Static analysis — Skylos (dead code + SAST + secrets); install once via `pipx install skylos`
make audit              # Run both audits (Go backend + desk/)
make audit-go           # Scan Go backend against .skylos/baseline.json
make audit-desk         # Scan desk/ submodule against desk/.skylos/baseline.json
make audit-go-baseline  # Re-snapshot the Go baseline after intentional cleanup
make audit-desk-baseline # Re-snapshot the desk/ baseline
```

### Skylos static analysis

Skylos complements the existing golangci-lint `unused` check with cross-language dead-code detection plus SAST (XSS/JWT/secrets/SCA) on the TypeScript side. Baselines live at `.skylos/baseline.json` (Go) and `desk/.skylos/baseline.json` (frontend) and are committed to git so new regressions fail while existing debt is accepted. Only the dead-code category is baselined by Skylos; `-a` security/quality findings surface every run and should be triaged. When intentional cleanup lands, regenerate the corresponding baseline and commit it.

Integration tests use the `integration` build tag and require Docker. `docker-compose.yml` provides:
- PostgreSQL 16 on port 5433 (user: `moca`, password: `moca_test`, db: `moca_test`)
- Redis 7 on port 6380
- Meilisearch v1.12 on port 7700

All services use tmpfs and health checks.

## Key Design Documents

| File | Purpose |
|------|---------|
| `docs/MOCA_SYSTEM_DESIGN.md` | Full framework architecture — MetaType, Document Runtime, API layer, permissions, hooks, workflows, database, caching, queuing, Kafka, React frontend, multitenancy, observability |
| `docs/MOCA_CLI_SYSTEM_DESIGN.md` | CLI tool architecture — 152 commands across 23 command groups, context detection, internal packages |
| `ROADMAP.md` | 30-milestone roadmap (MS-00 to MS-29), dependency graph, critical path, ~72 weeks to v1.0 |
| `docs/milestones/MS-00-architecture-validation-spikes-plan.md` | MS-00: Architecture validation spikes (complete) |
| `docs/milestones/MS-01-project-structure-configuration-plan.md` | MS-01: Project structure & config (complete) |
| `docs/milestones/MS-02-postgresql-foundation-redis-connection-layer-plan.md` | MS-02: PostgreSQL & Redis foundation (complete) |
| `docs/milestones/MS-03-metadata-registry-plan.md` | MS-03: Metadata registry (complete) |
| `docs/milestones/MS-04-document-runtime-plan.md` | MS-04: Document runtime (complete) |
| `docs/milestones/MS-05-query-engine-and-report-foundation-plan.md` | MS-05: Query engine & reports (complete) |
| `docs/milestones/MS-06-rest-api-layer-plan.md` | MS-06: REST API layer (complete) |
| `docs/milestones/MS-07-cli-foundation-plan.md` | MS-07: CLI foundation (complete) |
| `docs/milestones/MS-08-hook-registry-and-app-system-foundation-plan.md` | MS-08: Hook registry & app system (complete) |
| `docs/milestones/MS-09-cli-project-init-site-and-app-commands-plan.md` | MS-09: CLI init, site, app commands (complete) |
| `docs/milestones/MS-10-dev-server-process-management-hot-reload-plan.md` | MS-10: Dev server & hot reload (complete) |
| `docs/milestones/MS-11-cli-operational-commands-plan.md` | MS-11: CLI operational commands — DB, backup, config (complete) |
| `docs/milestones/MS-12-multitenancy-plan.md` | MS-12: Multitenancy — tenant isolation, schema management (complete) |
| `docs/milestones/MS-13-cli-app-scaffolding-user-management-developer-tools-plan.md` | MS-13: App scaffolding, user management, dev tools (complete) |
| `docs/milestones/MS-14-permission-engine-role-based-field-level-row-level-plan.md` | MS-14: Permission engine — RBAC, FLS, RLS (complete) |
| `docs/milestones/MS-15-background-jobs-scheduler-kafka-redis-events-search-sync-plan.md` | MS-15: Background jobs, scheduler, Kafka/Redis events, search sync (complete) |
| `docs/milestones/MS-16-cli-queue-events-search-monitor-log-commands-plan.md` | MS-16: CLI queue/events/search/monitor commands (complete) |
| `docs/milestones/MS-17-react-desk-foundation-plan.md` | MS-17: React Desk foundation (in progress) |
| `docs/MVP-VALIDATION-REPORT.md` | MVP release validation audit (MS-00 through MS-10) |
| `docs/moca-database-decision-report.md` | ADR: PostgreSQL 16+ with schema-per-tenant over CockroachDB |
| `docs/blocker-resolution-strategies.md` | Solutions for 4 critical architectural blockers |
| `docs/moca-cross-doc-mismatch-report.md` | Cross-document inconsistency resolution report |
| `docs/roadmap-gap-fix-summary.md` | Roadmap validation and gap fixes |
| `docs/adr/ADR-001-pg-tenant-isolation.md` | ADR: PostgreSQL schema-per-tenant via per-pool registry |
| `docs/adr/ADR-002-redis-streams-queue.md` | ADR: Redis Streams as job queue over dedicated broker |
| `docs/adr/ADR-003-go-workspace-composition.md` | ADR: Go workspace multi-module composition |
| `docs/adr/ADR-005-cobra-cli-extension.md` | ADR: Cobra CLI extension pattern (init + blank imports) |
| `docs/adr/ADR-006-meilisearch-tenant-isolation.md` | ADR: Meilisearch index-per-tenant isolation |

## Technology Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26+ (module: `github.com/osama1998H/moca`) |
| Frontend | React 19+ with TypeScript (shadcn ui) |
| Database | PostgreSQL 16+ (schema-per-tenant, JSONB, RLS) — pgx v5 / pgxpool |
| Cache / Queue | Redis 7+ (go-redis v9; cache + Redis Streams for jobs) |
| Event streaming | Apache Kafka (optional; Redis pub/sub fallback) |
| Search | Meilisearch v1.12 |
| Object storage | S3-compatible (MinIO) |
| CLI framework | Cobra |
| Linting | golangci-lint v2 (govet, errcheck, staticcheck, unused) |
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
  observe/           # Structured logging (slog), Prometheus metrics, tracing, health checks
  api/               # REST API gateway, middleware, rate limiting, transformers
  auth/              # OAuth2, SAML/OIDC SSO, JWT sessions, RBAC, field-level & row-level security
  hooks/             # HookRegistry, priority sorting, dependency resolution, DocEventDispatcher
  apps/              # AppManifest parser, app loader, installer
  tenancy/           # SiteManager (create/drop/list sites), SiteContext
  cli/               # Cobra command registry, thread-safe registration
  queue/             # Redis Streams producer/consumer, DLQ, scheduling, leader election, worker pool
  events/            # Event emitter with Kafka + Redis backends, transactional outbox
  search/            # Meilisearch indexer, query execution, sync daemon, multi-tenant indexing
  backup/            # Backup utilities
  notify/            # Email (SMTP/SES), in-app notifications, dispatcher
  i18n/              # Translation loading, extraction, .mo compilation, middleware
  encryption/        # Field-level AES-256-GCM encryption hook
  console/           # Developer console
  workflow/          # State machine, SLA timers, approval chains, evaluator
  storage/           # S3/MinIO adapter, local storage, thumbnails
  sitepath/          # Site path resolution helpers
  ui/                # Server-side UI helpers consumed by desk/
  testutils/         # Shared test helpers for pkg/*
internal/
  config/            # YAML config parser, validation, merge, env expansion
  drivers/           # Redis driver wrappers (4-DB client factory)
  context/           # CLI context resolver (project/site/env detection)
  output/            # CLI output formatters (TTY, JSON, Table, Progress, rich errors)
  process/           # Goroutine supervisor, PID file management
  serve/             # HTTP server extraction (composes DB, Redis, Registry, Gateway)
  lockfile/          # Distributed lock support (Redis-backed), PID file management
  scaffold/          # Project scaffolding templates
  deploy/            # Deploy setup/update/rollback, promotion
  generate/          # Infrastructure generation (Caddy, NGINX, systemd, Docker, K8s)
  docgen/            # CLI and API documentation generation
  testutil/          # Test utilities, benchmarking helpers (internal/testutil/bench/)
  dockerutil/        # Docker compose/client helpers used by CLI + tests
  releaseverify/     # Release artifact verification utilities
pkg/builtin/core/    # Builtin framework core doctypes (bootstrap, hooks, user controller)
  modules/core/      # Modular doctype definitions (JSON schemas + controllers)
    doctypes/
      doctype/              # DocType meta-definition
      user/                 # User entity
      role/                 # Role entity
      module_def/           # Module definition
      doc_field/            # Document field definition
      doc_perm/             # Document permissions
      has_role/             # Has-Role join table
      system_settings/      # System settings
      language/             # Language entity (i18n)
      translation/          # Translation strings
      notification/         # Notification records
      notification_settings/ # Per-user notification preferences
      sso_provider/         # OAuth2/SAML/OIDC SSO provider config
desk/                # Git submodule — React 19 + TypeScript frontend SPA (FormView, ListView, DocType Builder, Dashboard, Reports); separate repo: osama1998H/moca-desk
wiki/                # Git submodule — user-facing docs (osama1998H/Moca.wiki); regenerated by `make docs-generate`
```

Go multi-module workspace (`go.work`) composes the root module with installable app modules under `apps/`; builtin core lives in the root module at `pkg/builtin/core`.

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
- **234 test files** across the codebase with comprehensive coverage.

## Documentation Requirements

When making code changes, **always update the related documentation**:

- **Root repository docs (`docs/`)**: Update system design docs, ADRs, milestone plans, or any other documentation in `docs/` that describes the changed functionality. If a new feature is added, document it. If behavior changes, update the existing docs to match.
- **Wiki submodule (`wiki/`)**: Update the corresponding wiki pages to reflect the changes. The wiki is a git submodule — commit changes inside `wiki/` and update the submodule pointer in the root repository.
- **CLAUDE.md**: If the change affects the project structure, build commands, technology stack, or key architectural decisions documented here, update this file as well.

This applies to all AI-assisted development tools. Documentation is not optional — code changes without corresponding documentation updates are incomplete.

## Architecture Decision Records (MS-00)

These ADRs in `docs/` document proven patterns validated during the MS-00 spike phase:
- **ADR-001** (pg-tenant): `AfterConnect` for search_path, per-site pool registry, app-level idle eviction
- **ADR-002** (redis-streams): go-redis v9, XAutoClaim for at-least-once delivery, ZADD for scheduled jobs
- **ADR-003** (go-workspace): MVS resolves correctly, `replace` directives as escape hatch
- **ADR-005** (cobra-ext): `init()` + blank imports for app commands, `app:command` namespace convention
- **ADR-006** (meilisearch): Index-per-tenant (`{site}_{doctype}`), tenant-token for high-tenant-count, `waitForTask` required for all writes
