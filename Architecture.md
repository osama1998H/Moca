# Architecture

> A high-level guide to how Moca is structured, how the pieces fit together, and the key decisions behind the design. For the full specification see [`MOCA_SYSTEM_DESIGN.md`](./docs/MOCA_SYSTEM_DESIGN.md) and [`MOCA_CLI_SYSTEM_DESIGN.md`](./docs/MOCA_CLI_SYSTEM_DESIGN.md).

---

## What Moca Is

Moca is a **metadata-driven, multitenant, full-stack business application framework** built in Go. A single `MetaType` definition (JSON/YAML) drives database schema, field validation, document lifecycle, permissions, REST/GraphQL API generation, search indexing, and React UI rendering. It is a spiritual successor to the Frappe framework, redesigned from scratch to address Frappe's limitations around API customizability, concurrency, database capabilities, and frontend coupling.

---

## System Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                          CLIENTS                                    │
│    React Desk App    Portal (SSR)    Mobile Apps    Third-Party     │
└──────────┬──────────────┬──────────────┬──────────────┬─────────────┘
           │              │              │              │
           ▼              ▼              ▼              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                   API GATEWAY / EDGE LAYER                          │
│   Tenant Resolver → Auth → Rate Limiter → API Versioning → CORS    │
└─────────────────────────────┬───────────────────────────────────────┘
                              │
┌─────────────────────────────▼───────────────────────────────────────┐
│                      CORE RUNTIME ENGINE                            │
│                                                                     │
│  Metadata Registry    Document Runtime    Customizable API Layer    │
│  Permission Engine    Hook Registry       Workflow Engine           │
│  Query Engine         App / Plugin Mgr    Scheduler & Workers      │
└───────┬──────────────────┬──────────────────┬───────────────────────┘
        │                  │                  │
        ▼                  ▼                  ▼
   PostgreSQL            Redis             Kafka (optional)
   (schema-per-tenant)   (cache, queues,   (durable events,
                          sessions,         CDC, audit stream)
                          pub/sub)
        │
        ▼
   Meilisearch    MinIO (S3)    Prometheus + Grafana
   (full-text)    (file store)  (observability)
```

---

## Five Binaries

Moca compiles into five purpose-built binaries from the `cmd/` directory:

| Binary | Role | Scaling |
|--------|------|---------|
| `moca` | CLI tool — project init, site management, migrations, config, diagnostics | Developer workstation |
| `moca-server` | HTTP API + WebSocket server | Horizontal, behind a load balancer |
| `moca-worker` | Redis Streams job consumer | Horizontal, scale by queue pressure |
| `moca-scheduler` | Cron job trigger | Single leader (Redis distributed lock) |
| `moca-outbox` | Transactional outbox poller → Kafka/Redis publisher | Single leader (Redis distributed lock) |

In development mode, `moca serve` runs the server, worker, scheduler, and outbox as goroutines in a single process with hot reload.

---

## Project Layout

```
cmd/
  moca/                  # CLI binary (Cobra)
  moca-server/           # HTTP + WebSocket server
  moca-worker/           # Background job consumer
  moca-scheduler/        # Cron scheduler
  moca-outbox/           # Transactional outbox → Kafka poller

pkg/                     # Public framework packages
  meta/                  # MetaType registry, schema compiler, DDL generator, migrator
  document/              # Document interface, lifecycle engine, naming, validation
  orm/                   # PostgreSQL adapter, dynamic query builder, transactions, DDL
  api/                   # REST/GraphQL gateway, middleware, rate limiting, transformers
  auth/                  # Authentication stubs (full auth in MS-14)
  hooks/                 # HookRegistry, priority sorting, dependency resolution
  apps/                  # AppManifest parser, app loader, installer
  tenancy/               # SiteManager, SiteContext, tenant resolution
  cli/                   # Cobra command registry, thread-safe registration
  workflow/              # State machine, SLA timers, approval chains (planned)
  queue/                 # Redis Streams producer/consumer, DLQ (planned)
  events/                # Event emitter, Kafka producer/consumer (planned)
  search/                # Meilisearch indexer and query (planned)
  storage/               # S3/MinIO file adapter (planned)
  observe/               # Structured logging (slog), health checks, metrics
  notify/                # Email, push, in-app, SMS notifications (planned)
  ui/                    # Desk SPA serving, portal SSR, WebSocket hub (planned)

