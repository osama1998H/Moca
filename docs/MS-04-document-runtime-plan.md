# MS-04 - Document Runtime Plan

## Milestone Summary
- **ID:** MS-04
- **Name:** Document Runtime -- DynamicDoc, Lifecycle Engine, Naming, Validation
- **Roadmap Reference:** `ROADMAP.md` lines 339-377
- **Goal:** Implement the Document interface, DynamicDoc (map-backed), 18-event lifecycle engine, naming engine (6 strategies), field-level validation, DocContext, CRUD operations, and controller resolution.
- **Why it matters:** The Document is the core runtime abstraction users interact with. Every downstream milestone (API layer, query engine, hook system, search, workflows) depends on a working document runtime.
- **Position in roadmap:** Order #5 on the Backend Core critical path (MS-00 → MS-01 → MS-02 → MS-03 → **MS-04** → MS-05/MS-06)
- **Upstream dependencies:** MS-03 (Metadata Registry -- complete)
- **Downstream dependencies:** MS-05 (Query Engine), MS-06 (REST API), MS-08 (Hook Registry), MS-15 (Jobs/Events/Search), MS-23 (Workflow Engine)

---

## Vision Alignment

MS-04 is the central runtime engine of MOCA. The framework's core promise is that a single MetaType definition drives everything -- and the Document Runtime is where that MetaType definition becomes a living, lifecycle-managed record. Without this milestone, there are no CRUD operations, no lifecycle hooks, no naming, and no validation. It unlocks the entire API layer (MS-06), query engine (MS-05), and hook system (MS-08) that together form the MOCA MVP.

---

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `MOCA_SYSTEM_DESIGN.md` | §3.2.1 Document Interface | 300-322 | Document interface (10 methods), DynamicDoc struct fields |
| `MOCA_SYSTEM_DESIGN.md` | §3.2.2 DynamicDoc | 324-348 | Map-backed implementation, dirty tracking, child table storage |
| `MOCA_SYSTEM_DESIGN.md` | §3.2.3 Lifecycle Engine | 351-427 | 18 lifecycle events, state machine, event ordering per operation |
| `MOCA_SYSTEM_DESIGN.md` | §3.2.4 DocContext | 429-437 | DocContext struct: Site, User, Flags, TX, EventBus |
| `MOCA_SYSTEM_DESIGN.md` | §3.2.5 Naming Engine | 440-462 | 6 naming strategies, PG sequences for pattern naming |
| `MOCA_SYSTEM_DESIGN.md` | §3.1.2 FieldDef | 186-226 | Validation fields: Required, Unique, MaxLength, MinValue, MaxValue, ValidationRegex |
| `MOCA_SYSTEM_DESIGN.md` | §3.5 Hook Registry | 714-781 | 14 DocEvent constants, PrioritizedHandler, TypeOverrides/TypeExtensions |
| `MOCA_SYSTEM_DESIGN.md` | §End-to-end walkthrough | 1895-1919 | Full Insert flow: MetaType load → naming → lifecycle → TX → outbox → cache |
| `MOCA_SYSTEM_DESIGN.md` | §6.3 Outbox | 1205-1229 | tab_outbox schema, transactional outbox pattern |
| `ROADMAP.md` | MS-04 | 339-377 | Scope, deliverables, acceptance criteria, risks |
| `ROADMAP.md` | MS-05 | 380-418 | GetList contract (hardcoded SQL in MS-04, replaced by query builder in MS-05) |
| `ROADMAP.md` | MS-06 | 421-444 | REST CRUD mapping to Document operations |
| `ROADMAP.md` | MS-08 | 513-551 | HookRegistry integration, controller resolution, TypeOverrides/TypeExtensions |

---

## Research Notes

No web research was needed. All implementation details are sufficiently specified in the design documents and existing codebase.

Key findings from codebase inspection:

