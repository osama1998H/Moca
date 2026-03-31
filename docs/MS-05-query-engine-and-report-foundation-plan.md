# MS-05 — Query Engine and Report Foundation Plan

## Milestone Summary

- **ID:** MS-05
- **Name:** Query Engine and Report Foundation
- **Roadmap Reference:** `ROADMAP.md` lines 380–418
- **Goal:** Replace the hardcoded equality-only SQL in `GetList` with a metadata-driven `QueryBuilder` supporting 15 filter operators, transparent `_extra` JSONB access, Link field auto-joins, pagination, and introduce `ReportDef` for declarative query reports.
- **Why it matters:** `GetList` (MS-04) only supports `field = $N` equality filters. The API layer (MS-06) needs to translate arbitrary URL query parameters into safe, parameterized SQL. Without the query builder, every API filter/sort/pagination combination would require hand-written SQL — defeating the "define once, generate everything" MetaType vision.
- **Position in roadmap:** Milestone 6 of 30. Backend Core stream (Stream A). Estimated 2 weeks.
- **Upstream dependencies:** MS-03 (MetaType Registry — field metadata, FieldDef, Registry cache), MS-04 (Document Runtime — DynamicDoc, CRUD, GetList, DocContext)
- **Downstream dependencies:** MS-06 (REST API — translates URL params via QueryBuilder), MS-20 (GraphQL/Reports — uses ReportDef and QueryReport execution)

## Vision Alignment

Moca's core promise is that a single `MetaType` definition drives everything: schema, validation, API, search, and UI. MS-05 extends this to **querying** — the QueryBuilder reads MetaType field definitions to validate filter fields, detect `_extra` JSONB fields transparently, resolve Link field joins automatically, and generate safe parameterized SQL. This eliminates hand-written SQL for list/report queries across the entire framework, which is critical for the auto-generated REST API (MS-06) and eventual GraphQL layer (MS-20).

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `MOCA_SYSTEM_DESIGN.md` | §10.1 Dynamic Query Builder | 1654–1700 | QueryBuilder struct, Filter, Operator constants, design intent |
| `MOCA_SYSTEM_DESIGN.md` | §10.2 Report Builder | 1701–1725 | ReportDef struct, ReportColumn, QueryReport vs ScriptReport |
| `MOCA_SYSTEM_DESIGN.md` | §4.4 `_extra` JSONB Pattern | 1008–1022 | Transparent _extra access contract |
| `MOCA_SYSTEM_DESIGN.md` | §3.1.1–3.1.2 MetaType & FieldDef | 137–226 | Field resolution, Link field Options, Filterable flag |
| `MOCA_SYSTEM_DESIGN.md` | ADR-005 | 2087–2092 | _extra JSONB rationale and trade-offs |
| `ROADMAP.md` | MS-05 | 380–418 | Full milestone scope, deliverables, acceptance criteria |
| `ROADMAP.md` | MS-03 | 298–336 | Upstream: MetaType, FieldDef, Registry, column type mapping |
| `ROADMAP.md` | MS-04 | 339–376 | Upstream: DynamicDoc, GetList, ListOptions, DocContext |
| `pkg/document/crud.go` | GetList + ListOptions | 64–81, 1003–1121 | Current implementation to replace/refactor |
| `pkg/meta/fielddef.go` | FieldType constants | 25–80 | 33 field types including Link, DynamicLink |
| `pkg/meta/columns.go` | ColumnType + StandardColumns | 1–106 | Real column detection, _extra insertion point |
| `pkg/orm/postgres.go` | DBManager | — | Per-tenant pool registry (execution target) |
| `pkg/orm/transaction.go` | WithTransaction, TxFromContext | — | TX context pattern for query execution |

## Research Notes

No web research was needed. All implementation details are sufficiently specified by:
- The system design document's §10.1–10.2 (QueryBuilder struct, operators, ReportDef)
- The existing codebase patterns in `pkg/document/crud.go` (SQL generation, `pgx.Identifier` sanitization, parameterized queries)
- The `pkg/meta/` package (FieldDef, ColumnType mapping, StandardColumns for real-vs-_extra detection)
- PostgreSQL JSONB operator semantics (`->>` for text extraction, `::TYPE` for casting, `@>` for containment) are standard and well-documented in pgx

---

## Milestone Plan

### Task 1: Core QueryBuilder — Structure, 15 Operators, Parameterized SQL

