# MS-20 — GraphQL, Dashboard, Report, Translation, File Storage Plan

## Milestone Summary

- **ID:** MS-20
- **Name:** GraphQL, Dashboard, Report, Translation, File Storage
- **Roadmap Reference:** ROADMAP.md → MS-20 section (lines 1049-1085)
- **Goal:** GraphQL auto-generation from MetaType definitions, Desk Dashboard/Report views, translation/i18n system, S3 file storage with upload API
- **Why it matters:** Rounds out the platform for Beta. GraphQL is a differentiator over Frappe. Reports make Moca useful for business users. i18n enables international adoption. File storage completes the Attach field types.
- **Position in roadmap:** Order #10 of 30 milestones (Beta phase, MS-18 through MS-23)
- **Upstream dependencies:** MS-06 (REST API - complete), MS-17 (React Desk - complete), MS-05 (Query Engine - complete)
- **Downstream dependencies:** None directly. MS-25 (Testing) and MS-26 (Docs) eventually depend on all milestones.
- **Estimated duration:** 4 weeks

## Vision Alignment

MS-20 is a "rounding out" milestone that adds five horizontal capabilities cutting across the entire stack. GraphQL auto-generation extends the metadata-driven API philosophy — just as REST endpoints are auto-generated from MetaType definitions, GraphQL schemas will be too, reusing the same permission/validation/lifecycle pipeline. This is the single biggest API differentiator over Frappe, which has no GraphQL support.

Dashboard and Report views complete the Desk frontend's data presentation layer. The backend report engine already exists (`pkg/orm/report.go`) but has no frontend and no HTTP endpoint. Dashboards make the framework practical for business users who need at-a-glance metrics without writing code.

The translation/i18n system enables international deployment — a key requirement for an enterprise framework. File storage completes the attachment lifecycle that the Attach/AttachImage field types already stub out in the frontend.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `ROADMAP.md` | MS-20 | 1049-1085 | Milestone definition, deliverables, acceptance criteria |
| `MOCA_SYSTEM_DESIGN.md` | §3.3 Customizable API Layer | 466-540 | API architecture diagram showing GraphQL Gateway as parallel path to REST |
| `MOCA_SYSTEM_DESIGN.md` | §4.3 Per-Tenant Schema | 994-1005 | `tab_file` table DDL |
| `MOCA_SYSTEM_DESIGN.md` | §7.1 ModuleDef | 1301-1311 | ModuleDef struct with Reports[] and Dashboards[] |
| `MOCA_SYSTEM_DESIGN.md` | §9.1 Desk Architecture | 1470-1506 | Route map: `/app/dashboard/{name}`, `/app/report/{name}`, I18nProvider |
| `MOCA_SYSTEM_DESIGN.md` | §9.6 Translation Architecture | 1621-1651 | `tab_translation` DDL, extraction sources, caching strategy |
| `MOCA_SYSTEM_DESIGN.md` | §10.2 Report Builder | 1710-1725 | ReportDef struct definition |
| `MOCA_SYSTEM_DESIGN.md` | §15 Package Layout | 2008-2011 | `pkg/storage/` file list: s3.go, manager.go, thumbnail.go |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.20 Translation Management | 3126-3186 | CLI commands: translate export/import/status/compile |

## Research Notes

No external web research was needed. All implementation details are well-specified in the design documents. Key observations from codebase exploration:

- **GraphQL library choice:** `graphql-go/graphql` (runtime schema construction) is the right fit — Moca builds schemas dynamically from MetaType at runtime, not at compile time. `gqlgen` requires code generation which conflicts with the metadata-driven approach.
- **Report backend exists:** `pkg/orm/report.go` already has `ReportDef`, `ExecuteQueryReport()`, SQL template parsing, DDL rejection, and parameter validation. Only needs an HTTP handler wrapper and frontend.
- **StorageConfig exists:** `internal/config/types.go:101-115` already defines `StorageConfig` with Driver, Endpoint, Bucket, AccessKey, SecretKey fields. No config changes needed.
- **Desk router is simple:** `desk/src/router.tsx` uses react-router with lazy loading — easy to add dashboard/report routes.
- **AttachField has no upload:** `desk/src/components/fields/AttachField.tsx` stores filename only. File storage backend is needed to make it functional.
- **MO format:** Simple binary format (magic number + hash table + string offsets). Pure Go implementation is straightforward — no need for CGo gettext bindings.

