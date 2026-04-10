# Security Review — Moca Codebase
**Scan Date:** 2026-04-10  
**Reviewer:** Automated Security Analysis (Claude sec-ops)  
**Commits Reviewed:** 2026-04-09 23:56 → 2026-04-10 01:28 (9 commits — SSO auth, field encryption, backup encryption)  
**Status:** ⚠️ 2 HIGH-severity findings require fixes before release

---

## Findings

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

## Areas Reviewed

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

## Gaps & Uncertainty

- **SAML Audience Bypass** — Confirmed via code analysis against the crewjam/saml library behavior; integration testing against a real IdP would conclusively verify.
- **SPPrivateKey Usage** — Loaded and passed to the SP struct; assumed correct given library usage. Worth tracing fully.
- **IdP Certificate Validation** — Delegated to the crewjam/saml library; assumed secure. Pin the library version and monitor advisories.
- **Cross-tenant state bypass** — State tokens include site name; appears properly isolated, but not exhaustively tested.

---

## Recommended Follow-Up

**Immediate (Before Next Release):**
1. Fix SAML audience validation — pass `p.sp.EntityID` as the allowed audience (15-min fix).
2. Enforce or warn on unencrypted SSO secrets (Finding #2 — 30-min fix).

**Pre-1.0:**
1. Add startup detection and warning for plaintext SSO secrets when encryption is enabled.
2. Add HSM/Vault support for `MOCA_ENCRYPTION_KEY` lifecycle management.
3. Audit all `Password`-type DocFields for consistent encryption across all doctypes.
4. Penetration test SSO flows: state token bypass, assertion injection, redirect manipulation.
