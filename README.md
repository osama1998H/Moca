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
  <a href="https://github.com/osama1998H/Moca/releases/latest">
    <img src="https://img.shields.io/github/v/release/osama1998H/Moca?color=green&logo=github" alt="Latest Release">
  </a>
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
curl -fsSL https://raw.githubusercontent.com/osama1998H/moca/main/install.sh | MOCA_VERSION=1.0.3 sh
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
docs/                 # Design documents, milestone plans & ADRs
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
| [System Design](docs/MOCA_SYSTEM_DESIGN.md) | Full framework architecture — MetaType, Document Runtime, API, permissions, hooks, workflows, database, frontend, multitenancy, observability |
| [CLI Design](docs/MOCA_CLI_SYSTEM_DESIGN.md) | CLI tool architecture — 152 commands across 23 command groups |
| [Database Decision](docs/moca-database-decision-report.md) | ADR: PostgreSQL 16+ with schema-per-tenant over CockroachDB |
| [Blocker Resolutions](docs/blocker-resolution-strategies.md) | Solutions for 4 critical architectural blockers |
| [Cross-Doc Review](docs/moca-cross-doc-mismatch-report.md) | Cross-document consistency review (30 resolved mismatches) |

## Current Status

**v1.0 feature-complete** — 27 of 30 milestones implemented and tested. Only 3 post-v1.0 milestones remain (MS-27, MS-28, MS-29).

| Phase | Milestones | What they deliver |
|-------|-----------|-------------------|
| **Foundation** | MS-00 -- MS-01 | Architecture validation (6 ADRs), project structure, `moca.yaml` config |
| **Core Engine** | MS-02 -- MS-06 | PostgreSQL per-tenant pools, metadata registry, document lifecycle, query builder, REST API layer |
| **CLI & Apps** | MS-07 -- MS-11 | CLI foundation (152 commands), hook registry, site/app management, dev server, hot reload, DB/backup/config ops |
| **Multitenancy & Security** | MS-12 -- MS-14 | Schema-per-tenant isolation, app scaffolding, user management, RBAC/FLS/RLS permission engine |
| **Infrastructure** | MS-15 -- MS-16 | Background jobs, scheduler, Kafka/Redis events, search sync, CLI queue/events/search/monitor commands |
| **React Desk** | MS-17, MS-19 -- MS-20 | React app shell, FormView, ListView, DocType Builder, real-time WebSocket, dashboard, reports, i18n |
| **API & Webhooks** | MS-18 | API keys, webhooks, custom endpoints, per-DocType API config |
| **Deployment & Ops** | MS-21, MS-24 | Deploy setup/update/rollback, infrastructure generation (Caddy/NGINX/systemd/Docker/K8s), Prometheus metrics, tracing |
| **Security Hardening** | MS-22 | OAuth2, SAML/OIDC SSO, field-level encryption, email/in-app notifications |
| **Advanced** | MS-23, MS-25 | Workflow engine (state machine, SLA timers, approval chains), testing framework |
| **Release** | MS-26 | Documentation, packaging, v1.0 polish |

**Codebase:** 264 Go production files, 234 test files, 273 React component files, 5 binaries, full React SPA with 7 major views.

See the [Roadmap](ROADMAP.md) for the full milestone plan and the [Changelog](CHANGELOG.md) for detailed release notes.
