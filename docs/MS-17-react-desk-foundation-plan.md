# MS-17 — React Desk Foundation Plan

## Milestone Summary

- **ID:** MS-17
- **Name:** React Desk Foundation -- App Shell, MetaProvider, FormView, ListView
- **Roadmap Reference:** ROADMAP.md → MS-17 section (lines 933–978)
- **Goal:** Build the React Desk app shell, providers (Meta, Doc, Auth), metadata-driven FormView and ListView, and the field type component library. First time users see the Desk UI.
- **Why it matters:** This is the first frontend milestone — it turns Moca from a backend-only framework into a usable application platform. Users can see, create, and edit documents through a browser for the first time.
- **Position in roadmap:** Order #18 of 30 milestones (Alpha release phase, Stream C: Frontend)
- **Upstream dependencies:** MS-06 (REST API Layer), MS-14 (Permission Engine)
- **Downstream dependencies:** MS-19 (Desk Real-Time, Custom Field Types), MS-20 (GraphQL, Dashboard, Report), MS-23 (Workflow Engine)

## Vision Alignment

MS-17 is the pivotal milestone where Moca becomes a full-stack framework. The entire system design is built around the principle that a single MetaType definition drives everything — database schema, validation, API generation, and **UI rendering**. Until MS-17, that last piece is missing. The React Desk consumes MetaType definitions from `/api/v1/meta/{doctype}` and dynamically renders forms, lists, and navigation without any hardcoded views per DocType.

This milestone establishes the three-layer composition model (§17.2): Framework Desk (`desk/`), App Desk Extensions (per-app `.tsx` overrides), and Project Desk (site-specific overrides). While only the Framework Desk layer is built in MS-17, the architecture must support the extension points that MS-19 and MS-20 will add.

The field component library (20+ types mapped via `FIELD_TYPE_MAP`) is the atomic unit that FormView, ListView, and all future views depend on. Getting this right — especially the complex `ChildTableField` and `LinkField` — is critical to the framework's usability.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| ROADMAP.md | MS-17 | 933–978 | Milestone definition, scope, deliverables, acceptance criteria |
| MOCA_SYSTEM_DESIGN.md | §9 React Frontend Architecture | 1466–1568 | App shell layout, MetaProvider/DocProvider/AuthProvider design, FormView rendering, FieldRenderer dispatch, FIELD_TYPE_MAP (all 26 entries) |
| MOCA_SYSTEM_DESIGN.md | §17.2 Desk Composition Model | 2134–2143 | Three-layer composition (Framework/App/Project), `moca build desk` build process |
| MOCA_SYSTEM_DESIGN.md | §3.4 Permission Engine | 685–711 | PermRule structure, permission resolution order (consumed by AuthProvider/usePermissions) |
| pkg/meta/fielddef.go | FieldType constants + FieldDef | 1–150 | All 35 field types (29 storage + 6 layout), FieldDef struct with UI fields |
| pkg/api/rest.go | apiFieldDef, apiMetaResponse | 366–397 | Current meta API response — missing UI fields needed by frontend |
| pkg/meta/stubs.go | ViewMeta, LayoutHint | 90–97 | Empty placeholder structs that need populating |
| cmd/moca/build.go | NewBuildCommand | 15–31 | `moca build desk` placeholder (calls `notImplemented()`) |
| internal/serve/static.go | registerStaticFiles | 1–22 | Existing static file handler for `GET /desk/*` |
| internal/config/types.go | DevelopmentConfig | 117–150 | `DeskPort` and `DeskDevServer` config fields already defined |
| cmd/moca/serve.go | NewServeCommand | 20–173 | Dev server with subsystem supervisor |

## Research Notes

No external web research was needed. The design documents are comprehensive and the technology choices (React 19, Vite, TailwindCSS, TypeScript) are well-defined. Key implementation decisions:

