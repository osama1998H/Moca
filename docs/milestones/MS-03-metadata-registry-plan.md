# MS-03 - Metadata Registry Plan

## Milestone Summary
- **ID:** MS-03
- **Name:** Metadata Registry -- MetaType, FieldDef, Compiler, Redis Cache
- **Roadmap Reference:** `ROADMAP.md` lines 298-336
- **Goal:** Implement MetaType and FieldDef types, the schema compiler (JSON -> validated MetaType), the in-memory + Redis-backed metadata registry, and the schema migrator (MetaType diff -> DDL).
- **Why it matters:** MetaType is the foundational primitive. Document Runtime (MS-04), API layer (MS-06), Query Engine (MS-05), and Permission Engine (MS-14) all depend on compiled MetaType definitions. MS-03 is on the **critical path** to v1.0.
- **Position in roadmap:** Implementation Order #4 (after MS-00 spikes, MS-01 project structure, MS-02 database/redis foundation -- all complete)
- **Upstream dependencies:** MS-02 (PostgreSQL Foundation & Redis Connection Layer) -- provides `DBManager`, `WithTransaction`, `EnsureSystemSchema`, `RedisClients`, structured logging
- **Downstream dependencies:** MS-04 (Document Runtime), MS-05 (Query Engine), MS-06 (REST API Layer), MS-14 (Permission Engine)

## Vision Alignment

MetaType is the central abstraction of MOCA: a single definition drives database schema, CRUD API routes, GraphQL schema, search index config, and React UI rendering. MS-03 establishes the type system, compilation pipeline, storage layer, and caching infrastructure that every downstream milestone depends on. Without it, there are no document tables, no APIs, and no UI.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `MOCA_SYSTEM_DESIGN.md` | 3.1.1 MetaType struct | 137-181 | Canonical MetaType struct definition |
| `MOCA_SYSTEM_DESIGN.md` | 3.1.2 FieldDef + FieldType | 183-268 | FieldDef struct, 35 FieldType constants |
| `MOCA_SYSTEM_DESIGN.md` | 3.1.3 Metadata Lifecycle | 271-296 | Compile -> cache -> migrate pipeline |
| `MOCA_SYSTEM_DESIGN.md` | 3.2.3 NamingStrategy | 440-462 | NamingStrategy struct, 6 NamingRule values |
| `MOCA_SYSTEM_DESIGN.md` | 4.3 Per-Tenant Schema | 889-1006 | Table DDL: standard columns, child tables, tab_singles, tab_version, tab_audit_log, tab_doctype |
| `MOCA_SYSTEM_DESIGN.md` | 4.4 _extra JSONB | 1008-1022 | _extra column pattern and indexing |
| `MOCA_SYSTEM_DESIGN.md` | 5.1 Caching Layer | 1025-1058 | Redis key patterns, TTL strategy, invalidation |
| `MOCA_SYSTEM_DESIGN.md` | 15 Package Layout | 1936-1941 | pkg/meta/ file locations |
| `ROADMAP.md` | MS-03 | 298-336 | Scope, deliverables, acceptance criteria, risks |
| `internal/drivers/redis.go` | Key constants | 17-24 | `KeyMeta`, `KeySchema` already defined |
| `pkg/orm/schema.go` | EnsureSystemSchema | 1-83 | DDL pattern to follow |
| `pkg/orm/transaction.go` | WithTransaction | -- | Transaction pattern used by Migrator |

## Research Notes

No web research was needed. All implementation details are fully specified in the design documents and existing codebase patterns.

Key existing infrastructure MS-03 builds upon:
- `pkg/orm/postgres.go` -- `DBManager` with `ForSite()`, `SystemPool()`, per-tenant pgxpool
- `pkg/orm/transaction.go` -- `WithTransaction(ctx, pool, fn)` pattern
- `pkg/orm/schema.go` -- DDL pattern: `CREATE ... IF NOT EXISTS` inside `WithTransaction`, `pgx.Identifier{}.Sanitize()` for injection prevention
- `internal/drivers/redis.go` -- `RedisClients.Cache` on db 0, `KeyMeta = "meta:%s:%s"`, `KeySchema = "schema:%s:version"`
- `pkg/observe/logging.go` -- `slog`-based structured logging
- Test patterns: stdlib `testing` only, `//go:build integration`, `TestMain` with probe/skip, `t.Cleanup()`

## Milestone Plan

### Task 1: Type Definitions -- MetaType, FieldDef, NamingStrategy, and Stub Types

