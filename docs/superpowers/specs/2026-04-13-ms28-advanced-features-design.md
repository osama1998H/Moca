# MS-28: Advanced Features — VirtualDoc, CDC, Dev Console, Playwright

**Status:** Design Approved
**Date:** 2026-04-13
**Dependencies:** v1.0 (MS-00 through MS-26)
**Approach:** Priority-ordered with shared foundation first (Approach C)

---

## Summary

MS-28 delivers 7 post-v1.0 features that extend Moca's document runtime, event streaming, and developer tooling. The features are independent but share foundational infrastructure (event_log table, MetaType extensions). Implementation follows three phases: foundation layer, high-value runtime features, then developer CLI tools.

### Decisions

| # | Feature | Decision |
|---|---------|----------|
| 1 | VirtualDoc | Read-only first, write-capable opt-in |
| 2 | Event Sourcing | Hybrid PostgreSQL event_log + Kafka streaming |
| 3 | Dev Console | yaegi with graceful degradation, curated stdlib |
| 4 | App Publish | GitHub Releases as registry (no custom infra) |
| 5 | Test Run-UI | Playwright shell-out with JSON reporter |
| 6 | CDC Topics | Producer-only, external consumers manage offsets |
| 7 | Dev Playground | Swagger UI + GraphiQL (both backends already exist) |

### Implementation Order

1. **Foundation:** event_log table DDL, MetaType extensions (EventSourcing, CDCEnabled), CDC producer wiring
2. **High value:** VirtualDoc, Event Sourcing, CDC Topics
3. **Developer tools:** Dev Console, Dev Playground, App Publish, Test Run-UI

---

## 1. Foundation Layer

### 1.1 `event_log` Table (Per-Tenant Schema)

```sql
CREATE TABLE event_log (
    id           BIGSERIAL PRIMARY KEY,
    doctype      TEXT NOT NULL,
    docname      TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    payload      JSONB NOT NULL,
    prev_data    JSONB,
    user_id      TEXT NOT NULL,
    request_id   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_event_log_doctype_name ON event_log (doctype, docname, created_at);
CREATE INDEX idx_event_log_created_at ON event_log (created_at);
```

Lives in each tenant's schema alongside `_outbox`. Created by the migrator when a site is created or when a doctype with `EventSourcing: true` is first registered. Inserted in the same transaction as the document write and outbox row.

### 1.2 MetaType Extensions

Two new fields on `MetaType` in `pkg/meta/metatype.go`:

```go
EventSourcing bool `json:"event_sourcing"` // opt-in: writes append to event_log table
CDCEnabled    bool `json:"cdc_enabled"`    // changes published to per-doctype CDC Kafka topic
```

`IsVirtual bool` already exists and will be honored in CRUD/lifecycle paths.

### 1.3 CDC Producer Wiring in OutboxPoller

In `pkg/events/outbox.go`, the `OutboxPoller.processSite()` method gains a fan-out step:

1. Fetch pending outbox rows (existing)
2. Publish to `TopicDocumentEvents` (existing)
3. **New:** If doctype has `CDCEnabled: true`, also publish to `events.CDCTopic(site, doctype)` → `moca.cdc.{site}.{doctype}`
4. Both publishes must succeed before marking the row as published

The `CDCTopic()` function already exists in `pkg/events/event.go`.

---

## 2. VirtualDoc

### 2.1 VirtualSource Interface

**File:** `pkg/document/virtual.go` (new)

```go
type VirtualSource interface {
    // GetList uses the existing ListOptions from pkg/document/crud.go
    // (Filters, AdvancedFilters, Fields, Limit, Offset, OrderBy, etc.)
    GetList(ctx context.Context, opts ListOptions) ([]map[string]any, int, error)
    GetOne(ctx context.Context, name string) (map[string]any, error)
    Insert(ctx context.Context, values map[string]any) (string, error)
    Update(ctx context.Context, name string, values map[string]any) error
    Delete(ctx context.Context, name string) error
}
```