- **Transaction type is `pgx.Tx`** (not `*sql.Tx`), confirmed in `pkg/orm/transaction.go`. `orm.WithTransaction` and `orm.TxFromContext` are the transaction primitives to use.
- **`tab_outbox` does not yet exist** in `GenerateSystemTablesDDL()` (`pkg/meta/ddl.go`). MS-04 must add it. The other 5 system tables (`tab_doctype`, `tab_singles`, `tab_version`, `tab_audit_log`, plus the audit default partition) are already defined.
- **Stub packages** (`pkg/tenancy/doc.go`, `pkg/auth/doc.go`, `pkg/events/doc.go`) contain only package-level comments. MS-04 must add minimal concrete types (`SiteContext`, `User`, `Emitter`) to enable compilation.
- **`pkg/document/` exists** with only a `doc.go` package comment. All MS-04 code goes here.
- **Testing convention** is pure stdlib `testing` (no testify), `//go:build integration` for integration tests, Docker Compose PostgreSQL on port 5433, Redis on port 6379. Follow the `pkg/meta/` test patterns exactly.
- **`meta.TableName()`**, **`meta.StandardColumns()`**, and **`meta.ChildStandardColumns()`** are available and must be used for table name resolution and column layout.
- **`meta.NamingRule` constants** (NamingAutoIncrement, NamingByPattern, NamingByField, NamingByHash, NamingUUID, NamingCustom) and **`meta.NamingStrategy`** struct are fully defined in `pkg/meta/metatype.go`.

---

## Milestone Plan

### Task 1: Foundation Types -- Document, DynamicDoc, DocContext

- **Task ID:** MS-04-T1
- **Status:** Completed
- **Title:** Foundation Types -- Document interface, DynamicDoc, DocContext, and stub types
- **Description:**
  Implement the `Document` interface (10 methods), `DynamicDoc` struct (map-backed with dirty tracking and child table support), `DocContext` struct, and minimal concrete types in `pkg/tenancy`, `pkg/auth`, and `pkg/events` required for compilation.

  Files to create:
  - `pkg/document/document.go` -- `Document` interface, `DynamicDoc` struct, `NewDynamicDoc()` constructor
  - `pkg/document/context.go` -- `DocContext` struct, `NewDocContext()` constructor
  - `pkg/document/document_test.go` -- unit tests
  - `pkg/tenancy/site.go` -- `SiteContext` struct (Name string, plus pgxpool ref for future use)
  - `pkg/auth/user.go` -- `User` struct (Email, FullName, Roles []string)
  - `pkg/events/emitter.go` -- `Emitter` struct with no-op `Emit(topic string, payload any)` method

  Key implementation details:
  - `DynamicDoc` fields: `metaDef *meta.MetaType`, `values map[string]any`, `original map[string]any` (dirty tracking snapshot), `children map[string][]*DynamicDoc` (child rows keyed by Table field name), `isNew bool`
  - `Set(field, value)` validates field name exists in MetaType.Fields or is a standard column name; returns error for unknown fields
  - Dirty tracking: `original` is deep-copied from `values` at construction time (handles `map[string]any` nesting); `ModifiedFields()` compares `values` vs `original`
  - `GetChild(tableField)` returns `[]Document` (interface slice) from internal `[]*DynamicDoc`
  - `AddChild(tableField)` creates a new `DynamicDoc` using the child MetaType resolved from `FieldDef.Options`; auto-assigns incremental `idx`; requires `childMetas map[string]*meta.MetaType` passed to `NewDynamicDoc()`
  - `DocContext` embeds `context.Context`, holds `Site *tenancy.SiteContext`, `User *auth.User`, `Flags map[string]any`, `TX pgx.Tx`, `EventBus *events.Emitter`