- **State management:** TanStack Query (React Query) is the natural fit for server-state caching (meta, documents). It provides caching, background refetch, and optimistic updates out of the box.
- **Routing:** React Router v7 for `/desk/app/{doctype}/{name}` patterns.
- **Dev proxy:** Go's `httputil.ReverseProxy` can proxy `/desk/*` to Vite's dev server, including WebSocket upgrade for HMR.
- **Key gap found:** `apiFieldDef` in `pkg/api/rest.go:367–380` only exposes 11 fields. The frontend needs at least 8 additional fields from `FieldDef` (`in_list_view`, `in_filter`, `in_preview`, `hidden`, `depends_on`, `mandatory_depends_on`, `default`, `max_length`). This must be fixed before the frontend can render metadata-driven views.
- **Key gap found:** `ViewMeta` and `LayoutHint` in `pkg/meta/stubs.go:90–97` are empty structs. `LayoutHint` is embedded in `FieldDef` (line 122) but contributes no data. These need concrete fields for form layout control.

## Milestone Plan

### Task 1

- **Task ID:** MS-17-T1
- **Title:** Extend Meta API for Frontend Consumption and Implement Build/Dev Pipeline
- **Status:** Completed
- **Description:**
  Two backend changes that unblock all frontend work:

  **1. Meta API Enhancement:** Extend `apiFieldDef` (rest.go:367) to expose UI-critical fields from `FieldDef` (fielddef.go:121): `in_list_view`, `in_filter`, `in_preview`, `hidden`, `depends_on`, `mandatory_depends_on`, `default`, `max_length`, `max_value`, `min_value`, `width`. Extend `apiMetaResponse` (rest.go:383) with: `naming_rule`, `title_field`, `image_field`, `sort_field`, `sort_order`, `search_fields`, `track_changes`. Populate `ViewMeta` (stubs.go:90) with fields like `SortField`, `SortOrder`, `TitleField`, `ImageField`. Populate `LayoutHint` (stubs.go:97) with `ColSpan`, `Collapsible`, `CollapsedByDefault`, `Label`.

  **2. Build Pipeline:** Replace the `moca build desk` placeholder (build.go:24) with a real command that runs `npx vite build` in the `desk/` directory, outputting to `desk/dist/`. Wire `StaticDir` in `moca serve` (serve.go) to point at `desk/dist/` so the compiled SPA is served at `/desk/*` via the existing `registerStaticFiles` handler (static.go). Add a dev-mode reverse proxy: when `DeskDevServer: true` in config, `moca serve` proxies `/desk/*` to `localhost:{DeskPort}` (default 3000) instead of serving static files, handling WebSocket upgrade for Vite HMR.

- **Why this task exists:** The frontend cannot render metadata-driven views without the full field definitions in the API response. The build/dev pipeline must exist before any React code can be tested end-to-end. These are small, well-scoped backend changes that gate everything else.
- **Dependencies:** None (pure backend work)
- **Inputs / References:**
  - `pkg/api/rest.go` lines 366–430 (apiFieldDef, apiMetaResponse, buildMetaResponse)
  - `pkg/meta/fielddef.go` lines 120–150 (FieldDef with all UI fields)
  - `pkg/meta/stubs.go` lines 85–97 (empty ViewMeta and LayoutHint)
  - `cmd/moca/build.go` lines 15–31 (placeholder build desk)
  - `cmd/moca/serve.go` lines 20–173 (dev server)
  - `internal/serve/server.go` (ServerConfig.StaticDir)
  - `internal/serve/static.go` lines 1–22 (registerStaticFiles)
  - `internal/config/types.go` lines 117–150 (DeskPort, DeskDevServer)
- **Deliverable:**
  - Updated `apiFieldDef` and `apiMetaResponse` structs with UI fields
  - Populated `ViewMeta` and `LayoutHint` structs (no longer empty)
  - Updated `buildMetaResponse` to map new fields
  - Working `moca build desk` command (runs Vite build)
  - Dev proxy in `moca serve` for HMR
  - Tests for extended meta response and build command
- **Acceptance Criteria:**
  - `GET /api/v1/meta/User` response includes `in_list_view`, `in_filter`, `hidden`, `depends_on`, `default` on field definitions
  - `apiMetaResponse` includes `naming_rule`, `title_field`, `sort_field`
  - `moca build desk` exits 0 when `desk/` contains a valid Vite project
  - `moca serve` with `desk_dev_server: true` proxies `/desk/` to Vite dev server port
  - Existing API tests pass (new fields are additive, no breaking changes)