- **Task ID:** MS-03-T1
- **Status:** Completed
- **Title:** Type Definitions
- **Description:** Define all core data types: `MetaType` struct, `FieldDef` struct with all 35 `FieldType` constants (29 storage + 6 layout-only), `NamingStrategy`/`NamingRule` enum, and stub types for downstream milestones (`PermRule`, `WorkflowMeta`, `DocHookDefs`, `APIConfig`, `ViewMeta`, `LayoutHint`).
- **Why this task exists:** Every other component (compiler, registry, migrator) takes or returns MetaType/FieldDef values. This must come first.
- **Dependencies:** None (first task). Uses only Go stdlib.
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` 3.1.1 lines 137-181 (MetaType struct)
  - `MOCA_SYSTEM_DESIGN.md` 3.1.2 lines 183-268 (FieldDef, 35 FieldType constants)
  - `MOCA_SYSTEM_DESIGN.md` 3.2.3 lines 440-462 (NamingStrategy, NamingRule)
  - `MOCA_SYSTEM_DESIGN.md` 3.4 lines 685-701, 3.5 lines 714-778, 3.6 lines 790-804 (stub types)
- **Deliverables:**
  - `pkg/meta/fielddef.go` -- `FieldType` string type, 35 constants, `FieldDef` struct. Helpers: `IsStorable() bool`, `IsValid() bool`. Lookup: `var ValidFieldTypes map[FieldType]bool`
  - `pkg/meta/metatype.go` -- `MetaType` struct (Identity, Schema, Variants, Behavior, API/UI, Versioning fields). `NamingStrategy` struct, `NamingRule` string type with 6 constants (autoincrement, pattern, field, hash, uuid, custom)
  - `pkg/meta/stubs.go` -- Stub types: `PermRule`, `WorkflowMeta`, `DocHookDefs`, `APIConfig`, `ViewMeta`, `LayoutHint`. Each with doc comment noting which milestone completes it
  - `pkg/meta/fielddef_test.go` -- Tests for `IsStorable()` (6 layout types false, 29 storage true) and `IsValid()`
  - `pkg/meta/metatype_test.go` -- Tests for NamingRule constants
- **Risks / Unknowns:**
  - `FieldDef.Default` is `any` -- intentional for JSON flexibility. JSON unmarshal produces float64/string/bool
  - `MinValue`/`MaxValue` are `*float64` -- pointer semantics distinguish "not set" from zero
  - Design says "33 FieldTypes" but actual count is 35 (29 storage + 6 layout-only). Implementation follows the design doc's actual constant list

---

### Task 2: Schema Compiler -- JSON to Validated MetaType

- **Task ID:** MS-03-T2
- **Status:** Completed
- **Title:** Schema Compiler
- **Description:** Implement `Compile(jsonBytes []byte) (*MetaType, error)` that parses JSON, validates against all business rules, and returns a compiled MetaType. Also implement `TableName(doctypeName string) string` for the `tab_{snake_case}` naming convention.
- **Why this task exists:** The compiler is the gateway from raw definitions into validated MetaType structs. Both the Registry (for caching) and the Migrator (for DDL) depend on compiled MetaTypes.
- **Dependencies:** MS-03-T1 (type definitions, `IsValid()`, `IsStorable()`)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` 3.1.3 lines 271-296 (compilation pipeline)
  - `ROADMAP.md` lines 308-309, 314-315 (compiler spec, acceptance criteria)
  - `MOCA_SYSTEM_DESIGN.md` 4.3 line 896 (table naming: `tab_sales_order`)
- **Deliverables:**
  - `pkg/meta/compiler.go` -- `Compile()` function with collected (not short-circuited) error reporting. Validation rules:
    1. `Name` required
    2. `Module` required
    3. Each field's `FieldType` must be valid
    4. No duplicate field names
    5. Link/DynamicLink fields require non-empty `Options`
    6. Table/TableMultiSelect fields require non-empty `Options`
    7. SearchFields must reference existing field names
    8. TitleField must reference existing field name
    9. SortField must reference existing field or standard column
    10. NamingRule.Rule must be valid (or empty -- defaults to UUID)
    11. NamingRule "field" requires valid FieldName
    12. NamingRule "pattern" requires non-empty Pattern
  - `TableName()` with internal `toSnakeCase()` (handles consecutive capitals: HTTPConfig -> http_config)
  - `pkg/meta/compiler_test.go` -- Tests for valid SalesOrder, missing name, missing module, unknown field type, duplicate field names, Link without Options, multiple errors collected, TableName edge cases
  - `pkg/meta/testdata/SalesOrder.json` -- Reference fixture (7-10 fields covering Data, Currency, Date, Link, Table, Select, Check types; NamingRule: pattern "SO-.####")
- **Risks / Unknowns:**
  - `toSnakeCase` for consecutive capitals needs careful rune-walking logic (tested with ~10 edge cases including HTTPConfig, XMLParser)
  - Cross-MetaType validation (Link target exists?) deferred to Registry which has access to the full corpus

---

### Task 3: Column Type Mapping, DDL Generation, and Schema Migrator