- **Task ID:** MS-05-T1
- **Title:** Core QueryBuilder with fluent API, all operators, and parameterized SQL generation
- **Description:**
  Create `pkg/orm/query.go` with the `QueryBuilder` struct, `Filter`, `Operator`, `OrderClause` types, and the `Build()` method that generates parameterized SQL. Implement all 15 operators with correct `$N` placeholder management. Field validation checks fields against MetaType's known columns (real columns only in this task — `_extra` transparency is Task 2).

  **Key types:**
  - `Operator` string type with 15 constants (matching §10.1): `=`, `!=`, `>`, `<`, `>=`, `<=`, `like`, `not like`, `in`, `not in`, `between`, `is null`, `is not null`, `@>` (JSONB contains), `@@` (full-text — returns error until MS-15)
  - `Filter` struct: `Field`, `Operator`, `Value`
  - `OrderClause` struct: `Field`, `Direction`
  - `QueryBuilder` struct: holds `registry`, `site`, `doctype`, `meta`, `fields`, `filters`, `orderBy`, `groupBy`, `limit`, `offset`, `joins`, accumulated error

  **Fluent API:**
  - `NewQueryBuilder(registry *meta.Registry, site string) *QueryBuilder`
  - `For(doctype string) *QueryBuilder` — stores doctype (lazy MetaType load at Build time)
  - `Fields(fields ...string) *QueryBuilder`
  - `Where(filters ...Filter) *QueryBuilder`
  - `OrderBy(field, dir string) *QueryBuilder`
  - `GroupBy(fields ...string) *QueryBuilder`
  - `Limit(n int) *QueryBuilder` / `Offset(n int) *QueryBuilder`
  - `Build(ctx context.Context) (sql string, args []any, error)` — resolves MetaType, validates all fields, generates SELECT/WHERE/ORDER BY/LIMIT/OFFSET
  - `BuildCount(ctx context.Context) (sql string, args []any, error)` — same WHERE but `SELECT COUNT(*)`

  **Operator SQL generation:**
  - Simple comparisons (`=`, `!=`, `>`, `<`, `>=`, `<=`): `"field" <op> $N`
  - `like` / `not like`: `"field" LIKE $N` (caller provides `%` wildcards)
  - `in` / `not in`: expand slice to `"field" IN ($N, $N+1, ...)` — error on empty slice
  - `between`: 2-element value → `"field" BETWEEN $N AND $N+1`
  - `is null` / `is not null`: `"field" IS NULL` — value ignored
  - `@>` (JSONB contains): `"field" @> $N::jsonb` — value JSON-serialized
  - `@@` (full-text): returns error "full-text search not available until MS-15"

  **SQL injection protection:** All field names validated against MetaType, quoted via `pgx.Identifier{}.Sanitize()`. All values parameterized as `$N`. No string interpolation of user values.

- **Why this task exists:** The QueryBuilder is the central deliverable of MS-05. Everything else (JSONB transparency, joins, reports, GetList integration) builds on top of it. Starting with real-column-only validation keeps the first task focused and testable.
- **Dependencies:** None (first task)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §10.1 lines 1654–1700 (struct design, operator list)
  - `pkg/meta/columns.go` — `ColumnType()`, `StandardColumns()` for valid column set
  - `pkg/meta/fielddef.go` — FieldType constants
  - `pkg/document/crud.go:1003-1121` — current GetList SQL generation pattern to learn from
- **Deliverable:**
  - `pkg/orm/query.go` — QueryBuilder implementation
  - `pkg/orm/query_test.go` — Unit tests covering: each of the 15 operators, field validation errors, multi-filter combinations with correct `$N` indexing, empty filter edge case, limit/offset clamping, ORDER BY validation, GROUP BY
- **Risks / Unknowns:**
  - `$N` placeholder indexing across complex multi-operator filters must be carefully tracked (use an `argCounter` field on the builder, incremented per appended arg)
  - `in` with empty slice: recommend returning an error rather than generating `1=0`
  - `between` value validation: must be exactly 2 elements

---

### Task 2: `_extra` JSONB Transparency with Type Casting

