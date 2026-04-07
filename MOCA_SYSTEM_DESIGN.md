# Moca Framework — System Design & Architecture

**Status:** Proposed
**Date:** 2026-03-29
**Author:** Osama Muhammed
**Version:** 1.0.0

---

## 1. Executive Summary

Moca is a **metadata-driven, multitenant, full-stack business application framework** built in Go. It takes the core philosophy of Frappe — where a single DocType definition drives schema, validation, lifecycle, permissions, and UI — and rebuilds it with modern infrastructure choices and architectural improvements that address Frappe's known limitations.

**One-sentence definition:**
Moca is a metadata-driven, multitenant business application platform where a MetaType acts as the canonical contract for data schema, domain lifecycle, permissions, a fully customizable API layer, extension hooks, and a generated React-based UI.

### Technology Stack

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| Backend Runtime | **Go 1.26+** | Compiled performance, strong concurrency, single binary deployment |
| Frontend | **React.js 19+ (TypeScript)** | Component model maps naturally to metadata-driven rendering |
| Primary Database | **PostgreSQL 16+** | JSONB for dynamic fields, row-level security, partitioning for multitenancy |
| Cache | **Redis 7+ (cache mode)** | Metadata cache, session store, rate limiting, distributed locks |
| Queue | **Redis 7+ (streams)** | Lightweight async jobs, background tasks, real-time pub/sub |
| Event Streaming | **Apache Kafka** | Cross-service event bus, audit log, CDC, integration backbone |
| Search | **Meilisearch** | Full-text search across documents, typo-tolerant, faceted filtering |
| Object Storage | **S3-compatible (MinIO)** | File attachments, generated reports, export artifacts |
| Reverse Proxy | **Caddy / NGINX** | Automatic TLS, tenant-based routing |

### What Moca Fixes Over Frappe

| Frappe Limitation | Moca Improvement |
|---|---|
| Auto-generated API is rigid and non-customizable | Fully customizable API layer with middleware, transformations, versioning, GraphQL |
| Python GIL limits concurrency | Go goroutines for true parallel request handling |
| MariaDB-centric, limited query capabilities | PostgreSQL with JSONB, CTEs, window functions, partitioning |
| Monolithic process model | Decomposable into microservices when needed |
| Limited real-time capabilities | WebSocket pub/sub + Kafka event streaming built-in |
| No formal API versioning | First-class API versioning and deprecation lifecycle |
| Tightly coupled Desk UI | Decoupled React frontend consuming a metadata API |
| Hook ordering is implicit | Explicit priority-ordered hook registry with dependency resolution |
| No built-in rate limiting or API gateway | Integrated API gateway with rate limiting, throttling, API keys |

---

## 2. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         CLIENTS                                         │
│   React Desk App    Portal (SSR/SSG)    Mobile Apps    Third-Party      │
└──────────┬──────────────┬──────────────────┬──────────────┬─────────────┘
           │              │                  │              │
           ▼              ▼                  ▼              ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    API GATEWAY / EDGE LAYER                             │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐ │
│  │  Tenant   │ │   Auth   │ │  Rate    │ │  API     │ │  Request     │ │
│  │  Resolver │ │  Middleware│ │  Limiter │ │  Version │ │  Transformer │ │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘ └──────────────┘ │
│  ┌──────────┐ ┌──────────┐ ┌──────────────────────────────────────┐   │
│  │  CORS    │ │  Audit   │ │        Custom API Pipeline           │   │
│  │  Handler │ │  Logger  │ │  (user-defined middleware chains)    │   │
│  └──────────┘ └──────────┘ └──────────────────────────────────────┘   │
└──────────────────────────────┬──────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                      CORE RUNTIME ENGINE                                │
│                                                                         │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐ │
│  │  METADATA        │  │  DOCUMENT        │  │  CUSTOMIZABLE API       │ │
│  │  REGISTRY        │  │  RUNTIME         │  │  LAYER                  │ │
│  │                  │  │                  │  │                         │ │
│  │  MetaType Store  │  │  DynamicDoc      │  │  Route Builder          │ │
│  │  Field Resolver  │  │  Lifecycle Mgr   │  │  Middleware Engine      │ │
│  │  Schema Compiler │  │  Validation Eng  │  │  Response Transformer   │ │
│  │  Hot Reload      │  │  Naming Engine   │  │  GraphQL Generator      │ │
│  │  Version Tracker │  │  State Machine   │  │  API Key Manager        │ │
│  │                  │  │  Child Table Mgr │  │  Webhook Dispatcher     │ │
│  └─────────────────┘  └─────────────────┘  └─────────────────────────┘ │
│                                                                         │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐ │
│  │  PERMISSION      │  │  HOOK            │  │  WORKFLOW               │ │
│  │  ENGINE          │  │  REGISTRY        │  │  ENGINE                 │ │
│  │                  │  │                  │  │                         │ │
│  │  Role-Based      │  │  Doc Events      │  │  State Definitions      │ │
│  │  Field-Level     │  │  Scheduler Jobs  │  │  Transition Rules       │ │
│  │  Row-Level (RLS) │  │  API Middleware   │  │  Approval Chains        │ │
│  │  Custom Rules    │  │  UI Context      │  │  SLA Timers             │ │
│  │  API Scopes      │  │  Type Extensions │  │  Auto-Actions           │ │
│  └─────────────────┘  └─────────────────┘  └─────────────────────────┘ │
│                                                                         │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐ │
│  │  QUERY           │  │  APP / PLUGIN    │  │  SCHEDULER              │ │
│  │  ENGINE          │  │  MANAGER         │  │  & WORKERS              │ │
│  │                  │  │                  │  │                         │ │
│  │  Dynamic Query   │  │  App Manifest    │  │  Cron Jobs              │ │
│  │  Builder         │  │  Module Loader   │  │  Delayed Tasks          │ │
│  │  Report Builder  │  │  Migration Mgr   │  │  Event-Driven Tasks     │ │
│  │  Filter Engine   │  │  Asset Bundler   │  │  Worker Pools           │ │
│  │  Aggregations    │  │  Dependency Res.  │  │  Dead Letter Queue      │ │
│  └─────────────────┘  └─────────────────┘  └─────────────────────────┘ │
└───────┬──────────────────┬──────────────────┬───────────────────────────┘
        │                  │                  │
        ▼                  ▼                  ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────────────────────────┐
│  PostgreSQL  │  │    Redis     │  │          Kafka                   │
│              │  │              │  │                                  │
│  Schema Data │  │  Cache       │  │  doc.events.*                   │
│  Doc Data    │  │  Sessions    │  │  audit.log.*                    │
│  Metadata    │  │  Rate Limits │  │  integration.outbox.*           │
│  Audit Trail │  │  Pub/Sub     │  │  workflow.transitions.*         │
│  File Index  │  │  Job Queues  │  │  cdc.{tenant}.{doctype}.*      │
│              │  │  Dist. Locks │  │                                  │
└──────────────┘  └──────────────┘  └──────────────────────────────────┘
        │
        ▼
┌──────────────────────────────────────────────────────────────────────┐
│  SUPPORTING SERVICES                                                 │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────────┐ │
│  │ Meilisearch│  │ MinIO (S3) │  │ Prometheus │  │  Grafana       │ │
│  │ Full-text  │  │ File Store │  │ Metrics    │  │  Dashboards    │ │
│  └────────────┘  └────────────┘  └────────────┘  └────────────────┘ │
└──────────────────────────────────────────────────────────────────────┘
```

---

## 3. Core Subsystems — Deep Dive

### 3.1 Metadata Registry

The Metadata Registry is the heart of Moca. Every entity in the system — every DocType, every field, every permission rule — is defined as **runtime metadata**, not compile-time code.

#### 3.1.1 MetaType — The Core Primitive

```go
package meta

type MetaType struct {
    // Identity
    Name          string          `json:"name"`           // e.g., "SalesOrder"
    Module        string          `json:"module"`         // e.g., "selling"
    Label         string          `json:"label"`          // Human-readable
    Description   string          `json:"description"`

    // Schema
    Fields        []FieldDef      `json:"fields"`
    NamingRule    NamingStrategy  `json:"naming_rule"`
    SearchFields  []string        `json:"search_fields"`
    TitleField    string          `json:"title_field"`
    ImageField    string          `json:"image_field"`
    SortField     string          `json:"sort_field"`
    SortOrder     string          `json:"sort_order"`

    // Variants
    IsSingle      bool            `json:"is_single"`       // Singleton (settings)
    IsChildTable  bool            `json:"is_child_table"`  // Embedded in parent
    IsVirtual     bool            `json:"is_virtual"`      // External data source
    IsSubmittable bool            `json:"is_submittable"`  // Draft→Submit→Cancel

    // Behavior
    Permissions   []PermRule      `json:"permissions"`
    Workflow      *WorkflowMeta   `json:"workflow,omitempty"`
    Hooks         DocHookDefs     `json:"hooks"`

    // API Configuration (NEW — not in Frappe)
    APIConfig     *APIConfig      `json:"api_config,omitempty"`

    // UI Hints
    ViewConfig    ViewMeta        `json:"view_config"`

    // Versioning
    TrackChanges  bool            `json:"track_changes"`
    Version       int             `json:"version"`
    CreatedAt     time.Time       `json:"created_at"`
    ModifiedAt    time.Time       `json:"modified_at"`
}
```

#### 3.1.2 FieldDef — More Than a Column

```go
type FieldDef struct {
    // Identity
    Name          string      `json:"name"`
    Label         string      `json:"label"`
    FieldType     FieldType   `json:"field_type"`

    // Behavior
    Options       string      `json:"options"`       // Link target, Select options, etc.
    Required      bool        `json:"required"`
    Unique        bool        `json:"unique"`
    ReadOnly      bool        `json:"read_only"`
    Hidden        bool        `json:"hidden"`
    Default       any         `json:"default,omitempty"`
    DependsOn     string      `json:"depends_on"`    // JS expression for conditional visibility
    MandatoryDependsOn string `json:"mandatory_depends_on"`

    // Validation
    MaxLength     int         `json:"max_length,omitempty"`
    MinValue      *float64    `json:"min_value,omitempty"`
    MaxValue      *float64    `json:"max_value,omitempty"`
    ValidationRegex string   `json:"validation_regex,omitempty"`
    CustomValidator string   `json:"custom_validator,omitempty"` // registered func name

    // API (NEW)
    InAPI         bool        `json:"in_api"`          // Exposed in REST/GraphQL
    APIReadOnly   bool        `json:"api_read_only"`   // Writable in UI but not API
    APIAlias      string      `json:"api_alias"`       // Different name in API responses
    Searchable    bool        `json:"searchable"`      // Indexed in Meilisearch
    Filterable    bool        `json:"filterable"`      // Available as query filter

    // UI Layout
    InListView    bool        `json:"in_list_view"`
    InFilter      bool        `json:"in_filter"`
    InPreview     bool        `json:"in_preview"`
    Width         string      `json:"width,omitempty"`
    LayoutHint    LayoutHint  `json:"layout_hint"`

    // Indexing
    DBIndex       bool        `json:"db_index"`
    FullTextIndex bool        `json:"full_text_index"`
}

