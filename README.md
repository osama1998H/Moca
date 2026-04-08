<p align="center">
  <img
    src="logo-with-golang-avatar.png"
    width="400"
    alt="Moca Framework + Go"
    style="border-radius: 10%;"
  >
</p>

<h1 align="center">Moca</h1>

<p align="center">
  A metadata-driven, multitenant, full-stack business application framework built in Go.
</p>

<p align="center">
  <a href="https://github.com/osama1998H/Moca/actions/workflows/ci.yml">
    <img src="https://github.com/osama1998H/Moca/actions/workflows/ci.yml/badge.svg?branch=main&event=push" alt="CI">
  </a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">
  <img src="https://img.shields.io/badge/PostgreSQL-16+-4169E1?logo=postgresql&logoColor=white" alt="PostgreSQL 16+">
    <img src="https://img.shields.io/badge/Redis-7+-4169E1?logo=redis&logoColor=Red" alt="Redis 7+">
  <img src="https://img.shields.io/badge/React-19+-61DAFB?logo=react&logoColor=black" alt="React 19+">
  <img src="https://img.shields.io/badge/Release-v0.1.0--mvp-green" alt="Release: v0.1.0-mvp">
</p>

---

<img src="logo.png" width="80" align="right" alt="Moca logo"   style="border-radius: 10%;">