- **Why this task exists:** Every other task depends on these types. The `Document` interface is the contract consumed by all downstream milestones (MS-05, MS-06, MS-08, MS-15, MS-23).
- **Dependencies:** None (foundational)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` lines 300-348 (Document interface, DynamicDoc struct)
  - `MOCA_SYSTEM_DESIGN.md` lines 429-437 (DocContext struct)
  - `pkg/meta/metatype.go` (MetaType struct)
  - `pkg/meta/columns.go` (StandardColumns, ChildStandardColumns for field name validation)
  - `pkg/orm/transaction.go` (pgx.Tx type used in DocContext.TX)
- **Deliverable:** Compilable `pkg/document` package with passing unit tests covering: Get/Set, unknown field rejection, dirty tracking, ModifiedFields, child AddChild/GetChild with idx ordering, AsMap round-trip, ToJSON.
- **Risks / Unknowns:**
  - `AddChild()` needs access to the child MetaType. Solution: pass `childMetas map[string]*meta.MetaType` as a parameter to `NewDynamicDoc()`, keyed by Table field name. Avoids circular imports with a registry reference.
  - Deep-copy of `original` must handle `map[string]any` nested values correctly to prevent aliasing.

---

### Task 2: Naming Engine -- 6 Strategies with PG Sequence Support

- **Task ID:** MS-04-T2
- **Status:** Completed
- **Title:** Naming Engine -- uuid, field, hash, autoincrement, pattern, custom
- **Description:**
  Implement `NamingEngine` with all 6 naming strategies.

  Files to create:
  - `pkg/document/naming.go` -- `NamingEngine` struct, `GenerateName()`, pattern parser, custom func registry (`map[string]NamingFunc`)
  - `pkg/document/naming_test.go` -- unit tests (uuid, field, hash, custom)
  - `pkg/document/naming_integration_test.go` -- integration tests (autoincrement and pattern with real PG sequences)

  Strategy implementations:
  1. **uuid** -- `crypto/rand`-based UUID v4. No DB access. Default strategy.
  2. **field** -- `doc.Get(strategy.FieldName)`. Error if value is nil or empty string.
  3. **hash** -- SHA-256 of `doctype + sorted storable field values`, hex-encoded, truncated to 10 chars.
  4. **autoincrement** -- `CREATE SEQUENCE IF NOT EXISTS "seq_{tablename}"` (lazy, idempotent), then `SELECT nextval(...)`. Returns integer as string.
  5. **pattern** -- Parse e.g. `"SO-.####"`: extract prefix (`"SO-"`) and width (4 `#` chars). Use PG sequence `"seq_{tablename}_naming"`. Format result with zero-padded integer. Rule: exactly one contiguous `#` group required.
  6. **custom** -- Lookup by `strategy.CustomFunc` from `map[string]NamingFunc`. `NamingFunc` type: `func(ctx context.Context, doc Document) (string, error)`. Error if function not registered.

  `NamingFunc` and `RegisterNamingFunc(name string, fn NamingFunc)` provide the extension point for MS-08 and app controllers.

- **Why this task exists:** Document names are primary keys -- every Insert requires one before the transaction begins. Pattern naming with PG sequences is the most complex strategy and is explicitly called out as a risk in the roadmap.
- **Dependencies:** MS-04-T1 (needs `Document` interface for `doc.Get()`)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` lines 440-462 (6 naming strategies, PG sequence details)
  - `pkg/meta/metatype.go` lines 6-27 (NamingRule constants, NamingStrategy struct)
  - `ROADMAP.md` line 356 (acceptance: `"SO-.####"` → `"SO-0001"`, `"SO-0002"`, thread-safe)
  - `ROADMAP.md` line 363 (risk: PG sequences per site/doctype created dynamically)
- **Deliverable:** `NamingEngine` producing correct unique names for all 6 strategies. Integration test proving pattern naming concurrency safety: 10 concurrent goroutines all receive unique sequential names (TO-0001 through TO-0010), passing with `-race` flag.
- **Risks / Unknowns:**
  - PG sequence creation is DDL and must run outside a read-only transaction context. Use a separate non-transactional connection or execute sequence creation before `orm.WithTransaction` begins.
  - Pattern parsing edge cases (no `#`, multiple `#` groups, `#` not at end). Fail fast with a clear error at MetaType registration time if pattern is malformed.

---

### Task 3: Field-Level Validation and Type Coercion

