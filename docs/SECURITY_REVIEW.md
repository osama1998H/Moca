# Security Review — Moca Codebase

---

## Scan: 2026-04-15

**Scan Date:** 2026-04-15  
**Reviewer:** Automated Security Review (sec-ops scheduled task)  
**Commits Reviewed:**
- `9a725cb` — fix: resolve 7 DocType Builder DX issues (#22–#28) (#31) — 2026-04-14 19:46
- `617d239` — feat: DocType Builder with tree-native storage (#30) — 2026-04-14 15:24

**Files changed:** 25 files, 5018 insertions, 677 deletions  
**Status:** ⚠️ 1 HIGH + 2 MEDIUM findings require attention before production deployment

---

### Finding 1: Missing Authentication & Authorization on Developer API Endpoints

| Field | Value |
|---|---|
| **Severity** | HIGH |
| **Confidence** | High |
| **CWE** | CWE-306 (Missing Authentication for Critical Function) |
| **Files** | `pkg/api/dev_handler.go`, `internal/serve/server.go:354–358` |
| **Release Blocker** | Yes |

**Vulnerable Code:**
```go
// internal/serve/server.go ~line 354
if cfg.Config != nil && cfg.Config.Development.DeveloperMode && cfg.AppsDir != "" {
    devHandler := api.NewDevHandler(cfg.AppsDir, registry, logger)
    devHandler.RegisterDevRoutes(gw.Mux(), "v1")  // No auth middleware applied
    logger.Info("dev API endpoints enabled at /api/v1/dev/")
}
```

**Why Exploitable:** Dev routes are wired with no auth middleware. The codebase currently uses `NoopAuthenticator` (returns guest for all requests) and `AllowAllPermissionChecker` (permits every operation). The routes allow any caller to create, modify, or delete DocType definitions on the filesystem via `POST/PUT/DELETE /api/v1/dev/doctype`.

**Attack Scenario:** An attacker with network access to a running instance where `DeveloperMode: true` sends a `POST /api/v1/dev/doctype` request with a crafted DocType definition. Because no authentication check exists, the request succeeds and writes an arbitrary JSON file to `appsDir` on disk. Combined with Moca's hot-reload feature, this could be used to execute attacker-controlled schema definitions and escalate privileges within the application.

**Remediation:** Gate dev routes behind an explicit system-admin or developer-role check before registering them. At minimum, require the request to carry a valid session with a recognized privileged role and reject all others with HTTP 403. Even in `DeveloperMode`, unauthenticated write access to application schema files is unsafe.

---

### Finding 2: Path Traversal via Unvalidated `app` Field in File Path Construction

| Field | Value |
|---|---|
| **Severity** | Medium |
| **Confidence** | High |
| **CWE** | CWE-22 (Path Traversal) |
| **Files** | `pkg/api/dev_handler.go:130–169` |
| **Release Blocker** | Yes |

**Vulnerable Code:**
```go
dtDir := filepath.Join(h.appsDir, req.App, "modules", moduleSnake, "doctypes", dtSnake)
//                                 ^^^^^^^ req.App used directly — no format validation
```

**Why Exploitable:** `req.App` is validated only for emptiness and then embedded directly into a `filepath.Join` call. Unlike `req.Name` and `req.Module`, it is not transformed through `toSnakeCaseDev()`. A value such as `../../etc` or an absolute path could cause files to be written outside the intended `appsDir` boundary.

**Attack Scenario:** A caller (currently unauthenticated — see Finding 1) sends `{"app": "../../", "name": "Evil", "module": "Core", ...}`. The resolved `dtDir` escapes `appsDir`. The handler writes a JSON file to an arbitrary directory on the server, potentially overwriting application configuration or injecting files into other apps' directories.

**Remediation:** Add a strict allowlist validator for `req.App` (e.g., `^[a-z][a-z0-9_-]*$`). After computing the target path, call `filepath.Clean` and assert the result has `h.appsDir` as a prefix before any filesystem read or write.

---

### Finding 3: `DeveloperMode` Defaults to `true` in `moca init`

| Field | Value |
|---|---|
| **Severity** | Medium |
| **Confidence** | High |
| **CWE** | CWE-453 (Insecure Default Variable Initialization) |
| **Files** | `cmd/moca/init.go:276` |
| **Release Blocker** | No |

**Vulnerable Code:**
```go
Development: config.DevelopmentConfig{
    Port:          8000,
    Workers:       1,
    AutoReload:    true,
    DeveloperMode: true,  // enabled by default for every new project
},
```

**Why Exploitable:** Every project scaffolded with `moca init` ships with `DeveloperMode: true`. Developers who promote this config to staging or production without explicit review will expose the entire `/api/v1/dev/*` surface to the network.

**Attack Scenario:** A developer scaffolds a project, iterates locally, then deploys to a public-facing server without editing the generated config. The dev API is silently active in production, enabling schema manipulation by any network peer.

**Remediation:** Default to `DeveloperMode: false`. Print a startup warning if `DeveloperMode: true` and the process is not bound to localhost. Consider adding a `moca init --dev` flag to make the opt-in explicit.

---

### Finding 4: Filesystem Paths Leaked in API Error Responses

| Field | Value |
|---|---|
| **Severity** | Medium |
| **Confidence** | High |
| **CWE** | CWE-209 (Information Exposure Through an Error Message) |
| **Files** | `pkg/api/dev_handler.go:214–215` |
| **Release Blocker** | No |

**Vulnerable Code:**
```go
writeJSON(w, http.StatusNotFound, map[string]string{
    "error": "doctype not found at " + jsonPath,
    // e.g. "doctype not found at /home/ubuntu/myapp/apps/core/modules/selling/doctypes/so/so.json"
})
```

**Remediation:** Return `"doctype not found"` to the client and log the full path at `DEBUG` level using `slog`.

---

### [LOW] No Request Body Size Limit on Dev Endpoints

| Field | Value |
|---|---|
| **Severity** | Low |
| **Confidence** | High |
| **CWE** | CWE-400 (Uncontrolled Resource Consumption) |
| **Files** | `pkg/api/dev_handler.go:120` |
| **Release Blocker** | No |

The REST gateway applies a 1 MiB body limit, but `DevHandler` decodes request bodies without enforcing the same constraint. Wrap with `http.MaxBytesReader(w, r.Body, 1<<20)` before decoding.

---

### [LOW] Errors Silently Ignored in Directory Listing Handlers

| Field | Value |
|---|---|
| **Severity** | Low |
| **Confidence** | High |
| **CWE** | CWE-391 (Unchecked Error Condition) |
| **Files** | `pkg/api/dev_handler.go:242, 248` |
| **Release Blocker** | No |

`os.ReadDir` errors are discarded with `_`, silently returning empty results on permission or I/O failures. Check and log errors; return an appropriate HTTP status code.

---

### Positive Security Observations

- DocType and field names validated against strict regex patterns, limiting injection surface for most inputs.
- 13 reserved system field names are protected from user override.
- All routes (including dev) pass through CORS, tenant resolution, rate-limiting, and tracing middleware.
- No new Go module dependencies introduced (`go.mod` unchanged) — no supply-chain risk.
- Dev endpoints conditional on both `DeveloperMode: true` **and** a non-empty `AppsDir`.

---

### Top Risky Areas Reviewed

1. `pkg/api/dev_handler.go` — new file with write access to the filesystem; primary risk surface.
2. `internal/serve/server.go` — dev route registration with no auth wrapper.
3. `cmd/moca/init.go` — scaffolded default config enabling developer mode.

### Gaps & Uncertainty

- Full middleware chain applied to dev routes was inferred from `server.go` structure; direct integration test with auth enforcement would conclusively confirm Finding 1 exploitability.
- Hot-reload behaviour upon malicious DocType file writes was not fully traced — the exploitation window may be narrower if reload validation rejects malformed schemas.

### Recommended Follow-up Checks

1. **Verify auth stub replacement timeline** — if `NoopAuthenticator` is replaced before MS-17 ships, Finding 1 risk is reduced but not eliminated (dev routes still need an explicit guard).
2. **Add path-traversal tests** — `pkg/api/dev_handler_test.go` has no test cases for `req.App` containing `../` sequences.
3. **Audit `req.Layout` and `req.Permissions` deserialization** — the `LayoutTree` and permissions slice structures decoded from user input were not exhaustively reviewed.
4. **Review production deployment docs** — ensure `DeveloperMode` is explicitly called out as a value that must be `false` in any internet-facing deployment.

---

## Scan: 2026-04-10

**Scan Date:** 2026-04-10  
**Reviewer:** Automated Security Analysis (Claude sec-ops)  
**Commits Reviewed:** 2026-04-09 23:56 → 2026-04-10 01:28 (9 commits — SSO auth, field encryption, backup encryption)  
**Status:** ⚠️ 2 HIGH-severity findings require fixes before release

---

### Finding 1: SAML Audience Validation Bypass

| Field | Value |
|-------|-------|
| **Severity** | HIGH |
| **Confidence** | HIGH |
| **CWE** | CWE-347 — Improper Verification of Cryptographic Signature |
| **File** | `pkg/auth/saml.go` line 152 |
| **Release Blocker** | YES |

**Vulnerable Code:**
```go
assertion, err := p.sp.ParseResponse(r, []string{""})
```

**Why Exploitable:**
The SAML `ParseResponse` call passes `[]string{""}` as the allowed audiences. Per the SAML 2.0 specification (§2.5.1.4), every assertion MUST include an Audience restriction, and the SP must validate that the assertion's Audience matches its own entity ID. Passing an empty string disables this check, meaning Moca will accept SAML assertions issued for any other service provider — breaking the fundamental SAML security model that prevents assertion theft and cross-SP replay.

**Attack Scenario:**
1. Attacker controls a legitimate SAML-integrated service (e.g., `malicious-app.example.com`) at the same IdP Moca uses.
2. Attacker captures a SAML assertion for their own app (Audience = `malicious-app.example.com`) that identifies a target user by email.
3. Attacker replays this assertion to Moca's `/api/v1/auth/saml/acs` endpoint.
4. Moca accepts it (empty audience check passes), auto-provisions or logs in as the target user — full account takeover.

**Remediation:**
Replace:
```go
assertion, err := p.sp.ParseResponse(r, []string{""})
```
With:
```go
assertion, err := p.sp.ParseResponse(r, []string{p.sp.EntityID})
```
`p.sp.EntityID` is already set correctly to `metadataURL` at initialization (line 41) — just pass it as the valid audience.

---

### Finding 2: Plaintext Storage of SSO Secrets When Encryption Not Configured

| Field | Value |
|-------|-------|
| **Severity** | HIGH |
| **Confidence** | HIGH |
| **CWE** | CWE-312 — Cleartext Storage of Sensitive Information |
| **Files** | `pkg/api/sso_handler.go` lines 435–454 · `pkg/auth/sso_config_loader.go` lines 28–78 |
| **Release Blocker** | YES |

**Why Exploitable:**
OAuth2 `ClientSecret` and SAML `SPPrivateKey` are stored encrypted in the database only when `MOCA_ENCRYPTION_KEY` is set. If the variable is absent (the default for a fresh deployment), secrets are stored and loaded as plaintext without warning or enforcement. Database admins, backup contractors, or any attacker with DB read access can exfiltrate all SSO provider secrets. Database backups retain plaintext secrets indefinitely.

**Attack Scenario:**
1. Operator deploys Moca without setting `MOCA_ENCRYPTION_KEY` (easy to miss — it is optional).
2. Admin registers a Google OAuth2 provider with a production `client_secret`.
3. Secret is stored plaintext in `tab_sso_provider.client_secret`.
4. An attacker with DB read access (or via a leaked backup) extracts the secret.
5. Attacker uses the stolen client secret to impersonate Moca to Google's OAuth2 endpoint, enabling session hijack or exfiltration of user tokens.

**Remediation (Option A — Recommended):**
Require encryption for SSO secrets; reject load if unencrypted:
```go
// In sso_config_loader.go, loadAndDecryptConfig():
if cfg.ClientSecret != "" && !auth.IsEncrypted(cfg.ClientSecret) {
    return nil, fmt.Errorf("SSO provider %q: client_secret is not encrypted. "+
        "Set MOCA_ENCRYPTION_KEY and re-save the provider configuration.", cfg.ProviderName)
}
```

**Remediation (Option B — Softer):**
Warn at startup if SSO providers exist with unencrypted secrets, and document `MOCA_ENCRYPTION_KEY` as mandatory when SSO is in use.

---

### Finding 3: No Migration Path for Plaintext → Encrypted SSO Secrets

| Field | Value |
|-------|-------|
| **Severity** | MEDIUM |
| **Confidence** | MEDIUM |
| **CWE** | CWE-573 — Improper Following of Specification by Caller |
| **Files** | `pkg/api/sso_handler.go` line 52 · `internal/serve/server.go` line 147 |
| **Release Blocker** | NO |

**Why Exploitable:**
If encryption is added after initial deployment (operator sets `MOCA_ENCRYPTION_KEY` and restarts), existing plaintext secrets remain plaintext in the database — they are not re-encrypted on load. The operator receives no warning, creating a false sense of security. Plaintext secrets persist indefinitely unless the operator manually re-saves every SSO provider config.

**Remediation:**
Add a startup check that detects plaintext sensitive fields in SSO provider records when an encryptor is configured, and emit a clearly actionable warning:
```go
// In server.go NewServer(), after encryption setup:
if encryptor != nil {
    if providers := detectUnencryptedSSOSecrets(ctx, dbManager); len(providers) > 0 {
        logger.Warn("Unencrypted SSO secrets detected — they will NOT be automatically "+
            "re-encrypted. Re-save these providers to encrypt them.",
            slog.String("providers", strings.Join(providers, ", ")))
    }
}
```

---

### Areas Reviewed (2026-04-10)

| Component | File(s) | Status |
|-----------|---------|--------|
| OAuth2 Provider | `pkg/auth/oauth2.go` | ✅ Secure |
| OIDC Provider | `pkg/auth/oidc.go` | ✅ Secure |
| SAML Provider | `pkg/auth/saml.go` | ⚠️ Finding #1 |
| Field Encryption Hook | `pkg/encryption/field_encryption.go` | ✅ Secure |
| Crypto/Key Derivation | `pkg/auth/crypto.go`, `pkg/backup/encrypt.go` | ✅ Secure (HKDF + AES-256-CTR + HMAC-SHA256) |
| SSO API Handler | `pkg/api/sso_handler.go` | ⚠️ Finding #2 |
| SSO Config Loader | `pkg/auth/sso_config_loader.go` | ⚠️ Contributing to #2 |
| Session/State Mgmt | Redis-backed CSRF tokens | ✅ Secure |
| Redirect Validation | `isRelativePath()` check | ✅ Secure |
| User Auto-Provisioning | `pkg/auth/user_provisioner.go` | ✅ Secure |
| Backup Encryption | `pkg/backup/encrypt.go` | ✅ Secure |
| Tenant Isolation | State keys include site name | ✅ Appears correctly isolated |

---

### Gaps & Uncertainty (2026-04-10)

- **SAML Audience Bypass** — Confirmed via code analysis against the crewjam/saml library behavior; integration testing against a real IdP would conclusively verify.
- **SPPrivateKey Usage** — Loaded and passed to the SP struct; assumed correct given library usage. Worth tracing fully.
- **IdP Certificate Validation** — Delegated to the crewjam/saml library; assumed secure. Pin the library version and monitor advisories.
- **Cross-tenant state bypass** — State tokens include site name; appears properly isolated, but not exhaustively tested.

---

### Recommended Follow-Up (2026-04-10)

**Immediate (Before Next Release):**
1. Fix SAML audience validation — pass `p.sp.EntityID` as the allowed audience (15-min fix).
2. Enforce or warn on unencrypted SSO secrets (Finding #2 — 30-min fix).

**Pre-1.0:**
1. Add startup detection and warning for plaintext SSO secrets when encryption is enabled.
2. Add HSM/Vault support for `MOCA_ENCRYPTION_KEY` lifecycle management.
3. Audit all `Password`-type DocFields for consistent encryption across all doctypes.
4. Penetration test SSO flows: state token bypass, assertion injection, redirect manipulation.