- **Task ID:** MS-05-T2
- **Title:** Transparent `_extra` JSONB field access in QueryBuilder with type-aware casting
- **Description:**
  Extend the QueryBuilder's field resolution to detect whether a field is a real column or lives in `_extra` JSONB, and generate the appropriate SQL expression. For `_extra` fields, generate `_extra->>'field_name'` for text access and `(_extra->>'field_name')::TYPE` for typed comparisons.

  **Field classification logic** (`resolveFieldAccess`):
  1. Check if field is a standard column (`meta.StandardColumns()` / `meta.ChildStandardColumns()`)
  2. Check if field matches a `FieldDef` in `mt.Fields` where `meta.ColumnType(f.FieldType) != ""` → real column
  3. If field matches a `FieldDef` with `ColumnType == ""` (layout/Table type) → error: "field X is not queryable"
  4. If field is not found at all → treat as `_extra` field (custom/dynamic field), validate name against `^[a-z_][a-z0-9_]*$` regex

  **Type casting for `_extra` comparisons:**
  - Equality/LIKE operators: `_extra->>'field' = $N` (text, no cast needed)
  - Numeric comparisons (`>`, `<`, `>=`, `<=`, `between`): infer cast from Go value type — `int`/`float64` → `(_extra->>'field')::NUMERIC`, `bool` → `::BOOLEAN`, `time.Time` → `::TIMESTAMPTZ`
  - `in` / `not in`: `_extra->>'field' IN ($N, ...)` (text comparison)
  - `is null` / `is not null`: `_extra->>'field' IS NULL`
  - `@>` (JSONB contains): `_extra @> $N::jsonb` (operates on the _extra column itself, not extracted text)

  **`_extra` in SELECT:** `Fields("custom_color")` → `_extra->>'custom_color' AS "custom_color"`

- **Why this task exists:** The `_extra` JSONB transparency is a core Moca architectural promise (§4.4). Users and the API layer should be able to filter/sort on custom fields without knowing whether they are real columns or JSONB-stored. This is non-trivial because JSONB text extraction requires explicit type casting for non-text comparisons.
- **Dependencies:** MS-05-T1 (extends field resolution in QueryBuilder)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §4.4 lines 1008–1022 (`_extra` transparency contract)
  - `MOCA_SYSTEM_DESIGN.md` ADR-005 lines 2087–2092 (JSONB trade-offs)
  - `pkg/meta/columns.go` — `ColumnType()` for real-vs-extra detection
  - `pkg/document/crud.go:87-130` — `buildDocColumns()` pattern for column ordering
- **Deliverable:**
  - Updated `pkg/orm/query.go` — `resolveFieldAccess()` function, integrated into `Build()`
  - Updated `pkg/orm/query_test.go` — Tests for: real column generates `"field"`, _extra field generates `_extra->>'field'`, numeric comparison on _extra generates `::NUMERIC` cast, LIKE on _extra works without cast, non-queryable field (Table type) returns error, invalid field name regex returns error
- **Risks / Unknowns:**
  - **JSONB type casting runtime failures:** If `_extra->>'age'` contains `"abc"` and the query casts to `::NUMERIC`, PostgreSQL will error at runtime. This is a data quality issue, not a query builder issue — document this behavior.
  - **Unknown field names in `_extra`:** The builder cannot validate that a custom field actually exists in `_extra` data — it only validates the name format. This is by design (custom fields may be added per-document).

---

### Task 3: Link Field Auto-Joins (Depth ≤ 2)

- **Task ID:** MS-05-T3
- **Title:** Automatic JOIN generation for Link field dot-notation filters
- **Description:**
  When a filter or field uses dot notation (e.g., `customer.territory`), the QueryBuilder automatically resolves the Link chain: looks up the `customer` field on the source MetaType, verifies it is `FieldTypeLink`, reads `Options` to get the target DocType, loads that DocType's MetaType from the Registry, validates the sub-field, and generates a `LEFT JOIN`.

  **Join resolution (`resolveJoinChain`):**
  1. Split field path on `.` into segments
  2. Enforce max depth of 2 (e.g., `customer.territory.region` = depth 2, anything deeper → error)
  3. For each intermediate segment: find FieldDef, verify `FieldType == Link`, read `Options` for target DocType, load target MetaType via Registry, generate `LEFT JOIN`
  4. Join SQL: `LEFT JOIN "tab_<target>" AS "t1" ON "t1"."name" = "t0"."<link_field>"`
  5. Final segment resolved via `resolveFieldAccess` (Task 2) on the target MetaType — supports both real columns and `_extra` on the joined table
  6. Table aliasing: source table = `t0`, first join = `t1`, second = `t2`, etc.

  **Join deduplication:** Multiple filters on the same Link path (e.g., `customer.territory` and `customer.name`) must reuse the same JOIN alias. Track joins in a `map[string]JoinClause` keyed by the dot-prefix (e.g., `customer`).

  **DynamicLink:** Out of scope for MS-05. `FieldTypeDynamicLink` in a dot-notation path returns error: "DynamicLink fields do not support auto-joins".

  **Link fields in SELECT:** `Fields("customer.territory")` generates the JOIN and selects `"t1"."territory" AS "customer.territory"`.