- **Task ID:** MS-04-T3
- **Status:** Completed
- **Title:** Field Validator -- Type Coercion and All Validation Rules
- **Description:**
  Implement field-level validation and type coercion against `FieldDef` rules.

  Files to create:
  - `pkg/document/validator.go` -- `Validator` struct, `ValidateDoc(ctx *DocContext, doc *DynamicDoc, pool *pgxpool.Pool) error`, type coercion, per-rule checks, `ValidationError`/`FieldError` types
  - `pkg/document/validator_test.go` -- unit tests

  **Type coercion** (runs first, modifies doc in-place via `Set()`):
  - Grouped by PG column type to minimise duplication:
    - TEXT types (15 types): no coercion needed; values are stored as strings
    - Numeric (Int, Float, Currency, Percent, Rating, Duration): parse string → int64 / float64
    - Boolean (Check): parse `"true"`/`"1"` → `true`, `"false"`/`"0"` → `false`
    - Date/Datetime/Time: parse ISO 8601 strings → `time.Time`
  - Coercion failure produces a validation error (not a panic)

  **Validation rules** (run after coercion, all accumulated -- not short-circuit):
  - `Required`: value is nil or empty string
  - `MaxLength`: `len(string) > FieldDef.MaxLength` (only when `MaxLength > 0`)
  - `MinValue` / `MaxValue`: numeric range check (pointer fields, nil = no constraint)
  - `ValidationRegex`: compile and match; compiled patterns cached in `sync.Map`
  - `Select` options: value must appear in `FieldDef.Options` (newline-separated)
  - `MandatoryDependsOn`: if the named field has a truthy value, this field becomes required
  - `Unique`: `SELECT 1 FROM {table} WHERE {field} = $1 AND name != $2` (DB required)
  - `Link` (`FieldType == "Link"`): `SELECT 1 FROM {linked_table} WHERE name = $1` (DB required)
  - `CustomValidator`: lookup from `map[string]ValidatorFunc` registry; extension point for MS-08

  **`ValidationError`** type: wraps `[]FieldError{Field string, Message string, Rule string}`. Implements `error`. Allows MS-06 to return structured HTTP 422 responses.

