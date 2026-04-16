# Security Remediation Design Spec

**Date:** 2026-04-16
**Scope:** All findings from security reviews dated 2026-04-10, 2026-04-15, and 2026-04-16
**Delivery:** Single PR — all fixes, hardening, unit tests, and integration tests

---

## Summary

Three automated security scans identified 10 confirmed vulnerabilities across the dev API, SAML SSO, and encryption subsystems. Combined, the Critical + High findings constitute an **unauthenticated arbitrary file-write** on any Moca server with `developer_mode: true` (which is the scaffolding default). This spec covers remediation for all 10 findings plus proactive hardening.

---

## Findings and Remediations

### F1 — Dev API Routes Accessible Without Authorization (CRITICAL)

**Files:** `pkg/api/dev_handler.go`, `internal/serve/server.go:354-357`
**Root cause:** Dev routes registered on bare mux with no role check. `NoopAuthenticator` falls back to Guest. Even with real auth, no role guard exists.

**Fix:** Create a `devAuthMiddleware()` function in `pkg/api/dev_handler.go` that:
1. Extracts user from request context (set by the global `authMiddleware`)
2. Checks that `user.Roles` contains `"Administrator"`
3. Returns HTTP 403 with `{"error": "developer API requires Administrator role"}` if not

Refactor `RegisterDevRoutes` to accept variadic middleware and wrap each handler:

```go
func (h *DevHandler) RegisterDevRoutes(mux *http.ServeMux, version string, mw ...func(http.Handler) http.Handler) {
    wrap := func(hf http.HandlerFunc) http.Handler {
        var handler http.Handler = hf
        for i := len(mw) - 1; i >= 0; i-- {
            handler = mw[i](handler)
        }
        return handler
    }
    p := "/api/" + version + "/dev"
    mux.Handle("GET "+p+"/apps", wrap(h.HandleListApps))
    mux.Handle("POST "+p+"/doctype", wrap(h.HandleCreateDocType))
    mux.Handle("PUT "+p+"/doctype/{name}", wrap(h.HandleUpdateDocType))
    mux.Handle("GET "+p+"/doctype/{name}", wrap(h.HandleGetDocType))
    mux.Handle("DELETE "+p+"/doctype/{name}", wrap(h.HandleDeleteDocType))
}
```

In `internal/serve/server.go`, pass the middleware:

```go
devHandler.RegisterDevRoutes(gw.Mux(), "v1", devAuthMiddleware())
```

---

### F2 — Path Traversal via Unsanitized `req.App` and `req.Module` (HIGH)

**Files:** `pkg/api/dev_handler.go:130-159, 199-223`, `pkg/api/dev_validation.go`
**Root cause:** `req.App` checked only for emptiness. `req.Module` passes through `toSnakeCaseDev()` which preserves `.`, `/`, `..`. No containment check after `filepath.Join`.

**Fix — Two layers:**

**Layer 1 — Input validation** in `pkg/api/dev_validation.go`:

```go
var reAppName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func ValidateAppName(name string) error {
    if !reAppName.MatchString(name) {
        return errors.New("app name must match ^[a-z][a-z0-9_-]*$ (lowercase, digits, hyphens, underscores)")
    }
    return nil
}

func ValidateModuleName(name string) error {
    if !reAppName.MatchString(name) {
        return errors.New("module name must match ^[a-z][a-z0-9_-]*$ (lowercase, digits, hyphens, underscores)")
    }
    return nil
}
```

Call `ValidateAppName(req.App)` and `ValidateModuleName(req.Module)` in both `HandleCreateDocType` and `HandleUpdateDocType`, replacing the bare emptiness checks.

**Layer 2 — Path containment** in `pkg/api/dev_handler.go`:

```go
func (h *DevHandler) ensureInsideAppsDir(target string) error {
    abs, err := filepath.Abs(target)
    if err != nil {
        return fmt.Errorf("resolve path: %w", err)
    }
    base, err := filepath.Abs(h.appsDir)
    if err != nil {
        return fmt.Errorf("resolve appsDir: %w", err)
    }
    rel, err := filepath.Rel(base, abs)
    if err != nil || strings.HasPrefix(rel, "..") {
        return errors.New("path escapes apps directory")
    }
    return nil
}
```

Called after `filepath.Join` in `HandleCreateDocType` (after building `dtDir`) and `HandleUpdateDocType` (after building `jsonPath`).

**Additionally:** Add `ValidateDocTypeName(name)` at the top of `HandleUpdateDocType` for the URL-sourced `name` parameter, which currently has no validation.

---

### F3 — SAML Audience Validation Bypass (HIGH)

**File:** `pkg/auth/saml.go:152`
**Root cause:** `ParseResponse` called with `[]string{""}`, disabling audience restriction validation.

**Fix:** Replace `[]string{""}` with `[]string{p.sp.EntityID}`. The entity ID is already set correctly at construction (line 41).