- **Risks / Unknowns:**
  - Dev proxy must handle WebSocket upgrade for Vite HMR — Go's `httputil.ReverseProxy` supports this but needs explicit configuration
  - Adding fields to meta response increases payload size; only include fields the frontend actually consumes

---

### Task 2

- **Task ID:** MS-17-T2
- **Title:** Vite Project Scaffold, API Client, Auth/Meta/Doc Providers, and Login Page
- **Status:** Completed
- **Description:**
  Bootstrap the `desk/` directory with a complete React 19 + TypeScript + Vite + TailwindCSS project. Implement the foundational data layer:

  **API Client** (`desk/src/api/client.ts`): Typed HTTP client wrapping `fetch` with auth token interceptor (`credentials: include` for cookies), tenant header injection, error normalization (parse backend error responses into typed errors), and request/response logging in dev mode.

  **TypeScript Interfaces** (`desk/src/api/types.ts`): `MetaType`, `FieldDef`, `PermRule`, `DocResponse`, `ListResponse`, `LoginRequest`, `LoginResponse` — all matching the backend JSON shapes from Task 1's extended API.

  **AuthProvider** (`desk/src/providers/AuthProvider.tsx`): `useAuth()` hook exposing `user`, `login(email, password)`, `logout()`, `isAuthenticated`, `permissions`. Login calls `POST /api/v1/auth/login`, logout calls `POST /api/v1/auth/logout`. Session persistence via httpOnly cookie (backend-managed). Token refresh via `POST /api/v1/auth/refresh`.

  **MetaProvider** (`desk/src/providers/MetaProvider.tsx`): `useMetaType(doctype)` hook that fetches from `/api/v1/meta/{doctype}`, caches via TanStack Query (stale time: 5 minutes), returns typed `MetaType`.

  **DocProvider** (`desk/src/providers/DocProvider.tsx`): `useDocument(doctype, name)`, `useDocList(doctype, filters, pagination)`, `useDocCreate(doctype)`, `useDocUpdate(doctype, name)`, `useDocDelete(doctype, name)` hooks wrapping CRUD endpoints.

  **PermissionProvider** (`desk/src/providers/PermissionProvider.tsx`): `usePermissions(doctype)` returning `{canRead, canWrite, canCreate, canDelete, canSubmit, canCancel}`.

  **Router and Layout:** React Router with routes `/desk/login` and `/desk/app/*`. Protected route wrapper redirecting unauthenticated users to login. Minimal `DeskLayout` with sidebar slot, topbar slot, and content `<Outlet/>`.

  **Login Page:** Functional login form with email/password fields, error display, redirect to `/desk/app` on success.

- **Why this task exists:** Every view component depends on auth, meta, and document data. This task delivers the complete data plumbing and navigation skeleton. The login page is the first user-visible deliverable and proves the full stack works end-to-end.
- **Dependencies:** MS-17-T1 (meta API must expose UI fields; build pipeline must exist to test)
- **Inputs / References:**
  - MOCA_SYSTEM_DESIGN.md §9.1–9.2 lines 1466–1532 (providers, hooks, rendering model)
  - pkg/api/auth_handler.go (login/logout/refresh endpoint contracts)
  - pkg/api/rest.go (resource CRUD endpoint contracts, meta endpoint)
  - pkg/auth/user.go (User struct shape for TypeScript interface)
  - pkg/auth/permission.go (permission bitmask values)
- **Deliverable:**
  - `desk/` Vite project: `package.json`, `vite.config.ts`, `tsconfig.json`, `tailwind.config.ts`, `index.html`
  - `desk/src/main.tsx`, `desk/src/App.tsx`, `desk/src/router.tsx`
  - `desk/src/api/client.ts`, `desk/src/api/types.ts`
  - `desk/src/providers/AuthProvider.tsx`, `MetaProvider.tsx`, `DocProvider.tsx`, `PermissionProvider.tsx`
  - `desk/src/layouts/DeskLayout.tsx`
  - `desk/src/pages/Login.tsx`