## Milestone Plan

### Task 1

- **Task ID:** MS-20-T1
- **Title:** S3 File Storage Backend + Upload API
- **Status:** Completed
- **Description:**
  Implement the file storage subsystem from scratch. `pkg/storage/` currently contains only `doc.go` (stub). Create:
  1. **`pkg/storage/storage.go`** — `Storage` interface with Upload, Download, Delete, PresignedGetURL, PresignedPutURL methods. Factory function `NewStorage(cfg)` dispatching on `cfg.Driver` ("s3" vs "local").
  2. **`pkg/storage/s3.go`** — S3 adapter using `minio/minio-go/v7`. Wraps `*minio.Client` with per-site bucket prefix namespacing (`{site}/private/...` and `{site}/public/...`).
  3. **`pkg/storage/local.go`** — Local filesystem adapter for development mode, writing to `sites/{site}/files/`.
  4. **`pkg/storage/manager.go`** — `FileManager` struct owning Storage + DB. Methods: `HandleUpload` (validate size/type, generate unique object key, upload, insert `tab_file` row, return metadata), `HandleDownload` (permission check on `attached_to_doctype`, stream file), `HandleDelete` (permission check, remove from storage + DB), `GetSignedURL`.
  5. **`pkg/storage/thumbnail.go`** — `GenerateThumbnail(reader, maxWidth, maxHeight)` using `golang.org/x/image/draw`. Generate on upload for AttachImage fields, store as `{key}_thumb.{ext}`.
  6. **`pkg/storage/ddl.go`** — DDL for `tab_file` matching design doc (lines 994-1005), plus index on `(attached_to_doctype, attached_to_name)`.
  7. **`pkg/api/upload.go`** — `UploadHandler` with routes: `POST /api/v1/file/upload` (multipart), `GET /api/v1/file/{name}` (download), `DELETE /api/v1/file/{name}`, `GET /api/v1/file/{name}/url` (signed URL). Reuse `writeSuccess`/`writeError` from `pkg/api/response.go`, `SiteFromContext`/`UserFromContext` from `pkg/api/context.go`.

  Modify `internal/serve/server.go` to wire FileManager + UploadHandler. Add `minio/minio-go/v7` to `go.mod`.

- **Why this task exists:** File storage is foundational — Attach/AttachImage field types in the Desk frontend are non-functional without it. Dashboard chart exports and report CSV exports also benefit from storage.
- **Dependencies:** None (can start immediately)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §4.3 lines 994-1005 — `tab_file` schema
  - `MOCA_SYSTEM_DESIGN.md` §15 lines 2008-2011 — Package layout (s3.go, manager.go, thumbnail.go)
  - `internal/config/types.go` lines 101-115 — Existing `StorageConfig` struct
  - `pkg/storage/doc.go` lines 1-11 — Existing package stub
  - `pkg/api/response.go` — Response helpers to reuse
  - `pkg/api/rest.go` — Handler patterns to follow
- **Deliverable:**
  - `pkg/storage/storage.go`, `s3.go`, `local.go`, `manager.go`, `thumbnail.go`, `ddl.go`
  - `pkg/api/upload.go`
  - Unit tests: `pkg/storage/*_test.go`, `pkg/api/upload_test.go`
  - Integration test: `pkg/storage/integration_test.go` (with MinIO container)
- **Acceptance Criteria:**
  - `POST /api/v1/file/upload` accepts multipart file, stores in S3/local, creates `tab_file` entry, returns file metadata JSON
  - `GET /api/v1/file/{name}` streams file with access control (private files require read perm on `attached_to_doctype`)
  - `DELETE /api/v1/file/{name}` removes file from storage and DB
  - Signed URL generation returns time-limited URL
  - Thumbnail auto-generated for image uploads
  - `tab_file` table created during site provisioning
  - Local filesystem driver works in development mode (Driver="local")
  - All unit and integration tests pass
