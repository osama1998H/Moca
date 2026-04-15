# Moca v2 Roadmap Design Spec

**Date:** 2026-04-15
**Status:** Draft
**Author:** Osama + Claude

## Vision

Moca v2 transforms the framework from a developer tool into a **business application platform** — an API-first, cloud-native system where non-developers configure business logic visually, developers experience zero friction, and the UI feels as fast and polished as Linear.

**North Star:** An API-first business application platform that scales from laptop to global edge, where non-developers configure business logic visually, and developers experience zero friction.

**Design philosophy:** "Designed by Apple" — opinionated elegance, "it just works" simplicity, progressive disclosure (simple by default, powerful when needed).

## Strategic Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Target audience | All (solo dev to enterprise) | Progressive disclosure model |
| Integration model | API-first (like Supabase) | Moca is the backend-of-record; external systems connect via APIs |
| Ecosystem | No marketplace — Moca IS the platform | Trusted Go app system stays; value is the framework, not an app store |
| Portal SSR (MS-27) | Dropped | API-first — users bring their own frontend for public pages |
| WASM Marketplace (MS-29) | Dropped | No sandboxed plugins needed; framework is the product |
| UI paradigm | Linear meets Notion | Keyboard-first, blazing fast, minimal chrome, dark mode default |

## Release Strategy

Incremental waves. Each wave delivers one pillar fully. Users get value early and often.

| Wave | Theme | Summary |
|------|-------|---------|
| **v2.0** | No-Code Business Logic | Visual Workflow Builder, Formula Fields, Automation Rules Engine |
| **v2.1** | Developer Experience | Sub-second full-stack hot reload, typed SDK generation, one-command cloud deploy |
| **v2.2** | Linear-Speed UI | Keyboard-first Desk redesign, Command Palette, performance overhaul, dark mode |
| **v2.3** | Cloud-Native Scale | Horizontal scaling, edge computing, offline-first with sync |

---

## Wave v2.0: No-Code Business Logic

The flagship wave. Three interconnected features that transform Moca from a developer framework into a business platform.

### v2.0-A: Formula / Computed Fields

**What:** A new `formula` property on any numeric, text, or date field. Business users write spreadsheet-like expressions directly in the DocType definition or DocType Builder UI. No server scripts, no hooks, no Go code.

**Expression examples:**
```
total = qty * rate
net_total = total - (total * discount / 100)
grand_total = SUM(items.net_total)
tax_total = grand_total * tax_rate / 100
rounded_total = ROUND(grand_total + tax_total, 2)
due_date = ADD_DAYS(posting_date, payment_terms)
status_label = IF(grand_total > 10000, "High Value", "Standard")
```

**Expression language:**
- Arithmetic: `+`, `-`, `*`, `/`, `%`, `()`
- Comparison: `==`, `!=`, `>`, `<`, `>=`, `<=`
- Logical: `AND`, `OR`, `NOT`, `IF(cond, then, else)`
- Aggregation over child tables: `SUM(table.field)`, `COUNT(table)`, `AVG(table.field)`, `MIN()`, `MAX()`
- String: `CONCAT()`, `UPPER()`, `LOWER()`, `LEFT()`, `RIGHT()`, `LEN()`
- Date: `ADD_DAYS()`, `DIFF_DAYS()`, `NOW()`, `TODAY()`
- Math: `ROUND()`, `CEIL()`, `FLOOR()`, `ABS()`
- Cross-document lookup: `LOOKUP("Customer", customer, "credit_limit")`

**Architecture:**
- Formulas stored in `FieldDef.Formula` (string)
- **Compile-time:** MetaType compiler parses and validates formulas — catches syntax errors, undefined fields, circular dependencies, type mismatches
- **Runtime:** Formula evaluator runs in `BeforeSave` lifecycle hook, after validation but before database write. Dependency-sorted so `net_total` computes before `grand_total`
- **Frontend:** Real-time formula preview in FormView as users type values — computed fields update instantly (same expression engine compiled to TS or reimplemented)
- **DocType Builder:** Formula editor with autocomplete for field names, function names, and syntax highlighting

**Dependency graph:**
- Compiler builds a DAG of field dependencies from formulas
- Circular references detected and rejected at compile time
- Evaluation order is topological sort of the DAG

### v2.0-B: Visual Workflow Builder

