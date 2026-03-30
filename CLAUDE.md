# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Moca** is a metadata-driven, multitenant, full-stack business application framework built in Go. It is a spiritual successor to the [Frappe](https://frappeframework.com/) framework (behind ERPNext), redesigned from scratch. A single `MetaType` definition drives database schema, validation, document lifecycle, permissions, API generation, search indexing, and React UI rendering.

**Current state:** This repository is in the **design/planning phase only**. There is no source code yet — only design documents, a roadmap, and planning artifacts. Implementation begins at Milestone MS-00.

## Key Design Documents

| File | Purpose |
|------|---------|
| `MOCA_SYSTEM_DESIGN.md` | Full framework architecture — MetaType, Document Runtime, API layer, permissions, hooks, workflows, database, caching, queuing, Kafka, React frontend, multitenancy, observability |
| `MOCA_CLI_SYSTEM_DESIGN.md` | CLI tool architecture — 152 commands across 23 command groups, context detection, internal packages |
| `ROADMAP.md` | 30-milestone roadmap (MS-00 to MS-29), dependency graph, critical path, ~72 weeks to v1.0 |
| `ROADMAP_VALIDATION_REPORT.md` | Gap analysis of roadmap coverage |
| `docs/MS-00-architecture-validation-spikes-plan.md` | Detailed plan for the first milestone: 5 architecture validation spikes |
| `docs/moca-database-decision-report.md` | ADR: PostgreSQL 16+ with schema-per-tenant isolation over CockroachDB |
| `docs/blocker-resolution-strategies.md` | Solutions for 4 critical architectural blockers |
| `docs/moca-cross-doc-mismatch-report.md` | Cross-document consistency review (30 resolved mismatches) |

## Planned Technology Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.22+ |
| Frontend | React 19+ with TypeScript |
| Database | PostgreSQL 16+ (schema-per-tenant, JSONB, RLS) |
| Cache / Queue | Redis 7+ (go-redis v9; cache + Redis Streams for jobs) |
| Event streaming | Apache Kafka (optional; Redis pub/sub fallback) |
| Search | Meilisearch |
| Object storage | S3-compatible (MinIO) |
| CLI framework | Cobra |
| DB driver | pgx v5 / pgxpool |

## Planned Project Structure

```
cmd/
  moca/              # CLI binary (Cobra)
  moca-server/       # HTTP + WebSocket server
  moca-worker/       # Background job consumer (Redis Streams)
  moca-scheduler/    # Cron scheduler
  moca-outbox/       # Transactional outbox → Kafka poller
pkg/
  meta/              # MetaType registry, schema compiler, migrator
  document/          # Document interface, lifecycle, naming, validation
  api/               # REST + GraphQL gateway, rate limiting, webhooks
  orm/               # PostgreSQL adapter, dynamic query builder, DDL
  auth/              # Session, JWT, OAuth2, SSO, permissions
  hooks/             # HookRegistry, doc events, API middleware hooks
  workflow/          # State machine, SLA timers, approval chains
  tenancy/           # Site resolver middleware, SiteContext, site manager
  queue/             # Redis Streams producer/consumer, DLQ
  events/            # Kafka producer/consumer, transactional outbox
  search/            # Meilisearch indexer and query
  storage/           # S3/MinIO adapter
  observe/           # Prometheus metrics, OpenTelemetry tracing, logging
apps/core/           # Core framework doctypes (User, Role, DocType, etc.) — own go.mod
desk/                # React 19 + TypeScript frontend SPA
```

Go multi-module workspace (`go.work`) composes the root module with `apps/` modules.

## Key Architectural Decisions

- **Schema-per-tenant**: Each tenant gets its own PostgreSQL schema (e.g., `tenant_acme`). A `moca_system` schema holds global tables. Enforced via `pgxpool BeforeAcquire` resetting `search_path` on every connection.
- **MetaType-driven**: Every `MetaType` definition auto-generates table DDL, CRUD API routes, GraphQL schema, Meilisearch index config, and React form/list views.
- **`_extra JSONB` column**: Every document table includes a `_extra JSONB` column for dynamic/custom fields, avoiding schema migrations for customizations.
- **Transactional outbox**: DB writes and event publishing are kept consistent via an outbox table inside the same transaction, polled by `moca-outbox`.
- **Hook registry with explicit priorities**: App hooks declare numeric priority and dependency order; no implicit ordering.

## Current Milestone

**MS-00: Architecture Validation Spikes** (not yet started). This milestone establishes:
- Go workspace (`go.work`), Makefile, `.golangci.yml`, GitHub Actions CI
- 5 spikes: PostgreSQL schema isolation, Redis Streams, Go workspace multi-module, Meilisearch, Cobra CLI extensions

Build commands, test runner, and linting configuration will be added during MS-00.