```go
assertion, err := p.sp.ParseResponse(r, []string{p.sp.EntityID})
```

One-line change, fully contained.

---

### F4 — Plaintext Storage of SSO Secrets (HIGH)

**Files:** `pkg/api/sso_handler.go:425-454`, `pkg/auth/sso_config_loader.go`
**Root cause:** Secrets stored/loaded as plaintext when `MOCA_ENCRYPTION_KEY` is absent. No enforcement or warning.

**Fix — Hard enforcement** in `sso_handler.go` `loadAndDecryptConfig`:

```go
// Reject if no encryptor and secrets exist
if h.encryptor == nil {
    if cfg.ClientSecret != "" || cfg.SPPrivateKey != "" {
        return nil, fmt.Errorf("SSO provider %q has secrets but MOCA_ENCRYPTION_KEY is not configured; "+
            "set the encryption key and re-save the provider", cfg.ProviderName)
    }
}

// Reject if encryptor exists but secrets are not encrypted (plaintext migration gap)
if h.encryptor != nil {
    if cfg.ClientSecret != "" && !auth.IsEncrypted(cfg.ClientSecret) {
        return nil, fmt.Errorf("SSO provider %q: client_secret is not encrypted; "+
            "re-save the provider to encrypt it", cfg.ProviderName)
    }
    if cfg.SPPrivateKey != "" && !auth.IsEncrypted(cfg.SPPrivateKey) {
        return nil, fmt.Errorf("SSO provider %q: sp_private_key is not encrypted; "+
            "re-save the provider to encrypt it", cfg.ProviderName)
    }
}
```

This covers both Finding 4 (no key) and Finding 5 (key added, old secrets still plaintext).

---

### F5 — No Migration Path for Plaintext to Encrypted Secrets (MEDIUM)

**Files:** `internal/serve/server.go`
**Root cause:** No startup detection of plaintext secrets when encryption key is present.

**Fix:** Add startup check in `server.go` after encryptor initialization. For the default site pool (startup context), query `tab_sso_provider` for rows where `client_secret` or `sp_private_key` is non-empty and doesn't start with the `enc:v1:` prefix. Log a `WARN` with affected provider names. If the table doesn't exist (SSO not configured), skip silently.

```go
if encryptor != nil {
    if providers := detectUnencryptedSSOSecrets(ctx, dbPool); len(providers) > 0 {
        logger.Warn("unencrypted SSO secrets detected — re-save these providers to encrypt them",
            slog.String("providers", strings.Join(providers, ", ")))
    }
}
```

Note: In multi-tenant setups, this check runs against the default site pool at startup. Other tenants' secrets are caught at request time by the F4 hard enforcement. The startup warning is best-effort — it catches the common single-site case without iterating all tenant pools.

The hard block happens at request time (F4 fix). This startup warning is informational — gives operators early notice.

---

### F6 — DeveloperMode Defaults to true (MEDIUM)

**File:** `cmd/moca/init.go:276`
**Root cause:** `moca init` scaffolds config with `DeveloperMode: true`.

**Fix:**
1. Change default to `DeveloperMode: false`
2. Add `--dev` flag to the init command: `initCmd.Flags().Bool("dev", false, "Enable developer mode in generated config")`
3. Add startup warning in `internal/serve/server.go`: if `DeveloperMode: true` and the configured listen address (from `cfg.Config.Development.Port` / bind config) is not loopback (`127.0.0.1` or `::1`) or is `0.0.0.0`/`:`/empty (listen-all), log:

```
WARN developer mode is enabled on a non-loopback address; dev API routes are exposed to the network
```

---

### F7 — Filesystem Paths Leaked in API Error Responses (MEDIUM)

**File:** `pkg/api/dev_handler.go:226` and other error returns
**Root cause:** `jsonPath` and `err.Error()` (which may contain paths) sent directly to clients.

**Fix:** Strip internal details from all client-facing error messages in dev handlers. Log full details server-side at DEBUG level. Affected locations:

| Line | Current | Fixed |
|------|---------|-------|
| 51 | `err.Error()` | `"internal error"` + debug log |
| 161 | `"create directory: " + err.Error()` | `"failed to create directory"` + debug log |
| 173 | `"write: " + err.Error()` | `"failed to write doctype"` + debug log |
| 226 | `"doctype not found at " + jsonPath` | `"doctype not found"` + debug log |

---

### F8 — ValidateFieldDefs Missing Allowlist Check (MEDIUM)

**File:** `pkg/api/dev_validation.go:83-89`
**Root cause:** Checks for empty `field_type` but not against `meta.ValidFieldTypes`.

**Fix:** Add `IsValid()` check:

```go
func ValidateFieldDefs(fields map[string]meta.FieldDef) error {
    for name, fd := range fields {
        if fd.FieldType == "" {
            return errors.New("field '" + name + "' has no field_type")
        }
        if !meta.FieldType(fd.FieldType).IsValid() {
            return fmt.Errorf("field '%s' has unrecognized field_type %q", name, fd.FieldType)
        }
    }
    return nil
}
```

---

### F9 — No Request Body Size Limit on Dev Endpoints (LOW)

**File:** `pkg/api/dev_handler.go:120, 201`
**Root cause:** `json.NewDecoder(r.Body)` without `MaxBytesReader`.

**Fix:** Add as the first line in both `HandleCreateDocType` and `HandleUpdateDocType`:

```go
r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB, matches REST gateway limit
```

---

### F10 — Silently Ignored os.ReadDir Errors (LOW)

**File:** `pkg/api/dev_handler.go:254, 260, 287, 293`
**Root cause:** `entries, _ := os.ReadDir(...)` discards errors.

**Fix:** Check errors. If not `os.IsNotExist`, log and return 500. If not exists, return 404:

```go
entries, err := os.ReadDir(h.appsDir)
if err != nil {
    if os.IsNotExist(err) {
        writeJSON(w, http.StatusNotFound, map[string]string{"error": "doctype not found"})
        return
    }
    h.logger.Error("read apps directory", slog.String("error", err.Error()))
    writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
    return
}
```

Same pattern for inner `os.ReadDir(modulesDir)` calls — log error, continue to next app (don't abort the whole search, but don't silently swallow).

---

## Hardening

### H1 — Audit Other Disk-Writing Handlers

Pre-implementation audit of `pkg/backup/`, `pkg/storage/`, `pkg/api/`, `internal/scaffold/` found no additional vulnerable handlers:

- `pkg/backup/` — uses `sitepath.ValidateName()` before path construction.
- `pkg/storage/local.go` — storage keys are UUID-based, not user-controllable.
- `pkg/api/upload.go` — same UUID-based key pattern.
- `internal/scaffold/` — app names regex-validated before path construction.

The path traversal vulnerability is contained to `pkg/api/dev_handler.go`. During implementation, verify these findings still hold and add a brief code comment in `dev_handler.go` noting that this was audited.

### H2 — `moca init --dev` Flag

Explicit opt-in for developer mode during project scaffolding (covered in F6).

### H3 — Non-Loopback Startup Warning

Covered in F6 — warns when dev mode is exposed beyond localhost.

---

## Files Modified

| File | Changes |
|------|---------|
| `pkg/api/dev_handler.go` | Auth middleware, path containment, body limit, error sanitization, ReadDir error handling, ValidateDocTypeName on update path |
| `pkg/api/dev_validation.go` | `ValidateAppName`, `ValidateModuleName`, field type allowlist check |
| `internal/serve/server.go` | Pass dev middleware, startup SSO warning, dev mode loopback warning |
| `pkg/auth/saml.go` | Audience fix (line 152) |
| `pkg/api/sso_handler.go` | Encryption enforcement in `loadAndDecryptConfig` |
| `cmd/moca/init.go` | Default `DeveloperMode: false`, `--dev` flag |
| `pkg/api/dev_handler_test.go` | Unit tests for all dev handler fixes |
| `pkg/auth/saml_test.go` | Unit test for audience validation |
| `pkg/api/sso_handler_test.go` | Unit test for encryption enforcement |
| `pkg/api/dev_validation_test.go` | Unit tests for new validators |
| `pkg/api/dev_api_integration_test.go` (new) | Integration tests for auth + path traversal through full stack |

---

## Testing Plan

### Unit Tests

- `devAuthMiddleware` rejects nil user, Guest user, non-admin user; accepts admin
- `ValidateAppName` rejects `../../`, `foo/bar`, empty, `.hidden`; accepts `core`, `my-app`, `app_v2`
- `ValidateModuleName` same pattern
- `ensureInsideAppsDir` rejects paths that resolve outside appsDir
- `HandleUpdateDocType` rejects invalid `name` from URL path
- `ValidateFieldDefs` rejects unknown field types
- `HandleCreateDocType` with body > 1 MiB returns 413
- Error responses do not contain filesystem paths
- SAML `ParseResponse` with wrong audience is rejected
- SSO `loadAndDecryptConfig` rejects plaintext secrets when encryptor is nil
- SSO `loadAndDecryptConfig` rejects unencrypted secrets when encryptor is present

### Integration Tests (build tag: `integration`)

- `POST /api/v1/dev/doctype` with no auth header returns 403
- `POST /api/v1/dev/doctype` with Guest role returns 403
- `POST /api/v1/dev/doctype` with Administrator role returns 201
- `POST /api/v1/dev/doctype` with `app: "../../"` returns 400
- `DELETE /api/v1/dev/doctype/{name}` with non-admin returns 403
- Full round-trip: admin creates, gets, updates, deletes a doctype successfully