- **Risks / Unknowns:**
  - MinIO testcontainer setup for CI (Docker required)
  - Max upload size needs to be configurable (separate from the existing 1MB `maxRequestBody` in rest.go)
  - Content-type validation whitelist to prevent uploading executables

### Task 2

- **Task ID:** MS-20-T2
- **Title:** GraphQL Schema Auto-Generation + Playground
- **Status:** Not Started
- **Description:**
  Auto-generate a GraphQL schema from the MetaType registry and expose it with a playground. Create:
  1. **`pkg/api/graphql.go`** — `GraphQLHandler` struct wrapping `CRUDService`, `MetaResolver`, `PermissionChecker`. Routes: `POST /api/graphql` (query/mutation endpoint), `GET /api/graphql/playground` (GraphiQL). Schema builder: `buildSchema(ctx, site, registry)` iterates all API-enabled DocTypes, generates Object types from MetaType fields, Query types (`{doctype}(name)` and `all{DocType}s(limit, offset, filters)`), Mutation types (`create/update/delete{DocType}`).
  2. **`pkg/api/graphql_schema.go`** — Type mapping helpers: `fieldTypeToGraphQL(ft)` maps Moca FieldTypes to GraphQL scalars, `buildObjectType(mt)`, `buildInputType(mt)`, `buildEnumType()` for Select fields.
  3. **`pkg/api/graphql_resolver.go`** — Resolvers that delegate to `CRUDService` (same as REST handlers). Each resolver: extract site+user from context, build `DocContext`, call `CRUDService.Get/GetList/Insert/Update/Delete`. Link fields resolve via lazy-load with DataLoader batching to prevent N+1. Table (child) fields resolve as arrays.
  4. **`pkg/api/graphql_playground.go`** — Embedded GraphiQL HTML served via `embed` directive.

  Add `ListAll(ctx, site) ([]*MetaType, error)` method to `pkg/meta/registry.go` (current Registry only supports `Get` by name). Wire into `internal/serve/server.go`. Add `graphql-go/graphql` and `graph-gophers/dataloader/v7` to `go.mod`.

- **Why this task exists:** GraphQL is the flagship API differentiator over Frappe. The design document (§3.3 lines 500-510) shows it as a first-class path through the same request pipeline as REST.
- **Dependencies:** None (can start in parallel with T1)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §3.3 lines 466-540 — API architecture with GraphQL Gateway
  - `pkg/api/rest.go` lines 24-55 — `CRUDService` interface, `MetaResolver`, `newDocContext` patterns
  - `pkg/api/openapi.go` lines 122-186 — `FieldTypeToOpenAPI` type mapping logic to adapt
  - `pkg/meta/registry.go` — Registry.Get() to extend with ListAll()
  - `pkg/meta/stubs.go` lines 122-143 — `APIConfig` (Enabled, AllowList, etc.)
- **Deliverable:**
  - `pkg/api/graphql.go`, `graphql_schema.go`, `graphql_resolver.go`, `graphql_playground.go`
  - Modified `pkg/meta/registry.go` (ListAll method)
  - Unit tests: `pkg/api/graphql_test.go`, `graphql_resolver_test.go`
  - Integration test: `pkg/api/graphql_integration_test.go`
- **Acceptance Criteria:**
  - `POST /api/graphql` accepts queries and returns correct JSON (per ROADMAP acceptance criteria)
  - Every API-enabled DocType has query, list, create, update, delete operations
  - GraphQL mutation creates document with full lifecycle (validation, hooks, naming)
  - Link fields resolve to full referenced objects with permission checks
  - Table (child) fields resolve as arrays of child objects
  - Select fields expose as GraphQL Enum types
  - Permission checks on every query/mutation — no bypass via GraphQL
  - Field-level permissions strip unauthorized fields
  - `/api/graphql/playground` serves interactive GraphiQL UI
  - Schema regenerates when MetaTypes change (cache invalidation via existing schema version counter)
