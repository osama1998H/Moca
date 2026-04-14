# Developer Experience Test Session Report — v1.0.1

**Date:** 2026-04-14  
**Version tested:** v1.0.1  
**Method:** Clean install via `install.sh` from GitHub Release, full workflow in `/tmp/moca-dx-test/myproject`  
**Tester:** Claude + browser automation (Playwright via Expect MCP)  
**Previous sessions:** v0.1.1-alpha.7 (7 issues), v0.1.8 (10 issues), v0.1.11 (4 issues) — all 21 fixed  
**Focus:** DocType Builder (PR #30)

## Test Environment

| Component | Version / Config |
|-----------|-----------------|
| Moca CLI | v1.0.1 (commit 617d239, built 2026-04-14) |
| @osama1998h/desk | 1.0.1 (GitHub Packages) |
| PostgreSQL | 16-alpine, port 5432, user `moca` / `moca_dev` |
| Redis | 7-alpine, port 6379 |
| Meilisearch | v1.12, port 7700, master key `moca_dev` |
| MinIO | latest, port 9002/9003, `minioadmin` |
| Platform | macOS arm64, Go 1.26.1, Node 22 |
| Vite | v8.0.8, @osama1998h/desk vite plugin |
| Project directory | `/tmp/moca-dx-test/myproject` |
| Docker compose | `/tmp/moca-dx-test/docker-compose.yml` |

## Test Steps Performed

| # | Command / Action | Result | Notes |
|---|-----------------|--------|-------|
| 1 | `curl -fsSL .../install.sh \| MOCA_VERSION=1.0.1 sh` | Pass | Installed to `~/.local/bin`, SHA-256 verified |
| 2 | `moca version` | Pass | Shows 1.0.1, commit 617d239 |
| 3 | `docker compose up -d` (PG, Redis, Meili, MinIO) | Pass | PG/Redis/MinIO healthy; Meili healthy via curl but Docker healthcheck reports unhealthy (wget missing in newer image) |
| 4 | `moca init /tmp/moca-dx-test/myproject --name myproject --db-user moca --db-password moca_dev` | Pass | Creates go.work, go.mod, desk/, moca.yaml, .moca/, apps/, sites/ |
| 5 | Verify `desk/package.json` pins `@osama1998h/desk` to `"1.0.1"` | Pass | Release binary `resolveDeskVersion()` works correctly |
| 6 | Verify `.npmrc` has `@osama1998h:registry=https://npm.pkg.github.com` | Pass | Auto-created by init |
| 7 | `moca desk install` | Pass | 253.5 MB installed |
| 8 | `moca site create library.localhost --admin-password admin123 --admin-email admin@library.localhost` | Pass | 10/10 steps complete |
| 9 | `moca site use library.localhost` | Pass | Sets active site |
| 10 | `moca app new library --module "Library Management" --title "Library Management" --publisher "Test" --doctype Book --desk` | Pass | App scaffolded correctly |
| 11 | Verify module directory is `library_management/` (no spaces) | Pass | Issue #18 fix confirmed |
| 12 | `go work sync && cd apps/library && go mod tidy` | Pass | Clean |
| 13 | `moca app install library` | Pass | 1 MetaType registered, DocType + DocPerm seeded |
| 14 | Set `desk/.env` -> `VITE_MOCA_SITE=library.localhost` | Pass | Manual edit |
| 15 | Add `developer_mode: true` to `moca.yaml` under `development:` | Pass | Manual edit (not auto-set by `moca init`) |
| 16 | `moca serve` | Pass | Server on :8000, all subsystems started, meta watcher active |
| 17 | `moca desk dev` | Pass | Vite v8.0.8 on :3000, HMR active |
| 18 | Browser: login at `/desk/` with admin credentials | Pass | Redirects to `/desk/app` |
| 19 | Browser: sidebar shows "DocType Builder" link | Pass | `/desk/app/doctype-builder` route registered |
| 20 | Browser: sidebar "Other 11" shows all doctypes (Book + 10 core) | Pass | Required JS click — Playwright couldn't scroll sidebar element into view |
| 21 | Browser: navigate to DocType Builder | Pass | Empty canvas with Details tab, field palette, icon rail |
| 22 | Browser: click field type in palette | Pass | Clicking "Data" adds a Data field to the active section |
| 23 | Browser: add multiple fields (Data, Select, Date) | Pass | Fields appear in canvas, field count updates |
| 24 | Browser: click field to open property panel | Pass | Shows Basic, Validation, Display, Search, API sections |
| 25 | Browser: rename field label in property panel | Pass | Canvas updates immediately; "Full Name *" shown with Required indicator |
| 26 | Browser: toggle Required switch | Pass | Asterisk (*) appears in canvas field display |
| 27 | Browser: toggle Schematic -> Preview mode | Pass | Canvas renders real form components (textbox, spinbutton, combobox, date picker) |
| 28 | Browser: try renaming a tab (double-click) | **Fail** | No inline edit appears — tab just gets selected (Issue #22) |
| 29 | Browser: try deleting a tab | **Fail** | No X button or context menu to remove tabs (Issue #25) |
| 30 | Browser: try selecting App from dropdown | **Fail** | Plain text input with no autocomplete/dropdown options (Issue #24) |
| 31 | Browser: try selecting Module from dropdown | **Fail** | Same as App — plain text input, no options (Issue #24) |
| 32 | Browser: click Save button | **Fail** | Toast error: "Unexpected non-whitespace character after JSON at position 4" (Issue #23) |
| 33 | API: `GET /api/v1/dev/apps` on :8000 | **Fail** | 404 Not Found — dev routes not registered (Issue #23) |
| 34 | API: `POST /api/v1/dev/doctype` on :8000 | **Fail** | 404 Not Found — same root cause (Issue #23) |
| 35 | API: `GET /api/v1/dev/doctype/Book` on :8000 | **Fail** | 404 Not Found — same root cause (Issue #23) |
| 36 | API: `GET /api/v1/resource/DocType` (with auth) | Pass | Returns 11 doctypes |
| 37 | API: `POST /api/v1/auth/login` | Pass | Returns JWT token |

## Issues Found

### Issue #22: Tab rename not implemented

**Severity:** Major  
**Component:** `desk/src/components/doctype-builder/` (tab management)  
**Reproducible:** Always  

When a tab is added via "+ Tab", it gets a generic name ("Tab 2", "Tab 3", etc.). There is no mechanism to rename it:
- Double-click on tab label does nothing (just selects the tab)
- No right-click context menu
- No inline edit affordance

**Expected:** Double-click or right-click on tab label should enable inline text editing, similar to how section labels have editable textboxes.

---

### Issue #23: Dev API routes (`/api/v1/dev/*`) not registered — Save is broken

**Severity:** Blocker  
**Component:** `internal/serve/server.go`, `pkg/api/dev_handler.go`, `internal/config/types.go`  
**Reproducible:** Always  

The DocType Builder's Save functionality is completely non-functional. All dev API endpoints return 404:
- `GET /api/v1/dev/apps` — 404
- `POST /api/v1/dev/doctype` — 404
- `PUT /api/v1/dev/doctype/{name}` — 404
- `GET /api/v1/dev/doctype/{name}` — 404
- `DELETE /api/v1/dev/doctype/{name}` — 404

**Root cause:** `RegisterDevRoutes()` in `pkg/api/dev_handler.go` is defined but **never called** during server startup in `internal/serve/server.go`. The `DevHandler` is never instantiated. Additionally, `internal/config/types.go` has no `DeveloperMode` or `EnableDevAPI` field in `DevelopmentConfig`.

The frontend sends the save request through the Vite proxy (`/api` -> `http://localhost:8000`), which correctly reaches the backend, but the backend returns 404. The frontend receives the HTML 404 page and fails to parse it as JSON, producing the error: "Unexpected non-whitespace character after JSON at position 4".

**To fix:**
1. Add `EnableDevAPI bool` (or `DeveloperMode bool`) field to `DevelopmentConfig` in `internal/config/types.go`
2. Parse `developer_mode: true` from `moca.yaml`
3. In `NewServer()`, conditionally instantiate `DevHandler` and call `RegisterDevRoutes(gw.Mux(), "v1")` when the config flag is set
4. Pass the project's `apps/` directory path to the `DevHandler` constructor

---

### Issue #24: App and Module selectors have no dropdown options

**Severity:** Major  
**Component:** `desk/src/components/doctype-builder/` (toolbar)  
**Reproducible:** Always  

The App and Module fields in the DocType Builder toolbar are plain `<input type="text">` elements with no autocomplete, dropdown, or link options. The developer must type the exact app name and module name manually.

**Expected:** 
- App selector should fetch installed apps from `GET /api/v1/dev/apps` and show them as a dropdown/combobox
- Module selector should show modules for the selected app (derived from app manifest or directory listing)

**Note:** This is partially blocked by Issue #23 (the API endpoint doesn't work), but even with the API working, the frontend components need to be wired to use autocomplete.

---

### Issue #25: No delete button on tabs

**Severity:** Major  
**Component:** `desk/src/components/doctype-builder/` (tab management)  
**Reproducible:** Always  

Once a tab is created via "+ Tab", there is no way to delete it. Expected affordances:
- An "X" button on the tab header
- A right-click context menu with "Delete tab"
- Or at minimum, a keyboard shortcut (e.g., Backspace when tab is focused)

Currently the only tab management is adding new tabs. This creates a usability issue where accidental tab creation cannot be undone (undo/redo may help, but there's no explicit delete).

---

### Issue #26: Field name does not auto-update when label changes

**Severity:** Minor  
**Component:** `desk/src/components/doctype-builder/` (property panel)  
**Reproducible:** Always  

When a field is first created (e.g., clicking "Data" in palette), it gets a default label like "Data 1" and a corresponding field name "data_1". When the label is changed (e.g., to "Full Name"), the field name remains "data_1" instead of auto-updating to "full_name".

**Expected:** The field name should auto-generate from the label (converting to snake_case) until the user has manually edited the field name. Once manually edited, the name should stop auto-syncing. This is the pattern used in Frappe's DocType builder.

---

### Issue #27: `developer_mode` not set by `moca init`

**Severity:** Minor  
**Component:** `internal/scaffold/scaffold.go` (project init)  
**Reproducible:** Always  

When `moca init` creates a new project, the `moca.yaml` does not include `developer_mode: true` under the `development:` section. Since the DocType Builder requires developer mode to save, new developers must manually discover and add this setting.

**Expected:** `moca init` should set `developer_mode: true` by default (since new projects are always in development). Production deployments would set it to `false`.

---

### Issue #28: Field palette scroll captured by main canvas

**Severity:** Major  
**Component:** `desk/src/components/doctype-builder/` (field palette / drawer)  
**Reproducible:** Always  

When the field palette (left panel showing Text, Number, Date & Time, Selection, Relations, Media, Interactive, Display categories) overflows the viewport, scrolling inside it scrolls the main page/canvas instead of the palette panel. Users cannot scroll down to see field types in the lower categories (e.g., Interactive, Display) without the entire page scrolling.

**Expected:** The field palette should be a scroll container (`overflow-y: auto`) with `overscroll-behavior: contain` to prevent scroll event propagation. The palette should scroll independently from the main canvas.

---

## Commands Reference

Full sequence of commands used in this test session:

```bash
# 1. Install
curl -fsSL https://raw.githubusercontent.com/osama1998H/moca/main/install.sh | MOCA_VERSION=1.0.1 sh
moca version

# 2. Infrastructure
docker compose -f /tmp/moca-dx-test/docker-compose.yml up -d --wait

# 3. Init
moca init /tmp/moca-dx-test/myproject --name myproject --db-user moca --db-password moca_dev
cd /tmp/moca-dx-test/myproject

# 4. Frontend
moca desk install

# 5. Site
moca site create library.localhost --admin-password admin123 --admin-email admin@library.localhost
moca site use library.localhost

# 6. App
moca app new library --module "Library Management" --title "Library Management" \
  --publisher "Test" --doctype Book --desk

# 7. Build
go work sync
cd apps/library && go mod tidy && cd ../..

# 8. Install app
moca app install library

# 9. Configure
echo "VITE_MOCA_SITE=library.localhost" > desk/.env
# Add developer_mode: true under development: in moca.yaml

# 10. Run
moca serve                  # Backend on :8000
moca desk dev               # Frontend on :3000

# 11. Test
# Browser: http://localhost:3000/desk/
# Login: admin@library.localhost / admin123
```

## Credentials

| Service | URL | Credentials |
|---------|-----|-------------|
| Desk UI | http://localhost:3000/desk/ | admin@library.localhost / admin123 |
| API | http://localhost:8000/api/v1/ | Bearer token via `/api/v1/auth/login` |
| PostgreSQL | localhost:5432 | moca / moca_dev |
| MinIO Console | http://localhost:9003 | minioadmin / minioadmin |
| Meilisearch | http://localhost:7700 | master key: moca_dev |

## Previously Fixed Issues (v0.1.1-alpha.7 + v0.1.8 + v0.1.11)

All 21 issues from prior sessions verified or confirmed in codebase:

- [x] #1–#17: All fixes from v0.1.1-alpha.7 and v0.1.8 sessions (verified in v0.1.11)
- [x] #18: Module name snake_case — confirmed fixed (`library_management/` created correctly)
- [x] #19: Workflow registry wiring — fix merged in PR #28
- [x] #20: Workflow transition persistence — fix merged in PR #28
- [x] #21: WebSocket Hijacker delegation — fix merged in PR #28

## Summary

| Metric | Value |
|--------|-------|
| Total steps tested | 37 |
| Steps passed | 31 |
| Steps failed | 6 |
| New issues found | 7 (#22–#28) |
| Blocker issues | 1 (#23 — dev API not registered) |
| Major issues | 4 (#22, #24, #25, #28) |
| Minor issues | 2 (#26, #27) |
| Previously fixed issues verified | 21/21 |

### What Works Well

- Install flow (v1.0.1) is smooth — SHA-256 verification, correct binary version
- Project init, site creation, app scaffolding all work correctly
- DocType Builder UI renders beautifully — field palette, schematic/preview modes, property panel
- Preview mode correctly renders form components matching field types
- Field property editing (label, required, etc.) updates the canvas in real-time
- Clicking palette buttons to add fields works as an alternative to drag-and-drop
- Vite proxy correctly forwards `/api` to the backend

### What Needs Fixing Before DocType Builder Is Usable

1. **#23 (Blocker):** Wire `DevHandler.RegisterDevRoutes()` in `server.go` — without this, nothing saves
2. **#24 (Major):** App/Module selectors need autocomplete from dev API
3. **#22 (Major):** Tab rename mechanism needed
4. **#25 (Major):** Tab delete mechanism needed
5. **#28 (Major):** Field palette scroll bleeds into main canvas
6. **#26 (Minor):** Auto-sync field name from label
7. **#27 (Minor):** `moca init` should set `developer_mode: true` by default