- **Why this task exists:** Link field auto-joins are a headline feature of Moca's query engine (Frappe-inspired). Without them, filtering a Sales Order by customer territory would require manual SQL joins. The API layer (MS-06) needs this to support `?filters=[["customer.territory","=","West"]]` in URL parameters.
- **Dependencies:** MS-05-T2 (uses `resolveFieldAccess` for the final field in the join chain)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §10.1 lines 1654–1700 (JoinClause in QueryBuilder, "Link field auto-joins")
  - `pkg/meta/fielddef.go:25-26` — `FieldTypeLink`, `FieldTypeDynamicLink`
  - `pkg/meta/compiler.go:114-115` — Link requires non-empty `Options`
  - `pkg/meta/columns.go:19` — Link maps to TEXT column type
  - `ROADMAP.md` MS-05 risk: "Deeply nested joins (limit to depth 2)"
- **Deliverable:**
  - Updated `pkg/orm/query.go` — `resolveJoinChain()`, `JoinClause` struct, join deduplication map, integrated into `Build()` for both WHERE and SELECT
  - Updated `pkg/orm/query_test.go` — Tests for: single-level join generates correct LEFT JOIN, two-level join (`a.b.c`) generates two JOINs, same-prefix deduplication, depth > 2 returns error, non-Link field in path returns error, DynamicLink returns error, joined _extra field works with cast
- **Risks / Unknowns:**
  - **Registry calls during Build:** Each Link resolution requires loading the target DocType's MetaType. Since the Registry has L1 sync.Map cache, this should be sub-microsecond in practice. If the target DocType doesn't exist, return a clear error.
  - **Ambiguous column names:** Mandatory table aliasing (`t0`, `t1`, etc.) prevents ambiguity, but makes the generated SQL less readable for debugging. Consider adding a `QueryBuilder.DebugSQL()` method that returns annotated SQL.

---

### Task 4: GetList Integration, ReportDef, and End-to-End Tests

- **Task ID:** MS-05-T4
- **Title:** Wire QueryBuilder into GetList, implement ReportDef, write integration tests
- **Description:**
  This task has three parts:

  **Part A — Evolve ListOptions and refactor GetList:**
  - Add to `ListOptions`: `AdvancedFilters []orm.Filter` (new filter path), `Fields []string` (field selection), `GroupBy []string`, `OrderByMulti []orm.OrderClause` (multi-column sort)
  - Existing `Filters map[string]any` remains for backward compatibility — each entry converts to `orm.Filter{Field: k, Operator: orm.OpEquals, Value: v}`
  - Refactor `GetList` internals to construct a `QueryBuilder`, call `BuildCount()` and `Build()`, execute both, scan rows into `[]*DynamicDoc` as before
  - External behavior is identical for existing callers — same signature, same return types

  **Part B — ReportDef and QueryReport execution:**
  - Create `pkg/orm/report.go` with `ReportDef`, `ReportColumn`, `ReportFilter` structs (matching §10.2)
  - `ExecuteQueryReport(ctx, pool, def, params) ([]map[string]any, error)`:
    - Validates `def.Type == "QueryReport"` (ScriptReport → error: "not supported until MS-28")
    - Parses `def.Query` SQL template — named placeholders `%(param)s` converted to positional `$N`
    - Validates all required `ReportFilter` fields have values in `params`
    - Defense-in-depth: reject queries containing DDL keywords (`DROP`, `ALTER`, `TRUNCATE`, `DELETE`, `UPDATE`, `INSERT`) — QueryReport only allows SELECT
    - Executes query, scans rows into `[]map[string]any`

  **Part C — Integration tests:**
  - `pkg/orm/query_integration_test.go` (`//go:build integration`): end-to-end tests against real PostgreSQL
    - Create test MetaTypes with Link fields and `_extra` data
    - Test all operators produce correct results
    - Test `_extra` field filtering returns correct documents
    - Test Link auto-join filtering returns correct documents
    - Test pagination (offset + limit + total count)
  - `pkg/orm/report_integration_test.go`: QueryReport execution with parameter binding
  - Update `pkg/document/crud_integration_test.go` if GetList integration tests exist — verify backward compat

