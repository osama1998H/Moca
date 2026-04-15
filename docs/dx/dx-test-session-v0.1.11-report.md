# Developer Experience Test Session Report — v0.1.11

**Date:** 2026-04-12  
**Version tested:** v0.1.11  
**Method:** Clean install via `install.sh` from GitHub Release, full workflow in `/tmp/moca-dx-test/myproject`  
**Tester:** Claude + browser automation (Playwright)  
**Previous sessions:** v0.1.1-alpha.7 (7 issues), v0.1.8 (10 issues) — all 17 fixed  

## Test Environment

| Component | Version / Config |
|-----------|-----------------|
| Moca CLI | v0.1.11 (commit ebe40a1, built 2026-04-12) |
| @osama1998h/desk | 0.1.11 (GitHub Packages) |
| PostgreSQL | 16-alpine, port 5432, user `moca` / `moca_dev` |
| Redis | 7-alpine, port 6379 |
| Meilisearch | v1.12, port 7700, master key `moca_dev` |
| MinIO | latest, port 9002/9003, `minioadmin` |
| Platform | macOS arm64, Go 1.26.1, Node 22 |
| Project directory | `/tmp/moca-dx-test/myproject` |
| Docker compose | `/tmp/moca-dx-test/docker-compose.yml` |

## Test Steps Performed