- **Risks / Unknowns:**
  - **N+1 queries** for Link field resolution — DataLoader batching is critical for performance
  - **DynamicLink fields** (target DocType depends on another field value) — may need to resolve as generic JSON scalar initially
  - **Schema size** with many DocTypes — may need lazy construction or per-request schema filtering
  - `graphql-go/graphql` has no built-in subscription support (acceptable — WebSocket real-time already exists via MS-19)

### Task 3

- **Task ID:** MS-20-T3
- **Title:** Translation / i18n System (Backend + CLI + Frontend)
- **Status:** Not Started
- **Description:**
  Build the complete internationalization stack. Create:

  **Backend (`pkg/i18n/`):**
  1. **`pkg/i18n/loader.go`** — `Translator` struct with Redis cache + DB. `Translate(ctx, site, lang, source, context)` with fallback chain: Redis `i18n:{site}:{lang}` → PostgreSQL `tab_translation` → return source unchanged. `LoadAll(ctx, site, lang)` for bulk load. `Invalidate(ctx, site, lang)` for cache clearing.
  2. **`pkg/i18n/extractor.go`** — `Extractor` with methods: `ExtractFromMetaTypes(mts)` extracts field labels, descriptions, select options, section headings; `ExtractFromTSX(dir)` scans for `t("...")` patterns via regex; `ExtractFromTemplates(dir)` scans for `{{ _("text") }}` markers.
  3. **`pkg/i18n/format.go`** — Import/export in PO, CSV, JSON formats. `ExportPO/ImportPO`, `ExportCSV/ImportCSV`, `ExportJSON/ImportJSON`.
  4. **`pkg/i18n/compiler.go`** — MO binary format: `CompileMO(translations, writer)` and `LoadMO(reader)`. Pure Go implementation from GNU MO spec.
  5. **`pkg/i18n/ddl.go`** — `tab_translation` DDL per design doc lines 1628-1635: composite PK on `(source_text, language, context)`.
  6. **`pkg/i18n/middleware.go`** — `I18nMiddleware` reading `Accept-Language` header or user preference, storing language in context.
  7. **`pkg/i18n/transformer.go`** — Response transformer translating MetaType field labels and select options in API responses.

  **CLI (`cmd/moca/`):**
  Replace placeholder stubs in `cmd/moca/translate.go` with real implementations:
  - `export`: extract from MetaTypes + source files, write PO/CSV/JSON
  - `import`: read translation file, upsert into `tab_translation`, invalidate cache
  - `compile`: read PO → compile to MO binary
  - `status`: query coverage stats, render table

  **Frontend (`desk/src/`):**
  8. **`desk/src/providers/I18nProvider.tsx`** — React context providing `t(source)` function. Fetches translation bundle from `GET /api/v1/translations/{lang}`. Uses React Query with long staleTime.
  9. **`desk/src/hooks/useTranslation.ts`** — Convenience hook wrapping I18nContext.

  Add new API endpoint `GET /api/v1/translations/{lang}` returning all translations as JSON object.

- **Why this task exists:** i18n is required for international enterprise deployment. The design specifies a complete pipeline from string extraction through CLI workflow to runtime translation.
- **Dependencies:** None (self-contained, can start in parallel)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §9.6 lines 1621-1651 — Translation architecture, `tab_translation` DDL, caching strategy
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.20 lines 3126-3186 — CLI command specs (export, import, status, compile)
  - `MOCA_SYSTEM_DESIGN.md` §9.1 line 1504 — I18nProvider in Desk core infrastructure
  - `pkg/meta/registry.go` — Redis caching pattern (`i18n:{site}:{lang}` follows same L1/L2 approach)
  - `internal/output/table.go` — Table formatter for `translate status` output
  - `desk/src/providers/` — Existing provider patterns (Auth, Doc, Meta)
- **Deliverable:**
  - `pkg/i18n/` package: loader.go, extractor.go, format.go, compiler.go, ddl.go, middleware.go, transformer.go, doc.go
  - Updated `cmd/moca/translate.go` with real command implementations
  - `desk/src/providers/I18nProvider.tsx`, `desk/src/hooks/useTranslation.ts`
  - Unit tests: `pkg/i18n/*_test.go`
  - Integration test: `pkg/i18n/integration_test.go`
