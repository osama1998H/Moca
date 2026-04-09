# Developer Experience Test Session Report — v0.1.8

**Date:** 2026-04-10  
**Version tested:** v0.1.8  
**Method:** Clean install via `install.sh` from GitHub Release, full workflow in `/tmp/moca-dx-test`  
**Tester:** Claude + manual desk testing  

## Test Steps Performed

1. Tagged v0.1.8, pushed, CI passed (all 6 jobs) ✓
2. Installed moca v0.1.8 via `install.sh` ✓
3. Started Docker (PG, Redis, MinIO, Meilisearch) ✓
4. `moca init .` with DB/Redis config ✓
5. `moca desk install` ✓
6. `moca site create library.localhost` ✓
7. `moca app new library --doctype Book --desk` — **failed** (Issue #8)
8. Created additional doctypes (Author, Member, Loan) manually ✓
9. `moca app install library --site library.localhost` — **failed** then succeeded (Issues #9, #10)
10. `moca serve` + `moca desk dev` ✓
11. API CRUD testing — **failed** initially (Issues #9, #11, #12)
12. Desk login — **failed** initially (Issues #15, #16)
13. Desk sidebar — library doctypes not visible (Issue #11)
14. Desk child tables (DocField, DocPerm) — shimmer/loading (Issue #13)
15. Desk app extensions (custom page, sidebar) ✓ after `moca build desk`
16. GraphQL playground — accessible at `/api/graphql/playground` ✓
17. Swagger/OpenAPI — no endpoint exposed (Issue #17)

## Issues Found

### Previously Fixed (in local source, uncommitted)

| # | Issue | Severity | Component | Status |
|---|-------|----------|-----------|--------|
| 8 | App `go.mod` version missing `v` prefix — `resolveAppNewFrameworkDependency` returns `"0.1.8"` but Go modules require `"v0.1.8"` | Blocker | `cmd/moca/app.go:179` | **Fixed** — prepend `v` when version doesn't start with it |
| 9 | Scaffold doctype template missing `api_config` — new doctypes get `api_config: null`, API returns 404 even though doctype exists in registry | Major | `internal/scaffold/templates.go` | **Fixed** — template now includes `api_config: { enabled: true, allow_list: true, ... }` |
| 10 | Scaffold `title_field` references non-existent `"name"` field — `moca app install` fails with `title_field: references unknown field: "name"` | Major | `internal/scaffold/templates.go` | **Fixed** — changed to `"title"` which matches the scaffolded field |

---

### Open Issues

### Issue #11: `moca app install` doesn't seed DocType document records

**Severity:** Blocker  
**File:** `pkg/apps/installer.go`  

When installing an app, the installer registers MetaTypes in `tab_doctype` (the meta registry) and creates database tables. But it does NOT create corresponding records in `tab_doc_type` (the DocType document table).

The desk sidebar fetches doctypes via `GET /api/v1/resource/DocType` which reads from `tab_doc_type`. Without records there, newly installed app doctypes are invisible in the UI.

**Impact:** Library doctypes (Book, Author, Member, Loan) don't appear in the desk sidebar after `moca app install`.

**Fix:** The app installer should create a `tab_doc_type` document record for each registered MetaType, mirroring what `bootstrapCoreMeta` does for core doctypes during site creation (step 10/10: "seeding DocType document records").

---

### Issue #12: `moca app install` doesn't seed `tab_doc_perm` permission records

**Severity:** Blocker  
**File:** `pkg/apps/installer.go`  

The app installer registers MetaTypes with permissions defined in the JSON definition (`"permissions": [{"role": "System Manager", "doctype_perm": 63}]`), but does NOT create corresponding records in `tab_doc_perm`.

The permission engine (`pkg/api/rest.go:334`) reads from `tab_doc_perm` to check user access. Without records there, the API returns 403 for all app doctypes and the meta endpoint returns `PERMISSION_DENIED`.

**Impact:** All API calls to app doctypes fail with "permission denied". The desk can't fetch meta definitions for app doctypes.

**Fix:** During `moca app install`, for each MetaType's `permissions` array, insert a `tab_doc_perm` record with the corresponding role and permission bitmask.

---

### Issue #13: Core child doctypes (DocField, DocPerm, HasRole) missing permission records

**Severity:** Major  
**File:** `pkg/builtin/core/bootstrap.go`  

During site creation, permission records (`tab_doc_perm`) are seeded for top-level core doctypes (DocType, User, Role, ModuleDef, SSOProvider) but NOT for child table doctypes (DocField, DocPerm, HasRole).

The desk's `TableField` component calls `useMetaType("DocField")` which hits `GET /api/v1/meta/DocField`. This triggers a permission check that fails because no `tab_doc_perm` record exists for DocField.

**Impact:** Child table fields (Fields, Permissions) on the DocType form render as a shimmer/loading placeholder forever instead of showing the inline editable table.

**Fix:** The bootstrap seeding should include `tab_doc_perm` records for all child table doctypes, or the meta endpoint should skip permission checks for child tables.

---

### Issue #14: `setSite` not exported from `@osama1998h/desk`

**Severity:** Major  
**File:** `desk/src/index.ts`  

The API client has `export function setSite(site: string)` in `api/client.ts`, but `index.ts` does not re-export it. Projects cannot programmatically set the site name for API requests.

**Impact:** The only way to set the site name is via `VITE_MOCA_SITE` env var, which requires Vite's env injection pipeline to work through pre-bundled dependencies.

**Fix:** Add `export { setSite } from "./api/client";` to `desk/src/index.ts`.

---

### Issue #15: Scaffolded `desk/main.tsx` doesn't configure `siteName`

**Severity:** Major  
**File:** `internal/scaffold/desk_templates.go`  

The scaffolded `main.tsx` calls `createDeskApp({})` without setting `siteName`. The `createDeskApp` config accepts `siteName` which flows to the React context, but does NOT call `setSite()` on the API client — so even if set, the API client doesn't know about it.

**Impact:** Login fails with "X-Moca-Site header or subdomain required" on every fresh project.

**Fix (two parts):**
1. `createDeskApp()` should call `setSite(config.siteName)` internally when `siteName` is provided
2. Scaffold template should pass `siteName` from env: `createDeskApp({ siteName: import.meta.env.VITE_MOCA_SITE })`

---

### Issue #16: Scaffold doesn't generate `desk/.env` with `VITE_MOCA_SITE`

**Severity:** Major  
**File:** `internal/scaffold/desk.go`  

`moca init` scaffolds the `desk/` directory but does not create a `.env` file. The `VITE_MOCA_SITE` env var is the intended mechanism for site name configuration in development, but it's never set.

**Impact:** Developers must manually create `desk/.env` with `VITE_MOCA_SITE=<site-name>` and clear Vite's cache (`.vite/`) before the site header works.

**Fix:** `ScaffoldDesk` should generate a `desk/.env` file:
```
VITE_MOCA_SITE=
```
And `moca site use <name>` or `moca site create <name>` should update this file if a desk/ directory exists.

---

### Issue #17: OpenAPI/Swagger spec has no HTTP endpoint

**Severity:** Minor  
**File:** `internal/serve/server.go`  

The OpenAPI 3.0.3 spec generator is fully implemented (`pkg/api/openapi.go` with `GenerateSpec()`) including all CRUD paths, schema generation from MetaTypes, and custom endpoint documentation. However, no HTTP route serves the spec or a Swagger UI page.

The GraphQL playground is registered at `GET /api/graphql/playground`, but there is no equivalent for REST API documentation.

**Impact:** Developers cannot browse or test the REST API interactively. No Swagger UI or `/api/docs` endpoint exists.

**Fix:** Register two routes in `server.go`:
1. `GET /api/openapi.json` — serves the generated OpenAPI spec as JSON
2. `GET /api/docs` — serves a Swagger UI HTML page (similar to how `graphiql.html` is served for GraphQL)

---

## Summary

| # | Issue | Severity | Status |
|---|-------|----------|--------|
| 8 | App go.mod missing `v` prefix | Blocker | **Fixed** (local, uncommitted) |
| 9 | Scaffold missing `api_config` | Major | **Fixed** (local, uncommitted) |
| 10 | Scaffold `title_field` invalid | Major | **Fixed** (local, uncommitted) |
| 11 | App install doesn't seed `tab_doc_type` | Blocker | Open |
| 12 | App install doesn't seed `tab_doc_perm` | Blocker | Open |
| 13 | Core child doctypes missing permissions | Major | Open |
| 14 | `setSite` not exported from desk package | Major | Open |
| 15 | Scaffold `main.tsx` missing `siteName` | Major | Open |
| 16 | Scaffold missing `desk/.env` | Major | Open |
| 17 | OpenAPI spec has no HTTP endpoint | Minor | Open |

**3 fixed locally**, **7 open** (2 blockers, 4 major, 1 minor).

## Recommended Fix Priority

1. **Issue #11 + #12** (app install seeding) — Highest priority. These are blockers that break every new app install. Fix together in `pkg/apps/installer.go`.
2. **Issue #13** (child doctype permissions) — Fix in `pkg/builtin/core/bootstrap.go` alongside #12.
3. **Issue #14 + #15** (setSite export + createDeskApp wiring) — Fix together. Export `setSite` and call it inside `createDeskApp()`.
4. **Issue #16** (scaffold .env) — Quick fix in `internal/scaffold/desk.go`.
5. **Issue #17** (OpenAPI endpoint) — Low priority, additive feature.