- **Task ID:** MS-03-T3
- **Status:** Completed
- **Title:** DDL Generation and Migrator
- **Description:** Implement FieldType-to-PostgreSQL column mapping, DDL generation for document tables (standard columns, child tables, system tables), and the schema migrator (diff two MetaTypes -> DDL statements -> apply in transaction).
- **Why this task exists:** Bridges metadata to physical database schema. Without this, MetaType definitions cannot be materialized as tables and schema evolution requires manual DDL.
- **Dependencies:** MS-03-T1 (types, `IsStorable()`), MS-03-T2 (compiler for `TableName()`, test fixtures)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` 4.3 lines 889-1006 (all table DDL: standard columns, child tables, tab_singles, tab_version, tab_audit_log, tab_doctype)
  - `ROADMAP.md` lines 311-319 (column mapping spec, acceptance criteria)
  - `pkg/orm/schema.go` lines 23-83 (DDL pattern: `WithTransaction`, `pgx.Identifier.Sanitize()`, `CREATE ... IF NOT EXISTS`)
- **Deliverables:**
  - `pkg/meta/columns.go` -- `ColumnType(ft FieldType) string` mapping all 29 storable types:
    - Data/Text/LongText/Markdown/Code/HTMLEditor/Select/Color/Barcode/Signature/Password/Link/DynamicLink/Attach/AttachImage -> `TEXT`
    - Int -> `INTEGER`
    - Float/Currency/Percent/Rating/Duration -> `NUMERIC(18,6)`
    - Date -> `DATE`, Datetime -> `TIMESTAMPTZ`, Time -> `TIME`
    - Check -> `BOOLEAN`
    - JSON/Geolocation -> `JSONB`
    - Table/TableMultiSelect -> empty string (no column generated)
  - Standard columns (13): name, owner, creation, modified, modified_by, docstatus, idx, workflow_state, _extra, _user_tags, _comments, _assign, _liked_by
  - Child table extra columns (3): parent, parenttype, parentfield
  - `pkg/meta/ddl.go` -- `DDLStatement` struct. `GenerateTableDDL(mt *MetaType) []DDLStatement`. `GenerateSystemTablesDDL() []DDLStatement` producing DDL for tab_doctype, tab_singles, tab_version (with idx_version_ref index), tab_audit_log (`PARTITION BY RANGE(timestamp)` plus a default partition so inserts work immediately)
  - `pkg/meta/migrator.go` -- `Migrator` struct with `NewMigrator(db *orm.DBManager, logger *slog.Logger) *Migrator`. Methods:
    - `Diff(current, desired *MetaType) []DDLStatement` -- ADD COLUMN, DROP COLUMN, ALTER COLUMN TYPE, CREATE/DROP INDEX. When `current` is nil, delegates to `GenerateTableDDL`
    - `Apply(ctx context.Context, site string, statements []DDLStatement) error` -- executes all statements inside `WithTransaction`
    - `EnsureMetaTables(ctx context.Context, site string) error` -- convenience: generates and applies per-tenant system table DDL
  - `pkg/meta/columns_test.go` -- Verify all 29 storable types map correctly, all layout types return empty string
  - `pkg/meta/ddl_test.go` -- Regular table DDL, child table DDL, system tables DDL (all 4 tables present), index generation
  - `pkg/meta/migrator_test.go` -- Diff tests: add field, remove field, change type, add index, nil current (full create)
  - `pkg/meta/migrator_integration_test.go` (`//go:build integration`) -- Apply creates table, add-field migration, EnsureMetaTables creates all 4 system tables
- **Risks / Unknowns:**
  - `ALTER COLUMN TYPE` for incompatible transitions (TEXT -> INTEGER) may fail with existing data. Migrator generates the DDL mechanically; data migration is caller responsibility (future CLI)
  - `tab_audit_log` default partition: must be created in `EnsureMetaTables` or inserts fail immediately. Monthly partition automation deferred to MS-12
  - Geolocation -> JSONB (not PostGIS) per ROADMAP.md line 323 risk note
  - `DROP COLUMN` is destructive. Safety layer (confirmation prompts) deferred to MS-10

---

### Task 4: Three-Tier Cache Registry with End-to-End Integration Tests