| # | Command / Action | Result | Notes |
|---|-----------------|--------|-------|
| 1 | `curl -fsSL .../install.sh \| MOCA_VERSION=0.1.11 sh` | Pass | Installed to `~/.local/bin`, SHA-256 verified |
| 2 | `moca version` | Pass | Shows 0.1.11, commit ebe40a1 |
| 3 | `docker-compose up -d` (PG, Redis, Meili, MinIO) | Pass | All 4 healthy; port 9000 conflict required MinIO remap to 9002 |
| 4 | `moca init /tmp/moca-dx-test/myproject --name myproject --db-user moca --db-password moca_dev` | Pass | Creates go.work, go.mod, desk/, moca.yaml, .moca/, apps/, sites/ |
| 5 | Verify `desk/package.json` pins `@osama1998h/desk` to `"0.1.11"` | Pass | Release binary `resolveDeskVersion()` works correctly |
| 6 | `moca desk install` | Pass | 251.8 MB installed, @osama1998h/desk@0.1.11 in node_modules |
| 7 | `moca site create library.localhost --admin-password admin123 --admin-email admin@library.localhost` | Pass | 10/10 steps complete, schema + admin user + core doctypes seeded |
| 8 | `moca site use library.localhost` | Pass | Sets active site |
| 9 | `moca app new library --module "Library Management" --title "Library Management" --publisher "Test" --doctype Book --desk` | Pass | App scaffolded with go.mod, manifest, desk-manifest.json |
| 10 | Verify app `go.mod` uses `github.com/osama1998H/moca v0.1.11` (no replace) | Pass | Standalone project uses published version |
| 11 | Verify `go.work` includes `./apps/library` | Pass | Auto-updated |
| 12 | Add Library Member, Library Transaction doctypes (manual JSON + manifest update) | Pass | Followed scaffolded Book format |
| 13 | `go work sync && cd apps/library && go mod tidy` | Pass | Clean |
| 14 | `moca app install library` | Pass | 3 MetaTypes registered, DocType + DocPerm records seeded |
| 15 | Set `desk/.env` → `VITE_MOCA_SITE=library.localhost` | Pass | Manual edit |
| 16 | `moca serve` | Pass | Server on :8000, all subsystems started |
| 17 | `moca desk dev` | Pass | Vite on :3000, HMR active |
| 18 | Browser: login at `/desk/` with admin credentials | Pass | Redirects to `/desk/app` |
| 19 | Browser: sidebar shows all 13 doctypes (3 library + 10 core) | Pass | Book, Library Member, Library Transaction visible |
| 20 | Browser: Book list view renders with filters, columns, pagination | Pass | Existing record visible (ID 1) |
| 21 | API: `GET /api/v1/openapi.json` | Pass | All library doctypes in spec |
| 22 | API: `GET /api/v1/resource/DocType` | Pass | Returns 13 doctypes |
| 23 | Add `workflow` JSON to Book doctype definition | Pass | Hot-reload triggered: `meta watcher: hot reload complete` |
| 24 | API: `GET /api/v1/workflow/Book/1/state` | **Fail** | `NO_WORKFLOW` — registry not populated (Issue #18) |
| 25 | Fix: wire `SyncWorkflows()` in server startup + hot-reload | Pass | After fix: `workflow registered: Book Approval` in logs |
| 26 | API: `GET /api/v1/workflow/Book/1/state` (after fix) | Pass | Returns `Draft` state + `Submit for Review` action |
| 27 | API: `POST /api/v1/workflow/Book/1/transition` `{"action":"Submit for Review"}` | Partial | Transition executes (returns Pending Review) but state not persisted (Issue #20) |

## Issues Found

### Issue #18: Module name snake_case conversion produces spaces in directory names

**Severity:** Major  
**Component:** `internal/scaffold/scaffold.go` (snake_case conversion logic)  
**Reproducible:** Always, when module name has spaces  

When the module name contains spaces (e.g., `"Library Management"`), the scaffold's snake_case conversion produces `library _management` (space before underscore) instead of `library_management`. This affects:

- Module directory: `apps/library/modules/library _management/`
- Doctype directories: `doctypes/library _member/`
- Doctype JSON files: `library _member.json`

The framework installer uses the same broken conversion, so it's internally consistent — everything works. But it creates filesystem paths with spaces that are hostile to shell scripts, `find` commands, and general developer ergonomics.

**To fix:** Audit the `toSnakeCase()` or equivalent conversion function in the scaffold package. Likely a missing `strings.TrimSpace()` or incorrect handling of space → underscore conversion where the space is kept alongside the underscore.

**Test case:**
```bash
moca app new testapp --module "Multi Word Module" --doctype "Test Item" --desk
ls apps/testapp/modules/
# Expected: multi_word_module/
# Actual:   multi _word _module/
```

---

### Issue #19: Workflow registry not populated from MetaType JSON definitions

**Severity:** Major  
**Component:** `internal/serve/server.go` (line ~307)  
**Status:** Fixed locally (not in v0.1.11 release)  

The `WorkflowRegistry` is created empty during server startup and never populated from MetaType `.Workflow` fields. This means adding a `"workflow"` block to any doctype JSON has no effect — the workflow API returns `NO_WORKFLOW` for all doctypes.

**Root cause:** `NewServer()` creates `wfRegistry := workflow.NewWorkflowRegistry()` but nothing iterates MetaTypes to call `wfRegistry.Set(site, doctype, mt.Workflow)`.

**Fix applied:**
1. Added `wfRegistry` to `Server` struct and `WfRegistry()` accessor
2. Added `SyncWorkflows(ctx, sites)` method — iterates all MetaTypes per site, registers active workflows
3. Called at startup in `cmd/moca/serve.go` after `NewServer()`
4. Called via new `WatcherConfig.OnReload` callback after each hot-reload

**Files changed:**
- `internal/serve/server.go` — `Server` struct, `SyncWorkflows()`, `WfRegistry()`
- `cmd/moca/serve.go` — startup sync + `OnReload` callback wiring
- `pkg/meta/watcher.go` — added `OnReload func(ctx, []string)` to `WatcherConfig`

**Test case:**
```bash
# Add workflow to any doctype JSON:
# "workflow": { "name": "My Flow", "doc_type": "Book", "is_active": true, "states": [...], "transitions": [...] }

# Restart server, check logs for:
# "workflow registered","site":"...","doctype":"Book","workflow":"My Flow"

# API should return state:
curl -H "Authorization: Bearer $TOKEN" -H "X-Moca-Site: library.localhost" \
  http://localhost:8000/api/v1/workflow/Book/1/state
# Expected: {"data":{"state":{...},"actions":[...]}}
```

---

### Issue #20: Workflow transition API does not persist state to database

**Severity:** Major  
**Component:** `pkg/api/workflow_handler.go` (`handleTransition`, line ~80)  

The `POST /api/v1/workflow/{doctype}/{name}/transition` endpoint:
1. Loads the document from DB
2. Calls `engine.Transition()` which mutates `doc.Set("workflow_state", ...)` in memory
3. Returns the new state in the response
4. **Never calls `docManager.Save()` to persist the change**

The next `GET .../state` loads a fresh document from DB where `workflow_state` is still null/empty, so it falls back to the initial state (Draft).

**Design note:** The `WorkflowBridge` (hooks approach) works correctly — it hooks into `EventBeforeSave`/`EventAfterSave` during document saves. The dedicated REST endpoint is the one missing persistence.

**To fix:** After `engine.Transition()` succeeds, call `docManager.Save(docCtx, doc)` (or equivalent) to persist the `workflow_state` field change.

**Test case:**
```bash
TOKEN=$(curl -s -X POST http://localhost:8000/api/v1/auth/login \
  -H "Content-Type: application/json" -H "X-Moca-Site: library.localhost" \
  -d '{"email":"admin@library.localhost","password":"admin123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['access_token'])")

# Transition
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "X-Moca-Site: library.localhost" \
  -H "Content-Type: application/json" \
  "http://localhost:8000/api/v1/workflow/Book/1/transition" \
  -d '{"action":"Submit for Review"}'
# Should return: "current_state": "Pending Review"

# Verify persistence
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Moca-Site: library.localhost" \
  "http://localhost:8000/api/v1/workflow/Book/1/state"
# Expected: "current_state": "Pending Review"
# Actual:   "current_state": "Draft"  <-- state not persisted
```

---

### Issue #21: WebSocket handshake fails with 501 (http.Hijacker not implemented)

**Severity:** Minor  
**Component:** `internal/serve/websocket.go`  

The Desk frontend attempts WebSocket connections to `/ws?token=...` but the server responds with 501. The server log shows:

```
ws: accept failed: failed to accept WebSocket connection: http.ResponseWriter does not implement http.Hijacker
```

This likely means the `http.ResponseWriter` is wrapped by middleware (metrics, logging, etc.) that doesn't forward the `http.Hijacker` interface.

**Impact:** WebSocket-based real-time updates (notifications, live data) don't work. The frontend retries with exponential backoff, generating console errors. Functionally non-blocking.

**To fix:** Ensure the response writer wrapper used by the middleware stack implements `http.Hijacker` by delegating to the underlying writer. Common pattern:
```go
func (w *wrappedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
        return hj.Hijack()
    }
    return nil, nil, errors.New("http.Hijacker not supported")
}
```

**Test case:**
```bash
# Start server, open desk in browser, check console for:
# "WebSocket connection to 'ws://localhost:3000/ws?token=...' failed: Error during WebSocket handshake: Unexpected response code: 501"

# Server logs show:
# "ws: accept failed","error":"failed to accept WebSocket connection: http.ResponseWriter does not implement http.Hijacker"
```

---

## Commands Reference

Full sequence of commands used in this test session:

```bash
# 1. Install
curl -fsSL https://raw.githubusercontent.com/osama1998H/moca/main/install.sh | MOCA_VERSION=0.1.11 sh
moca version

# 2. Infrastructure
mkdir -p /tmp/moca-dx-test
# Create docker-compose.yml with PG:5432, Redis:6379, Meili:7700, MinIO:9002
docker-compose -f /tmp/moca-dx-test/docker-compose.yml up -d

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

# 7. Additional doctypes (manually created JSON files in):
#    apps/library/modules/library _management/doctypes/library _member/library _member.json
#    apps/library/modules/library _management/doctypes/library _transaction/library _transaction.json
# Updated manifest.yaml to list all 3 doctypes

# 8. Build
go work sync
cd apps/library && go mod tidy && cd ../..

# 9. Install app
moca app install library

# 10. Configure desk
echo "VITE_MOCA_SITE=library.localhost" > desk/.env

# 11. Run
moca serve                  # Backend on :8000
moca desk dev               # Frontend on :3000

# 12. Test
# Browser: http://localhost:3000/desk/
# Login: admin@library.localhost / admin123
# API: curl -H "X-Moca-Site: library.localhost" http://localhost:8000/api/v1/openapi.json
```

## Credentials

| Service | URL | Credentials |
|---------|-----|-------------|
| Desk UI | http://localhost:3000/desk/ | admin@library.localhost / admin123 |
| API | http://localhost:8000/api/v1/ | Bearer token via `/api/v1/auth/login` |
| Swagger | http://localhost:8000/api/docs | — |
| PostgreSQL | localhost:5432 | moca / moca_dev |
| MinIO Console | http://localhost:9003 | minioadmin / minioadmin |
| Meilisearch | http://localhost:7700 | master key: moca_dev |

## Previously Fixed Issues (v0.1.1-alpha.7 + v0.1.8)

All 17 issues from prior sessions confirmed fixed in v0.1.11:

- [x] #1: Desk version `^0.1.0` unresolvable → release binary pins exact version
- [x] #2: No `go.work` after `moca init` → `initGoWorkspace()` creates it
- [x] #3: Wrong replace path in app go.mod → standalone uses published version
- [x] #4: No `go.mod` in project root → created by init
- [x] #5: Builtin core nested-module gap → moved to `pkg/builtin/core`
- [x] #6: Server doesn't load app hooks → app init registry + auto-loading
- [x] #7: Vite can't resolve app extensions → resolve aliases + fs.allow
- [x] #8: App go.mod version missing `v` prefix → prepend `v`
- [x] #9: Scaffold doctype template missing `api_config` → included
- [x] #10: Scaffold `title_field` references non-existent field → fixed to `"title"`
- [x] #11: App install doesn't seed DocType records → `seedDocTypeAndPermRecords()`
- [x] #12: App install doesn't seed DocPerm records → combined with #11
- [x] #13: Core child doctypes missing permissions → added
- [x] #14: `setSite` not exported from desk → re-exported
- [x] #15: Scaffold main.tsx doesn't configure siteName → `createDeskApp()` calls `setSite()`
- [x] #16: Scaffold missing `desk/.env` → template added
- [x] #17: No OpenAPI/Swagger endpoint → added GET `/api/v1/openapi.json` + `/api/docs`

## Summary

| Metric | Value |
|--------|-------|
| Total steps tested | 27 |
| Steps passed | 25 |
| Steps failed | 1 (workflow registry empty) |
| Steps partial | 1 (transition not persisted) |
| New issues found | 4 (#18–#21) |
| Issues fixed during session | 1 (#19 — workflow registry wiring) |
| Previously fixed issues verified | 17/17 |