- **Acceptance Criteria:**
  - `moca translate export --app crm --lang ar` extracts strings to PO file (per ROADMAP)
  - `moca translate import FILE --language ar` imports translations into `tab_translation`
  - `moca translate status` shows coverage table (APP, LANGUAGE, TRANSLATED, TOTAL, COVERAGE%)
  - `moca translate compile` produces binary MO files (per ROADMAP)
  - `Accept-Language` header causes translated field labels in API responses
  - Desk UI `t("string")` returns translated text for user's language (per ROADMAP)
  - Redis cache `i18n:{site}:{lang}` populated on first request, invalidated on import/compile
  - `tab_translation` table created during site provisioning
- **Risks / Unknowns:**
  - MO binary format byte-order handling — well-documented but needs careful implementation. Could defer MO and use JSON/DB as primary format initially.
  - TSX extraction regex won't catch template literals like `` t(`text ${var}`) `` — document limitation, extract simple string literals only
  - RTL language support in frontend layout is out of scope but architecture must not prevent it

### Task 4

- **Task ID:** MS-20-T4
- **Title:** Dashboard & Report Frontend Views + Backend Handlers
- **Status:** Not Started
- **Description:**
  Build Dashboard and Report pages for Desk, plus their backend API handlers. The report backend engine exists (`pkg/orm/report.go`) but has no HTTP endpoint or frontend.

  **Backend:**
  1. **`pkg/api/report.go`** — `ReportHandler`: `GET /api/v1/report/{name}/meta` returns ReportDef (columns, filters, chart config); `POST /api/v1/report/{name}/execute` runs QueryReport with parameters via `orm.ExecuteQueryReport()`, returns `{ data, columns, chart_config }`. Permission check: user must have read perm on the report's DocType.
  2. **`pkg/api/dashboard.go`** — `DashboardHandler`: `GET /api/v1/dashboard/{name}` returns DashDef (widget configurations); `GET /api/v1/dashboard/{name}/widget/{idx}` returns computed widget data (count, sum, etc. via aggregate queries).
  3. Dashboard definition types: `DashDef { Name, Label string; Widgets []DashWidget }`, `DashWidget { Type string; Config map[string]any }` — types: "number_card", "chart", "list", "shortcut".

  **Frontend:**
  4. **`desk/src/pages/ReportView.tsx`** — Fetch report meta, render filter controls (reuse FilterBar pattern from `ListView.tsx:25-99`), "Run Report" button, data table from ReportColumn[], chart from ChartConfig using Recharts, CSV export.
  5. **`desk/src/pages/DashboardView.tsx`** — Fetch DashDef, render widget grid. Widget types: NumberCard (metric + label + trend), ChartWidget (Recharts bar/line/pie), ListWidget (recent docs via `useDocList`), ShortcutCard (link to route).
  6. **`desk/src/components/dashboard/`** — NumberCard.tsx, ChartWidget.tsx, ListWidget.tsx, ShortcutCard.tsx
  7. **`desk/src/components/report/`** — ReportFilters.tsx, ReportChart.tsx, ReportTable.tsx
  8. **`desk/src/providers/ReportProvider.tsx`** — `useReportMeta(name)`, `useReportExecute(name)` hooks
  9. **`desk/src/providers/DashboardProvider.tsx`** — `useDashboardDef(name)`, `useDashboardWidget(name, idx)` hooks
  10. Add types to `desk/src/api/types.ts`: ReportDef, ReportColumn, ReportFilter, ChartConfig, DashDef, DashWidget

  Update `desk/src/router.tsx` to add routes:
  - `/app/dashboard/:name` → `<DashboardView />`
  - `/app/report/:name` → `<ReportView />`

  Add `recharts` to `desk/package.json`. Wire backend handlers in `internal/serve/server.go`.