- **Why this task exists:** Validation is a core acceptance criterion and a hard contract for the API layer. MS-06 maps `ValidationError` directly to HTTP 422 response bodies.
- **Dependencies:** MS-04-T1 (needs `Document`, `DynamicDoc`, `DocContext`)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` lines 186-226 (FieldDef validation fields)
  - `pkg/meta/fielddef.go` (FieldDef struct, FieldType constants, `IsStorable()`)
  - `ROADMAP.md` lines 357-359 (acceptance criteria: required, type coercion, link validation)
- **Deliverable:** `Validator` with passing unit tests covering: required, max length, min/max value, regex, select options, mandatory depends on, type coercion for each FieldType group. Unique and Link rules tested in MS-04-T5 integration tests (require real DB).
- **Risks / Unknowns:**
  - 30+ field types. Mitigated by grouping on PG column type (15 TEXT types are a no-op for coercion).
  - Unique and Link validation fire DB queries per field. Acceptable for v1; query batching is a future optimisation.

---

### Task 4: Lifecycle Engine, Controller Resolution, and CRUD Operations

- **Task ID:** MS-04-T4
- **Status:** Completed
- **Title:** Lifecycle Dispatcher, Controllers, and CRUD (Insert/Update/Delete/Get/GetList)
- **Description:**
  Implement the lifecycle event dispatcher, controller resolution, and all CRUD operations. This is the core of MS-04 -- it wires together all previous tasks. Also adds `tab_outbox` DDL to `GenerateSystemTablesDDL()`.

  Files to create / modify:
  - `pkg/document/lifecycle.go` -- `DocLifecycle` interface (16 methods), `DocEvent` string constants (14), `LifecycleDispatcher`
  - `pkg/document/controller.go` -- `BaseController` (embeddable no-op), `ControllerRegistry`, resolution logic
  - `pkg/document/crud.go` -- `DocManager` struct (main entry point), Insert, Update, Delete, Get, GetList, Singles support
  - `pkg/meta/ddl.go` -- add `tab_outbox` statement to `GenerateSystemTablesDDL()`

  **DocLifecycle interface** (16 methods, all `(ctx *DocContext, doc Document) error`, all optional via BaseController embedding):

  | Group | Methods |
  |-------|---------|
  | Insert | BeforeInsert, AfterInsert |
  | Validate | BeforeValidate, Validate |
  | Save | BeforeSave, AfterSave |
  | Update | OnUpdate |
  | Submit | BeforeSubmit, OnSubmit |
  | Cancel | BeforeCancel, OnCancel |
  | Delete | OnTrash, AfterDelete |
  | Change | OnChange |
  | Rename | `BeforeRename(ctx, doc, oldName, newName string) error`, `AfterRename(ctx, doc, oldName, newName string) error` (controller-only; not in DocEvent constants) |

  **14 DocEvent constants** (string type for readability): `before_insert`, `after_insert`, `before_validate`, `validate`, `before_save`, `after_save`, `on_update`, `before_submit`, `on_submit`, `before_cancel`, `on_cancel`, `on_trash`, `after_delete`, `on_change`.

  **BaseController**: struct with all 16 methods returning nil. Embed in custom controllers to implement only the methods needed.

  **ControllerRegistry**:
  - `TypeOverrides map[string]DocLifecycleFactory` -- fully replaces the controller for a doctype
  - `TypeExtensions map[string][]DocLifecycleExtension` -- wraps the resolved controller
  - Resolution: check `TypeOverrides[doctype]` first; if absent use `BaseController` + apply `TypeExtensions`
  - `RegisterOverride(doctype string, factory DocLifecycleFactory)` and `RegisterExtension(doctype string, ext DocLifecycleExtension)` are the extension points consumed by MS-08

  **DocManager** (holds `*meta.Registry`, `*orm.DBManager`, `*NamingEngine`, `*Validator`, `*ControllerRegistry`):

  **Insert(ctx *DocContext, doctype string, values map[string]any) (Document, error)**:
  1. Load MetaType from registry
  2. Create DynamicDoc with values
  3. Generate name via NamingEngine (outside transaction)
  4. Resolve controller; dispatch: BeforeInsert → BeforeValidate → Validate + field validation → BeforeSave
  5. `orm.WithTransaction`: INSERT parent row, INSERT child rows (parent/parenttype/parentfield/idx set), INSERT tab_outbox (event_type "doc.created"), INSERT tab_audit_log (action "Create")
  6. After commit: dispatch AfterInsert → AfterSave → OnChange (errors logged, not fatal)
  7. Return document

  **Update(ctx *DocContext, doctype, name string, values map[string]any) (Document, error)**:
  1. Get existing doc, apply values via Set()
  2. Dispatch: BeforeValidate → Validate + field validation → BeforeSave → OnUpdate
  3. `orm.WithTransaction`: UPDATE parent (only ModifiedFields()), diff and sync child rows, INSERT tab_outbox ("doc.updated"), INSERT tab_audit_log ("Update", changes JSONB)
  4. After commit: dispatch AfterSave → OnChange

  **Delete(ctx *DocContext, doctype, name string) error**:
  1. Load doc; dispatch: OnTrash
  2. `orm.WithTransaction`: DELETE child rows, DELETE parent row, INSERT tab_outbox ("doc.deleted"), INSERT tab_audit_log ("Delete")
  3. After commit: dispatch AfterDelete

  **Get(ctx *DocContext, doctype, name string) (Document, error)**:
  - `SELECT * FROM {table} WHERE name = $1`; build DynamicDoc; load child rows for each Table field

  **GetList(ctx *DocContext, doctype string, filters map[string]any, orderBy string, limit, offset int) ([]Document, int, error)**:
  - Hardcoded SQL with basic WHERE clause construction (MS-05 replaces this with the query builder)

  **Singles support** (MetaType.IsSingle == true):
  - Get: `SELECT field, value FROM tab_singles WHERE doctype = $1` → reconstruct as DynamicDoc
  - Set: `INSERT INTO tab_singles (doctype, field, value) VALUES ... ON CONFLICT (doctype, field) DO UPDATE SET value = EXCLUDED.value`

  **tab_outbox DDL** (add to `GenerateSystemTablesDDL()` in `pkg/meta/ddl.go`):
  ```sql
  CREATE TABLE IF NOT EXISTS tab_outbox (
      "id"            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
      "event_type"    TEXT NOT NULL,
      "topic"         TEXT NOT NULL,
      "partition_key" TEXT,
      "payload"       JSONB NOT NULL,
      "created_at"    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      "processed"     BOOLEAN NOT NULL DEFAULT false
  )
  ```

- **Why this task exists:** Lifecycle + CRUD is the heart of MS-04. All acceptance criteria depend on this task: `Insert("SalesOrder", values)` triggering events in order, naming, validation, and child table cascades.
- **Dependencies:** MS-04-T1 (DynamicDoc, DocContext), MS-04-T2 (NamingEngine), MS-04-T3 (Validator)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` lines 351-427 (lifecycle events, state machine, event ordering)
  - `MOCA_SYSTEM_DESIGN.md` lines 714-781 (DocEvent constants, TypeOverrides/TypeExtensions, PrioritizedHandler)
  - `MOCA_SYSTEM_DESIGN.md` lines 1895-1919 (end-to-end Insert walkthrough)
  - `MOCA_SYSTEM_DESIGN.md` lines 1205-1229 (tab_outbox schema and outbox pattern)
  - `pkg/orm/transaction.go` (WithTransaction, TxFromContext)
  - `pkg/meta/ddl.go` (GenerateSystemTablesDDL -- add tab_outbox here)
  - `pkg/meta/columns.go` (StandardColumns, ChildStandardColumns for INSERT column lists)
