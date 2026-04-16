# Security Review — 2026-04-16

**Scan date:** 2026-04-16  
**Scope:** Changes merged to default branch in the last 24 hours  
**Commits reviewed:**
- `b4f5bb7` — docs: add Moca v2 roadmap design spec
- `b6a9da6` — update readme again
- `0a86c19` — update readme
- `25f2eac` — refactor: reorganize docs/ and remove spikes/
- `ddc36f2` — refactoring spikes
- `6fe437e` — fix: resolve 4 DocType Builder DX issues (#29–#32) ← **primary security focus**
- `4179bf1` — adding dx session

---

## Findings

### FINDING-1 — Dev API Routes Accessible Without Authorization

| Field | Value |
|---|---|
| **Severity** | Critical |
| **Confidence** | High |
| **CWE** | CWE-306 (Missing Authentication for Critical Function) / CWE-862 (Missing Authorization) |
| **Release blocker** | Yes |

**Affected files:**
- `pkg/api/dev_handler.go` lines 33–40 (route registration), 118–194 (create), 198–247 (update), 285–311 (delete)
- `internal/serve/server.go` lines 354–357 (registration, no auth wrapper)

**Why this is exploitable:**

The `authMiddleware` in `pkg/api/gateway.go` applies to the entire mux, but it falls back to a `guestUser()` (role: `"Guest"`) for unauthenticated requests rather than returning HTTP 401. The dev handler (`RegisterDevRoutes`) registers five routes on the bare mux — create, update, get, delete DocType, and list apps — without any role-based authorization check. There is no `HasRole("Administrator")` or equivalent guard anywhere in the dev handler. As a result, any HTTP request from an unauthenticated client (or any authenticated user with any role) can call these routes when `developer_mode: true` is set in config.

**Attack scenario:**

An external attacker who can reach the server (or an authenticated user with only the `Guest` role) issues a `POST /api/v1/dev/doctype` with a valid JSON body. The request passes the auth middleware as a Guest, hits `HandleCreateDocType`, and creates a new DocType definition file on disk. The attacker can also call `DELETE /api/v1/dev/doctype/{name}` to remove existing DocType definitions, disrupting application structure. If combined with Finding 2 (path traversal), this escalates to arbitrary file write.

**Remediation:**

Add a role-check guard at the top of every dev handler method (or extract it into a single dev-mode middleware):

```go
// In pkg/api/dev_handler.go, add a helper:
func requireDevRole(w http.ResponseWriter, r *http.Request) bool {
    user, ok := UserFromContext(r.Context())
    if !ok || !slices.Contains(user.Roles, "Administrator") {
        writeJSON(w, http.StatusForbidden, map[string]string{
            "error": "developer API requires Administrator role",
        })
        return false
    }
    return true
}
```

Call `requireDevRole` as the first statement in each handler. Alternatively, wrap the entire `RegisterDevRoutes` registration with a role-checking middleware applied to the `/api/v1/dev/` prefix pattern.

---

### FINDING-2 — Path Traversal via Unsanitized `req.App` and `req.Module` Fields

| Field | Value |
|---|---|
| **Severity** | High |
| **Confidence** | High |
| **CWE** | CWE-22 (Improper Limitation of a Pathname to a Restricted Directory) |
| **Release blocker** | Yes |

**Affected files:**
- `pkg/api/dev_handler.go` lines 130–159 (`HandleCreateDocType`), 211–223 (`HandleUpdateDocType`)

**Why this is exploitable:**

`req.App` is used directly in `filepath.Join(h.appsDir, req.App, "modules", ...)` with no character validation — only an emptiness check (line 130). `req.Module` is passed through `toSnakeCaseDev()` → `meta.toSnakeCase()`, which preserves `.`, `/`, and `..` characters verbatim (the function only converts uppercase to lowercase and inserts underscores at word boundaries). `filepath.Join` in Go resolves `..` path components normally, so:

```
filepath.Join("/apps", "../../etc", "modules", "core", "doctypes", "x", "x.json")
// → "/etc/modules/core/doctypes/x/x.json"
```

`HandleCreateDocType` calls `os.MkdirAll(dtDir, 0o755)` followed by `os.WriteFile(jsonPath, data, 0o644)` — so an attacker can create directories and write arbitrary JSON files at any path reachable by the server process. It also writes a `.go` stub file (`os.WriteFile(goPath, []byte(stub), 0o644)`) whose content includes `req.Name` (validated) and `dtSnake` (derived from validated name), so the Go stub content itself is safe, but the path it is written to is not.

Additionally, `HandleUpdateDocType` takes `name` from `r.PathValue("name")` (no `ValidateDocTypeName` check for this path) and passes it through `toSnakeCaseDev` — allowing `name = ".."` to produce a traversed directory path used in file writes.

**Attack scenario:**

An attacker sends:

```http
POST /api/v1/dev/doctype HTTP/1.1
Content-Type: application/json

{
  "name": "Exploit",
  "app": "../../",
  "module": "etc",
  "fields": {"title": {"field_type": "Data"}}
}
```

The server resolves `dtDir` to `filepath.Join("/var/app/apps", "../../", "modules", "etc", "doctypes", "exploit")`, which resolves to `/modules/etc/doctypes/exploit` (or similar above the apps root). `os.MkdirAll` creates this path and `os.WriteFile` writes an attacker-controlled JSON file there. With write access to sensitive paths (e.g., application config directories, cron job directories, or web-accessible locations), this could enable remote code execution.

**Remediation:**

Validate `req.App` with an allowlist regex (alphanumeric, hyphen, underscore; no dots or slashes), and apply the same restriction to `req.Module`. After building `dtDir`, verify the resolved path is still under `h.appsDir` using `filepath.Rel`:

```go
// Validate app name
if !regexp.MustCompile(`^[a-z][a-z0-9_-]*$`).MatchString(req.App) {
    writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid app name"})
    return
}

// After filepath.Join, confirm we stayed inside appsDir:
rel, err := filepath.Rel(h.appsDir, dtDir)
if err != nil || strings.HasPrefix(rel, "..") {
    writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
    return
}
```

Apply the same guard in `HandleUpdateDocType`, and add `ValidateDocTypeName(name)` to `HandleUpdateDocType` for the URL-sourced `name` value.

---

## Informational — ValidateFieldDefs Does Not Check Against Field Type Allowlist

| Field | Value |
|---|---|
| **Severity** | Medium |
| **Confidence** | Medium |
| **Release blocker** | No |

**Introduced by:** `pkg/api/dev_validation.go` line 83–89 (new function), called from `dev_handler.go` lines 148, 215.

The new `ValidateFieldDefs` function correctly rejects empty `field_type` values (addressing issue #32), but does not validate that the value is one of the 35 recognized `FieldType` values defined in `pkg/meta/fielddef.go`. The registry compiler (`pkg/meta/compiler.go` line 233) does call `f.FieldType.IsValid()` at registration time, but errors there are currently logged as non-fatal warnings (dev_handler.go line 189: `"registry registration failed (non-fatal)"`). The JSON file with an invalid field type is still written to disk and could cause silent misbehavior when the DocType is later loaded by the production registry.

**Recommended follow-up:** Call `meta.FieldType(fd.FieldType).IsValid()` inside `ValidateFieldDefs` and return an error for unrecognized types, so the bad definition is rejected before being persisted.

---

## Summary

Two release-blocking vulnerabilities exist in `pkg/api/dev_handler.go`, both introduced or exposed by the #29–#32 DX fix PR. The root issues predate this PR but were not worsened by the new field-type validation code — they remain unmitigated:

1. **Critical**: All five dev-mode API routes (`/api/v1/dev/*`) are callable by unauthenticated (Guest) users with zero authorization enforcement.
2. **High**: User-controlled `req.App` and `req.Module` fields flow unsanitized into `filepath.Join`, enabling path traversal to write files outside the apps directory.

Combined, these two issues constitute an unauthenticated arbitrary file-write vulnerability on any Moca server running with `developer_mode: true`.

---

## Top Risky Areas Reviewed

- `pkg/api/dev_handler.go` — file I/O from user-supplied fields, no auth guard
- `pkg/api/gateway.go` + `pkg/api/middleware.go` — auth middleware Guest fallback
- `pkg/auth/authenticator.go` — Guest fallback confirmed; no enforcement for dev routes
- `internal/serve/server.go` — dev route registration path
- `desk/src/pages/DocTypeBuilder.tsx` — frontend field-type validation (benign, matches backend)
- `pkg/meta/compiler.go` + `pkg/meta/fielddef.go` — field type allowlist (downstream of the issue)

## Gaps and Uncertainty

- The `desk` submodule was updated but is not locally checked out in full; frontend review was limited to git diff output. No XSS vectors were identified in the diff, but a full audit of the React components was not possible.
- Developer mode is presumably not enabled in production; however, there is no network-level enforcement of this assumption (e.g., binding dev routes only to loopback). If an operator misconfigures `developer_mode: true` on a public-facing server, these issues become externally exploitable.
- It is not confirmed whether the Go stub file write path in `HandleCreateDocType` can reach a location that would be executed (e.g., a plugin directory scanned at startup). This deserves investigation if path traversal is confirmed exploitable.

## Recommended Follow-Up Checks

1. **Immediately add role check** to all five dev handler methods (or a surrounding middleware) — one-line fix with high impact.
2. **Add app/module name sanitization** with a strict regex and a `filepath.Rel` containment guard.
3. **Audit all other handlers** that write to disk based on user input for similar path traversal patterns (e.g., backup, storage, scaffold handlers).
4. **Consider binding dev routes to localhost only** in addition to auth enforcement, as a defense-in-depth measure.
5. **Upgrade `ValidateFieldDefs`** to check against the `meta.ValidFieldTypes` allowlist.