**What:** A drag-and-drop editor in the Desk UI for designing document workflows — state machines, approval chains, escalation rules, and SLA timers. Replaces hand-editing WorkflowMeta JSON.

**UI components:**
1. **Workflow Canvas** — React Flow (or similar) for node/edge rendering with zoom, pan, minimap
2. **State Node Editor** — configure state properties (name, style, SLA, allowed actions)
3. **Transition Editor** — configure guard conditions using a visual condition builder (field + operator + value)
4. **Condition Builder** — reusable visual component: "When [field] [operator] [value] AND/OR..." — generates the guard expression
5. **Approval Chain Designer** — sequential or parallel approver assignment with delegation rules
6. **SLA Configuration** — deadline relative to state entry, escalation action (notify, auto-transition, reassign)
7. **Simulation Mode** — step through workflow with mock data, see which transitions are available at each state

**Architecture:**
- The visual builder produces the same `WorkflowMeta` JSON the v1 workflow engine already consumes
- No new backend — the builder is a frontend tool that writes to the existing workflow data model
- Simulation mode uses the existing `workflow.Evaluator` via API call with dry-run flag
- Deployed workflows are versioned — editing creates a new version; active documents continue on their original version

### v2.0-C: Automation Rules Engine

**What:** A trigger-condition-action system that non-developers use to automate business processes. No code required.

**Mental model:** "When [event] happens on [DocType], if [conditions] are met, then do [actions]."

**Trigger types:**

| Trigger | Description |
|---------|-------------|
| `on_create` | Document is created |
| `on_update` | Document is updated |
| `on_submit` | Document is submitted |
| `on_cancel` | Document is cancelled |
| `on_value_change` | Specific field value changes |
| `on_schedule` | Cron schedule (daily, weekly, custom) |
| `on_date_field` | Relative to a date field (e.g., 3 days before `due_date`) |

**Condition builder** (same visual component as workflow guards):
- Field conditions: `grand_total > 5000 AND status == "Submitted"`
- Cross-document: `LOOKUP("Customer", customer, "is_vip") == true`
- Time-based: `DIFF_DAYS(creation, TODAY()) > 30`

**Action types:**

| Action | Description |
|--------|-------------|
| `set_value` | Set a field value on the current document |
| `create_document` | Create a new document of any type with field mappings |
| `update_document` | Update a linked document |
| `send_email` | Send email using a template + recipients |
| `send_notification` | In-app notification to users/roles |
| `call_webhook` | POST to an external URL with document data |
| `enqueue_job` | Enqueue a background job |
| `trigger_workflow` | Force a workflow transition |
| `log_event` | Write to audit log |

**Architecture:**
- Rules stored as documents (AutomationRule DocType)
- On server startup, rules loaded and registered as hooks with the existing HookRegistry
- Trigger maps to document lifecycle events (BeforeSave, AfterInsert, etc.)
- Condition evaluated using the same expression engine as formulas
- Actions executed via a dispatcher that maps action type to handler
- Scheduled triggers registered with the existing scheduler (moca-scheduler)
- Execution logged to an AutomationLog DocType for debugging

**Error handling:**
- Failed condition evaluation: rule skipped and logged
- Failed action: subsequent actions still execute (configurable: stop-on-error or continue)
- Failed actions retried via existing DLQ mechanism
- Rate limiting per rule to prevent infinite loops (e.g., rule A triggers rule B triggers rule A)

---

## Wave v2.1: Developer Experience

Three features that make working with Moca feel like the future.

### v2.1-A: Sub-Second Full-Stack Hot Reload

**What:** Change any MetaType definition, workflow, automation rule, or Go hook — and see the result across the entire stack (database, API, search index, React UI) in under 1 second.

**Reload pipeline:**
1. File watcher detects change to `.json` MetaType file or Go source
2. Diff engine compares new MetaType against in-memory compiled version. Produces `SchemaDiff` — added/removed/modified fields
3. Safe migration check classifies each diff as safe (add column, add index, widen varchar) or unsafe (drop column, change type). Unsafe changes require `--force` or confirmation
4. Database migrator applies safe DDL changes transactionally
5. Search index updater patches Meilisearch index settings
6. API route regenerator rebuilds route table from updated MetaType
7. SDK regenerator (if watch mode active) regenerates typed client code
8. WebSocket push sends `meta_changed` event to connected Desk clients → React Query cache invalidated → re-render