type FieldType string

const (
    FieldTypeData         FieldType = "Data"
    FieldTypeText         FieldType = "Text"
    FieldTypeLongText     FieldType = "LongText"
    FieldTypeMarkdown     FieldType = "Markdown"
    FieldTypeCode         FieldType = "Code"
    FieldTypeInt          FieldType = "Int"
    FieldTypeFloat        FieldType = "Float"
    FieldTypeCurrency     FieldType = "Currency"
    FieldTypePercent      FieldType = "Percent"
    FieldTypeDate         FieldType = "Date"
    FieldTypeDatetime     FieldType = "Datetime"
    FieldTypeTime         FieldType = "Time"
    FieldTypeDuration     FieldType = "Duration"
    FieldTypeSelect       FieldType = "Select"
    FieldTypeLink         FieldType = "Link"
    FieldTypeDynamicLink  FieldType = "DynamicLink"
    FieldTypeTable        FieldType = "Table"        // Child table
    FieldTypeTableMultiSelect FieldType = "TableMultiSelect"
    FieldTypeAttach       FieldType = "Attach"
    FieldTypeAttachImage  FieldType = "AttachImage"
    FieldTypeCheck        FieldType = "Check"        // Boolean
    FieldTypeColor        FieldType = "Color"
    FieldTypeGeolocation  FieldType = "Geolocation"
    FieldTypeJSON         FieldType = "JSON"
    FieldTypePassword     FieldType = "Password"
    FieldTypeRating       FieldType = "Rating"
    FieldTypeSignature    FieldType = "Signature"
    FieldTypeBarcode      FieldType = "Barcode"
    FieldTypeHTMLEditor   FieldType = "HTMLEditor"

    // Layout-only types (no storage)
    FieldTypeSectionBreak FieldType = "SectionBreak"
    FieldTypeColumnBreak  FieldType = "ColumnBreak"
    FieldTypeTabBreak     FieldType = "TabBreak"
    FieldTypeHTML         FieldType = "HTML"          // Static HTML in form
    FieldTypeButton       FieldType = "Button"
    FieldTypeHeading      FieldType = "Heading"
)
```

#### 3.1.3 Metadata Lifecycle

```
┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│  JSON/YAML   │───▶│   Schema     │───▶│  Compiled    │───▶│   Cached     │
│  Definition  │    │   Validator  │    │  MetaType    │    │  in Redis    │
└──────────────┘    └──────────────┘    └──────────────┘    └──────────────┘
                                               │
                           ┌───────────────────┼───────────────────┐
                           ▼                   ▼                   ▼
                    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
                    │  DB Schema   │    │  API Route   │    │  Search      │
                    │  Migrator    │    │  Generator   │    │  Index       │
                    │  (Postgres)  │    │  (REST+GQL)  │    │  Configurator│
                    └──────────────┘    └──────────────┘    └──────────────┘