internal/                # Private implementation details
  config/                # YAML config parser, validation, merge, env expansion
  drivers/               # Redis driver wrappers (4-DB client factory)
  context/               # CLI context resolver (project/site/env detection)
  output/                # CLI output formatters (TTY, JSON, Table, Progress)
  process/               # Goroutine supervisor, PID file management
  serve/                 # HTTP server composition (DB + Redis + Registry + Gateway)
  scaffold/              # Code generation templates
  lockfile/              # App version lockfile management

pkg/builtin/core/        # Builtin framework core doctypes (User, Role, DocType, Module, etc.)
docs/                    # Milestone plans, ADRs, validation reports
```

Go multi-module workspace (`go.work`) composes the root module with installable application modules; builtin core lives in the root module at `pkg/builtin/core`.

---

## Core Concepts

### MetaType — The Heart of Moca

Everything in Moca flows from a `MetaType` definition. A MetaType is a JSON/YAML document that describes a business entity (like "Sales Order" or "Customer"). From that single definition, the framework automatically generates:

1. **Database table** with typed columns, indexes, and a `_extra JSONB` column for dynamic fields
2. **Document lifecycle** with hooks (BeforeInsert, Validate, BeforeSave, OnSubmit, OnCancel, etc.)
3. **REST API endpoints** (list, get, create, update, delete, count, bulk)
4. **GraphQL types** with relations inferred from Link fields
5. **Permission rules** (role-based, field-level, row-level)
6. **Meilisearch index** configuration for full-text search
7. **React form and list views** rendered dynamically from field definitions

MetaTypes are loaded, validated by the Schema Compiler, cached in Redis, and hot-reloaded at runtime when definitions change.

### Document Runtime

Every record in the system is a `Document`. The default implementation (`DynamicDoc`) is map-backed — no code generation required. Documents go through a rich lifecycle:

```
NEW → BeforeInsert → Validate → BeforeSave → SAVED (Draft)
                                                ├── Update cycle
                                                └── Submit → SUBMITTED → Cancel → CANCELLED
                                              Delete → OnTrash → DELETED
```

Each lifecycle event fires registered hooks in explicit priority order with dependency resolution between apps.

### Customizable API Layer

This is the biggest architectural improvement over Frappe. Each MetaType can declare its own `APIConfig` controlling: which endpoints are exposed, field aliasing between internal and API names, per-endpoint middleware chains, API versioning with field mapping per version, rate limits, webhook triggers, and custom endpoints with handler functions.

### Hook Registry

Hooks are the extension mechanism. Apps register hooks programmatically in `hooks.go` with explicit numeric priorities and dependency declarations. Hook types include document lifecycle events, global events, API middleware, scheduler jobs, UI context injectors, type extensions, and whitelisted API methods.

### App System

Each installable Moca app is a self-contained Go module with its own `go.mod`, manifest, modules, DocTypes, hooks, migrations, fixtures, and optional React desk extensions. Builtin framework apps such as `pkg/builtin/core` stay in the root module. Installable apps are composed into the framework via Go workspaces (`go.work`). The installation lifecycle is: Download → Validate Manifest & Dependencies → Migrate Schema → Register Hooks & Routes → Seed Fixtures.

---

## Multitenancy

Moca uses **schema-per-tenant** isolation in PostgreSQL. Each site (tenant) gets its own PostgreSQL schema, its own connection pool (with `search_path` set via `AfterConnect`), and namespaced resources across all infrastructure:

| Resource | Isolation Method |
|----------|-----------------|
| Database | PostgreSQL schema per tenant + RLS as defense-in-depth |
| Cache | Redis key prefix `{site}:` |
| Queue | Redis stream prefix `moca:queue:{site}:` |
| Files | S3 bucket prefix `{site}/` |
| Search | Meilisearch index prefix `{site}_` |
| Events | Kafka partition key includes site name |

Tenant resolution happens at the edge layer via subdomain, HTTP header (`X-Moca-Site`), or URL path prefix, producing a `SiteContext` that flows through every request.

---

## Data Architecture

### PostgreSQL

The database has two layers:

**System schema** (`moca_system`) — shared across all tenants, stores site registry, installed apps, and migration history.

**Per-tenant schemas** (`tenant_{name}`) — each contains auto-generated tables from MetaType definitions (`tab_sales_order`, `tab_customer`, etc.), plus system tables for singles, versions, audit log, file index, outbox, and translations.

Every document table includes standard fields (name, owner, creation, modified, docstatus, workflow_state) and a `_extra JSONB` column for dynamic/custom fields added without DDL migrations.

### Redis

Redis serves four roles using logical database separation:

- **DB 0 — Cache:** MetaType definitions, hot documents, resolved permissions, site config
- **DB 1 — Queues:** Redis Streams for background jobs (default, long, critical, scheduler queues) with dead-letter handling
- **DB 2 — Sessions:** User session data
- **DB 3 — Pub/Sub:** Document change notifications (WebSocket), MetaType change broadcasts, config change broadcasts

Cache invalidation uses a write-through model with event-based cross-instance flushing via Kafka or Redis pub/sub.

### Kafka (Optional)

Kafka handles durable event distribution for audit logs, CDC, webhook dispatch, workflow transitions, search sync, and notifications. It is **not** in the hot path of CRUD — it runs asynchronously via the transactional outbox pattern:

```
BEGIN TX → INSERT document → INSERT event into outbox table → COMMIT TX
    ↓ (async)