- **Acceptance Criteria:**
  - `npm run dev` in `desk/` starts Vite dev server on port 3000
  - `http://localhost:3000/desk/login` renders login form
  - Submitting valid credentials stores session, redirects to `/desk/app`
  - `useMetaType("User")` returns typed MetaType with all fields including `in_list_view`, `in_filter`
  - `useDocList("User", {})` returns paginated user list from API
  - Unauthenticated access to `/desk/app/*` redirects to `/desk/login`
  - TypeScript compiles with strict mode, no `any` in public hook APIs
- **Risks / Unknowns:**
  - Cookie-based auth with SPA requires `credentials: "include"` and proper CORS config; backend CORS middleware must allow Vite dev server origin (`http://localhost:3000`)
  - TanStack Query cache invalidation strategy needs upfront design to avoid stale data in FormView after saves

---

### Task 3

- **Task ID:** MS-17-T3
- **Title:** Field Component Library and FieldRenderer
- **Status:** Not Started
- **Description:**
  Build the complete field component library and the `FieldRenderer` dispatch component. `FieldRenderer` reads `field_type` from `FieldDef` and renders the corresponding component via `FIELD_TYPE_MAP` (as specified in §9.2 lines 1534–1567). Each field component receives a standard `FieldProps` interface: `{field: FieldDef, value: any, onChange: (v: any) => void, readOnly: boolean, error?: string}`.

  **All 35 field types organized in priority tiers:**

  **Tier 1 — Must have (16 types):** Data, Text, LongText, Int, Float, Currency, Date, Datetime, Select, Link (autocomplete searching `/api/v1/resource/{options}?search=`), Check, Attach, Table (ChildTable with inline add/delete/reorder rows), SectionBreak, ColumnBreak, TabBreak

  **Tier 2 — Should have (9 types):** Markdown (with preview toggle), Code (syntax highlighting via CodeMirror/Monaco), JSON (code editor), Percent, Time, Duration, Color, Rating, AttachImage

  **Tier 3 — Stub with placeholder (10 types):** DynamicLink, TableMultiSelect, Geolocation, Password, Signature, Barcode, HTMLEditor, HTML, Button, Heading

  Layout types (SectionBreak, ColumnBreak, TabBreak) are not input fields — they control form structure. They must work as layout containers that group subsequent fields into sections, columns, and tabs.

  **Critical complex components:**
  - `LinkField`: Debounced autocomplete calling `GET /api/v1/resource/{options}?search={query}&limit=10`, dropdown with results, click-to-navigate link to referenced document
  - `ChildTableField`: Essentially a nested form — fetches child MetaType, renders inline editable rows with add/delete buttons, row reorder, dirty tracking per row

- **Why this task exists:** The field library is the highest-risk, highest-effort component and the atomic unit that FormView and ListView both depend on. Isolating it allows focused testing of each field independently before composing them into views.
- **Dependencies:** MS-17-T2 (needs API client for Link autocomplete, needs MetaType TypeScript types)
- **Inputs / References:**
  - MOCA_SYSTEM_DESIGN.md §9.2 lines 1509–1567 (FormView rendering, FieldRenderer, FIELD_TYPE_MAP)
  - pkg/meta/fielddef.go lines 1–117 (all 35 FieldType constants, storage vs layout classification)
  - pkg/meta/fielddef.go lines 120–150 (FieldDef struct — Options, DependsOn, Required, ReadOnly, etc.)
- **Deliverable:**
  - `desk/src/components/fields/FieldRenderer.tsx` — dispatch component
  - `desk/src/components/fields/types.ts` — `FieldProps`, `FIELD_TYPE_MAP`
  - ~25 field component files in `desk/src/components/fields/` (Data, Text, Int, Float, Currency, Date, Datetime, Select, Link, Check, Attach, Code, Markdown, JSON, Color, Rating, Percent, Time, Duration, LongText, AttachImage, Password + stubs)
  - `desk/src/components/fields/ChildTableField.tsx` — inline editable child table
  - 3 layout components in `desk/src/components/layout/` (SectionBreak, ColumnBreak, TabBreak)
  - `desk/src/components/fields/index.ts` — barrel export