Moca is a spiritual successor to the [Frappe](https://frappeframework.com/) framework (the engine behind [ERPNext](https://erpnext.com/)), redesigned from scratch with Go, PostgreSQL, and React. A single **MetaType** definition drives database schema, validation, document lifecycle, permissions, API generation, search indexing, and UI rendering.

## Installation

Install the Moca CLI and all binaries with a single command:

```bash
curl -fsSL https://raw.githubusercontent.com/osama1998H/moca/main/install.sh | sh
```

Or install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/osama1998H/moca/main/install.sh | MOCA_VERSION=0.1.0-mvp sh
```

Or download directly from [GitHub Releases](https://github.com/osama1998H/moca/releases).

### Nightly Builds

Nightly builds are published automatically from the `main` branch every day at midnight UTC. They include the latest features and fixes but may be unstable.

```bash
curl -fsSL https://raw.githubusercontent.com/osama1998H/moca/main/install.sh | MOCA_VERSION=nightly sh
```

Or download directly from the [nightly release](https://github.com/osama1998H/moca/releases/tag/nightly).

#### Nightly Desk Frontend

The desk React frontend (`@osama1998h/desk`) is also published nightly to GitHub Packages:

```bash
npm install @osama1998h/desk@nightly --registry=https://npm.pkg.github.com
```

### From Source

```bash
git clone https://github.com/osama1998H/moca.git
cd moca
make build    # Builds all 5 binaries to bin/
```

## Quick Start

```bash
# 1. Initialize a new project
moca init my-erp

# 2. Create a site (requires running PostgreSQL and Redis)
cd my-erp
moca site create mysite.localhost --admin-password secret123

# 3. Start the development server
moca serve

# 4. Define a MetaType, save the JSON file, and watch it hot-reload
#    The REST API is auto-generated at http://localhost:8000/api/v1/resource/{doctype}
```

## Why Moca?

| Frappe Limitation | Moca Improvement |
|---|---|
| Rigid, non-customizable auto-generated API | Fully customizable API layer with middleware, versioning, GraphQL |
| Python GIL limits concurrency | Go goroutines for true parallel request handling |
| MariaDB-centric | PostgreSQL with JSONB, CTEs, window functions, partitioning |
| Monolithic process model | Decomposable into microservices when needed |
| Limited real-time capabilities | WebSocket pub/sub + Kafka event streaming built-in |
| Tightly coupled Desk UI | Decoupled React frontend consuming a metadata API |
| Implicit hook ordering | Explicit priority-ordered hook registry with dependency resolution |

## Technology Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26+ |
| Frontend | React 19+ with TypeScript |
| Database | PostgreSQL 16+ (schema-per-tenant, JSONB, RLS) |
| Cache / Queue | Redis 7+ (cache + Redis Streams for jobs) |
| Event Streaming | Apache Kafka (optional; Redis pub/sub fallback) |
| Search | Meilisearch |
| Object Storage | S3-compatible (MinIO) |
| Reverse Proxy | Caddy / NGINX |

## Project Structure

```
cmd/                  # Binary entry points
  moca/               #   CLI tool (Cobra)
  moca-server/        #   HTTP + WebSocket server
  moca-worker/        #   Background job consumer
  moca-scheduler/     #   Cron scheduler
  moca-outbox/        #   Transactional outbox poller
pkg/                  # Core framework packages
  meta/               #   MetaType registry & schema compiler
  document/           #   Document lifecycle & validation
  api/                #   REST + GraphQL gateway
  orm/                #   PostgreSQL adapter & query builder
  auth/               #   Session, JWT, OAuth2, permissions
  hooks/              #   Hook registry & event system
  workflow/           #   State machine, SLA timers, approvals
  tenancy/            #   Site resolver & multitenancy middleware
  queue/              #   Redis Streams producer/consumer
  events/             #   Kafka producer/consumer & outbox
  search/             #   Meilisearch indexer
  storage/            #   S3/MinIO adapter
  observe/            #   Prometheus, OpenTelemetry, logging
pkg/builtin/core/     # Builtin framework core doctypes (User, Role, DocType, etc.)
desk/                 # React 19 + TypeScript frontend SPA
spikes/               # Architecture validation prototypes (MS-00)
docs/                 # Design documents & milestone plans
```

## Key Architectural Decisions

- **Schema-per-tenant** — each tenant gets its own PostgreSQL schema, enforced via `pgxpool AfterConnect` setting `search_path` per pool (one pool per tenant)
- **MetaType-driven** — every MetaType auto-generates table DDL, CRUD routes, GraphQL schema, search index config, and React views
- **`_extra` JSONB column** — every document table includes a dynamic field column, avoiding schema migrations for customizations
- **Transactional outbox** — DB writes and event publishing kept consistent via an outbox table polled by `moca-outbox`
- **Hook registry with explicit priorities** — app hooks declare numeric priority and dependency order

## Documentation

| Document | Description |
|----------|-------------|
| [Roadmap](ROADMAP.md) | 30-milestone roadmap to v1.0 (~72 weeks), dependency graph, critical path |
| [System Design](MOCA_SYSTEM_DESIGN.md) | Full framework architecture — MetaType, Document Runtime, API, permissions, hooks, workflows, database, frontend, multitenancy, observability |
| [CLI Design](MOCA_CLI_SYSTEM_DESIGN.md) | CLI tool architecture — 152 commands across 23 command groups |
| [Database Decision](docs/moca-database-decision-report.md) | ADR: PostgreSQL 16+ with schema-per-tenant over CockroachDB |
| [Blocker Resolutions](docs/blocker-resolution-strategies.md) | Solutions for 4 critical architectural blockers |
| [Cross-Doc Review](docs/moca-cross-doc-mismatch-report.md) | Cross-document consistency review (30 resolved mismatches) |

## Current Status

**MVP complete (v0.1.0-mvp)** — MS-00 through MS-10 are fully implemented and tested:

| Milestone | What it delivers |
|-----------|-----------------|
| MS-00 | Architecture validation spikes (5 ADRs) |
| MS-01 | Project structure, `moca.yaml` config system |
| MS-02 | PostgreSQL per-tenant pools, Redis client factory, health checks |
| MS-03 | MetaType registry, schema compiler, DDL migrator, 3-tier cache |
| MS-04 | Document lifecycle (16 events), naming, validation, CRUD |
| MS-05 | Query builder (15 operators), JSONB transparency, auto-joins |
| MS-06 | REST API layer, rate limiting, transformers, versioning |
| MS-07 | CLI foundation (24+ commands), context resolver, output formatters |
| MS-08 | Hook registry, app manifest system, core doctypes |
| MS-09 | `moca init`, site/app/db CLI commands, migration runner |
| MS-10 | Dev server, hot reload, process supervisor |

See the [Roadmap](ROADMAP.md) for the full 30-milestone plan to v1.0 and the [Changelog](CHANGELOG.md) for detailed release notes.

## License

TBD