```

**Hot Reload:** When a MetaType definition changes at runtime (through the Desk UI, API, or filesystem watch during development), Moca:
1. Validates the new schema against the existing data.
2. Generates and executes a migration diff (ALTER TABLE, add indexes, etc.).
3. Invalidates the Redis metadata cache.
4. Publishes a `meta.changed` event to Kafka (or Redis pub/sub if Kafka is disabled) so all running instances refresh.
5. Re-registers API routes if `APIConfig` changed.
6. Updates the Meilisearch index mapping.

**Development Mode Filesystem Trigger:** When running via `moca serve`, the server watches `*/doctypes/*.json` files for changes and triggers the full hot reload pipeline above. This enables a fast edit-save-refresh loop for MetaType development. The `--no-watch` flag disables this filesystem trigger. Note: `moca dev watch` watches only frontend assets (`.tsx`, `.css`), not MetaType JSON files — MetaType watching is handled by the server process itself.

---

### 3.2 Document Runtime

The Document Runtime is the engine that manages the lifecycle of every record in the system.

#### 3.2.1 Core Interfaces

```go
package document

// Document is the runtime representation of a single record.
type Document interface {
    Meta() *meta.MetaType
    Name() string
    Get(field string) any
    Set(field string, value any) error
    GetChild(tableField string) []Document
    AddChild(tableField string) (Document, error)
    IsNew() bool
    IsModified() bool
    ModifiedFields() []string
    AsMap() map[string]any
    ToJSON() ([]byte, error)
}

// DynamicDoc is the default implementation — a map-backed document.
type DynamicDoc struct {
    metaDef   *meta.MetaType
    values    map[string]any
    original  map[string]any       // snapshot at load time for dirty tracking
    children  map[string][]*DynamicDoc
    isNew     bool
    siteCtx   *tenancy.SiteContext
}

// VirtualDoc wraps an external data source behind the Document interface.
type VirtualDoc struct {
    metaDef   *meta.MetaType
    source    VirtualSource
    values    map[string]any
}

// VirtualSource is the adapter interface for Virtual DocTypes.
type VirtualSource interface {
    GetList(ctx context.Context, filters Filters, page Pagination) ([]map[string]any, int, error)
    GetOne(ctx context.Context, name string) (map[string]any, error)
    Insert(ctx context.Context, values map[string]any) (string, error)
    Update(ctx context.Context, name string, values map[string]any) error
    Delete(ctx context.Context, name string) error
}
```

#### 3.2.2 Document Lifecycle Engine

This is the stateful business document model — far richer than basic CRUD.

```
                         ┌─────────┐
                         │  NEW    │
                         └────┬────┘
                              │
                    BeforeInsert()
                    BeforeValidate()
                    Validate()
                    BeforeSave()
                              │
                              ▼
                    ┌─────────────────┐
                    │     SAVED       │
                    │   (Draft if     │
                    │  submittable)   │
                    └────┬───────┬────┘
                         │       │
               [update]  │       │  [submit] (if submittable)
                         │       │
            BeforeValidate()   BeforeSubmit()
            Validate()         OnSubmit()
            BeforeSave()            │
            OnUpdate()              ▼
                         │   ┌──────────────┐
                         │   │  SUBMITTED   │
                         │   │  (docstatus  │
                         │   │     = 1)     │
                         │   └──────┬───────┘
                         │          │
                         │    [cancel]
                         │          │
                         │     BeforeCancel()
                         │     OnCancel()
                         │          │
                         │          ▼
                         │   ┌──────────────┐
                         │   │  CANCELLED   │
                         │   │  (docstatus  │
                         │   │     = 2)     │
                         │   └──────────────┘
                         │
               [delete]  │
                         │
                    OnTrash()
                    AfterDelete()
                         │
                         ▼
                    ┌──────────┐
                    │ DELETED  │
                    └──────────┘
```

```go
// DocLifecycle defines all the hooks a controller can implement.
// Each method is optional — implement only what you need.
type DocLifecycle interface {
    BeforeInsert(ctx *DocContext, doc Document) error
    AfterInsert(ctx *DocContext, doc Document) error
    BeforeValidate(ctx *DocContext, doc Document) error
    Validate(ctx *DocContext, doc Document) error
    BeforeSave(ctx *DocContext, doc Document) error
    AfterSave(ctx *DocContext, doc Document) error
    OnUpdate(ctx *DocContext, doc Document) error
    BeforeSubmit(ctx *DocContext, doc Document) error
    OnSubmit(ctx *DocContext, doc Document) error
    BeforeCancel(ctx *DocContext, doc Document) error
    OnCancel(ctx *DocContext, doc Document) error
    OnTrash(ctx *DocContext, doc Document) error
    AfterDelete(ctx *DocContext, doc Document) error
    BeforeRename(ctx *DocContext, doc Document, oldName, newName string) error
    AfterRename(ctx *DocContext, doc Document, oldName, newName string) error
    OnChange(ctx *DocContext, doc Document) error // idempotent — may fire multiple times
}

// DocContext carries request-scoped data through the lifecycle.
type DocContext struct {
    context.Context
    Site      *tenancy.SiteContext
    User      *auth.User
    Flags     map[string]any       // per-request flags (skip validation, silent, etc.)
    TX        *sql.Tx              // current transaction
    EventBus  *events.Emitter      // for publishing domain events
}
```

#### 3.2.3 Naming Engine

```go
type NamingStrategy struct {
    Rule       NamingRule  `json:"rule"`
    Pattern    string      `json:"pattern,omitempty"`     // e.g., "SO-.####"
    FieldName  string      `json:"field_name,omitempty"`  // for ByField
    CustomFunc string      `json:"custom_func,omitempty"` // registered function
}

type NamingRule string

const (
    NamingAutoIncrement NamingRule = "autoincrement"
    NamingByPattern     NamingRule = "pattern"     // SO-0001, SO-0002
    NamingByField       NamingRule = "field"       // use a field value as name
    NamingByHash        NamingRule = "hash"        // short hash
    NamingUUID          NamingRule = "uuid"
    NamingCustom        NamingRule = "custom"      // call registered function
)
```

The naming engine uses PostgreSQL sequences for pattern-based naming (per tenant, per DocType) to ensure uniqueness under concurrency without table-level locks.

---

### 3.3 Customizable API Layer (Major Improvement Over Frappe)

This is the single biggest architectural improvement in Moca. Frappe auto-generates a REST API that is functional but rigid. Moca provides a **fully customizable API pipeline**.

#### 3.3.1 The Problem with Frappe's API

Frappe generates endpoints like `/api/resource/{doctype}/{name}` with fixed request/response shapes. You cannot: rename fields in the response, hide internal fields, add computed fields, version the API, add custom middleware per-endpoint, compose multiple DocTypes into one endpoint, or throttle specific consumers differently.

#### 3.3.2 Moca's API Architecture

```
                    Incoming Request
                          │
                          ▼
               ┌─────────────────────┐
               │   Tenant Resolver   │  ← resolves site from subdomain/header
               └──────────┬──────────┘
                          │
                          ▼
               ┌─────────────────────┐
               │   Auth Middleware    │  ← JWT / API Key / Session / OAuth2
               └──────────┬──────────┘
                          │
                          ▼
               ┌─────────────────────┐
               │   Rate Limiter      │  ← per-user, per-API-key, per-tenant
               └──────────┬──────────┘
                          │
                          ▼
               ┌─────────────────────┐
               │   API Version       │  ← /api/v1, /api/v2 routing
               │   Router            │
               └──────────┬──────────┘
                          │
              ┌───────────┼───────────┐
              ▼           ▼           ▼
    ┌──────────────┐ ┌──────────┐ ┌────────────┐
    │  Auto-       │ │  Custom  │ │  GraphQL   │
    │  Generated   │ │  API     │ │  Gateway   │
    │  REST API    │ │  Routes  │ │            │
    └──────┬───────┘ └────┬─────┘ └─────┬──────┘
           │              │             │
           ▼              ▼             ▼
    ┌─────────────────────────────────────────┐
    │        Request Pipeline                  │
    │                                          │
    │  1. Request Transformer (reshape input)  │
    │  2. Permission Check                     │
    │  3. Validation Layer                     │
    │  4. Document Runtime (CRUD + lifecycle)  │
    │  5. Response Transformer (reshape out)   │
    │  6. Cache Layer                          │
    │  7. Audit Logger                         │
    └─────────────────────────────────────────┘
```

#### 3.3.3 APIConfig — Per-DocType API Customization

```go
// APIConfig is defined per MetaType — this is what makes Moca's API customizable.
type APIConfig struct {
    // Exposure
    Enabled           bool              `json:"enabled"`            // false = no auto API
    BasePath          string            `json:"base_path"`          // override default path
    Versions          []APIVersion      `json:"versions"`

    // Auto-Generated Endpoints Control
    AllowList         bool              `json:"allow_list"`         // GET /resource
    AllowGet          bool              `json:"allow_get"`          // GET /resource/:name
    AllowCreate       bool              `json:"allow_create"`       // POST /resource
    AllowUpdate       bool              `json:"allow_update"`       // PUT /resource/:name
    AllowDelete       bool              `json:"allow_delete"`       // DELETE /resource/:name
    AllowBulk         bool              `json:"allow_bulk"`         // POST /resource/bulk
    AllowCount        bool              `json:"allow_count"`        // GET /resource/count

    // Rate Limiting
    RateLimit         *RateLimitConfig  `json:"rate_limit,omitempty"`

    // Pagination
    MaxPageSize       int               `json:"max_page_size"`      // default 100
    DefaultPageSize   int               `json:"default_page_size"`  // default 20

    // Response Shaping
    DefaultFields     []string          `json:"default_fields"`     // fields returned by default
    ExcludeFields     []string          `json:"exclude_fields"`     // never return these
    AlwaysInclude     []string          `json:"always_include"`     // always return these
    ComputedFields    []ComputedField   `json:"computed_fields"`    // server-computed on response

    // Custom Middleware Chain
    Middleware        []string          `json:"middleware"`          // registered middleware names

    // Webhooks
    Webhooks          []WebhookConfig   `json:"webhooks,omitempty"`

    // Custom Endpoints
    CustomEndpoints   []CustomEndpoint  `json:"custom_endpoints,omitempty"`
}

type APIVersion struct {
    Version       string          `json:"version"`         // "v1", "v2"
    Status        string          `json:"status"`          // "active", "deprecated", "sunset"
    SunsetDate    *time.Time      `json:"sunset_date,omitempty"`
    FieldMapping  map[string]string `json:"field_mapping"`  // v2 field name → internal field
    ExcludeFields []string        `json:"exclude_fields"`
    AddedFields   []ComputedField `json:"added_fields"`
}

type ComputedField struct {
    Name       string `json:"name"`
    Type       string `json:"type"`
    Expression string `json:"expression"` // Go expression or registered function name
}

type CustomEndpoint struct {
    Method     string   `json:"method"`      // GET, POST, etc.
    Path       string   `json:"path"`        // e.g., "/:name/approve"
    Handler    string   `json:"handler"`     // registered handler function name
    Middleware []string `json:"middleware"`
    RateLimit  *RateLimitConfig `json:"rate_limit,omitempty"`
}

type WebhookConfig struct {
    Event      string            `json:"event"`       // "after_insert", "on_update", etc.
    URL        string            `json:"url"`
    Method     string            `json:"method"`
    Headers    map[string]string `json:"headers"`
    Secret     string            `json:"secret"`      // HMAC signing secret
    RetryCount int               `json:"retry_count"`
    Filters    map[string]any    `json:"filters"`     // only fire if doc matches
}
```

#### 3.3.4 GraphQL Auto-Generation

Moca generates a GraphQL schema from MetaType definitions automatically. Every DocType becomes a type, every Link field becomes a relation, and every Table field becomes a nested list.

```go
package gqlgen

// GenerateSchema reads all MetaTypes for a site and produces
// a complete GraphQL schema with:
//   - Query type with list + get for each DocType
//   - Mutation type with create/update/delete for each DocType
//   - Input types for create and update
//   - Filter input types for list queries
//   - Subscription type for real-time document changes
//   - Relay-style pagination (edges/nodes/pageInfo)
func GenerateSchema(registry *meta.Registry) (*graphql.Schema, error)
```

#### 3.3.5 Request / Response Transformer Pipeline

```go
// Transformer modifies request or response payloads.
// This is the core abstraction that makes Moca's API truly customizable.
type Transformer interface {
    TransformRequest(ctx *APIContext, req *Request) (*Request, error)
    TransformResponse(ctx *APIContext, resp *Response) (*Response, error)
}

// Built-in transformers
type FieldRemapper struct { ... }     // rename fields between internal ↔ API
type FieldFilter struct { ... }       // include/exclude fields
type ComputedInjector struct { ... }  // add server-computed fields
type Paginator struct { ... }         // normalize pagination params
type Expander struct { ... }          // expand Link fields into nested objects
type Localizer struct { ... }         // translate labels based on Accept-Language
```

**Example: Custom API for a "SalesOrder" DocType**

```yaml
# In the SalesOrder MetaType definition
api_config:
  enabled: true
  base_path: /api/v1/orders
  versions:
    - version: v1
      status: active
      field_mapping:
        customer_name: customer    # API uses "customer", internally "customer_name"
        grand_total: total
      exclude_fields: [docstatus, modified_by, _user_tags]
    - version: v2
      status: active
      field_mapping:
        customer_name: customer
        grand_total: total_amount
      added_fields:
        - name: tax_summary
          type: JSON
          expression: "computeTaxSummary(doc)"
  rate_limit:
    requests_per_minute: 60
    burst: 10
  max_page_size: 200
  middleware:
    - validateAPIKey
    - enrichCustomerData
  webhooks:
    - event: on_submit
      url: https://erp.example.com/webhooks/order-submitted
      secret: ${WEBHOOK_SECRET}
      retry_count: 3
  custom_endpoints:
    - method: POST
      path: "/:name/approve"
      handler: approveSalesOrder
      middleware: [requireManagerRole]
    - method: GET
      path: "/dashboard/summary"
      handler: orderDashboardSummary
```

---

### 3.4 Permission Engine

```go
type PermRule struct {
    Role           string `json:"role"`
    DocTypePerm    int    `json:"doctype_perm"`  // bitmask: read|write|create|delete|submit|cancel|amend
    FieldLevelRead []string `json:"field_level_read,omitempty"`
    FieldLevelWrite []string `json:"field_level_write,omitempty"`
    MatchField     string `json:"match_field,omitempty"`   // row-level: user.company == doc.company
    MatchValue     string `json:"match_value,omitempty"`
    CustomRule     string `json:"custom_rule,omitempty"`   // registered Go func for complex checks
}

// APIScopePerm controls what an API key can access (NEW — not in Frappe)
type APIScopePerm struct {
    Scope      string   `json:"scope"`       // "orders:read", "orders:write"
    DocTypes   []string `json:"doc_types"`
    Operations []string `json:"operations"`  // "read", "write", "delete"
    Filters    map[string]any `json:"filters"` // only docs matching this
}
```

**Permission resolution order:**
1. API Scope check (for API key auth)
2. Role-based DocType permission
3. Field-level permission filtering
4. Row-level match (user attribute = doc field)
5. Custom rule evaluation
6. PostgreSQL RLS policies (defense in depth for multitenancy)

---

### 3.5 Hook Registry

Moca's hook system is an explicit, priority-ordered, dependency-aware extension registry.

```go
package hooks

type HookRegistry struct {
    // Document lifecycle hooks — map[doctype]map[event][]handler
    DocEvents       map[string]map[DocEvent][]PrioritizedHandler

    // Global document hooks — fire for ALL doctypes
    GlobalDocEvents map[DocEvent][]PrioritizedHandler

    // API hooks
    APIMiddleware   map[string][]MiddlewareHandler     // per-route
    GlobalAPIMiddleware []MiddlewareHandler             // all routes
    RequestTransformers map[string][]Transformer
    ResponseTransformers map[string][]Transformer

    // Scheduler
    CronJobs        []CronJobDef
    EventJobs       map[string][]EventJobHandler       // trigger on Kafka topics

    // UI Context
    FormContextHooks map[string][]ContextHook           // inject data into form context
    ListContextHooks map[string][]ContextHook
    PortalContextHooks []ContextHook                    // inject into SSR context

    // Type Extensions
    TypeOverrides    map[string]DocLifecycleFactory     // fully replace controller
    TypeExtensions   map[string][]DocLifecycleExtension // extend/wrap controller

    // Schema Extensions
    VirtualFields    map[string][]VirtualFieldProvider  // add non-stored fields

    // Whitelisted API Methods
    APIMethods       map[string]APIMethodHandler         // /api/method/{name}
}

type PrioritizedHandler struct {
    Handler   DocEventHandler
    Priority  int        // lower = runs first; default 500
    AppName   string     // which app registered this
    DependsOn []string   // must run after these app hooks
}

type DocEvent string

const (
    EventBeforeInsert   DocEvent = "before_insert"
    EventAfterInsert    DocEvent = "after_insert"
    EventBeforeValidate DocEvent = "before_validate"
    EventValidate       DocEvent = "validate"
    EventBeforeSave     DocEvent = "before_save"
    EventAfterSave      DocEvent = "after_save"
    EventOnUpdate       DocEvent = "on_update"
    EventBeforeSubmit   DocEvent = "before_submit"
    EventOnSubmit       DocEvent = "on_submit"
    EventBeforeCancel   DocEvent = "before_cancel"
    EventOnCancel       DocEvent = "on_cancel"
    EventOnTrash        DocEvent = "on_trash"
    EventAfterDelete    DocEvent = "after_delete"
    EventOnChange       DocEvent = "on_change"
)
```

**Key improvement over Frappe:** Hook ordering in Frappe is implicit and can cause subtle bugs when multiple apps hook into the same event. Moca makes priority explicit and supports dependency declarations between app hooks.

---

### 3.6 Workflow Engine

```go
package workflow

type WorkflowMeta struct {
    Name           string            `json:"name"`
    DocType        string            `json:"doc_type"`
    IsActive       bool              `json:"is_active"`
    States         []WorkflowState   `json:"states"`
    Transitions    []Transition      `json:"transitions"`
    SLARules       []SLARule         `json:"sla_rules,omitempty"`
}

type WorkflowState struct {
    Name         string `json:"name"`
    Style        string `json:"style"`       // "Success", "Warning", "Danger", etc.
    DocStatus    int    `json:"doc_status"`   // 0=Draft, 1=Submitted, 2=Cancelled
    AllowEdit    string `json:"allow_edit"`   // role
    UpdateField  string `json:"update_field"` // field to set on state entry
    UpdateValue  string `json:"update_value"`
}

type Transition struct {
    From          string   `json:"from"`
    To            string   `json:"to"`
    Action        string   `json:"action"`         // button label: "Approve", "Reject"
    AllowedRoles  []string `json:"allowed_roles"`
    Condition     string   `json:"condition"`       // expression: "doc.grand_total > 0"
    AutoAction    string   `json:"auto_action"`     // registered func to run on transition
    RequireComment bool    `json:"require_comment"`
}

type SLARule struct {
    State          string        `json:"state"`
    MaxDuration    time.Duration `json:"max_duration"`
    EscalationRole string       `json:"escalation_role"`
    EscalationAction string     `json:"escalation_action"` // registered func
}
```

---

## 4. Data Architecture — PostgreSQL

### 4.1 Database Per Tenant

Each Moca site (tenant) gets its own PostgreSQL **schema** within a shared database cluster, or optionally its own database. This provides strong isolation while allowing efficient resource usage.

```
┌─────────────────────────────────────────────────────┐
│  PostgreSQL Cluster                                  │
│                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────┐ │
│  │ schema:       │  │ schema:       │  │ schema:     │ │
│  │ tenant_acme   │  │ tenant_globex │  │ moca_system │ │
│  │               │  │               │  │             │ │
│  │ tab_sales_ord │  │ tab_sales_ord │  │ sites       │ │
│  │ tab_customer  │  │ tab_customer  │  │ apps        │ │
│  │ tab_item      │  │ tab_item      │  │ site_apps   │ │
│  │ tab_singles   │  │ tab_singles   │  │ migrations  │ │
│  │ tab_versions  │  │ tab_versions  │  │             │ │
│  │ tab_audit_log │  │ tab_audit_log │  │             │ │
│  └──────────────┘  └──────────────┘  └────────────┘ │
└─────────────────────────────────────────────────────┘
```

### 4.2 Core System Tables

> **Note:** The system schema name `moca_system` is configurable via `moca.yaml:infrastructure.database.system_db`. All SQL examples in this document use the default name. The framework reads this value from configuration at startup and uses it for all system-level queries.

```sql
-- moca_system schema: shared across all tenants (default name; configurable via system_db)

CREATE TABLE moca_system.sites (
    name            TEXT PRIMARY KEY,              -- "acme.moca.cloud"
    db_schema       TEXT NOT NULL UNIQUE,           -- "tenant_acme"
    status          TEXT NOT NULL DEFAULT 'active', -- active, suspended, migrating
    plan            TEXT,
    config          JSONB NOT NULL DEFAULT '{}',
    admin_email     TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    modified_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE moca_system.apps (
    name            TEXT PRIMARY KEY,
    version         TEXT NOT NULL,
    title           TEXT,
    description     TEXT,
    publisher       TEXT,
    dependencies    JSONB NOT NULL DEFAULT '[]',
    manifest        JSONB NOT NULL                 -- full AppManifest as JSON
);

CREATE TABLE moca_system.site_apps (
    site_name       TEXT REFERENCES moca_system.sites(name),
    app_name        TEXT REFERENCES moca_system.apps(name),
    installed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    app_version     TEXT NOT NULL,
    PRIMARY KEY (site_name, app_name)
);
```

### 4.3 Per-Tenant Schema (auto-generated from MetaType)

```sql
-- Per-tenant schema, e.g., tenant_acme
-- Tables are auto-generated from MetaType definitions.

-- Example: A "SalesOrder" MetaType produces this table:
CREATE TABLE tenant_acme.tab_sales_order (
    -- Standard fields (every document has these)
    name            TEXT PRIMARY KEY,
    owner           TEXT NOT NULL,
    creation        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    modified        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    modified_by     TEXT NOT NULL,
    docstatus       SMALLINT NOT NULL DEFAULT 0,    -- 0=Draft, 1=Submitted, 2=Cancelled
    idx             INTEGER NOT NULL DEFAULT 0,
    workflow_state  TEXT,

    -- Fields from MetaType definition (auto-generated columns)
    customer_name   TEXT,
    transaction_date DATE,
    delivery_date   DATE,
    grand_total     NUMERIC(18,6) DEFAULT 0,
    currency        TEXT DEFAULT 'USD',
    status          TEXT DEFAULT 'Draft',
    notes           TEXT,

    -- Dynamic / overflow fields stored as JSONB
    _extra          JSONB NOT NULL DEFAULT '{}',

    -- User tags, comments count, etc.
    _user_tags      TEXT,
    _comments       TEXT,
    _assign         TEXT,
    _liked_by       TEXT
);

-- Child table example: "SalesOrderItem" (is_child_table = true)
CREATE TABLE tenant_acme.tab_sales_order_item (
    name            TEXT PRIMARY KEY,
    parent          TEXT NOT NULL,                   -- FK to parent document
    parenttype      TEXT NOT NULL,                   -- "SalesOrder"
    parentfield     TEXT NOT NULL,                   -- "items" (the Table field name)
    idx             INTEGER NOT NULL DEFAULT 0,      -- row order
    owner           TEXT NOT NULL,
    creation        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    modified        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    modified_by     TEXT NOT NULL,

    item_code       TEXT,
    item_name       TEXT,
    qty             NUMERIC(18,6) DEFAULT 0,
    rate            NUMERIC(18,6) DEFAULT 0,
    amount          NUMERIC(18,6) DEFAULT 0,
    warehouse       TEXT,
    _extra          JSONB NOT NULL DEFAULT '{}'
);

-- Singles table (for Single DocTypes like "SystemSettings")
CREATE TABLE tenant_acme.tab_singles (
    doctype         TEXT NOT NULL,
    field           TEXT NOT NULL,
    value           TEXT,
    PRIMARY KEY (doctype, field)
);

-- Version history (for track_changes)
CREATE TABLE tenant_acme.tab_version (
    name            TEXT PRIMARY KEY,
    ref_doctype     TEXT NOT NULL,
    docname         TEXT NOT NULL,
    data            JSONB NOT NULL,            -- diff / changelog
    owner           TEXT NOT NULL,
    creation        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_version_ref ON tenant_acme.tab_version(ref_doctype, docname);

-- Audit log (immutable, append-only)
CREATE TABLE tenant_acme.tab_audit_log (
    id              BIGSERIAL PRIMARY KEY,
    doctype         TEXT NOT NULL,
    docname         TEXT NOT NULL,
    action          TEXT NOT NULL,             -- "Create", "Update", "Submit", etc.
    user_id         TEXT NOT NULL,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ip_address      INET,
    user_agent      TEXT,
    changes         JSONB,                     -- field-level diff
    request_id      TEXT                       -- correlation ID
) PARTITION BY RANGE (timestamp);

-- Metadata tables (DocType definitions stored as data)
CREATE TABLE tenant_acme.tab_doctype (
    name            TEXT PRIMARY KEY,
    module          TEXT NOT NULL,
    definition      JSONB NOT NULL,             -- full MetaType as JSON
    version         INTEGER NOT NULL DEFAULT 1,
    is_custom       BOOLEAN DEFAULT false,
    owner           TEXT NOT NULL,
    creation        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    modified        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- File attachments index
CREATE TABLE tenant_acme.tab_file (
    name            TEXT PRIMARY KEY,
    file_name       TEXT NOT NULL,
    file_url        TEXT NOT NULL,              -- S3/MinIO URL
    file_size       BIGINT,
    content_type    TEXT,
    attached_to_doctype TEXT,
    attached_to_name    TEXT,
    is_private      BOOLEAN DEFAULT true,
    owner           TEXT NOT NULL,
    creation        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 4.4 The `_extra` JSONB Column Pattern

Every auto-generated table includes an `_extra JSONB` column. This handles:

1. **Custom Fields** added at runtime without ALTER TABLE.
2. **App-specific fields** that shouldn't pollute the main column set.
3. **Gradual migration** — new fields start in `_extra`, then get promoted to real columns when stable.

The query engine understands `_extra` transparently:

```go
// doc.Get("custom_field") checks columns first, then _extra->>'custom_field'
// Filters work the same way — the query builder checks if a field is a column
// or needs JSONB extraction.
```

---

## 5. Redis Architecture

Redis serves three distinct roles in Moca, using logical databases or key prefixes for isolation.

### 5.1 Caching Layer

```
Key Pattern                              Purpose                     TTL
─────────────────────────────────────────────────────────────────────────
meta:{site}:{doctype}                    Compiled MetaType cache     Until invalidated
doc:{site}:{doctype}:{name}              Hot document cache          5 min
perm:{site}:{user}:{doctype}             Resolved permissions        2 min
session:{token}                          User session data           24h
config:{site}                            Site configuration          Until invalidated
schema:{site}:version                    Schema version counter      Permanent
link:{site}:{doctype}:{field}:{value}    Link field autocomplete     1 min
```

**Redis DB Assignments** (configured in `moca.yaml`):
- `db_cache: 0` — metadata, document, and config caches
- `db_queue: 1` — Redis Streams for background jobs
- `db_session: 2` — user sessions
- `db_pubsub: 3` — pub/sub channels for WebSocket real-time and event broadcast

**Pub/Sub Channel Patterns** (on `db_pubsub`):
```
pubsub:doc:{site}:{doctype}:{name}      Document change notifications (WebSocket)
pubsub:meta:{site}:{doctype}            MetaType change broadcasts
pubsub:config:{site}                    Config change broadcasts
```

**Cache invalidation strategy:**
Moca uses a **write-through + event-based invalidation** model. When a document is saved, the document cache is updated immediately. When metadata changes, a Kafka event (or Redis pub/sub when Kafka is disabled) triggers all instances to flush their local and Redis caches.

### 5.1.1 Configuration Sync Contract

Site configuration has two representations:
- **At rest (source of truth for CLI):** YAML files on disk — `sites/{site}/site_config.yaml`, `sites/common_site_config.yaml`, and `moca.yaml`.
- **At runtime (source of truth for server):** PostgreSQL `moca_system.sites.config` JSONB column, cached in Redis as `config:{site}`.

**Sync rules:**
1. `moca site create` writes the initial config to YAML **and** syncs it to the database.
2. `moca config set` writes to the appropriate YAML file **and** updates the database atomically. It then publishes a `config.changed` event (via Kafka or Redis pub/sub) to invalidate all server instances' config caches.
3. The running server reads config **only** from the database (via Redis cache). It never reads YAML files directly.
4. `moca config get --resolved` merges all YAML layers to show the effective config. `moca config get --runtime` queries the server's active config from the database.
5. On `moca deploy update`, all YAML configs are re-synced to the database as part of the update lifecycle.

### 5.2 Queue Layer (Redis Streams)

```
Stream Name                              Purpose
─────────────────────────────────────────────────────────────────────
moca:queue:{site}:default                General background tasks
moca:queue:{site}:long                   Long-running tasks (reports, exports)
moca:queue:{site}:critical               High-priority tasks (webhooks, emails)
moca:queue:{site}:scheduler              Cron-triggered tasks
moca:deadletter:{site}                   Failed tasks for inspection/retry
```

```go
package queue

type Job struct {
    ID         string         `json:"id"`
    Site       string         `json:"site"`
    Type       string         `json:"type"`       // "send_email", "generate_report", etc.
    Payload    map[string]any `json:"payload"`
    Priority   int            `json:"priority"`
    MaxRetries int            `json:"max_retries"`
    Retries    int            `json:"retries"`
    CreatedAt  time.Time      `json:"created_at"`
    RunAfter   *time.Time     `json:"run_after,omitempty"` // delayed execution
    Timeout    time.Duration  `json:"timeout"`
}

type WorkerPool struct {
    streams    []string
    workers    int
    handlers   map[string]JobHandler
    dlq        string
}
```

### 5.3 Distributed Locks & Rate Limiting

```
moca:lock:{site}:{resource}              Distributed mutex (naming sequences, migrations)
moca:ratelimit:{site}:{user}:{endpoint}  Sliding window rate limiter
moca:ratelimit:{apikey}:{endpoint}       API key rate limiter
```

---

## 6. Kafka Event Streaming

Kafka is used for durable, cross-service event distribution. It is **not** in the hot path of regular CRUD — it runs asynchronously alongside Redis queues.

### 6.1 Topic Design

```
Topic                                    Purpose                    Partitions   Retention
──────────────────────────────────────────────────────────────────────────────────────────
moca.doc.events                          All document lifecycle      12           7 days
                                         events (partitioned by
                                         tenant+doctype)

moca.audit.log                           Immutable audit trail       6            90 days

moca.meta.changes                        MetaType schema changes     3            30 days
                                         (triggers cache flush,
                                         re-indexing)

moca.integration.outbox                  Outbound webhook/           6            3 days
                                         integration events

moca.workflow.transitions                Workflow state changes      6            30 days

moca.cdc.{tenant}.{doctype}             Change Data Capture for     varies       configurable
                                         specific doctypes
                                         (optional, per-config)

moca.notifications                       User notification events    6            3 days

moca.search.indexing                     Search re-indexing tasks    3            1 day
```

### 6.2 Event Schema

```go
package events

type DocumentEvent struct {
    // Envelope
    EventID     string    `json:"event_id"`      // UUID
    EventType   string    `json:"event_type"`     // "doc.created", "doc.submitted", etc.
    Timestamp   time.Time `json:"timestamp"`
    Source      string    `json:"source"`          // "moca-core"

    // Tenant
    Site        string    `json:"site"`

    // Payload
    DocType     string    `json:"doctype"`
    DocName     string    `json:"docname"`
    Action      string    `json:"action"`          // "insert", "update", "submit", etc.
    User        string    `json:"user"`
    Data        any       `json:"data,omitempty"`  // the document or diff
    PrevData    any       `json:"prev_data,omitempty"` // for updates
    RequestID   string    `json:"request_id"`      // correlation
}
```

### 6.3 Event Consumers

```
┌──────────────────────┐
│  moca.doc.events     │
└──────────┬───────────┘
           │
     ┌─────┼─────────────────────────────┐
     ▼     ▼                             ▼
┌─────────┐ ┌─────────────┐  ┌──────────────────┐
│ Webhook │ │ Search Index │  │  External ERP    │
│ Dispatch│ │ Updater      │  │  Sync Consumer   │
│ (moca)  │ │ (Meilisearch)│  │  (custom app)    │
└─────────┘ └─────────────┘  └──────────────────┘

┌──────────────────────┐
│  moca.meta.changes   │
└──────────┬───────────┘
           │
     ┌─────┼──────────────┐
     ▼     ▼              ▼
┌─────────┐ ┌──────────┐ ┌──────────────┐
│ Cache   │ │ API Route│ │ Search Schema│
│ Flusher │ │ Reloader │ │ Updater      │
└─────────┘ └──────────┘ └──────────────┘
```

### 6.4 Transactional Outbox Pattern

To guarantee that document saves and Kafka events are consistent, Moca uses the **transactional outbox** pattern:

```
1. BEGIN transaction
2. INSERT/UPDATE document in PostgreSQL
3. INSERT event into outbox table (same transaction)
4. COMMIT transaction

5. Background poller reads outbox table
6. Publishes events to Kafka
7. Marks outbox rows as published
```

```sql
CREATE TABLE tenant_acme.tab_outbox (
    id              BIGSERIAL PRIMARY KEY,
    event_type      TEXT NOT NULL,
    topic           TEXT NOT NULL,
    partition_key   TEXT NOT NULL,
    payload         JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at    TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'pending' -- pending, published, failed
);

CREATE INDEX idx_outbox_pending ON tenant_acme.tab_outbox(status) WHERE status = 'pending';
```

### 6.5 Kafka-Optional Architecture

Kafka can be disabled for small deployments via `moca.yaml` (`kafka.enabled: false`) or `moca init --no-kafka`. When Kafka is disabled, the system operates in **minimal event mode** using Redis pub/sub as a fallback:

| Feature | With Kafka | Without Kafka (Redis fallback) |
|---------|-----------|-------------------------------|
| Document lifecycle events (`moca.doc.events`) | Durable Kafka topic | Redis pub/sub (fire-and-forget, no persistence) |
| Audit log (`moca.audit.log`) | 90-day durable retention | Written directly to `tab_audit_log` table only (no streaming) |
| MetaType cache flush (`moca.meta.changes`) | Kafka broadcast | Redis pub/sub broadcast |
| Webhook dispatch (`moca.integration.outbox`) | Via outbox → Kafka pipeline | Via outbox → direct HTTP dispatch (synchronous poller) |
| Workflow transitions (`moca.workflow.transitions`) | Kafka topic | Redis pub/sub |
| CDC (`moca.cdc.*`) | **Available** | **Not available** — requires Kafka |
| Search sync (`moca.search.indexing`) | Kafka → Meilisearch consumer | Direct sync on document save (in-request or background job) |
| Notification events (`moca.notifications`) | Kafka topic | Redis pub/sub + direct delivery |

**Process changes when Kafka is disabled:**
- `moca-outbox` process: Still runs but publishes to Redis pub/sub channels and dispatches webhooks directly instead of writing to Kafka.
- `moca-search-sync` process: Not started. Search indexing happens synchronously on document save or via a Redis Streams background job.
- All other processes (`moca-server`, `moca-worker`, `moca-scheduler`) operate normally.

**Limitations of minimal mode:**
- No durable event replay (Redis pub/sub is fire-and-forget).
- No CDC for external consumers.
- Audit log retention depends solely on the database (no streaming analytics).
- Webhook delivery has weaker ordering guarantees.

---

## 7. App / Plugin System

### 7.1 App Manifest

```go
package apps

type AppManifest struct {
    // Identity
    Name         string       `json:"name"`          // "crm", "accounting"
    Title        string       `json:"title"`         // "Moca CRM"
    Version      string       `json:"version"`       // semver
    Publisher    string       `json:"publisher"`
    License      string       `json:"license"`
    Description  string       `json:"description"`

    // Dependencies
    MocaVersion  string       `json:"moca_version"`  // minimum framework version
    Dependencies []AppDep     `json:"dependencies"`   // other apps this depends on

    // Contents
    Modules      []ModuleDef  `json:"modules"`
    // Hooks are registered programmatically via hooks.go (see §7.3).
    // The manifest declares only which lifecycle events the app hooks into,
    // for dependency resolution and documentation. Registration uses RegisterHook() in init().
    Fixtures     []FixtureDef `json:"fixtures"`       // seed data
    Migrations   []Migration  `json:"migrations"`

    // Assets
    StaticAssets []AssetBundle `json:"static_assets"`  // JS, CSS for Desk
    PortalPages  []PortalPage `json:"portal_pages"`    // SSR page definitions
}

type AppDep struct {
    App        string `json:"app"`
    MinVersion string `json:"min_version"`
}

type ModuleDef struct {
    Name       string       `json:"name"`       // "Selling", "Buying"
    Label      string       `json:"label"`
    Icon       string       `json:"icon"`
    Color      string       `json:"color"`
    Category   string       `json:"category"`   // "Modules", "Settings", "Administration"
    Entities   []string     `json:"entities"`    // DocType names in this module
    Pages      []PageDef    `json:"pages"`       // custom pages
    Reports    []ReportDef  `json:"reports"`     // report definitions
    Dashboards []DashDef    `json:"dashboards"`  // dashboard definitions
}

type Migration struct {
    Version    string   `json:"version"`
    Up         string   `json:"up"`          // SQL or registered Go function
    Down       string   `json:"down"`
    DependsOn  []string `json:"depends_on"`  // other migrations that must run first
}
```

### 7.2 App Installation Lifecycle

```
┌────────────┐    ┌────────────┐    ┌────────────┐    ┌────────────┐
│  Download  │───▶│  Validate  │───▶│  Migrate   │───▶│  Register  │
│  Package   │    │  Manifest  │    │  Database   │    │  Hooks     │
└────────────┘    │  + Deps    │    │  Schema     │    │  + Routes  │
                  └────────────┘    └────────────┘    └────────────┘
                                                            │
                                                            ▼
                                                      ┌────────────┐
                                                      │  Seed      │
                                                      │  Fixtures  │
                                                      └────────────┘
```

### 7.3 Directory Structure of a Moca App

```
apps/
  crm/
    manifest.yaml              # AppManifest
    hooks.go                   # Hook registration code
    modules/
      selling/
        doctypes/
          sales_order/
            sales_order.json   # MetaType definition
            sales_order.go     # Controller (DocLifecycle implementation)
            sales_order_test.go
            sales_order_list.tsx  # Custom list view component (optional)
            sales_order_form.tsx  # Custom form component (optional)
        pages/
          sales_dashboard.tsx
          sales_analytics.tsx
        reports/
          sales_by_customer.json  # Report definition
          sales_by_customer.go    # Report data source
      buying/
        doctypes/
          purchase_order/
            purchase_order.json
            purchase_order.go
    templates/
      portal/
        order_status.html      # SSR portal page
        order_status.go        # get_context equivalent
    fixtures/
      selling_settings.json    # Default settings data
    migrations/
      001_initial.sql
      002_add_tax_tables.sql
    public/
      js/
        crm_custom.js
      css/
        crm.css
    tests/
      setup_test.go            # Test helpers and fixtures
    go.mod                     # App Go module (composed via go.work)
    go.sum
```

**Build Composition Model:** Each app is a separate Go module with its own `go.mod`. The project root contains a `go.work` file (Go workspaces) that includes the framework and all installed apps. `moca app get` and `moca app install` update `go.work` to include the new app module. `moca serve` and `moca build server` compile all apps into the single `moca-server` binary via the workspace. `moca build app APP_NAME` verifies that a single app compiles cleanly within the workspace context but does not produce a standalone binary.

---

## 8. Multitenancy — Site Architecture

### 8.1 Tenant Resolution

```go
package tenancy

type SiteResolver struct {
    cache    *redis.Client
    db       *sql.DB         // system DB connection
    strategy ResolutionStrategy
}

type ResolutionStrategy int

const (
    ResolveBySubdomain ResolutionStrategy = iota  // acme.moca.cloud
    ResolveByHeader                                // X-Moca-Site: acme
    ResolveByPath                                  // /sites/acme/api/...
)

type SiteContext struct {
    Name           string
    DBSchema       string
    Config         SiteConfig
    InstalledApps  []string
    DBPool         *pgxpool.Pool    // per-site connection pool
    RedisPrefix    string
    StorageBucket  string
}

// Middleware: every request is wrapped with a SiteContext
func (r *SiteResolver) Middleware() func(http.Handler) http.Handler
```

### 8.2 Per-Site Isolation

| Resource | Isolation Method |
|----------|-----------------|
| Database | PostgreSQL schema per tenant (with RLS as defense-in-depth) |
| Cache | Redis key prefix: `{site_name}:` |
| Queue | Redis stream prefix: `moca:queue:{site}:` |
| Files | S3 bucket prefix: `{site_name}/` |
| Search | Meilisearch index prefix: `{site_name}_` |
| Kafka | Partition key includes site name |
| Config | Per-site config in system DB |

### 8.3 Site Lifecycle

```
create-site acme.moca.cloud
    │
    ├── 1. Create PostgreSQL schema "tenant_acme"
    ├── 2. Create system tables (tab_singles, tab_version, etc.)
    ├── 3. Run core framework migrations
    ├── 4. Create Administrator user
    ├── 5. Create Redis key namespace
    ├── 6. Create S3 storage bucket/prefix
    ├── 7. Create Meilisearch index
    ├── 8. Register site in moca_system.sites
    ├── 9. Warm metadata cache
    │
    ▼
install-app crm --site acme.moca.cloud
    │
    ├── 1. Resolve app dependencies
    ├── 2. Run app migrations for this site
    ├── 3. Register app hooks in site context
    ├── 4. Create MetaType tables
    ├── 5. Seed fixture data
    ├── 6. Update moca_system.site_apps
    │
    ▼
    Site ready for use
```

---

## 9. React Frontend Architecture

### 9.1 Overview

The React frontend (Moca Desk) is a **metadata-driven application shell**. It does not contain hardcoded views for each DocType. Instead, it fetches MetaType definitions from the backend and renders forms, lists, and dashboards dynamically.

```
┌─────────────────────────────────────────────────────────────────┐
│  Moca Desk (React 19 + TypeScript)                              │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  App Shell                                                │   │
│  │  ┌────────┐ ┌────────────┐ ┌────────────┐ ┌───────────┐ │   │
│  │  │ Sidebar│ │ Breadcrumbs│ │ Search Bar │ │ User Menu │ │   │
│  │  │ (Module│ │            │ │ (Cmd+K)    │ │           │ │   │
│  │  │  Nav)  │ │            │ │            │ │           │ │   │
│  │  └────────┘ └────────────┘ └────────────┘ └───────────┘ │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Route-Based Content Area                                 │   │
│  │                                                           │   │
│  │  /app/{doctype}         → <ListView meta={...} />         │   │
│  │  /app/{doctype}/{name}  → <FormView meta={...} doc={...}/>│   │
│  │  /app/{module}/page/{p} → <CustomPage />                  │   │
│  │  /app/dashboard/{name}  → <Dashboard config={...} />      │   │
│  │  /app/report/{name}     → <ReportView config={...} />     │   │
│  │                                                           │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Core UI Infrastructure                                   │   │
│  │                                                           │   │
│  │  MetaProvider     → fetches & caches MetaType definitions │   │
│  │  DocProvider      → fetches, caches, manages document     │   │
│  │  AuthProvider     → user session, permissions             │   │
│  │  WebSocketProvider→ real-time updates                     │   │
│  │  ThemeProvider    → light/dark, custom themes             │   │
│  │  I18nProvider     → internationalization                  │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### 9.2 Metadata-Driven Rendering

```tsx
// Simplified: The FormView renders entirely from metadata
function FormView({ doctype, name }: Props) {
  const meta = useMetaType(doctype);       // fetched from /api/meta/{doctype}
  const doc = useDocument(doctype, name);   // fetched from /api/resource/{doctype}/{name}
  const perms = usePermissions(doctype);

  return (
    <FormLayout meta={meta}>
      {meta.fields.map(field => (
        <FieldRenderer
          key={field.name}
          field={field}
          value={doc.get(field.name)}
          onChange={(v) => doc.set(field.name, v)}
          readOnly={!perms.canWrite || field.read_only}
          hidden={evaluateExpression(field.depends_on, doc)}
        />
      ))}
    </FormLayout>
  );
}

// FieldRenderer picks the right component based on field_type
function FieldRenderer({ field, value, onChange, ...props }: FieldProps) {
  const Component = FIELD_TYPE_MAP[field.field_type];
  return <Component field={field} value={value} onChange={onChange} {...props} />;
}

const FIELD_TYPE_MAP: Record<FieldType, React.ComponentType> = {
  'Data':        DataField,
  'Text':        TextField,
  'Int':         IntField,
  'Float':       FloatField,
  'Currency':    CurrencyField,
  'Date':        DateField,
  'Datetime':    DatetimeField,
  'Select':      SelectField,
  'Link':        LinkField,          // autocomplete + link to another doctype
  'Table':       ChildTableField,    // inline editable table
  'Check':       CheckboxField,
  'Attach':      FileAttachField,
  'AttachImage': ImageAttachField,
  'Code':        CodeEditorField,
  'Markdown':    MarkdownField,
  'JSON':        JSONEditorField,
  'Color':       ColorPickerField,
  'Rating':      RatingField,
  'Geolocation': MapField,
  'Signature':   SignatureField,
  'HTMLEditor':  RichTextEditorField,
  // Layout types
  'SectionBreak': SectionBreak,
  'ColumnBreak':  ColumnBreak,
  'TabBreak':     TabBreak,
  'Button':       ActionButton,
};
```

### 9.3 Custom Field Type Registry (Extensible)

Apps can register custom field types that the Desk will render:

```tsx
// In an app's Desk extension:
import { registerFieldType } from '@osama1998h/desk';

registerFieldType('TreeSelect', TreeSelectField);
registerFieldType('KanbanStatus', KanbanStatusField);
```

### 9.4 Real-Time Updates via WebSocket

```
Client                    Server                     Redis Pub/Sub
  │                         │                            │
  │  WS Connect             │                            │
  │ ──────────────────────▶ │                            │
  │                         │  SUBSCRIBE (on db_pubsub)  │
  │                         │  pubsub:doc:{site}:{doctype}:*│
  │                         │ ──────────────────────────▶│
  │                         │                            │
  │                         │      (another user saves)  │
  │                         │ ◀──────────────────────────│
  │  { type: "doc_update",  │                            │
  │    doctype, name, data }│                            │
  │ ◀────────────────────── │                            │
  │                         │                            │
  │  (React state updates)  │                            │
```

### 9.5 Portal / SSR Layer

For public-facing pages (customer portals, websites), Moca uses **server-side rendering** via Go templates + optional React hydration:

```go
package portal

type PortalPage struct {
    Route       string            `json:"route"`       // "/orders/{name}"
    Template    string            `json:"template"`     // "order_status.html"
    Controller  string            `json:"controller"`   // registered Go function
    AuthRequired bool             `json:"auth_required"`
    Roles       []string          `json:"roles,omitempty"`
}

// Controller signature
type PortalController func(ctx *PortalContext) (map[string]any, error)
```

### 9.6 Translation Architecture

Moca supports internationalization (i18n) for both the Desk UI and server-rendered content.

**Translation storage:** Translations are stored in a per-tenant table:

```sql
CREATE TABLE tenant_acme.tab_translation (
    source_text     TEXT NOT NULL,
    language        TEXT NOT NULL,          -- "ar", "fr", "de"
    translated_text TEXT NOT NULL,
    context         TEXT,                   -- "DocType:SalesOrder", "label", "option"
    app             TEXT,                   -- originating app
    PRIMARY KEY (source_text, language, context)
);
```

**String extraction:** Translatable strings are extracted from:
- MetaType definitions: field labels, description, select options, section headings.
- Portal templates: `{{ _("text") }}` markers.
- Desk UI: `t("text")` calls in `.tsx` files.

The CLI command `moca translate export` extracts all translatable strings from these sources.

**Backend `Accept-Language` flow:**
1. The `I18nProvider` middleware reads the `Accept-Language` header (or user preference from session).
2. The `Localizer` transformer (§3.3.5) translates MetaType labels, select options, and error messages in API responses.
3. Translations are cached in Redis as `i18n:{site}:{lang}` with invalidation on `moca translate import/compile`.

**Compiled translations:** `moca translate compile` produces binary `.mo` files in `sites/{site}/translations/{lang}.mo` for fast runtime lookup. The framework loads these at startup and on cache invalidation.

---

## 10. Query Engine

### 10.1 Dynamic Query Builder

```go
package query

type QueryBuilder struct {
    site       *tenancy.SiteContext
    doctype    string
    meta       *meta.MetaType
    fields     []string
    filters    []Filter
    orderBy    []OrderClause
    groupBy    []string
    limit      int
    offset     int
    joins      []JoinClause    // auto-generated from Link fields
}

type Filter struct {
    Field    string      `json:"field"`
    Operator Operator    `json:"operator"`
    Value    any         `json:"value"`
}

type Operator string

const (
    OpEquals       Operator = "="
    OpNotEquals    Operator = "!="
    OpGreaterThan  Operator = ">"
    OpLessThan     Operator = "<"
    OpGTE          Operator = ">="
    OpLTE          Operator = "<="
    OpLike         Operator = "like"
    OpNotLike      Operator = "not like"
    OpIn           Operator = "in"
    OpNotIn        Operator = "not in"
    OpBetween      Operator = "between"
    OpIsNull       Operator = "is"
    OpIsNotNull    Operator = "is not"
    // NEW — not in Frappe
    OpContains     Operator = "@>"      // JSONB contains
    OpFullText     Operator = "@@"      // PostgreSQL full-text search
    OpSimilar      Operator = "similar" // trigram similarity
)

// The query builder understands:
// - Regular columns vs _extra JSONB fields
// - Link field auto-joins
// - Permission filters (row-level security)
// - Child table sub-queries
// - Aggregation queries for reports
```

### 10.2 Report Builder

```go
type ReportDef struct {
    Name          string            `json:"name"`
    DocType       string            `json:"doc_type"`       // primary doctype
    Type          string            `json:"type"`           // "QueryReport", "ScriptReport"
    Columns       []ReportColumn    `json:"columns"`
    Filters       []ReportFilter    `json:"filters"`        // user-configurable
    Query         string            `json:"query,omitempty"`// SQL for QueryReport
    DataSource    string            `json:"data_source"`    // Go func for ScriptReport
    ChartConfig   *ChartConfig      `json:"chart_config,omitempty"`
    IsCacheable   bool              `json:"is_cacheable"`
    CacheTTL      time.Duration     `json:"cache_ttl"`
}
```

---

## 11. Observability & Operations

### 11.1 Metrics (Prometheus)

```
moca_http_requests_total{site, method, path, status}
moca_http_request_duration_seconds{site, method, path}
moca_document_operations_total{site, doctype, operation}
moca_document_operation_duration_seconds{site, doctype, operation}
moca_cache_hits_total{site, cache_type}
moca_cache_misses_total{site, cache_type}
moca_queue_jobs_total{site, queue, status}
moca_queue_job_duration_seconds{site, queue, job_type}
moca_kafka_events_published_total{topic}
moca_kafka_consumer_lag{topic, consumer_group}
moca_active_websocket_connections{site}
moca_db_query_duration_seconds{site, operation}
moca_db_pool_active_connections{site}
```

### 11.2 Structured Logging

```go
// Every log entry includes tenant context
logger.Info("document saved",
    "site", ctx.Site.Name,
    "doctype", doc.Meta().Name,
    "docname", doc.Name(),
    "user", ctx.User.Email,
    "request_id", ctx.RequestID(),
    "duration_ms", elapsed.Milliseconds(),
)
```

### 11.3 Health Checks

```
GET /health          → { status: "ok", version: "1.0.0" }
GET /health/ready    → checks PostgreSQL, Redis, Kafka, Meilisearch
GET /health/live     → lightweight liveness probe
```

---

## 12. Deployment Architecture

### 12.1 Single-Instance (Development / Small Scale)

```
┌──────────────────────────────────────┐
│  Single Binary: moca-server          │
│                                      │
│  HTTP Server + WebSocket             │
│  Background Workers (in-process)     │
│  Scheduler (in-process)              │
│  Outbox Poller (in-process)          │
└──────────┬───────────────────────────┘
           │
    ┌──────┼──────┐
    ▼      ▼      ▼
  PG    Redis   MinIO
 (single instances, Kafka optional)
```

### 12.2 Production (Horizontally Scaled)

```
                    ┌──────────────┐
                    │  Load        │
                    │  Balancer    │
                    │  (Caddy)     │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Moca API │ │ Moca API │ │ Moca API │   (stateless, N replicas)
        │ Server 1 │ │ Server 2 │ │ Server 3 │
        └──────────┘ └──────────┘ └──────────┘
              │            │            │
              └────────────┼────────────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │  Worker  │ │  Worker  │ │  Worker  │   (job consumers, N replicas)
        │  Pool 1  │ │  Pool 2  │ │  Pool 3  │
        └──────────┘ └──────────┘ └──────────┘
              │            │            │
              └────────────┼────────────┘
                           │
    ┌──────────┬───────────┼───────────┬──────────┐
    ▼          ▼           ▼           ▼          ▼
┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐
│ PG     │ │ Redis  │ │ Kafka  │ │ Meili  │ │ MinIO  │
│ Cluster│ │ Cluster│ │ Cluster│ │ Cluster│ │ Cluster│
│ (HA)   │ │ (HA)   │ │ (3+)   │ │        │ │        │
└────────┘ └────────┘ └────────┘ └────────┘ └────────┘
```

### 12.3 Process Types

| Process | Role | Scaling |
|---------|------|---------|
| `moca-server` | HTTP API + WebSocket | Horizontal, behind LB |
| `moca-worker` | Redis queue consumer | Horizontal, by queue pressure |
| `moca-scheduler` | Cron job trigger | Single leader (Redis lock) |
| `moca-outbox` | Outbox → Kafka publisher | Single leader (Redis lock) |
| `moca-search-sync` | Kafka → Meilisearch | Horizontal, by topic partition |

---

## 13. Security Architecture

### 13.1 Authentication Methods

| Method | Use Case |
|--------|----------|
| **Session Cookie** (HttpOnly, Secure) | Desk UI, Portal |
| **JWT Bearer Token** | Mobile apps, SPAs |
| **API Key + Secret** | Server-to-server integration |
| **OAuth2** | Third-party app authorization |
| **SAML / OIDC** | Enterprise SSO |

### 13.2 API Key System (New)

```go
type APIKey struct {
    KeyID        string          `json:"key_id"`
    SecretHash   string          `json:"-"`            // bcrypt hash, never exposed
    Label        string          `json:"label"`
    User         string          `json:"user"`          // associated user
    Scopes       []APIScopePerm  `json:"scopes"`        // what this key can access
    RateLimit    *RateLimitConfig `json:"rate_limit"`
    IPAllowlist  []string        `json:"ip_allowlist"`
    ExpiresAt    *time.Time      `json:"expires_at"`
    LastUsedAt   *time.Time      `json:"last_used_at"`
    IsActive     bool            `json:"is_active"`
}
```

### 13.3 Defense in Depth

```
Layer 1: Network     → TLS everywhere, IP allowlists for admin
Layer 2: Edge        → Rate limiting, request size limits, CORS
Layer 3: Auth        → Session/JWT/API Key validation
Layer 4: App         → Permission engine (role + field + row level)
Layer 5: Database    → PostgreSQL RLS policies per tenant schema
Layer 6: Audit       → Immutable audit log for all mutations
Layer 7: Encryption  → Sensitive fields encrypted at rest (AES-256-GCM)
```

---

## 14. Complete Request Lifecycle

Here is the full journey of a `POST /api/v1/resource/SalesOrder` request:

```
1.  TCP connection hits load balancer
2.  Routed to a moca-server instance
3.  TENANT RESOLUTION: subdomain → SiteContext (from Redis cache or system DB)
4.  AUTH: JWT decoded → user loaded → session validated
5.  RATE LIMIT: check sliding window in Redis
6.  API VERSION: route matched to v1 handler
7.  REQUEST TRANSFORM: v1 field mapping applied (e.g., "customer" → "customer_name")
8.  PERMISSION CHECK: user has "create" on SalesOrder?
9.  DOCUMENT CREATION:
    a. MetaType loaded from Redis cache (or DB → cache)
    b. DynamicDoc created with submitted values
    c. Naming engine generates document name (SO-0042)
    d. LIFECYCLE: BeforeInsert → BeforeValidate → Validate → BeforeSave
    e. Field-level validation (required, type, regex, custom)
    f. Permission filter (field-level write check)
    g. BEGIN PostgreSQL transaction
    h. INSERT into tab_sales_order
    i. INSERT child rows into tab_sales_order_item
    j. INSERT event into tab_outbox
    k. COMMIT transaction
    l. LIFECYCLE: AfterInsert → OnChange
    m. Document cached in Redis
10. HOOK EXECUTION: registered doc_events["SalesOrder"]["after_insert"] handlers
11. RESPONSE TRANSFORM: v1 field mapping applied, excluded fields removed
12. AUDIT LOG: async write to audit table
13. RESPONSE: 201 Created with transformed document JSON
14. ASYNC (next tick):
    a. Outbox poller publishes to moca.doc.events
    b. Search indexer updates Meilisearch
    c. WebSocket broadcast to connected clients viewing this doctype
    d. Webhook dispatcher fires configured webhooks
```

---

## 15. Framework Package Layout

```
moca/
├── cmd/
│   ├── moca-server/         # HTTP API + WebSocket server
│   ├── moca-worker/         # Background job consumer
│   ├── moca-scheduler/      # Cron scheduler
│   ├── moca/                # CLI tool (create-site, install-app, bench, migrate)
│   └── moca-outbox/         # Outbox → Kafka publisher
│
├── pkg/
│   ├── meta/                # MetaType, FieldDef, ViewMeta, NamingStrategy
│   │   ├── metatype.go
│   │   ├── fielddef.go
│   │   ├── registry.go      # in-memory + Redis-backed metadata registry
│   │   ├── compiler.go      # validates and compiles raw JSON → MetaType
│   │   └── migrator.go      # schema diff → ALTER TABLE statements
│   │
│   ├── document/            # Document runtime engine
│   │   ├── document.go      # Document interface + DynamicDoc
│   │   ├── lifecycle.go     # lifecycle event dispatcher
│   │   ├── naming.go        # naming engine
│   │   ├── validator.go     # field-level validation
│   │   ├── virtual.go       # VirtualDoc + VirtualSource
│   │   └── controller.go    # controller resolution + composition
│   │
│   ├── api/                 # Customizable API layer
│   │   ├── gateway.go       # main router + middleware chain
│   │   ├── rest.go          # auto-generated REST endpoints
│   │   ├── graphql.go       # auto-generated GraphQL schema
│   │   ├── version.go       # API versioning engine
│   │   ├── transformer.go   # request/response transformation pipeline
│   │   ├── ratelimit.go     # per-user, per-key, per-tenant rate limiting
│   │   ├── apikey.go        # API key management
│   │   └── webhook.go       # outbound webhook dispatcher
│   │
│   ├── orm/                 # Database adapter layer
│   │   ├── postgres.go      # PostgreSQL connection + pool management
│   │   ├── query.go         # dynamic query builder
│   │   ├── transaction.go   # transaction manager
│   │   ├── schema.go        # DDL generation from MetaType
│   │   └── migrate.go       # migration runner
│   │
│   ├── auth/                # Authentication + authorization
│   │   ├── session.go       # session management
│   │   ├── jwt.go           # JWT issuing + validation
│   │   ├── oauth2.go        # OAuth2 provider + consumer
│   │   ├── sso.go           # SAML / OIDC integration
│   │   └── permission.go    # permission resolution engine
│   │
│   ├── hooks/               # Extension registry
│   │   ├── registry.go      # HookRegistry + priority resolution
│   │   ├── docevents.go     # document event hook dispatcher
│   │   └── middleware.go    # API middleware hook system
│   │
│   ├── workflow/            # Workflow engine
│   │   ├── engine.go        # state machine + transition logic
│   │   ├── sla.go           # SLA timer management
│   │   └── approval.go      # approval chain logic
│   │
│   ├── tenancy/             # Multitenancy
│   │   ├── resolver.go      # site resolution middleware
│   │   ├── context.go       # SiteContext
│   │   ├── manager.go       # create/delete/migrate sites
│   │   └── config.go        # per-site configuration
│   │
│   ├── queue/               # Background job system
│   │   ├── producer.go      # enqueue jobs to Redis Streams
│   │   ├── consumer.go      # worker pool + consumer groups
│   │   ├── scheduler.go     # cron-based job scheduling
│   │   └── deadletter.go    # DLQ handling + retry logic
│   │
│   ├── events/              # Kafka event system
│   │   ├── producer.go      # publish to Kafka topics
│   │   ├── consumer.go      # consume from Kafka topics
│   │   ├── outbox.go        # transactional outbox poller
│   │   └── schema.go        # event schema definitions
│   │
│   ├── search/              # Full-text search
│   │   ├── indexer.go       # Meilisearch index management
│   │   ├── sync.go          # Kafka → Meilisearch sync
│   │   └── query.go         # search query API
│   │
│   ├── storage/             # File storage
│   │   ├── s3.go            # S3/MinIO adapter
│   │   ├── manager.go       # file upload/download + access control
│   │   └── thumbnail.go     # image processing
│   │
│   ├── ui/                  # UI serving layer
│   │   ├── desk.go          # serve React Desk SPA + assets
│   │   ├── portal.go        # SSR portal page renderer
│   │   ├── websocket.go     # WebSocket hub for real-time
│   │   └── assets.go        # static asset serving + bundling
│   │
│   ├── notify/              # Notification system
│   │   ├── email.go         # email sending (SMTP / SES)
│   │   ├── push.go          # push notifications
│   │   ├── inapp.go         # in-app notification store
│   │   └── sms.go           # SMS gateway
│   │
│   └── observe/             # Observability
│       ├── metrics.go       # Prometheus metrics
│       ├── tracing.go       # OpenTelemetry tracing
│       ├── logging.go       # structured logging
│       └── health.go        # health check endpoints
│
├── apps/                    # Built-in and community apps
│   ├── core/                # Core framework doctypes (User, Role, DocType, etc.)
│   └── ...
│
├── desk/                    # React frontend source
│   ├── src/
│   │   ├── App.tsx
│   │   ├── providers/       # MetaProvider, DocProvider, AuthProvider, etc.
│   │   ├── components/
│   │   │   ├── fields/      # One component per FieldType
│   │   │   ├── form/        # FormView, FormLayout, FormToolbar
│   │   │   ├── list/        # ListView, ListFilters, ListPagination
│   │   │   ├── dashboard/   # Dashboard, DashboardWidget
│   │   │   ├── report/      # ReportView, ReportFilters, Charts
│   │   │   └── common/      # Sidebar, Breadcrumbs, SearchBar, etc.
│   │   ├── hooks/           # useMetaType, useDocument, usePermissions, etc.
│   │   ├── api/             # API client layer (REST + WebSocket)
│   │   └── utils/           # expression evaluator, formatters, etc.
│   ├── package.json
│   └── vite.config.ts
│
├── portal/                  # Portal templates
│   ├── templates/
│   └── static/
│
└── go.work                  # Go workspace: composes framework + installed apps
```

---

## 16. ADR Summary — Key Architecture Decisions

### ADR-001: PostgreSQL Schema-Per-Tenant over Database-Per-Tenant

**Decision:** Use PostgreSQL schemas for tenant isolation with RLS as defense-in-depth.
**Rationale:** Schema-per-tenant allows connection pooling across tenants, simpler operational management, and cross-tenant reporting when needed. Full database isolation is available as an opt-in for enterprise tenants.
**Trade-off:** Slightly weaker isolation than separate databases; mitigated by RLS policies.

### ADR-002: Redis Streams over Dedicated Queue (RabbitMQ/SQS)

**Decision:** Use Redis Streams for background job queuing.
**Rationale:** Redis is already required for caching and sessions. Adding Streams avoids another infrastructure dependency. Redis Streams provide consumer groups, acknowledgment, and dead-letter capabilities sufficient for our workload.
**Trade-off:** Less feature-rich than RabbitMQ (no topic exchanges, delayed queues are manual). Acceptable given Kafka handles the event streaming use case.

### ADR-003: Kafka for Event Streaming, Not for Job Queuing

**Decision:** Kafka handles durable event distribution (audit, CDC, integrations, webhook triggers). Redis handles transient job queues.
**Rationale:** Kafka's strengths are durability, replay, and fan-out to multiple consumers. Job queues need low-latency dequeue and ack, which Redis Streams handles better.
**Trade-off:** Two messaging systems to operate. Justified by the clear separation of concerns.

### ADR-004: Transactional Outbox over Dual-Write

**Decision:** Use the transactional outbox pattern for Kafka event publishing.
**Rationale:** Dual-write (save to DB + publish to Kafka) risks inconsistency if either fails. The outbox table participates in the same DB transaction, guaranteeing at-least-once delivery.
**Trade-off:** Slight increase in event latency (poll interval). Mitigated by setting the poll interval to 100ms.

### ADR-005: JSONB _extra Column for Dynamic Fields

**Decision:** Every document table includes a `_extra JSONB` column for fields that don't have dedicated columns.
**Rationale:** Allows runtime field addition without DDL. Custom Fields added by apps or users go into `_extra` first. When a field is stable and performance-critical, it can be promoted to a real column via migration.
**Trade-off:** JSONB fields are slightly slower to query and can't have traditional indexes (GIN indexes are available but heavier).

### ADR-006: Meilisearch over Elasticsearch

**Decision:** Use Meilisearch for full-text document search.
**Rationale:** Meilisearch is significantly simpler to operate, has excellent typo tolerance out of the box, and handles the search volumes of a business application framework without the operational complexity of Elasticsearch. If a tenant needs analytical search at scale, they can configure an Elasticsearch integration via the hook system.
**Trade-off:** Less powerful aggregation and analytics capabilities than Elasticsearch.

### ADR-007: Decoupled React Frontend over Server-Rendered Desk

**Decision:** The Desk UI is a standalone React SPA that consumes backend metadata APIs, replacing Frappe's server-rendered + client-patched Desk.
**Rationale:** Clean separation of concerns. The React frontend can be developed, tested, and deployed independently. The metadata API becomes the contract, enabling mobile apps and third-party UIs. Server-side rendering is preserved for the Portal layer only.
**Trade-off:** Requires maintaining a metadata API contract. Initial development cost is higher, but long-term maintainability is significantly better.

---

## 17. Scalability Targets

| Dimension | Target (per instance) | Horizontal Strategy |
|-----------|----------------------|---------------------|
| Concurrent HTTP connections | 10,000 | Add more moca-server replicas |
| Document writes/sec | 5,000 | Shard by tenant across DB schemas |
| Background jobs/sec | 2,000 | Add more moca-worker replicas |
| WebSocket connections | 50,000 | Sticky sessions + Redis pub/sub fan-out |
| Kafka throughput | 100,000 events/sec | Increase partitions + consumers |
| Tenants per cluster | 10,000 | Schema-per-tenant; large tenants get dedicated DB |
| Search index size per tenant | 10M documents | Meilisearch sharding by tenant prefix |

---

## 17.1 Terminology Glossary

To avoid ambiguity, the following terms have precise meanings throughout Moca documentation:

| Term | Definition |
|------|-----------|
| **App** | A Moca application package — has `manifest.yaml`, modules, DocTypes, hooks, and its own `go.mod`. |
| **Module** | A logical grouping of DocTypes within an App (e.g., "Selling", "Buying"). |
| **Desk Extension** | App-provided React components that extend the Desk UI (custom field types, views, dashboard widgets). Formerly "desk plugin." |
| **CLI Extension** | App-registered custom CLI commands via `cli.RegisterCommand()` in `hooks.go`, discovered at build time. |
| **Hook** | A programmatic Go function registered in `hooks.go` that intercepts document lifecycle events or API middleware. |
| **Plugin** (reserved) | Future WASM-based sandboxed extension model for untrusted third-party code. Not currently implemented. |

### 17.2 Frontend Desk Composition Model

The Desk UI is composed from three layers:

1. **Framework Desk** (`moca/desk/`): The base React application — core providers, components, and views. Published as the `@osama1998h/desk` npm package to GitHub Packages (`npm.pkg.github.com`). Projects consume it via `package.json`. See [ADR-007](docs/ADR-007-desk-distribution-and-extensibility.md) for the full distribution model.
2. **App Desk Extensions** (declared in `apps/*/desk/desk-manifest.json`): Apps declare desk extensions — field types, pages, sidebar items, and dashboard widgets. Registration uses `registerFieldType()`, `registerPage()`, `registerSidebarItem()`, and `registerDashboardWidget()` APIs.
3. **Project Desk** (`my-project/desk/`): Project-level overrides for theming, custom routes, or site-specific components in `desk/src/overrides/`.

**Build process:** `moca build desk` compiles all three layers: framework desk is the base; app extensions are discovered by scanning `apps/*/desk/desk-manifest.json` for structured declarations, with a legacy fallback to `desk/setup.ts`; the generated `.moca-extensions.ts` file contains typed imports and registration calls; project desk overrides are applied last. If two apps register a component for the same DocType view, the app with higher priority in `moca.yaml` wins.

---

## 18. What to Revisit as the System Grows

1. **Event sourcing for specific DocTypes** — Some high-value document types (financial transactions, audit-sensitive records) may benefit from full event sourcing rather than just audit logging. This should be opt-in per MetaType.

2. **gRPC for internal service communication** — If Moca is decomposed into microservices, gRPC between internal services would be more efficient than REST. The current design keeps this option open.

3. **Multi-region deployment** — The current schema-per-tenant model assumes a single PostgreSQL cluster. For geo-distributed deployments, investigate CockroachDB or Citus for distributed PostgreSQL.

4. **Plugin sandboxing** — Currently, app code runs in the same process. For a marketplace model with untrusted plugins, consider WASM-based sandboxing for hook execution.

5. **AI/ML integration** — A dedicated MetaType field type for embeddings, vector search via pgvector, and hook-based ML pipeline triggers.

---

*This document defines the architecture for Moca v1.0. All decisions should be revisited as real-world usage patterns emerge.*