`GetList` reuses the existing `document.ListOptions` type rather than defining new filter/pagination types. Write methods are optional. A `ReadOnlySource` embed provides default `ErrVirtualReadOnly` returns for Insert/Update/Delete, so adapter authors only implement `GetList` + `GetOne`:

```go
var ErrVirtualReadOnly = errors.New("virtual document source is read-only")

type ReadOnlySource struct{}

func (ReadOnlySource) Insert(context.Context, map[string]any) (string, error)       { return "", ErrVirtualReadOnly }
func (ReadOnlySource) Update(context.Context, string, map[string]any) error          { return ErrVirtualReadOnly }
func (ReadOnlySource) Delete(context.Context, string) error                          { return ErrVirtualReadOnly }
```

### 2.2 VirtualDoc Struct

```go
type VirtualDoc struct {
    metaDef  *meta.MetaType
    source   VirtualSource
    values   map[string]any
    original map[string]any
    isNew    bool
}
```

Implements the full `Document` interface:

- `Meta()` → returns `metaDef`
- `Name()` → returns `values["name"]`
- `Get(field)` / `Set(field, value)` → read/write from `values` map, Set tracks dirty state
- `GetChild(tableField)` → returns empty slice (virtual docs don't have child tables)
- `AddChild(tableField)` → returns error (not supported)
- `IsNew()` / `IsModified()` / `ModifiedFields()` → dirty tracking against `original` snapshot
- `AsMap()` / `ToJSON()` → serialize from `values`

### 2.3 VirtualSource Registry

**File:** `pkg/document/virtual_registry.go` (new)

```go
type VirtualSourceRegistry struct {
    mu      sync.RWMutex
    sources map[string]VirtualSource
}

func NewVirtualSourceRegistry() *VirtualSourceRegistry
func (r *VirtualSourceRegistry) Register(doctype string, src VirtualSource)
func (r *VirtualSourceRegistry) Get(doctype string) (VirtualSource, bool)
func (r *VirtualSourceRegistry) List() []string
```

Apps register virtual sources during app initialization (via hook or `init()`). The registry is held by `DocManager`.

### 2.4 CRUD Integration

In `DocManager`, the routing becomes:

1. Load MetaType for requested doctype
2. If `metaDef.IsVirtual` → delegate to `VirtualSource` via the registry
3. If not virtual → existing DynamicDoc + PostgreSQL path

For virtual reads: `GetOne()` → wrap result in `VirtualDoc`, run read-side lifecycle hooks. For virtual writes (if supported): run `BeforeInsert`/`BeforeValidate`/`Validate`/`BeforeSave` lifecycle hooks, then call `source.Insert()`, then `AfterInsert`/`AfterSave`.

### 2.5 API / Permissions

No changes needed. REST and GraphQL handlers already operate through `DocManager`. VirtualDoc implements `Document`, so it flows through the same API endpoints. OpenAPI/GraphQL schema generators read from MetaType — virtual doctypes get API routes automatically. Permission engine reads `MetaType.Permissions` — no special handling needed.

---

## 3. CDC Topics (Producer-Only)

### 3.1 CDC Flow

On document write (Insert/Update/Delete), if the doctype has `CDCEnabled: true` and Kafka is configured, the outbox row is published to **both** `moca.doc.events` and `moca.cdc.{site}.{doctype}`.

Same `DocumentEvent` struct — no new envelope type. The CDC topic gives external consumers a filtered, per-doctype stream rather than the firehose `moca.doc.events` topic.

### 3.2 Topic Auto-Creation

Kafka topics are created lazily on first publish (franz-go supports auto-create). Topic naming via existing `CDCTopic()` function.

### 3.3 CLI Extension

The existing `moca events tail` command (MS-16) gains a `--cdc` flag:

```
moca events tail --cdc --doctype SalesOrder --site mysite
```

Tails `moca.cdc.mysite.SalesOrder`. No new command needed.

### 3.4 Redis-Only Fallback

When Kafka is not configured (minimal mode), CDC is not available. The `cdc_enabled` flag is silently ignored. This is documented in the system design as a known limitation of minimal mode.

---

## 4. Event Sourcing (Opt-In Per MetaType)

### 4.1 Write Path Integration

In `pkg/document/crud.go`, after the existing `insertOutbox()` call within the same transaction:

```go
if metaDef.EventSourcing {
    insertEventLog(txCtx, tx, eventLogRow)
}
```

### 4.2 EventLogRow Struct

```go
type EventLogRow struct {
    ID        int64           `json:"id"`
    DocType   string          `json:"doctype"`
    DocName   string          `json:"docname"`
    EventType string          `json:"event_type"`
    Payload   json.RawMessage `json:"payload"`
    PrevData  json.RawMessage `json:"prev_data,omitempty"`
    UserID    string          `json:"user_id"`
    RequestID string          `json:"request_id"`
    CreatedAt time.Time       `json:"created_at"`
}
```

`Payload` is the full `DocumentEvent` JSON. `PrevData` is the document's previous state (captured on Update, nil on Insert).

### 4.3 Event Query & Replay

**Package:** `pkg/document/eventlog/` (new)

```go
func GetHistory(ctx context.Context, pool *pgxpool.Pool, doctype, docname string, opts QueryOpts) ([]EventLogRow, error)
func Replay(ctx context.Context, pool *pgxpool.Pool, doctype, docname string) (map[string]any, error)
```

- `GetHistory` — returns ordered event stream for a specific document
- `Replay` — reconstructs current state by applying events in order (for auditing, not hot-path reads)

### 4.4 REST API Endpoint

```
GET /api/v1/resource/{doctype}/{name}/events
```

Returns event history for a document. Only available when `EventSourcing` is true for that doctype. Requires read permission.

### 4.5 Retention Policy

Configurable in `moca.yaml`:

```yaml
event_sourcing:
  retention_days: 0  # 0 = keep forever
```

A periodic background job (via existing scheduler) prunes rows older than the retention period. Pruned events can optionally be archived to S3/MinIO before deletion.

### 4.6 Kafka Projection

When Kafka is configured, event log rows are also projected via the outbox → Kafka pipeline (same as CDC). External consumers can subscribe to the event stream for analytics or replication. When Kafka is not configured, the PostgreSQL event_log table is the sole record.

---

## 5. `moca dev console` (yaegi REPL)

### 5.1 Command

```
moca dev console [flags]

Flags:
  --site string    Target site (auto-detected from context)
  --verbose        Show package loading details
```

**File:** `cmd/moca/dev_console.go` (new)

### 5.2 Architecture

1. Resolve project context and site
2. Boot a `Services` instance (DB, Redis, Registry — via existing `newServices()`)
3. Initialize yaegi interpreter with curated stdlib
4. Start interactive REPL loop (read → interpret → print)

### 5.3 Curated Console Stdlib

**Package:** `pkg/console/stdlib.go` (new)

```go
type Console struct {
    DocManager  *document.DocManager
    Registry    *meta.Registry
    Site        *tenancy.SiteContext
    QueryRunner *orm.QueryRunner
}

func (c *Console) Get(doctype, name string) (map[string]any, error)
func (c *Console) GetList(doctype string, filters ...any) ([]map[string]any, error)
func (c *Console) Insert(doctype string, values map[string]any) (string, error)
func (c *Console) Update(doctype, name string, values map[string]any) error
func (c *Console) Delete(doctype, name string) error
func (c *Console) SQL(query string, args ...any) ([]map[string]any, error)
func (c *Console) Meta(doctype string) (*meta.MetaType, error)
func (c *Console) Sites() ([]string, error)
func (c *Console) UseSite(name string) error
```

Injected as `moca` symbol in the interpreter:

```go
> docs, _ := moca.GetList("User")
> moca.Get("SalesOrder", "SO-001")
> moca.SQL("SELECT count(*) FROM tabUser")
```

### 5.4 Graceful Degradation

On startup, attempt to load each package into yaegi. Log warnings for failures, continue with what loaded:

```
⚠ Could not load pkg/search (meilisearch CGo dependency) — use moca dev execute instead
✓ Loaded: document, meta, orm, tenancy, events
moca>
```

### 5.5 Line Editing

`peterh/liner` or `golang.org/x/term` for readline support (history, arrow keys, ctrl-c). History persisted to `~/.moca/console_history`.

### 5.6 Dependencies

`traefik/yaegi` added to `go.mod`. Only imported in `cmd/moca` (CLI binary), not in library packages.

---

## 6. `moca dev playground`

### 6.1 Command

```
moca dev playground [flags]

Flags:
  --port int       Playground port (default: 8001)
  --site string    Target site (auto-detected from context)
  --no-open        Don't auto-open browser
```

**File:** `cmd/moca/dev_playground.go` (new)

### 6.2 Architecture

A lightweight HTTP server that serves a unified landing page and proxies to the running Moca server's existing Swagger UI and GraphiQL endpoints.

Routes:

- `GET /` — landing page with links to Swagger and GraphiQL
- `GET /swagger` → reverse proxy to `{server}/api/docs`
- `GET /graphql` → reverse proxy to `{server}/api/graphql/playground`

### 6.3 Why Proxy

Proxying avoids CORS issues and lets the playground inject auth headers automatically. On startup, if a dev user exists (e.g., `Administrator`), generate a short-lived token and inject it into both Swagger and GraphiQL via a JS shim — zero-config authenticated exploration.

### 6.4 Server Dependency

Requires the Moca server to be running. If no server is detected, prints an error suggesting `moca dev start` first.

### 6.5 Landing Page

Embedded HTML template:

```
┌─────────────────────────────────┐
│  Moca API Playground            │
│  Site: mysite.localhost          │
│                                 │
│  [Swagger UI]  [GraphiQL]       │
│                                 │
│  Server: http://localhost:8000   │
│  OpenAPI: /api/v1/openapi.json  │
│  GraphQL: /api/graphql          │
└─────────────────────────────────┘
```

---

## 7. `moca app publish`

### 7.1 Command

```
moca app publish APP_NAME [flags]

Flags:
  --tag string     Release tag (auto-detected from manifest version)
  --dry-run        Validate and show what would be published
  --notes string   Release notes (default: auto-generated from changelog)
```

**File:** `cmd/moca/app_publish.go` (new)

### 7.2 Flow

1. **Validate** — Read app manifest (`apps/{app}/manifest.json`), verify required fields
2. **Build** — Package app into `{app}-{version}.tar.gz`. Exclude `.git/`, `node_modules/`, test fixtures, build artifacts
3. **Tag** — Create git tag `v{version}` on the app's repo (if not already tagged)
4. **Publish** — Use GitHub API (`gh` CLI or `go-github`) to create a release with:
   - Title: `{app} v{version}`
   - Body: changelog/notes
   - Assets: tarball + `manifest.json`

### 7.3 Manifest Extensions

Existing app manifest gains optional publishing fields:

```json
{
  "name": "crm",
  "version": "0.1.0",
  "description": "CRM application for Moca",
  "license": "MIT",
  "moca_version": ">=1.0.0",
  "repository": "github.com/myorg/moca-crm",
  "author": "My Org",
  "keywords": ["crm", "sales"]
}
```

`repository` is required for publish.

### 7.4 Auth

Uses existing GitHub credentials: `gh auth status` first, falls back to `GITHUB_TOKEN` env var. Clear error if neither is available.

### 7.5 `--dry-run` Output

```
App:         crm
Version:     0.1.0
Repository:  github.com/myorg/moca-crm
Tag:         v0.1.0
Archive:     crm-0.1.0.tar.gz (2.3 MB)
Files:       47 files, 12 directories
Excluded:    .git/, node_modules/, *_test.go

✓ Manifest valid
✓ No uncommitted changes
✓ Tag v0.1.0 does not exist yet
Ready to publish. Run without --dry-run to create the release.
```

---

## 8. `moca test run-ui` (Playwright)

### 8.1 Command

```
moca test run-ui [flags]

Flags:
  --app string        Test a specific app's UI tests
  --site string       Test site (auto-created if not specified)
  --headed            Run in headed mode (visible browser)
  --browser string    Browser: "chromium" (default), "firefox", "webkit"
  --workers int       Parallel workers (default: 1)
  --filter string     Run tests matching pattern
  --update-snapshots  Update visual regression snapshots
  --keep-site         Don't cleanup test site after run
  --verbose           Show Playwright's full output alongside structured results
```

**File:** `cmd/moca/test_run_ui.go` (new)

### 8.2 Flow

1. **Prerequisite check** — Verify `npx` and `playwright` are available. Print install instructions if not
2. **Test site** — Create ephemeral test site (reuse existing site provisioning from `moca test run`)
3. **Start dev server** — Boot Moca server pointed at test site on ephemeral port
4. **Environment** — Pass to Playwright via env vars:
   ```
   MOCA_TEST_BASE_URL=http://localhost:{port}
   MOCA_TEST_SITE={sitename}
   MOCA_TEST_USER=Administrator
   MOCA_TEST_PASSWORD={auto-generated}
   ```
5. **Execute** — Shell out: `npx playwright test --reporter=json,line --config={config_path} {filter_args}`
6. **Parse JSON report** — Transform Playwright JSON output into Moca's output format (TTY/JSON/table via `internal/output/`)
7. **Cleanup** — Stop dev server, drop test site (unless `--keep-site`)

### 8.3 Test File Convention

UI tests live in `apps/{app}/tests/ui/` or `desk/tests/ui/`:

```
apps/crm/tests/ui/
  sales-order.spec.ts
  customer-list.spec.ts
  playwright.config.ts
```

If no `playwright.config.ts` exists, Moca generates a default one pointing at the test server URL.

### 8.4 JSON Report Parsing

Extract from Playwright's JSON reporter: suite/test names, pass/fail/skip, duration, error messages, screenshot paths.

Output:

```
UI Tests — crm (chromium)

  ✓ Sales Order creation flow          (2.3s)
  ✓ Customer list filtering            (1.1s)
  ✗ Invoice PDF download               (3.4s)
    Error: Expected download to start within 5000ms
    Screenshot: tests/ui/results/invoice-pdf-download-1.png

3 tests: 2 passed, 1 failed (6.8s)
```

### 8.5 Isolation

All Playwright interaction lives in `cmd/moca/test_run_ui.go`. No Node.js dependencies in `pkg/` or `internal/`. JSON report parsing is pure Go (`encoding/json`).

---

## New Files Summary

| File | Purpose |
|------|---------|
| `pkg/document/virtual.go` | VirtualDoc, VirtualSource, ReadOnlySource |
| `pkg/document/virtual_registry.go` | VirtualSourceRegistry |
| `pkg/document/eventlog/eventlog.go` | EventLogRow, GetHistory, Replay |
| `pkg/console/stdlib.go` | Console helpers for yaegi REPL |
| `cmd/moca/dev_console.go` | `moca dev console` command |
| `cmd/moca/dev_playground.go` | `moca dev playground` command |
| `cmd/moca/app_publish.go` | `moca app publish` command |
| `cmd/moca/test_run_ui.go` | `moca test run-ui` command |

## Modified Files Summary

| File | Change |
|------|--------|
| `pkg/meta/metatype.go` | Add `EventSourcing`, `CDCEnabled` fields |
| `pkg/document/crud.go` | Add event_log insert in write TX, VirtualDoc routing |
| `pkg/events/outbox.go` | CDC fan-out in OutboxPoller |
| `cmd/moca/dev.go` | Wire console and playground subcommands |
| `cmd/moca/test_cmd.go` | Wire run-ui subcommand |
| `cmd/moca/events.go` | Add `--cdc` flag to `events tail` |
| `pkg/api/rest.go` | Add `/resource/{doctype}/{name}/events` endpoint |
| `go.mod` | Add `traefik/yaegi`, `peterh/liner` |

## New Dependencies

| Dependency | Purpose | Scope |
|------------|---------|-------|
| `traefik/yaegi` | Go interpreter for dev console | `cmd/moca` only |
| `peterh/liner` | Readline/history for REPL | `cmd/moca` only |