- **Deliverable:** Working `DocManager` with all 5 CRUD operations, tab_outbox DDL, lifecycle event dispatch, controller resolution, Singles support, and unit tests for lifecycle event ordering and controller override/extension.
- **Risks / Unknowns:**
  - **Largest task.** Split across 3 files (lifecycle.go, controller.go, crud.go) for manageability.
  - Child row diffing during Update: must detect added, removed, and modified children by comparing current children (loaded via Get) against incoming values, keyed by `name` or positional `idx`.
  - After-commit hooks (AfterInsert, AfterSave, OnChange) run outside the transaction. Errors must be logged with `slog` but must not propagate to the caller or cause a rollback.
  - `tab_outbox` must exist before first CRUD call. Ensure `EnsureMetaTables()` (from `pkg/meta/migrator.go`) runs during `DocManager` initialization.

---

### Task 5: Integration Tests -- Full Lifecycle Validation

- **Task ID:** MS-04-T5
- **Status:** Completed
- **Title:** Integration Tests -- End-to-End Document Runtime
- **Description:**
  Comprehensive integration tests using Docker Compose PostgreSQL (port 5433) and Redis (port 6379), covering all MS-04 acceptance criteria.

  Files to create:
  - `pkg/document/integration_test.go` (`//go:build integration`)

  Test cases:

  | # | Test | Verifies |
  |---|------|---------|
  | 1 | Insert lifecycle flow | Events fire in correct order (recording controller); parent + child rows in PG; tab_outbox "doc.created"; tab_audit_log "Create" |
  | 2 | Pattern naming concurrency | 10 goroutines with `"TO-.####"` → TO-0001..TO-0010, all unique, `-race` clean |
  | 3 | Field validation | Missing required → error; regex violation → error; unique violation → error; link to nonexistent doc → error |
  | 4 | Type coercion | String `"42"` → int 42 for Int field; `"true"` → bool for Check field |
  | 5 | Update lifecycle | Only changed fields in UPDATE SQL; OnUpdate fires; tab_audit_log "Update" with changes |
  | 6 | Delete lifecycle | OnTrash + AfterDelete fire; parent + child rows removed |
  | 7 | Singles support | Set values → tab_singles entries; Get values → correct DynamicDoc |
  | 8 | Controller resolution | TypeOverride replaces BaseController; TypeExtension augments it |

  Test infrastructure (matching `pkg/meta/` conventions exactly):
  - `TestMain(m *testing.M)` for shared setup: create test tenant schema, run `EnsureMetaTables()` + `GenerateSystemTablesDDL()` (which now includes tab_outbox), compile and register test MetaTypes (TestOrder with child TestOrderItem)
  - Pure stdlib `testing`, no testify
  - `t.Helper()` on all helper functions, `t.Logf()` for narration, `t.Cleanup()` for teardown
  - `t.Skip()` with graceful `os.Exit(0)` when Docker infrastructure unavailable
  - Unique schema per test run (using random suffix or test name) to prevent cross-test interference

  Recording controller for event-order tests:
  ```go
  type recordingController struct {
      BaseController
      events []string
  }
  // Implements all 16 methods by appending the event name to events slice
  ```