**Safety:**
- Diff engine makes reload surgical, not full-restart
- Safe vs. unsafe classification prevents accidental data loss
- Go hook changes require `go build` — dev server auto-rebuilds and hot-swaps via graceful restart (~2-3s)

### v2.1-B: Type-Safe SDK Generation

**What:** `moca generate sdk` reads MetaType definitions and produces a fully typed client SDK. Every DocType becomes a typed API with autocomplete, validation, and documentation.

**Generated TypeScript example:**
```typescript
import { MocaClient } from "@moca/sdk";

const moca = new MocaClient({
  url: "https://myapp.moca.cloud",
  site: "acme",
  token: "Bearer ..."
});

// Full autocomplete for every field
const order = await moca.SalesOrder.create({
  customer: "CUST-001",
  posting_date: "2025-04-15",
  items: [{ item_code: "ITEM-001", qty: 10, rate: 99.50 }]
});

order.grand_total;  // number
order.status;       // "Draft" | "Submitted" | "Cancelled"

// Typed filters
const orders = await moca.SalesOrder.list({
  filters: { status: "Submitted", grand_total: [">", 5000] },
  fields: ["name", "customer", "grand_total"],
  orderBy: "posting_date",
  limit: 20
});

// Workflow actions typed per workflow definition
await moca.SalesOrder.workflow(order.name).approve({ comment: "Looks good" });

// Real-time subscriptions
moca.SalesOrder.subscribe(order.name, (event) => {
  console.log(`${event.user} updated ${event.fields.join(", ")}`);
});
```

**Supported languages:**

| Language | Output | Use Case |
|----------|--------|----------|
| TypeScript | npm package | Web frontends, Node.js backends |
| Python | pip package | Data scripts, Django/FastAPI backends, ML pipelines |
| Go | Go module | Microservices, CLI tools |
| Dart | pub package | Flutter mobile apps |

**Per DocType generation:**
- Type definition with all fields, correct types, optional/required
- CRUD methods (create, get, list, update, delete)
- Filter types (field-specific operators)
- Workflow action methods (per workflow state)
- Event subscription method
- Child table types (nested)
- Link field validation (referenced DocType)
- Select field union types (from options list)
- Formula fields marked as read-only

**Schema drift detection:**
- SDK includes schema version hash
- Runtime warning if server schema has drifted from generated SDK
- CI integration: `moca generate sdk --check` exits non-zero if SDK is stale

### v2.1-C: One-Command Cloud Deployment

**What:** `moca deploy cloud` provisions a full production stack on a cloud provider. SSL, database, cache, search, storage, CDN — all configured automatically.

**Supported providers (v2.1 launch):**

| Provider | Infra Model |
|----------|-------------|
| DigitalOcean | App Platform + Managed DB + Spaces |
| AWS | ECS/Fargate + RDS + ElastiCache + S3 |
| GCP | Cloud Run + Cloud SQL + Memorystore + GCS |
| Fly.io | Machines + Tigris + Upstash |
| Self-hosted | Docker Compose on any VPS (enhanced v1 `moca deploy setup`) |

**Features:**
- **Zero config** — sensible defaults for everything. One command, no YAML editing
- **Environment management** — `moca deploy cloud --env staging` creates isolated staging. `moca deploy promote staging production` promotes with zero downtime
- **Rollback** — `moca deploy rollback` reverts to previous deployment in seconds
- **Scaling** — `moca deploy scale --replicas 4 --workers 2` adjusts horizontally
- **Logs** — `moca deploy logs --follow` streams from all containers
- **Cost estimation** — shows estimated monthly cost before deploying

**Architecture:**
- Provider-specific adapters implement `CloudProvider` interface: `Provision()`, `Deploy()`, `Scale()`, `Rollback()`, `Destroy()`
- Each adapter uses the provider's API/CLI (DO CLI, AWS CDK, gcloud, flyctl)
- Infrastructure state tracked in `.moca/deploy.state.json` (local)
- Binary cross-compiled for target OS/arch during deploy
- Database migrations run as deploy step with rollback on failure
- Health check validates deployment before switching traffic