Outbox poller → Publish to Kafka topic → Mark as published
```

When Kafka is disabled (`kafka.enabled: false`), the system falls back to Redis pub/sub with reduced durability (no replay, no CDC, fire-and-forget events). The outbox poller switches to direct HTTP webhook dispatch.

---

## CLI Architecture

The `moca` CLI is a single Go binary that **embeds** all management logic — it speaks PostgreSQL wire protocol, manages Redis natively, generates infrastructure configs from Go templates, and supervises processes directly. No shelling out to external tools.

The CLI is context-aware: it detects whether it's inside a project directory, which site is active, and what environment (dev/staging/prod) it's targeting. It is organized into layers:

```
Command Layer (Cobra)  →  commands: init, site, app, serve, deploy, doctor, db, config, ...
Context Resolver       →  project detection (moca.yaml), environment, active site
Service Layer          →  project, site, app, schema, process, deploy, backup, config, health
Driver Layer           →  PostgreSQL (pgx), Redis (go-redis), Kafka, Meilisearch, S3
Output Layer           →  TTY (color), JSON, Table, Progress bars
```

---

## Frontend Architecture

The React Desk is a **metadata-driven application shell**. It contains no hardcoded views — instead it fetches MetaType definitions from the backend and renders forms, lists, dashboards, and reports dynamically. Core infrastructure includes MetaProvider, DocProvider, AuthProvider, WebSocketProvider, ThemeProvider, and I18nProvider.

Apps can extend the Desk with custom field types (`registerFieldType()`), form/list view overrides, and dashboard widgets. The build composes three layers: framework desk (`@moca/desk`), app desk extensions, and optional project-level overrides.

Real-time updates flow via WebSocket backed by Redis pub/sub, pushing document change notifications to connected clients.

---

## Security Model

Moca implements defense in depth across seven layers:

1. **Network** — TLS everywhere, IP allowlists for admin endpoints
2. **Edge** — Rate limiting (per-user, per-API-key, per-tenant), request size limits, CORS
3. **Authentication** — Session cookies, JWT, API keys with scopes, OAuth2, SAML/OIDC
4. **Application** — Permission engine with role-based, field-level, and row-level checks plus custom rule evaluation
5. **Database** — PostgreSQL RLS policies per tenant schema as defense-in-depth
6. **Audit** — Immutable, partitioned audit log for all mutations with correlation IDs
7. **Encryption** — Sensitive fields encrypted at rest (AES-256-GCM)

---

## Key Architecture Decisions (ADRs)

| Decision | Choice | Trade-off |
|----------|--------|-----------|
| **Tenant isolation** | PostgreSQL schema-per-tenant with RLS | Slightly weaker than DB-per-tenant; mitigated by RLS. Allows connection pooling and simpler ops. |
| **Job queuing** | Redis Streams | Less feature-rich than RabbitMQ; avoids extra infrastructure since Redis is already required. |
| **Event streaming** | Kafka (optional, Redis pub/sub fallback) | Two messaging systems to operate; justified by clear separation between transient jobs and durable events. |
| **Event consistency** | Transactional outbox pattern | Slight latency increase (~100ms poll); guarantees at-least-once delivery without dual-write risks. |
| **Dynamic fields** | `_extra JSONB` column on every table | Slower to query than real columns (GIN indexes help); enables runtime field addition without DDL. |
| **Full-text search** | Meilisearch | Less powerful analytics than Elasticsearch; far simpler to operate for business app search volumes. |
| **Frontend** | Decoupled React SPA consuming metadata API | Higher initial cost; better long-term maintainability, enables mobile and third-party UIs. |
| **Multi-app composition** | Go workspaces (`go.work`) | MVS version resolution can conflict across many apps; mitigated by pre-install validation and `replace` directives. |

Full ADR details are in `MOCA_SYSTEM_DESIGN.md` §16 and `docs/blocker-resolution-strategies.md`.

---

## Request Lifecycle

The full journey of a `POST /api/v1/resource/SalesOrder`:

```
 1. Load balancer routes to a moca-server instance
 2. Tenant resolver: subdomain → SiteContext (from Redis cache or system DB)
 3. Auth middleware: JWT/session → user loaded
 4. Rate limiter: sliding window check in Redis
 5. API version router: matched to v1 handler
 6. Request transformer: v1 field mapping applied ("customer" → "customer_name")
 7. Permission check: user has "create" on SalesOrder?
 8. Document creation:
    a. MetaType loaded from Redis cache
    b. DynamicDoc created, naming engine generates name (SO-0042)
    c. Lifecycle: BeforeInsert → Validate → BeforeSave
    d. BEGIN transaction → INSERT doc + children + outbox event → COMMIT
    e. Lifecycle: AfterInsert → OnChange
    f. Document cached in Redis
 9. Hook execution: registered after_insert handlers (priority-ordered)