- **Acceptance Criteria:**
  - `FieldRenderer` given `field_type: "Data"` renders a text input; `"Select"` renders dropdown; `"Check"` renders checkbox
  - `LinkField` with `options: "User"` shows autocomplete dropdown with API results
  - `ChildTableField` renders inline rows with add/delete; editing a cell marks the row dirty
  - `SelectField` with `options: "Draft\nSubmitted\nCancelled"` renders dropdown with 3 choices
  - `DateField` renders a date picker; `DatetimeField` includes time selection
  - All Tier 1 components fully functional; Tier 2 functional; Tier 3 render placeholder with field label
  - Layout types tested: SectionBreak creates a collapsible section, ColumnBreak splits into columns
- **Risks / Unknowns:**
  - `ChildTableField` is the most complex component — it's a nested FormView with its own MetaType, inline editing, and dirty tracking. Budget significant time here.
  - Third-party dependencies (CodeMirror vs Monaco for code editor, date picker library) need selection. Prefer lightweight options to minimize bundle size.
  - `LinkField` autocomplete must debounce API calls (300ms) and handle large result sets gracefully

---

### Task 4

- **Task ID:** MS-17-T4
- **Title:** FormView, ListView, App Shell (Sidebar, Breadcrumbs, Command-K Search)
- **Status:** Not Started
- **Description:**
  Build the two primary views and complete the app shell, integrating all components from Tasks 2 and 3.

  **Layout Parser** (`desk/src/utils/layoutParser.ts`): Convert the flat `FieldDef[]` array from MetaType into a nested structure: `{tabs: [{label, sections: [{label, collapsible, columns: [{fields: FieldDef[]}]}]}]}`. Logic: TabBreak starts a new tab, SectionBreak starts a new section, ColumnBreak starts a new column. Fields before any SectionBreak go into a default section. Handle edge cases: consecutive layout breaks, fields with no preceding section.

  **FormView** (`/desk/app/{doctype}/{name}` and `/desk/app/{doctype}/new`):
  - Uses `useMetaType(doctype)` + `useDocument(doctype, name)` + `usePermissions(doctype)`
  - Parses layout with `layoutParser`, renders tabs/sections/columns
  - Each field rendered via `FieldRenderer` with value from document, `onChange` wired to local state
  - Dirty tracking: compare current state to initial document snapshot, show indicator
  - Save button: PUT for existing docs, POST for new docs via DocProvider hooks
  - Cancel reverts to last-saved state
  - Respects `read_only`, `hidden`, `depends_on` (conditional show/hide via expression evaluation), `mandatory_depends_on`
  - Title bar with document name (from `title_field` or `name`), breadcrumbs

  **ListView** (`/desk/app/{doctype}`):
  - Uses `useMetaType(doctype)` to determine visible columns (`in_list_view: true`) and filter fields (`in_filter: true`)
  - Uses `useDocList(doctype, filters, pagination)` to fetch data
  - Table with sortable columns, filter bar, pagination (page size selector, page navigation)
  - Row click navigates to FormView
  - "New" button (hidden if `!canCreate`) navigates to `/desk/app/{doctype}/new`

  **App Shell Completion:**
  - **Sidebar** (`desk/src/components/shell/Sidebar.tsx`): Fetch modules from API (list ModuleDef or derive from registered MetaTypes), group DocTypes by module, render collapsible module sections with DocType links
  - **Topbar** (`desk/src/components/shell/Topbar.tsx`): Breadcrumbs (Home > DocType > Document Name), user menu (profile info, logout button)
  - **Command-K Search** (`desk/src/components/shell/CommandPalette.tsx`): Modal triggered by Cmd+K / Ctrl+K, searches DocType names client-side, optional document title search via API