**Boundaries:**
- Not a managed hosting platform — you own the infrastructure
- Not multi-region by default (that's v2.3)
- Not Kubernetes by default (but `--platform k8s` available for advanced users)

---

## Wave v2.2: Linear-Speed UI

The Desk rebuilt with one principle: every interaction must feel instant.

### v2.2-A: Keyboard-First Architecture

Every action in the Desk is reachable via keyboard. Mouse is supported, keyboard is preferred.

**Global shortcuts:**

| Shortcut | Action |
|----------|--------|
| `Cmd+K` | Command Palette |
| `Cmd+N` | New document |
| `Cmd+S` | Save document |
| `Cmd+Enter` | Submit document |
| `Cmd+/` | Toggle sidebar |
| `Cmd+.` | Quick actions menu (context-aware) |
| `Cmd+Shift+P` | Switch between sites |
| `Esc` | Close / cancel / go back |
| `G then L` | Go to List view (vim-style chord) |
| `G then D` | Go to Dashboard |
| `G then R` | Go to Reports |
| `G then W` | Go to Workflow Builder |
| `G then A` | Go to Automation Rules |
| `?` | Shortcut cheat sheet overlay |

**ListView shortcuts:** `J`/`K` navigate, `Enter` opens, `X` selects, `/` focuses filter bar, `C` creates new.

**FormView shortcuts:** `Tab`/`Shift+Tab` navigate fields, `Cmd+Shift+1..9` switch tabs, `Cmd+D` duplicate, `Cmd+L` copy link.

### v2.2-B: Command Palette (Cmd+K)

The single entry point for everything. Search documents, navigate, execute actions, switch context.

**Capabilities:**
- Search documents (full-text via Meilisearch, ranked by relevance + recency)
- Navigate to DocType lists or specific documents
- Execute actions ("Create Sales Order", "Run Revenue Report", "Clear Cache")
- Switch site (`site:acme`)
- Run automations
- Open settings
- Inline calculator (`42 * 1.15` shows result)
- Recent history (shown before typing)

**Implementation:**
- Frontend queries Meilisearch + backend API on each keystroke (debounced 150ms)
- New `/api/v1/search/global` endpoint fans out to Meilisearch + MetaType registry + action registry
- Frecency scoring — frequently accessed documents rank higher
- Extensible: apps register custom commands via `registerCommand({ name, action, shortcut, keywords })`

### v2.2-C: Performance Overhaul

**Target:** Every interaction under 100ms perceived latency. Lists render in under 50ms.

**Optimistic UI:**
- Save shows success immediately while API in-flight
- Delete shows undo toast (3s grace period) before sending request
- Workflow transitions show new state immediately, roll back on error

**Prefetching:**
- Hover on document link → prefetch data
- ListView loads → prefetch first 5 documents
- Tab navigation → prefetch next tab's components
- MetaType definitions cached aggressively

**Virtual scrolling:**
- ListView uses TanStack Virtual — render only visible rows
- Handles 100K+ rows without lag
- ChildTable fields use virtual scrolling for large child sets

**Bundle optimization:**
- Route-level code splitting
- Field components lazy-loaded by type
- Initial bundle target: under 150KB gzipped

**Animations:**
- Page transitions: shared layout animation (Framer Motion)
- List to Form: selected row expands into form (connected animation)
- Modals: spring-based entrance (physical feel)
- Skeleton loading: content-aware skeletons matching actual layout
- All animations respect `prefers-reduced-motion`

### v2.2-D: Dark Mode & Design System

**Dark mode:**
- Default theme (switchable to light)
- True dark palette, not inverted colors
- Syntax highlighting adapts to theme
- Charts and dashboard widgets have dark variants
- Persisted per-user, synced to server preference

**Design system:**
- Typography: Inter Variable (body) + JetBrains Mono (code)
- Spacing: 4px base grid (8/12/16/24/32px scale)
- Colors: semantic tokens (--color-success, --color-danger, etc.) + DocType accent colors
- Density modes: Comfortable (default), Compact (power users), Spacious (presentations)
- Status pills: consistent colored badges across all views
- Empty states: illustrated with actionable CTAs
- Loading: content-aware skeletons, no spinners
- Errors: inline with fix suggestions, not modal alerts

### v2.2-E: ListView Redesign

New capabilities beyond v1:
- **Bulk actions** — select rows, bulk update/delete/export/workflow transition
- **Column customization** — drag reorder, show/hide, resize. Saved per-user per-DocType
- **Saved filters** — named views ("My Open Orders", "Overdue Invoices"). Shared or private
- **Kanban toggle** — switch to kanban board grouped by any Select/Link field
- **Calendar toggle** — calendar view for DocTypes with date fields
- **Inline editing** — double-click cell to edit in place (text, number, select, check fields)
- **Advanced filters** — date ranges, between, not in, nested AND/OR groups
- **Grouping** — group by any field with collapsible groups and subtotals

### v2.2-F: FormView Redesign

New capabilities:
- **Activity timeline** — right sidebar: who changed what, when. Comments, workflow transitions, automation executions
- **Inline comments** — click any field to comment (like Google Docs)
- **Field-level change indicators** — green pulse on modified fields after save
- **Linked documents panel** — collapsible sidebar showing backlinks
- **Quick actions bar** — floating bottom bar: Save, Submit, Approve, Reject, Print, Duplicate, Delete (adapts to workflow state)
- **Split view** — compare two document versions side by side
- **Breadcrumb navigation** — Module > DocType > Document

---

## Wave v2.3: Cloud-Native Scale

Moca becomes infrastructure that works at any scale, anywhere, even without a network.

### v2.3-A: Horizontal Scaling

Moca servers become fully stateless. Run 1 or 100 instances behind a load balancer.

**Changes from v1:**

| Concern | v1 | v2.3 |
|---------|-----|------|
| Connection pooling | Per-process pgxpool | External pooler (PgBouncer/Supavisor) shared across instances |
| MetaType registry | In-memory per process | Shared Redis registry + local cache with pub/sub invalidation |
| File uploads | Direct to app server | Presigned URLs direct to S3 |
| WebSocket | Bound to single server | Redis pub/sub fan-out across all servers |
| Search sync | Single daemon | Partitioned — each worker claims a tenant shard |

**Key components:**

**Stateless server refactoring:**
- MetaType registry becomes read-through cache: Redis is source of truth, local cache with TTL
- `meta_changed` events via Redis pub/sub — all instances invalidate within milliseconds

**Connection pooling tier:**
- PgBouncer/Supavisor between Moca and PostgreSQL
- Transaction-mode pooling
- Per-tenant `SET search_path` as first statement per transaction

**WebSocket fan-out:**
- Client connects to any server
- Server subscribes to Redis pub/sub for client's DocType subscriptions
- Any server publishing a change → all servers push to relevant clients

**Read replicas:**
- `database.read_replicas` config in `moca.yaml`
- QueryBuilder tags queries as `read` or `write`
- Read queries to replica pool (round-robin)
- Sticky-read window (500ms) after writes to avoid stale reads

**Auto-scaling signals:**
- CPU > 70% → scale up app replicas
- Queue depth > 1000 → scale up workers
- P95 response > 500ms → scale up app replicas
- Configurable thresholds

### v2.3-B: Edge Computing

Deploy Moca's read path close to users worldwide. Sub-100ms reads globally.

**Architecture: Hub and Spoke**

```
                    Primary Hub (us-east)
                    PG Write + Redis + Meilisearch
                           |
              +------------+------------+
              |            |            |
        Edge: EU      Edge: Asia   Edge: LATAM
        PG Replica    PG Replica   PG Replica
        Redis Cache   Redis Cache  Redis Cache
        Meili replica Meili replica Meili replica
```

**How it works:**
- **Reads** served from nearest edge (PG replica + Meilisearch replica + Redis cache)
- **Writes** forwarded to primary hub (200-400ms write latency, 20-50ms read latency)
- **Routing** via Anycast DNS or geo-routing (Cloudflare/Fly.io)
- **Cache invalidation** — Redis pub/sub propagates from hub to all edges (under 500ms)

**Consistency model:**
- Read-after-write consistency within same session via `X-Moca-Write-Seq` header
- Eventual consistency across sessions (~500ms)
- Strong consistency opt-in via `?consistency=strong` query param

**Edge deploy:**
```bash
moca deploy edge --region eu-fra --region ap-sgp
```

### v2.3-C: Offline-First / Local-First

The React Desk works without internet. Documents stored locally, edits happen offline, sync on reconnect.

**Local storage layer (browser):**
- IndexedDB (via Dexie.js) stores documents, MetaTypes, user state
- Service Worker intercepts API requests — serves from IndexedDB if offline
- Background Sync API queues pending writes

**Sync protocol:**
- Document versioning via `_version` counter
- New `_changelog` table stores per-field diffs
- Sync endpoint: `POST /api/v1/sync` — batch local changes, receive remote changes
- Algorithm: client sends pending writes + last_sync_seq, server returns remote changes + conflicts

**Conflict resolution:**
- **Auto-resolve (different fields):** merge automatically
- **Auto-resolve (LWW):** same field, last-write-wins if configured
- **Manual resolve:** same field, both values shown side by side, user picks

**Selective sync:**
- Users configure which DocTypes available offline
- Role-based defaults (e.g., "Sales Reps get Sales Order + Customer + Item offline")
- Incremental sync — only changed documents since last sync
- Storage quota management with warnings

**Offline capabilities:**

| Feature | Offline |
|---------|---------|
| View documents | Full |
| Create/edit documents | Full (queued for sync) |
| FormView rendering | Full (MetaType cached) |
| ListView with filters | Full (queries IndexedDB) |
| Search | Limited (local IndexedDB, not Meilisearch) |
| Workflow transitions | Queued |
| Formula computation | Full (runs in browser) |
| File attachments | Queued (cached in Service Worker) |
| Dashboard/Reports | Partial (local data only) |
| Automation rules | Server-only |

**Network status indicator:**
- Persistent status bar: green (online), yellow (syncing), red (offline)
- Pending sync count with queue details
- Force retry option

### v2.3-D: Deployment Architecture Summary

```
CLIENT LAYER
  Browser (Desk SPA)              Mobile (SDK)
  - IndexedDB (offline)           - SQLite (offline)
  - Service Worker                - Sync engine
  - WebSocket                     - Push notifications
        |                               |
EDGE LAYER
  Geo-routed (Anycast DNS / CDN)
  - Static assets (CDN cached)
  - Read API (PG replica + Redis)
  - Search (Meilisearch replica)
  - Write proxy to Hub
        |
HUB LAYER
  App Servers (N)    Workers (M)    Scheduler/Outbox (leader)
        |                |                |
  SHARED SERVICES
  - PgBouncer -> PostgreSQL Primary
  - Redis (cache + queue + pub/sub)
  - Meilisearch Primary
  - S3 (object storage)
  - Kafka (optional)
```

---

## Cross-Cutting Concerns

### Shared Expression Engine

v2.0's formula fields, workflow conditions, and automation rule conditions all use the **same expression engine**. This is a single implementation with multiple consumers:

- **Go implementation** (server-side, authoritative): compiles expressions to an AST, evaluates at runtime. This is the source of truth — all saves validated server-side
- **TypeScript implementation** (client-side, preview-only): native TS port of the same grammar and evaluator. Used for real-time formula preview in FormView. NOT a WASM compilation of the Go code — a separate implementation that must pass the same test suite. Client-side evaluation is advisory; server re-evaluates on save
- **Syntax:** inspired by spreadsheet formulas + Python expressions, deliberately NOT a general-purpose language
- **Security:** no function calls except whitelisted built-ins, no variable assignment, no loops, no imports

### Shared Condition Builder UI

The visual condition builder component is reused across:
- Workflow transition guards
- Automation rule conditions
- ListView advanced filters
- Permission rule conditions (future)

Single component, consistent UX, one codebase to maintain.

### Migration Path from v1

- v2 waves are additive — no breaking changes to v1 APIs or data models
- Formula fields are opt-in per field (existing fields unchanged)
- Workflow builder writes the same JSON the v1 engine consumes
- Automation rules are a new DocType (no changes to existing hooks)
- UI redesign is a full replacement but the API contract is identical
- Horizontal scaling requires config changes but no code changes
- Offline-first is opt-in per deployment

### What's NOT in v2

- Real-time collaboration (Google Docs-style co-editing)
- AI/ML integration (copilot, embeddings, NLP queries)
- Visual report/dashboard builder (reports stay developer-configured)
- Visual form/page builder beyond DocType Builder
- Permission rules wizard
- Integration hub / pre-built connectors
- Mobile native apps (SDK enables Flutter/React Native but no first-party app)
- Portal SSR
- WASM plugin sandboxing / marketplace