- **Why this task exists:** Reports and dashboards make Moca useful for business users. The report backend exists but is inaccessible without HTTP endpoints and a frontend.
- **Dependencies:** None hard. Benefits from T1 (file storage for export) but not blocked.
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §10.2 lines 1710-1725 — ReportDef struct
  - `MOCA_SYSTEM_DESIGN.md` §7.1 lines 1301-1311 — ModuleDef with Reports[] and Dashboards[]
  - `MOCA_SYSTEM_DESIGN.md` §9.1 lines 1488-1492 — Desk route map for dashboard/report
  - `pkg/orm/report.go` lines 1-176 — Existing ReportDef, ExecuteQueryReport, SQL parsing
  - `desk/src/pages/ListView.tsx` lines 25-99 — FilterBar pattern to reuse for report filters
  - `desk/src/router.tsx` — Current routes to extend
  - `desk/src/providers/DocProvider.tsx` — Hook patterns to follow
- **Deliverable:**
  - `pkg/api/report.go`, `pkg/api/dashboard.go`
  - `desk/src/pages/ReportView.tsx`, `desk/src/pages/DashboardView.tsx`
  - `desk/src/components/dashboard/` (4 widget components)
  - `desk/src/components/report/` (3 components)
  - `desk/src/providers/ReportProvider.tsx`, `DashboardProvider.tsx`
  - Updated `desk/src/router.tsx`, `desk/src/api/types.ts`
  - Unit tests: `pkg/api/report_test.go`, `pkg/api/dashboard_test.go`
- **Acceptance Criteria:**
  - Dashboard widget shows filtered document count (per ROADMAP)
  - Report renders data table + chart (per ROADMAP)
  - Report filters generate correct query parameters
  - Report data table displays sortable columns with correct types
  - Report chart renders bar/line/pie per ChartConfig
  - CSV export from report results works
  - NumberCard widget displays metric with label
  - Dashboard/Report routes accessible from Desk navigation
  - Permission checks enforced on report execution and dashboard data
- **Risks / Unknowns:**
  - Recharts bundle size (~40KB gzipped) — acceptable for a desk app
  - Report result pagination — `ExecuteQueryReport` returns all rows; may need LIMIT/OFFSET wrapper for large datasets
  - DashDef storage — store as special MetaType in `tab_doctype` or create dedicated `tab_dashboard` table
  - Dashboard widget data aggregation queries need to be safe (same DDL rejection as reports)

## Recommended Execution Order

1. **MS-20-T1** (S3 Storage) — Start first. Pure backend, no frontend dependencies, longest backend work. Unblocks the Attach field types.
2. **MS-20-T3** (Translation) — Start in parallel with T1. Full-stack but self-contained. The CLI commands are a distinct deliverable.
3. **MS-20-T2** (GraphQL) — Start week 2. Depends on stable MetaType registry (already stable). Needs careful design for nested type resolution.
4. **MS-20-T4** (Dashboard + Report) — Start week 2. Frontend-heavy. Benefits from having backend tasks stabilized.

All four tasks can be developed in parallel since they have no hard inter-dependencies. The ordering optimizes for integration risk.

## Open Questions

1. **DashDef storage:** Should dashboard definitions live in `tab_doctype` as a special MetaType, or in a dedicated `tab_dashboard` system table? The ModuleDef struct (line 1310) references `DashDef` but the storage mechanism isn't specified.
2. **GraphQL DynamicLink fields:** The target DocType depends on another field's runtime value. Should these resolve as a generic `JSON` scalar, or attempt runtime type resolution?
3. **MO compilation priority:** Is `.mo` binary compilation a hard requirement for Beta, or can we defer it and use JSON/database as the primary translation backend?
4. **Report result pagination:** Should `ExecuteQueryReport` be extended with LIMIT/OFFSET, or handle pagination at the HTTP handler level?

## Out of Scope for This Milestone

- ScriptReport type (security concern — explicitly excluded in ROADMAP)
- Portal SSR translations (deferred to MS-27)
- Visual workflow builder (post-v1.0)
- GraphQL subscriptions (WebSocket real-time already exists via MS-19)
- RTL layout support in Desk (future milestone)
- File virus scanning / malware detection