- **Task ID:** MS-03-T4
- **Status:** Completed
- **Title:** Three-Tier Cache Registry
- **Description:** Implement the `Registry` -- the central access point for MetaType lookups -- backed by L1 in-memory (`sync.Map`), L2 Redis (`meta:{site}:{doctype}`), L3 PostgreSQL (`tab_doctype`). Includes MetaType registration (compile + migrate + cache), cache invalidation, schema version tracking, and comprehensive integration tests validating all MS-03 acceptance criteria.
- **Why this task exists:** The Registry is how every other framework component accesses MetaType definitions. The three-tier cache ensures low-latency lookups while maintaining durability and cross-instance consistency.
- **Dependencies:** MS-03-T1, MS-03-T2 (compiler), MS-03-T3 (migrator, DDL, EnsureMetaTables)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` 3.1.3 lines 271-296 (metadata lifecycle)
  - `MOCA_SYSTEM_DESIGN.md` 5.1 lines 1035-1058 (Redis cache keys, invalidation strategy)
  - `internal/drivers/redis.go` lines 17-24 (`KeyMeta = "meta:%s:%s"`, `KeySchema = "schema:%s:version"`)
  - `pkg/orm/postgres.go` (`DBManager.ForSite()` for tenant pool access)
- **Deliverables:**
  - `pkg/meta/registry.go` -- `Registry` struct with `sync.Map` L1 cache. Constructor: `NewRegistry(db *orm.DBManager, redisCache *redis.Client, logger *slog.Logger) *Registry`. Methods:
    - `Get(ctx, site, doctype) (*MetaType, error)` -- L1 -> L2 -> L3 cascade. Returns `ErrMetaTypeNotFound` sentinel if absent from all tiers
    - `Register(ctx, site, jsonBytes) (*MetaType, error)` -- Compile, diff/migrate DDL, upsert `tab_doctype`, populate L1+L2, increment `schema:{site}:version`
    - `Invalidate(ctx, site, doctype) error` -- Clear L1 + L2 (not L3; PostgreSQL is source of truth)
    - `InvalidateAll(ctx, site) error` -- Clear all L1+L2 entries for the site
    - `SchemaVersion(ctx, site) (int64, error)` -- Read `schema:{site}:version` from Redis
  - `pkg/meta/registry_test.go` -- Unit tests: L1 hit returns without touching Redis/DB, ErrMetaTypeNotFound when all tiers empty, Invalidate clears L1
  - `pkg/meta/registry_integration_test.go` (`//go:build integration`) -- End-to-end tests:
    - Register SalesOrder then Get (verify all fields round-trip correctly)
    - L1 -> L2 -> L3 cascade (clear each tier, verify fallback still works)
    - Schema version increments with each Register call
    - Invalid JSON rejected with specific validation errors
    - Register updated MetaType with extra field, verify new column exists in DB via `information_schema.columns`
    - All 29 storable FieldType column types verified against DB via `information_schema.columns`
    - `tab_audit_log` is partitioned (verify via `pg_class.relkind = 'p'`)
- **Risks / Unknowns:**
  - L1 staleness in multi-instance deployments: acceptable for MS-03. Cross-instance invalidation via Redis pub/sub is planned for MS-10 (hot reload)
  - `Register()` atomicity: `tab_doctype` upsert and DDL apply must run in the same transaction. PostgreSQL DDL is transactional, so this is safe with `WithTransaction`
  - Redis serialization: JSON for now (simple, debuggable). MessagePack/protobuf can be profiled later if needed

---

## Recommended Execution Order

1. **MS-03-T1** -- Type Definitions (no dependencies; unblocks everything)
2. **MS-03-T2** -- Schema Compiler (depends on T1; unblocks T3 and T4)
3. **MS-03-T3** -- DDL Generation and Migrator (depends on T1, T2)
4. **MS-03-T4** -- Three-Tier Cache Registry + Integration Tests (depends on T1, T2, T3)

T1 and T2 are strictly sequential. T3 and T4 can be parallelized by two developers (different files), but T4's integration tests require T3's migrator. Single-developer: follow the order above.

## Open Questions

1. **Default partition for `tab_audit_log`:** The partitioned table rejects inserts until a partition exists. Recommendation: create a default partition in `EnsureMetaTables` so the system works out of the box. Monthly partition automation is deferred to MS-12.
2. **`FieldDef.Default` type validation:** JSON unmarshal produces float64/string/bool for `any`. The compiler should NOT validate Default values against FieldType -- that is MS-04 (Document Runtime) territory.
3. **Cross-MetaType Link validation:** Verifying that a Link field's target DocType actually exists requires the full Registry corpus. Deferred to MS-04 `Insert()` time, not compiler time.

## Out of Scope for This Milestone

- Hot reload / filesystem watching (MS-10)
- API route generation from MetaType (MS-06)
- Search index configuration from MetaType (MS-15)
- GraphQL schema generation (MS-06)
- Custom field runtime (`_extra` JSONB read/write at query time) (MS-04/MS-05)
- Row-Level Security policies (MS-12)
- Cross-MetaType Link validation at registration time (MS-04)
- VirtualDoc / VirtualSource (MS-28)
- Kafka event broadcasting on `meta.changed` (MS-15)
- Cross-instance cache invalidation via Redis pub/sub (MS-10)