- **Why this task exists:** The QueryBuilder is useless without wiring it into the existing CRUD layer and providing ReportDef for the reporting pipeline. Integration tests prove the full stack works against real PostgreSQL — unit tests alone can't catch SQL dialect issues or JSONB casting failures.
- **Dependencies:** MS-05-T1, MS-05-T2, MS-05-T3
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §10.2 lines 1701–1725 (ReportDef struct)
  - `pkg/document/crud.go:64-81` — current ListOptions to evolve
  - `pkg/document/crud.go:1003-1121` — current GetList to refactor
  - `ROADMAP.md` MS-05 acceptance criteria (6 criteria to verify)
- **Deliverable:**
  - Updated `pkg/document/crud.go` — evolved `ListOptions`, refactored `GetList`
  - `pkg/orm/report.go` — `ReportDef`, `ReportColumn`, `ReportFilter`, `ExecuteQueryReport()`
  - `pkg/orm/report_test.go` — unit tests for ReportDef validation, SQL template parsing, DDL rejection
  - `pkg/orm/query_integration_test.go` — integration tests covering all acceptance criteria
  - `pkg/orm/report_integration_test.go` — integration test for QueryReport execution
- **Risks / Unknowns:**
  - **Backward compatibility:** Existing GetList callers using `Filters map[string]any` must produce identical SQL and results. Test this explicitly.
  - **QueryReport SQL template syntax:** The `%(param)s` placeholder convention (Frappe-inspired) vs `@param` (pgx named args) — recommend `%(param)s` for Frappe familiarity, converted to `$N` at execution time.
  - **ReportDef SQL validation:** Keyword-based DDL rejection is a heuristic. A crafted query could bypass it (e.g., `SELECT * FROM (DELETE ...)`). Since QueryReport SQL is developer-authored (not user input), this is defense-in-depth, not a security boundary.

---

## Recommended Execution Order

1. **MS-05-T1** — Core QueryBuilder (foundation — everything depends on this)
2. **MS-05-T2** — `_extra` JSONB transparency (extends T1's field resolution)
3. **MS-05-T3** — Link field auto-joins (uses T2's `resolveFieldAccess`)
4. **MS-05-T4** — GetList integration + ReportDef + integration tests (wires T1–T3 into production code)

Tasks are strictly sequential. T1+T2 could potentially be one PR if the implementer prefers, as they're tightly coupled. T4 Part B (ReportDef) is somewhat independent and could be started in parallel with T3 if desired.

```
T1 ──→ T2 ──→ T3 ──→ T4
                       ├── Part A: GetList integration (needs T1-T3)
                       ├── Part B: ReportDef (needs T1 only, can overlap T3)
                       └── Part C: Integration tests (needs all)
```

## Open Questions

1. **`_extra` field name validation:** Should the QueryBuilder accept any field name as `_extra` (with regex validation), or should it require that custom fields be registered somewhere? The current plan uses regex-only validation since custom fields may not be in the MetaType definition. This matches the Frappe model where custom fields are loosely coupled.

2. **GetList signature change:** Should `ListOptions` gain the new fields (backward-compatible addition), or should a new `QueryOptions` type replace it? Recommend evolving `ListOptions` — avoids a breaking change and the existing equality-filter path converts cleanly to `orm.Filter` internally.

3. **QueryBuilder execution:** Should `Build()` return `(sql, args)` for the caller to execute, or should QueryBuilder have an `Execute()` method that runs the query? The design doc shows `Build() → (sql, args)`, which is more flexible — the caller controls the connection/transaction. Recommend keeping this design.

## Out of Scope for This Milestone

- **ScriptReport** — deferred to MS-28 (requires Go plugin/scripting infrastructure)
- **Full-text search operator `@@`** — defined as a constant but returns error at Build time; implemented in MS-15 (Meilisearch integration)
- **`similar` operator (trigram similarity)** — listed in §10.1 but not in MS-05 deliverables; defer to MS-15
- **Permission-filtered queries (row-level security)** — mentioned in §10.1 but depends on MS-14 (Auth & Permissions)
- **Child table sub-queries** — mentioned in §10.1 but not in MS-05 scope; defer to MS-06 or MS-20
- **GraphQL query generation** — MS-20
- **Query result caching** — optimize after MS-06 proves the API patterns
- **Hot reload of MetaType affecting cached query plans** — MS-10