- **Why this task exists:** This is the integration task that composes everything into the user-facing application. FormView and ListView share providers, routing, and shell — they are tested meaningfully only together. This task delivers all 6 acceptance criteria from the roadmap.
- **Dependencies:** MS-17-T3 (field components), MS-17-T2 (providers, router)
- **Inputs / References:**
  - MOCA_SYSTEM_DESIGN.md §9.1 lines 1472–1506 (app shell diagram: sidebar, breadcrumbs, search bar, routes)
  - MOCA_SYSTEM_DESIGN.md §9.2 lines 1509–1532 (FormView rendering with useMetaType, useDocument, usePermissions)
  - ROADMAP.md MS-17 lines 952–958 (acceptance criteria)
  - pkg/meta/fielddef.go lines 120–150 (FieldDef: DependsOn, Hidden, InListView, InFilter for view logic)
- **Deliverable:**
  - `desk/src/pages/FormView.tsx` — metadata-driven form with tab/section/column layout
  - `desk/src/pages/ListView.tsx` — metadata-driven table with filters and pagination
  - `desk/src/components/shell/Sidebar.tsx` — module-grouped DocType navigation
  - `desk/src/components/shell/Topbar.tsx` — breadcrumbs and user menu
  - `desk/src/components/shell/CommandPalette.tsx` — Cmd+K search modal
  - `desk/src/hooks/useDirtyTracking.ts` — form dirty state management
  - `desk/src/utils/layoutParser.ts` — flat fields to nested tab/section/column structure
  - `desk/src/utils/expressionEval.ts` — evaluate `depends_on` expressions against document state
- **Acceptance Criteria:**
  - `http://localhost:8000/desk` shows login page (serves from `desk/dist/` after build)
  - After login, sidebar shows modules with DocTypes
  - Navigate to `/desk/app/User` — ListView shows columns matching `in_list_view` fields
  - Click a user row — FormView shows with sections, columns, fields populated from document
  - Edit a field, dirty indicator appears; click Save, PUT request sent; values persist on reload
  - Click "New" on ListView, fill form, click Save, POST request sent; new document appears in list
  - LinkField shows autocomplete; ChildTableField renders inline rows
  - Cmd+K opens search palette; typing "User" shows the User DocType
  - Breadcrumbs update: Home > User > Administrator
- **Risks / Unknowns:**
  - Layout parsing (flat field array → nested structure) must handle edge cases: fields before any SectionBreak, consecutive layout breaks, missing TabBreak (all fields in default tab)
  - `depends_on` expression evaluation needs a simple evaluator (e.g., `eval_if("doc.status == 'Draft'", doc)`) — must be safe (no arbitrary JS eval)
  - Dirty tracking with nested ChildTable rows requires recursive comparison
  - Performance with large forms (50+ fields) — monitor and add virtualization if needed

## Recommended Execution Order

1. **MS-17-T1** — Backend first: extend meta API and build pipeline. Small scope (1–2 days), unblocks everything.
2. **MS-17-T2** — React scaffold + providers + login. Can begin the Vite setup in parallel with T1 while waiting for API changes. Proves full-stack connectivity.
3. **MS-17-T3** — Field component library. Highest risk and effort. Start with Tier 1 (Data, Int, Select, Date, Check, Link), then ChildTable, then Tier 2.
4. **MS-17-T4** — FormView + ListView + shell. Integration task that delivers all acceptance criteria. Last because it depends on everything else.

## Open Questions

- **Module list source:** Should the sidebar fetch `ModuleDef` documents from the API, or derive the module list from registered MetaTypes? The `ModuleDef` doctype exists in `apps/core/` but may not be populated. Fallback: group by MetaType's `module` field.
- **Expression evaluator for `depends_on`:** What expression syntax does the backend expect? Frappe uses Python-like `eval:doc.status == "Draft"`. Need to define the JS-side evaluator format.
- **TailwindCSS or component library?** The roadmap says TailwindCSS. Should we use a component library (shadcn/ui, Radix) on top, or build from scratch with Tailwind utilities?
- **Bundle splitting strategy:** Should each field component be lazy-loaded, or is a single bundle acceptable for v1?

## Out of Scope for This Milestone

- WebSocket real-time updates (MS-19)
- Custom field type registry / app extension registration (MS-19)
- Dashboard and Report views (MS-20)
- Portal SSR layer (MS-27)
- Internationalization / i18n (MS-20)
- GraphQL integration (MS-20)
- Version tracking / document timeline (MS-19)