10. Response transformer: field mapping, excluded fields removed
11. Audit log: async write
12. Response: 201 Created
13. Async: outbox → Kafka, search index update, WebSocket broadcast, webhooks
```

---

## Deployment Models

### Development (Single Process)

`moca serve` runs all components as goroutines in one process with filesystem watching for MetaType hot reload. Requires only PostgreSQL and Redis (both provided via `docker-compose.yml`).

### Production (Horizontally Scaled)

```
Load Balancer (Caddy)
    │
    ├── moca-server ×N   (stateless HTTP/WS, behind LB)
    ├── moca-worker ×N   (job consumers, scale by queue depth)
    ├── moca-scheduler ×1 (single leader via Redis lock)
    ├── moca-outbox ×1   (single leader via Redis lock)
    │
    └── Infrastructure: PG cluster, Redis cluster, Kafka 3+, Meilisearch, MinIO
```

Infrastructure configs are generated via `moca generate` (Caddy, systemd, Docker Compose, Kubernetes manifests).

---

## Roadmap Context

Moca follows a 30-milestone roadmap across four workstreams (Core, CLI, Frontend, Ops). MS-00 through MS-10 are complete (architecture validation through dev server & hot reload), establishing the MVP at v0.1.0. Next milestones target CLI operational commands (MS-11), multitenancy (MS-12), and the permission engine (MS-14). The estimated path to v1.0 is approximately 72 weeks. Full details in [`ROADMAP.md`](./ROADMAP.md).

---

## Further Reading

| Document | Contents |
|----------|----------|
| [`MOCA_SYSTEM_DESIGN.md`](./docs/MOCA_SYSTEM_DESIGN.md) | Complete framework specification — MetaType, Document Runtime, API layer, permissions, hooks, workflows, database, caching, queuing, Kafka, frontend, multitenancy, observability |
| [`MOCA_CLI_SYSTEM_DESIGN.md`](./docs/MOCA_CLI_SYSTEM_DESIGN.md) | CLI tool architecture — 152 commands across 23 groups, context detection, internal packages |
| [`ROADMAP.md`](./ROADMAP.md) | 30-milestone roadmap with dependency graph and critical path |
| [`SECURITY.md`](./SECURITY.md) | Security policy, vulnerability reporting, threat model |
| [`docs/blocker-resolution-strategies.md`](./docs/blocker-resolution-strategies.md) | Solutions for 4 critical architectural blockers |
| [`docs/moca-database-decision-report.md`](./docs/moca-database-decision-report.md) | ADR: PostgreSQL 16+ with schema-per-tenant over CockroachDB |
| [`docs/MVP-VALIDATION-REPORT.md`](./docs/MVP-VALIDATION-REPORT.md) | MVP release validation audit (MS-00 through MS-10) |