- **Why this task exists:** Integration tests are an explicit deliverable ("Integration tests: full lifecycle with hooks, naming, validation") and the only way to verify the combined behaviour of naming + validation + lifecycle + CRUD against real PostgreSQL.
- **Dependencies:** MS-04-T1 through MS-04-T4 (all tasks)
- **Inputs / References:**
  - `ROADMAP.md` lines 354-360 (acceptance criteria -- every line must have a corresponding test)
  - `pkg/meta/migrator_integration_test.go` (TestMain pattern, helper structure)
  - `pkg/meta/registry_integration_test.go` (DB + Redis setup, pool creation)
- **Deliverable:** All integration tests pass with `-race` flag. `make test-integration` (or equivalent) runs green.
- **Risks / Unknowns:**
  - Test isolation: unique schema per run OR explicit cleanup via `t.Cleanup`. Prefer unique schema to avoid shared state between parallel test runs.
  - Recording controller must implement all 16 DocLifecycle methods to satisfy the interface (BaseController embedding handles the no-ops cleanly).

---

## Recommended Execution Order

```
MS-04-T1  ──┬──  MS-04-T2 (Naming)    ──┬──  MS-04-T4 (Lifecycle + CRUD)  ──  MS-04-T5 (Integration Tests)
            └──  MS-04-T3 (Validator)  ──┘
```

1. **MS-04-T1** -- Foundation types (all others depend on it)
2. **MS-04-T2** and **MS-04-T3** in parallel -- independent of each other
3. **MS-04-T4** -- Lifecycle, controller resolution, CRUD (depends on T1 + T2 + T3)
4. **MS-04-T5** -- Integration tests (depends on all)

---

## Open Questions

- **Child MetaType resolution in DynamicDoc**: Should `NewDynamicDoc` accept a pre-resolved `childMetas map[string]*meta.MetaType` parameter, or hold a `*meta.Registry` reference for on-demand lookup? Pre-resolved is simpler and avoids circular import concerns; registry reference is more flexible. Recommend pre-resolved for T1; can be refactored later if DocManager needs lazy loading.
- **`OutboxEntry` struct**: Define a typed Go struct for outbox entries in `pkg/document/` (for type safety in CRUD writes), or use raw parameterized SQL directly? Recommendation: define a minimal `OutboxEntry` struct now -- it will be reused by MS-15 when the outbox poller is implemented.

---

## Out of Scope for This Milestone

- **VirtualDoc** -- deferred to MS-28
- **Workflow state transitions** -- MS-23; `workflow_state` and `docstatus` exist as standard columns but no engine logic in MS-04
- **Version tracking** -- `tab_version` writes deferred; `TrackChanges` flag read but not acted on
- **Permission checks** -- MS-14; no field-level or doctype-level permission enforcement
- **HookRegistry** -- MS-08; MS-04 defines the extension points (`ControllerRegistry`, `DocLifecycle` interface, `DocEvent` constants) but does not implement the full hook registry
- **Query builder** -- MS-05; `GetList` uses hardcoded SQL in MS-04
- **Redis document caching** -- post-CRUD cache population/invalidation deferred to MS-06/MS-12
- **Submit() / Cancel() methods on DocManager** -- lifecycle events `BeforeSubmit`, `OnSubmit`, `BeforeCancel`, `OnCancel` exist in the dispatcher, but dedicated `Submit()` and `Cancel()` CRUD methods are not an explicit MS-04 deliverable; they are implemented in MS-23
