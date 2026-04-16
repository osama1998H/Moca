# Developer Experience Test Session Report

**Date:** 2026-04-15
**Version tested:** v1.0.4
**Method:** Install via `install.sh` from GitHub release pipeline
**Tester:** Claude + Osama (manual browser testing)
**Previous sessions:** v0.1.1-alpha.7 -> v0.1.8 -> v0.1.11 -> v1.0.1 -> v1.0.2

## Test Environment

| Component | Version / Config |
|-----------|-----------------|
| OS | macOS arm64 (Darwin 25.4.0) |
| Go | 1.26.1 |
| Node | 22 |
| PostgreSQL | 16 (Docker, port 5433) |
| Redis | 7 (Docker, port 6380) |
| Meilisearch | v1.12 (Docker, port 7700) |
| MinIO | Latest (Docker, ports 9002/9003) |
| Moca CLI | v1.0.4 (installed via install.sh) |
| Desk | @osama1998h/desk (from GitHub Packages, published by release pipeline) |

## Test Steps Performed

| # | Command / Action | Result | Notes |
|---|------------------|--------|-------|
| 1 | `git tag v1.0.4 && git push origin v1.0.4` | Pass | Tag created at commit b4f5bb7 |
| 2 | Wait for Release pipeline | Pass | Binary + desk npm package published (~2 min) |
| 3 | `curl ... install.sh \| MOCA_VERSION=1.0.4 sh` | Pass | Installed to ~/.local/bin, checksum verified |
| 4 | `moca version` | Pass | Shows v1.0.4, commit b4f5bb71, Go 1.26.1 |
| 5 | `docker compose up -d --wait` | Pass | All 5 containers healthy |
| 6 | `moca init /tmp/moca-dx-test/myproject --name myproject --db-user moca --db-password moca_test --db-port 5433 --redis-port 6380` | **Fail** | "Failed to scaffold desk frontend" — no error detail. See Issue #33 |
| 7 | Re-run `moca init` to different path | Pass | Second attempt succeeded; moved into place |
| 8 | Verify project structure | Pass | go.work, go.mod, desk/, moca.yaml, moca.lock, apps/, sites/ |
| 9 | Verify `developer_mode: true` in moca.yaml | Pass | Set correctly (fixed since v1.0.2) |
| 10 | Verify `desk/.env` has `VITE_MOCA_SITE` | **Fail** | File exists but value is empty: `VITE_MOCA_SITE=`. See Issue #34 |
| 11 | Manually set `VITE_MOCA_SITE=library.localhost` | Pass | Workaround |
| 12 | `moca desk install` | Pass | 253.5 MB installed |
| 13 | `moca site create library.localhost --admin-password admin123 --admin-email admin@library.localhost` | Pass | Schema created, admin seeded, 10 steps completed |
| 14 | `moca site use library.localhost` | Pass | Active site set |
| 15 | `moca app new library --module "Library Management" --title "Library Management" --publisher "Test" --doctype Book --desk` | Pass | App scaffolded with Book doctype |
| 16 | Verify module directory name | Pass | `library_management` (no space — regression #18 confirmed fixed) |
| 17 | `go work sync` | Pass | |
| 18 | `go mod tidy` (with GONOSUMCHECK) | Pass | New tag needs time for Go sum DB |
| 19 | `moca app install library` | Pass | MetaTypes registered, DocType + DocPerm seeded |
| 20 | `moca serve` (backend) | Pass | Listening on http://0.0.0.0:8000 |
| 21 | `moca desk dev` (frontend) | Pass | Vite on http://localhost:3000/desk/ |
| 22 | `curl POST /api/v1/auth/login` (with X-Moca-Site) | Pass | JWT returned, login successful |
| 23 | Browser login at /desk/login | **Fail** | "authentication failed". See Issue #35 |
| 24 | `curl POST /api/v1/auth/login` (with stale Bearer header) | **Fail** | Confirms Issue #35 — stale JWT causes 401 |
| 25 | Create BookReader DocType via DocType Builder (browser) | **Partial** | "BookReader created" then "Failed to load DocType: doctype not found". See Issue #36 |
| 26 | Verify BookReader files on disk | Pass | book_reader.json + book_reader.go created in apps/library/modules/ |
| 27 | Check backend logs for registration | **Fail** | `registry register default/BookReader: relation "tab_doctype" does not exist` — wrong site |
| 28 | Check BookReader JSON field_types | Pass | Data, Date, Int all populated correctly (v1.0.2 #32 fixed) |
| 29 | Check BookReader JSON for api_config | **Fail** | Missing `api_config` block. See Issue #37 |
| 30 | Check backend WARN logs | **Fail** | "leader election: lock lost" every ~15 min. See Issue #38 |

## Issues Found

### Issue #33: `moca init` fails intermittently on desk scaffolding

**Severity:** Major
**Component:** `cmd/moca/init.go` — desk scaffolding step
**Reproducible:** Intermittent (1 of 2 attempts failed)

The first `moca init` run failed with "Failed to scaffold desk frontend" with no additional error detail. A second run to a different path succeeded. The desk/ directory was created but empty on the failed attempt.

**Impact:** New developers hitting this on first run get a broken project with no actionable error message.
**Fix:** Add detailed error output to the desk scaffolding step. Investigate the transient failure cause (possibly a race condition in directory creation or template extraction).

---

### Issue #34: `VITE_MOCA_SITE` empty in scaffolded `desk/.env`

**Severity:** Major
**Component:** `internal/scaffold/desk.go` or `internal/scaffold/scaffold.go`
**Reproducible:** Always

`moca init` creates `desk/.env` with `VITE_MOCA_SITE=` (empty string). This was supposed to be fixed in v1.0.2 but the value is still blank. The site name is not known at `moca init` time (sites are created later), but the empty value causes the frontend API client to not send the `X-Moca-Site` header, leading to `TENANT_REQUIRED` errors.

**Impact:** Every new project requires manually editing `desk/.env` after site creation.
**Fix:** Either:
1. Populate `VITE_MOCA_SITE` during `moca site create` (update the .env as a post-creation step)
2. Add a `moca desk env` command that writes the active site to `desk/.env`
3. Have the Vite plugin auto-detect the site from `moca.yaml` if `VITE_MOCA_SITE` is empty

---

### Issue #35: Stale JWT in localStorage blocks login (auth middleware intercepts login endpoint)

**Severity:** Blocker
**Component:** `pkg/api/middleware.go:259`, `pkg/auth/authenticator.go:60-63`, `desk/src/api/client.ts`
**Reproducible:** Always (when stale token exists in localStorage from prior session on same port)

When a stale/expired JWT exists in browser localStorage (from a previous DX session or dev cycle on the same port), the API client sends it as `Authorization: Bearer <stale_token>` with every request — including `POST /api/v1/auth/login`. The auth middleware at `middleware.go:222` does not skip auth for `/api/v1/auth/*` routes. The `Authenticate()` function at `authenticator.go:60-63` returns an error for expired tokens (instead of falling through to Guest). The middleware returns 401 "authentication failed" before the login handler ever runs.

**Root cause chain:**
1. `client.ts:15`: `let accessToken = localStorage.getItem("moca_access_token")` — loads stale token
2. `AuthProvider.tsx:34-38`: `restore()` skips `clearAuth()` when no refresh token exists — stale access token persists
3. `client.ts:82-84`: `request()` sends stale Bearer header on all requests including login
4. `middleware.go:222-226`: Auth middleware does NOT skip `/api/v1/auth/login`
5. `authenticator.go:62-63`: Expired Bearer token → error → 401

**Impact:** Any developer reusing the same browser across Moca sessions (or after server restarts with new JWT secrets) is permanently locked out until they clear localStorage.

**Fix (two-pronged):**
1. **Backend:** Skip auth middleware for `POST /api/v1/auth/login` and `POST /api/v1/auth/refresh` (these endpoints don't need auth)
2. **Frontend:** In `AuthProvider.restore()`, call `setAccessToken(null)` when there's no refresh token (clear orphaned access tokens)

**Workaround:** Clear localStorage at `http://localhost:3000` (DevTools > Application > Local Storage > Clear) or use incognito window.

---

### Issue #36: Dev API creates DocType under wrong site ("default" instead of actual tenant)

**Severity:** Blocker
**Component:** `pkg/api/dev_handler.go:188,349-354`
**Reproducible:** Always

The Dev API handler uses a private `siteFromContext(r)` function (line 349) that reads a raw string `"site"` from context and falls back to `"default"`. The rest of the API uses the exported `SiteFromContext(ctx)` (from `context.go:46`) which reads from the tenant middleware's `*tenancy.SiteContext`.

**Backend log confirms:**
```
registry register default/BookReader: query current: ERROR: relation "tab_doctype" does not exist (SQLSTATE 42P01)
```

**Root cause:** `dev_handler.go:349-354` uses wrong context key. The tenant middleware stores the site under a typed key (`siteKey`), but `siteFromContext` looks for a raw string key `"site"` — they never match, so it always falls back to `"default"`.

**Impact:** DocType Builder is completely broken — DocTypes are saved to disk but never registered in the database. The success toast ("BookReader created") is misleading.

**Fix:** Replace `siteFromContext(r)` in dev_handler.go with `SiteFromContext(r.Context()).Name`, or remove the private function entirely and use the exported one.

---

### Issue #37: DocType Builder omits `api_config` from saved JSON

**Severity:** Major
**Component:** `desk/src/pages/DocTypeBuilder.tsx` (payload builder) or `pkg/api/dev_handler.go` (server-side defaults)
**Reproducible:** Always

DocTypes created via the DocType Builder do not include the `api_config` block in their JSON definition. The scaffolded Book doctype (created by `moca app new --doctype Book`) correctly includes `api_config: { enabled: true, ... }`, but builder-created DocTypes do not.

Without `api_config`, the REST API will not auto-generate CRUD routes for the DocType.

**Impact:** DocTypes created via the builder have no API endpoints.
**Fix:** Either the frontend should include a default `api_config` in the payload, or the backend should inject a default `api_config` when not present.

---

### Issue #38: "leader election: lock lost" log spam in single-instance mode

**Severity:** Minor
**Component:** `pkg/queue/leader.go` or scheduler subsystem
**Reproducible:** Always (every ~15 minutes)

When running `moca serve` in single-instance mode (one process handles HTTP + worker + scheduler), the leader election subsystem logs "leader election: lock lost (not owner)" every ~15 minutes. This is expected behavior (no competing workers to contest the lock), but the WARN-level log spam clutters the output.

**Impact:** Log noise; may confuse developers into thinking something is wrong.
**Fix:** Either suppress this warning in single-instance mode, or downgrade to DEBUG level.

## Regression Check (v1.0.2 Open Issues)

| Issue | Description | Status in v1.0.4 |
|-------|-------------|-------------------|
| #29 | Dev API fetch bypasses centralized API client | **Fixed** — no raw `fetch()` in DocTypeBuilder |
| #30 | No way to delete a column once added | **Fixed** — `removeColumn` implemented in builder store |
| #31 | DocType name validation not surfaced in frontend | **Fixed** — auto-PascalCase formatting on name input |
| #32 | Saved field_type is empty string | **Fixed** — BookReader JSON shows correct field_types |

## Commands Reference

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/osama1998H/moca/main/install.sh | MOCA_VERSION=1.0.4 sh

# Infrastructure
docker compose up -d --wait

# Project setup
moca init /tmp/moca-dx-test/myproject --name myproject --db-user moca --db-password moca_test --db-port 5433 --redis-port 6380
cd /tmp/moca-dx-test/myproject
moca desk install
moca site create library.localhost --admin-password admin123 --admin-email admin@library.localhost
moca site use library.localhost

# App setup
moca app new library --module "Library Management" --title "Library Management" --publisher "Test" --doctype Book --desk
go work sync
cd apps/library && GONOSUMCHECK=github.com/osama1998H/moca go mod tidy && cd ../..
moca app install library

# Manual fix required
echo "VITE_MOCA_SITE=library.localhost" > desk/.env

# Run servers
moca serve          # Backend on :8000
moca desk dev       # Frontend on :3000
```

## Credentials

| Service | URL | Credentials |
|---------|-----|-------------|
| Desk UI | http://localhost:3000/desk/ | admin@library.localhost / admin123 |
| Backend API | http://localhost:8000/api/v1/ | JWT + `X-Moca-Site: library.localhost` |
| PostgreSQL | localhost:5433 | moca / moca_test |
| Redis | localhost:6380 | (no auth) |
| Meilisearch | localhost:7700 | master key: moca_dev |
| MinIO | localhost:9003 | minioadmin / minioadmin |

## Summary

| Metric | Value |
|--------|-------|
| Version | v1.0.4 |
| Steps executed | 30 |
| New issues found | 6 (#33-#38) |
| Blockers | 2 (#35 stale JWT blocks login, #36 dev handler wrong site) |
| Major | 3 (#33 init fails intermittently, #34 empty VITE_MOCA_SITE, #37 missing api_config) |
| Minor | 1 (#38 leader election log spam) |
| v1.0.2 regressions fixed | 4/4 (#29, #30, #31, #32 all resolved) |
| Enhancement noted | Password visibility toggle (eye icon) on login form |

**Overall:** All 4 open issues from v1.0.2 are fixed. However, 2 new blockers emerged: the stale JWT/auth middleware issue (#35) prevents browser login after server restarts, and the dev handler site context mismatch (#36) makes the DocType Builder non-functional (creates files but fails to register). Both have clear root causes and straightforward fixes.
