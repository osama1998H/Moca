# MS-22 — Security Hardening: OAuth2, SAML/OIDC, Encryption, Notifications Plan

## Milestone Summary

- **ID:** MS-22
- **Name:** Security Hardening — OAuth2, SAML/OIDC, Encryption, Notifications
- **Roadmap Reference:** ROADMAP.md → MS-22 section (lines 1134-1167)
- **Goal:** Enterprise SSO (OAuth2, SAML, OIDC), sensitive field encryption, email/in-app notifications, backup encryption.
- **Why it matters:** Beta-blocking security requirements. Enterprise customers need SSO. Encryption is compliance-required. Notifications are essential for document-driven workflows.
- **Position in roadmap:** Order #23 of 30 milestones (5th in Beta phase)
- **Estimated duration:** 3 weeks
- **Upstream dependencies:** MS-14 (Permission Engine — complete), MS-15 (Background Jobs/Events — complete)
- **Downstream dependencies:** Not a direct blocker for any milestone, but part of Beta phase. MS-23 (Workflow Engine) will use notifications for escalation.

## Vision Alignment

MS-22 closes the last major security and communication gaps before Moca can enter Beta. The framework already has session-based auth, JWT tokens, API keys, and a full RBAC permission engine. What's missing is enterprise-grade SSO (OAuth2/SAML/OIDC) for organizations that require centralized identity, encryption at rest for compliance, and a notification system to drive document-event communication.

Notifications are a foundational building block: MS-23 (Workflow Engine) needs them for SLA escalation, and any real-world business app needs email and in-app alerts on document events. Building them here means the workflow engine can focus on state machine logic without re-inventing delivery.

Encryption completes the "Defense in Depth" architecture (§13.3 of the system design): Layer 7 requires sensitive fields encrypted at rest with AES-256-GCM. Backup encryption extends this to disaster recovery compliance.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `ROADMAP.md` | MS-22 | 1134-1167 | Milestone definition, scope, acceptance criteria |
| `MOCA_SYSTEM_DESIGN.md` | §13 Security Architecture | 1840-1880 | Auth methods table (OAuth2, SAML/OIDC), Defense in Depth layers, AES-256-GCM encryption |
| `MOCA_SYSTEM_DESIGN.md` | §15 Package Layout (notify/) | 2019-2023 | Planned notify/ package structure: email.go, push.go, inapp.go, sms.go |
| `MOCA_SYSTEM_DESIGN.md` | §15 Package Layout (auth/) | 1968-1973 | Planned auth/ files: oauth2.go, sso.go |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.21 Notification Configuration | 3246-3278 | `moca notify test-email` and `moca notify config` CLI specs |
| `ROADMAP.md` | MS-06 scope | 434 | OAuth2 explicitly deferred from API layer to MS-22 |
| `ROADMAP.md` | MS-11 scope | 663 | Backup encryption explicitly deferred to MS-22 |
| `ROADMAP.md` | MS-14 scope | 805 | OAuth2/SAML/OIDC explicitly deferred to MS-22 |

## Research Notes

**Go libraries for SSO (no web research needed — standard ecosystem choices):**
- `golang.org/x/oauth2` — OAuth2 client with provider presets (Google, GitHub, etc.)
- `github.com/coreos/go-oidc/v3` — OIDC discovery and ID token verification
- `github.com/crewjam/saml` — SAML 2.0 Service Provider (recommended in ROADMAP.md risks section)
- `github.com/aws/aws-sdk-go-v2/service/ses` — AWS SES for email (optional, SMTP is primary)

**Encryption approach:** AES-256-GCM for field-level encryption (standard library `crypto/aes` + `crypto/cipher`). For streaming backup encryption, AES-CTR + HMAC-SHA256 (encrypt-then-MAC) since GCM has a 64 GB limit per nonce and backups can be large.

**Integration pattern:** SSO flows create standard sessions via the existing `SessionManager.Create()`, so the existing auth middleware chain handles post-SSO requests automatically — no middleware changes needed.

---

## Milestone Plan

### Task 1: AES-256-GCM Field Encryption + Backup Encryption

- **Task ID:** MS-22-T1
- **Title:** Sensitive Field Encryption & Backup Encryption
- **Status:** Completed

- **Description:**
  Build the cryptographic foundation that the rest of MS-22 depends on. Two sub-deliverables:

  **1A. Core cipher module (`pkg/auth/crypto.go`):**
  - `FieldEncryptor` struct holding a 32-byte AES-256 key (from 64-char hex string via `MOCA_ENCRYPTION_KEY` env var or config)
  - `Encrypt(plaintext) → string`: returns `enc:v1:` + base64(12-byte-random-nonce || AES-GCM-ciphertext)
  - `Decrypt(ciphertext) → string`: detects `enc:v1:` prefix, decodes, splits nonce, decrypts
  - The `enc:v1:` prefix prevents double-encryption and enables future key rotation versioning

  **1B. Document lifecycle integration (`pkg/auth/field_encryption.go`):**
  - Add `PostLoadTransformer` interface to `DocManager` (following the existing `SetHookDispatcher` pattern at `pkg/document/crud.go:392`)
  - `SetPostLoadTransformer(t PostLoadTransformer)` on DocManager
  - Call transformer at the end of `Get()` (after line 1115), `GetList()`, and `GetSingle()` — after row scan, before return
  - `FieldEncryptionHook` implements both:
    - **BeforeSave hook** (registered via HookRegistry): iterates `meta.Fields` for `FieldTypePassword` fields, encrypts values that lack the `enc:v1:` prefix
    - **PostLoadTransformer**: decrypts `FieldTypePassword` fields that have the `enc:v1:` prefix
  - Wire in `internal/serve/server.go` after line 107: if `MOCA_ENCRYPTION_KEY` is set, create `FieldEncryptor`, register BeforeSave hook on HookRegistry, set PostLoadTransformer on DocManager

  **1C. Backup encryption (`pkg/backup/encrypt.go`):**
  - `EncryptStream(w io.Writer, key []byte) → (io.WriteCloser, error)`: streaming AES-CTR + HMAC-SHA256
  - `DecryptStream(r io.Reader, key []byte) → (io.Reader, error)`: verifies HMAC, decrypts
  - File format: `MOCA_ENC_V1` (8-byte magic) + salt (32 bytes) + IV (16 bytes) + encrypted-data + HMAC (32 bytes)
  - Key derived from config key via HKDF(SHA-256, key, salt)
  - Modify `pkg/backup/create.go`: chain encryption writer when `opts.Encrypt` is true; output `.enc` extension
  - Modify `pkg/backup/restore.go`: auto-detect `.enc` extension, wrap reader with `DecryptStream`
  - Modify `pkg/backup/types.go`: add `Encrypt bool` and `EncryptionKey string` to `CreateOptions` and `RestoreOptions`
  - Modify `cmd/moca/backup.go`: add `--encrypt` flag to create, `--decrypt` flag to restore (auto-detect)

- **Why this task exists:** Encryption is a self-contained primitive with zero external library dependencies (all Go stdlib). Both field encryption and backup encryption use the same key material. OAuth2 provider secrets (Task 2) and notification credentials need encryption at rest.

- **Dependencies:** None (first task)

- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §13.3 Defense in Depth, line 1879: "Layer 7: Encryption → Sensitive fields encrypted at rest (AES-256-GCM)"
  - `ROADMAP.md` MS-22, line 1145: "Transparent field encryption for Password and sensitive fields"
  - `ROADMAP.md` MS-22, line 1148: "Backup encryption: moca backup create --encrypt"
  - `ROADMAP.md` MS-11, line 663: "no encryption (MS-22)" — deferred from MS-11
  - `pkg/document/crud.go` lines 380-404: DocManager struct, SetHookDispatcher pattern
  - `pkg/document/crud.go` lines 1056-1122: Get() method — transformer insertion point
  - `internal/config/types.go`: BackupConfig already has `EncryptionKey` and `Encrypt` fields
  - `pkg/backup/create.go`: existing gzip compression pipeline to chain encryption into
  - `pkg/backup/restore.go`: existing decompression pipeline

- **Deliverable:**
  - `pkg/auth/crypto.go` + `pkg/auth/crypto_test.go`
  - `pkg/auth/field_encryption.go` + `pkg/auth/field_encryption_test.go`
  - `pkg/backup/encrypt.go` + `pkg/backup/encrypt_test.go`
  - Modified: `pkg/document/crud.go` (PostLoadTransformer interface + calls in Get/GetList/GetSingle)
  - Modified: `pkg/backup/create.go`, `pkg/backup/restore.go`, `pkg/backup/types.go`
  - Modified: `cmd/moca/backup.go` (--encrypt/--decrypt flags)
  - Modified: `internal/serve/server.go` (encryption wiring)

- **Acceptance Criteria:**
  - `FieldEncryptor.Encrypt()` → `Decrypt()` roundtrip succeeds for arbitrary strings
  - Wrong key returns an error on decrypt
  - Double-encryption is prevented (idempotent on `enc:v1:` prefix)
  - Password-type fields stored in DB have `enc:v1:` prefix (encrypted at rest)
  - Password-type fields returned from `DocManager.Get()` are decrypted transparently
  - `moca backup create --encrypt` produces `.enc` file with valid header
  - `moca backup restore` with `.enc` file auto-detects and decrypts correctly
  - Wrong encryption key on restore returns a clear error
  - All new code has unit tests; backup encryption has integration test

- **Risks / Unknowns:**
  - Key rotation strategy: the `v1` version prefix allows future multi-key support, but rotation migration (re-encrypting all fields) is deferred
  - Streaming encryption for very large backups (>64 GB) — AES-CTR + HMAC has no practical size limit, unlike GCM

---

### Task 2: OAuth2, SAML 2.0 SP, and OIDC Integration

- **Task ID:** MS-22-T2
- **Title:** Enterprise SSO — OAuth2 Authorization Code Flow, SAML 2.0 SP, OIDC Discovery
- **Status:** Completed

- **Description:**
  Implement three SSO protocols that create standard Moca sessions, integrating seamlessly with the existing auth middleware chain.

  **2A. SSO Provider doctype (`pkg/builtin/core/modules/core/doctypes/sso_provider/`):**
  - New `sso_provider.json` MetaType definition with fields:
    - `provider_name` (Data, required, unique) — e.g., "google", "okta-saml"
    - `protocol` (Select: "oauth2" / "oidc" / "saml")
    - `client_id` (Data), `client_secret` (Password — encrypted via Task 1)
    - `authorization_url`, `token_url`, `userinfo_url` (Data)
    - `scopes` (Data), `redirect_url` (Data)
    - `idp_metadata_url` (Data), `idp_metadata_xml` (LongText) — SAML
    - `certificate` (LongText), `private_key` (Password — encrypted via Task 1) — SAML
    - `enabled` (Check), `auto_create_user` (Check), `default_role` (Link to Role)
  - Table auto-created by MetaType migrator (existing pattern)

  **2B. OAuth2 provider (`pkg/auth/oauth2.go`):**
  - `OAuth2Provider` struct wrapping `golang.org/x/oauth2.Config`
  - `HandleAuthorize(w, r)`: generates redirect URL with CSRF `state` param (stored in Redis, 10-min TTL)
  - `HandleCallback(w, r)`: validates state, exchanges code for token, fetches userinfo, find-or-create local User, creates session via `SessionManager.Create()`, redirects to Desk with `moca_sid` cookie
  - User auto-provisioning: if user doesn't exist locally and `auto_create_user` is enabled, create User doc with random password and `default_role`

  **2C. OIDC provider (`pkg/auth/oidc.go`):**
  - `OIDCProvider` wrapping `github.com/coreos/go-oidc/v3`
  - Extends OAuth2 flow: auto-discovers endpoints from `/.well-known/openid-configuration`
  - Validates `id_token` via `IDTokenVerifier`
  - Claims mapping: `sub` → user lookup key, `email` → User.Email, `name` → User.FullName

  **2D. SAML 2.0 SP (`pkg/auth/saml.go`):**
  - `SAMLProvider` wrapping `github.com/crewjam/saml.ServiceProvider`
  - `HandleMetadata(w, r)`: serves SP metadata XML at `/api/v1/auth/saml/metadata`
  - `HandleACS(w, r)`: processes SAML assertion (POST binding), extracts NameID and attributes, maps to local User, creates session
  - Parses IdP metadata from URL or inline XML

  **2E. User provisioner (`pkg/auth/user_provisioner.go`):**
  - `UserProvisioner.FindOrCreate(ctx, site, email, fullName, defaultRole) → (*User, error)`
  - Shared by all three SSO flows
  - Creates User document with random password (never used — SSO users authenticate via IdP only)

  **2F. SSO HTTP handler (`pkg/api/sso_handler.go`):**
  - `SSOHandler` loads SSO provider configs from DB, constructs provider instances
  - Routes:
    - `GET /api/v1/auth/oauth2/authorize?provider={name}` — redirect to IdP
    - `GET /api/v1/auth/oauth2/callback?provider={name}` — code exchange
    - `GET /api/v1/auth/saml/metadata?provider={name}` — SP metadata
    - `POST /api/v1/auth/saml/acs?provider={name}` — assertion consumer
  - Registered in `internal/serve/server.go` after the existing `authHandler.RegisterRoutes()` (line 179)

  **Key integration point:** SSO flows end by calling `SessionManager.Create()`, which produces a `moca_sid` cookie. The existing `authMiddleware` in `pkg/api/middleware.go` already checks session cookies as its second auth tier — no middleware modification needed.

- **Why this task exists:** Enterprise SSO is a Beta-blocking requirement. OAuth2/OIDC cover cloud identity providers (Google, Okta, Azure AD). SAML covers legacy enterprise IdPs. All three share the session-creation pattern.

- **Dependencies:** MS-22-T1 (field encryption for client_secret and private_key storage)

- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §13.1, lines 1847-1851: Auth methods table listing OAuth2, SAML/OIDC
  - `MOCA_SYSTEM_DESIGN.md` §15, lines 1971-1972: Planned `pkg/auth/oauth2.go` and `pkg/auth/sso.go`
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.21, line 3278: SSO configured via Desk UI Settings > Authentication
  - `ROADMAP.md` MS-22, lines 1143-1144, 1151-1153: OAuth2/SAML/OIDC deliverables and acceptance criteria
  - `pkg/auth/session.go`: SessionManager.Create() — the integration point for all SSO flows
  - `pkg/auth/authenticator.go`: MocaAuthenticator 3-tier chain — SSO sessions feed into tier 2
  - `pkg/auth/user_loader.go`: UserLoader.LoadByEmail() — user lookup after SSO
  - `internal/serve/server.go` lines 114-118: Existing auth wiring point

- **Deliverable:**
  - `pkg/builtin/core/modules/core/doctypes/sso_provider/sso_provider.json`
  - `pkg/auth/oauth2.go` + `pkg/auth/oauth2_test.go`
  - `pkg/auth/oidc.go` + `pkg/auth/oidc_test.go`
  - `pkg/auth/saml.go` + `pkg/auth/saml_test.go`
  - `pkg/auth/user_provisioner.go` + `pkg/auth/user_provisioner_test.go`
  - `pkg/api/sso_handler.go` + `pkg/api/sso_handler_test.go`
  - Modified: `internal/serve/server.go` (SSO handler wiring)
  - Modified: `pkg/builtin/core/manifest.yaml` (add SSOProvider doctype)
  - Modified: `go.mod` (add golang.org/x/oauth2, coreos/go-oidc/v3, crewjam/saml)

- **Acceptance Criteria:**
  - OAuth2 flow: redirect → authorize → code → token → session cookie set → user logged in
  - OIDC: configure Google/Okta provider, login flow completes and creates session
  - SAML: configure IdP metadata, SSO login flow completes via POST binding ACS
  - CSRF state parameter validated on callback (reject replayed/expired states)
  - Auto-provisioned users get the configured default role
  - `client_secret` and `private_key` stored encrypted at rest (via Task 1)
  - Unit tests with httptest.Server mocking IdP responses for all three protocols
  - SAML tests use pre-built assertion XML

- **Risks / Unknowns:**
  - SAML 2.0 has many edge cases (clock skew, multiple assertions, encrypted assertions). Start with POST binding + unencrypted assertions (most common). Add redirect binding and assertion encryption as follow-up if needed.
  - `crewjam/saml` library maturity — it's the most used Go SAML library but less battle-tested than Java/Python equivalents

---

### Task 3: Notification System — Email, In-App, Event-Driven Dispatch

- **Task ID:** MS-22-T3
- **Title:** Email Sending (SMTP/SES), In-App Notifications, Event-Driven Dispatch
- **Status:** Completed

- **Description:**
  Build the full notification subsystem from the currently-stubbed `pkg/notify/` package.

  **3A. Notification config types (`internal/config/types.go`):**
  - Add `NotificationConfig` struct with `EmailConfig` (provider, SMTP settings, SES settings)
  - Add `SMTPConfig` (host, port, user, password, use_tls, from_name, from_addr)
  - Add `SESConfig` (region, from_addr)
  - Add `Notification NotificationConfig` field to `ProjectConfig`

  **3B. Email sender (`pkg/notify/email.go`):**
  - `EmailSender` interface: `Send(ctx, EmailMessage) error`
  - `EmailMessage`: To, CC, BCC, Subject, HTMLBody, TextBody, Attachments, Headers
  - `SMTPSender`: implementation using `net/smtp` with STARTTLS support
  - `SESSender`: implementation using `github.com/aws/aws-sdk-go-v2/service/ses` (optional dependency)
  - `NewEmailSender(cfg EmailConfig) → (EmailSender, error)`: factory based on provider string

  **3C. Template renderer (`pkg/notify/template.go`):**
  - `TemplateRenderer` using `html/template` from stdlib
  - Default templates: `notification_email.html`, `password_reset.html`, `welcome.html`
  - `Render(name string, data map[string]any) → (html, text string, error)`
  - Templates embedded via `embed.FS`

  **3D. In-app notifications (`pkg/notify/inapp.go`):**
  - New `Notification` doctype at `pkg/builtin/core/modules/core/doctypes/notification/notification.json`:
    - `for_user` (Link to User, indexed), `type` (Select: info/warning/error/success)
    - `subject` (Data), `message` (Text), `document_type` (Data), `document_name` (Data)
    - `read` (Check, default 0, indexed), `email_sent` (Check, default 0)
  - `InAppNotifier` struct:
    - `Create(ctx, site, notif)` — inserts Notification document
    - `MarkRead(ctx, site, user, names...)` — marks notifications as read
    - `GetUnread(ctx, site, user, limit) → ([]Notification, count, error)`

  **3E. Notification settings doctype:**
  - New `NotificationSettings` doctype: per-doctype+event notification rules
    - `document_type` (Link to DocType), `event` (Select: on_create/on_update/on_submit/on_cancel)
    - `recipients` (Data — comma-separated roles or field names), `subject_template`, `message_template`
    - `send_email` (Check), `send_notification` (Check), `enabled` (Check)

  **3F. Event-driven dispatch (`pkg/notify/dispatcher.go`):**
  - `NotificationDispatcher` registers global hooks on `EventAfterInsert`, `EventAfterSave`, `EventOnSubmit`, `EventOnCancel` via the existing `HookRegistry`
  - On each event: load matching `NotificationSettings`, resolve recipients (expand roles → user emails), render templates, create in-app notifications directly, enqueue email jobs via `queue.Producer.Enqueue()` (uses existing Redis Streams + DLQ for retry)
  - Real-time push: publish to Redis PubSub channel `pubsub:notify:{site}:{user}` for the Desk notification bell (uses existing WebSocket bridge in `internal/serve/server.go` line 284)

  **3G. Notification API endpoints (`pkg/api/notification_handler.go`):**
  - `GET /api/v1/notifications` — list unread for current user
  - `GET /api/v1/notifications/count` — unread count (for bell badge)
  - `PUT /api/v1/notifications/mark-read` — mark specific or all as read
  - Registered in `internal/serve/server.go`

- **Why this task exists:** Notifications are required for document-event communication and are a prerequisite for MS-23 (Workflow Engine) escalation. Email and in-app are the two channels in scope; push and SMS are explicitly deferred.

- **Dependencies:** None strictly (can run in parallel with MS-22-T2 after T1). Uses existing queue/events infrastructure from MS-15.

- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §15 Package Layout, lines 2019-2023: notify/ with email.go, push.go, inapp.go, sms.go
  - `ROADMAP.md` MS-22, lines 1146-1147, 1156-1157: Email/in-app deliverables and acceptance criteria
  - `pkg/notify/doc.go`: Existing stub with architecture description
  - `pkg/hooks/registry.go`: HookRegistry for registering notification dispatch hooks
  - `pkg/queue/producer.go`: Queue producer for enqueuing email delivery jobs
  - `internal/serve/server.go` lines 109-112: Webhook dispatcher pattern (reference for notification dispatcher wiring)
  - `internal/serve/server.go` lines 284-290: PubSub bridge for real-time delivery

- **Deliverable:**
  - `pkg/notify/email.go` + `pkg/notify/email_test.go`
  - `pkg/notify/template.go` + `pkg/notify/template_test.go`
  - `pkg/notify/inapp.go` + `pkg/notify/inapp_test.go`
  - `pkg/notify/dispatcher.go` + `pkg/notify/dispatcher_test.go`
  - `pkg/notify/types.go` (shared types: EmailMessage, InAppNotification, etc.)
  - `pkg/builtin/core/modules/core/doctypes/notification/notification.json`
  - `pkg/builtin/core/modules/core/doctypes/notification_settings/notification_settings.json`
  - `pkg/api/notification_handler.go` + `pkg/api/notification_handler_test.go`
  - Modified: `internal/config/types.go` (NotificationConfig)
  - Modified: `internal/serve/server.go` (notification wiring)
  - Modified: `pkg/builtin/core/manifest.yaml` (add Notification, NotificationSettings doctypes)

- **Acceptance Criteria:**
  - SMTP email sending works (verified with `moca notify test-email`)
  - Email templates render correctly with document data
  - In-app notifications created for document events when NotificationSettings are configured
  - `GET /api/v1/notifications` returns unread notifications for the authenticated user
  - `GET /api/v1/notifications/count` returns accurate unread count
  - `PUT /api/v1/notifications/mark-read` updates read status
  - Email delivery failures are retried via the existing DLQ mechanism
  - Real-time notification push via WebSocket/PubSub for Desk UI
  - Unit tests with mock SMTP server for email, PostgreSQL for in-app CRUD

- **Risks / Unknowns:**
  - AWS SES dependency is optional — SMTP is the primary and required provider. If `aws-sdk-go-v2` adds too much dependency weight, SES can be deferred to a follow-up
  - Template design: starting with minimal built-in templates; custom template CRUD is out of scope for this milestone

---

### Task 4: CLI Commands + Server Wiring + End-to-End Integration Tests

- **Task ID:** MS-22-T4
- **Title:** CLI Notify Commands, Complete Server Wiring, End-to-End Tests
- **Status:** Not Started

- **Description:**
  Wire everything together, add CLI commands, and verify end-to-end.

  **4A. `moca notify` CLI command group (`cmd/moca/notify.go`):**
  - `moca notify test-email --site SITE --to EMAIL --provider smtp|ses|sendgrid`
    - Loads NotificationConfig from project config
    - Constructs EmailSender, sends a test message
    - Reports success/failure with diagnostic details (connection, auth, delivery)
  - `moca notify config --site SITE --set KEY=VALUE --json`
    - Reads/writes notification config values
    - `--json` outputs current config as JSON
    - `--set` updates values (e.g., `smtp.host=smtp.gmail.com`, `smtp.port=587`)
  - Register in `cmd/moca/commands.go`

  **4B. Complete server wiring (`internal/serve/server.go`):**
  - After existing auth setup (line 118):
    1. **Field encryption**: if `MOCA_ENCRYPTION_KEY` env var is set, create `FieldEncryptor`, register BeforeSave hook, set PostLoadTransformer on DocManager
    2. **SSO providers**: create `SSOHandler`, load provider configs, register routes on gateway mux
    3. **Notifications**: create `EmailSender` from config, create `InAppNotifier`, create `NotificationDispatcher`, register hooks on HookRegistry, create and register `NotificationHandler` routes
  - Register `notify_email` job type in worker subsystem for background email delivery

  **4C. End-to-end integration tests:**
  - `pkg/auth/oauth2_integration_test.go`: Full redirect → callback → session cycle with httptest IdP
  - `pkg/auth/saml_integration_test.go`: SAML ACS with pre-built assertion → session
  - `pkg/backup/encrypt_integration_test.go`: `backup create --encrypt` → `backup restore` → verify data integrity
  - `pkg/notify/integration_test.go`: Document save → notification settings match → email enqueued → in-app notification created
  - `cmd/moca/notify_integration_test.go`: CLI command smoke tests

- **Why this task exists:** Integration is where the pieces come together. CLI commands are the user-facing interface for notification config. End-to-end tests validate the full stack.

- **Dependencies:** MS-22-T1, MS-22-T2, MS-22-T3 (all must be complete)

- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.21, lines 3246-3278: `moca notify test-email` and `moca notify config` specs
  - `ROADMAP.md` MS-22, lines 1149, 1158-1159: CLI deliverables and acceptance criteria
  - `internal/serve/server.go`: Central wiring point (all new subsystems connect here)
  - `cmd/moca/commands.go`: CLI command registration
  - `cmd/moca/backup.go`: Reference for CLI command patterns

- **Deliverable:**
  - `cmd/moca/notify.go` + `cmd/moca/notify_test.go`
  - Modified: `cmd/moca/commands.go` (register notify commands)
  - Modified: `internal/serve/server.go` (complete wiring for encryption, SSO, notifications)
  - Integration test files for auth, backup, and notify packages

- **Acceptance Criteria:**
  - `moca notify test-email --to admin@test.com` sends test email and reports success/failure
  - `moca notify config --set smtp.host=smtp.gmail.com` updates notification provider settings
  - `moca notify config --json` outputs current notification config
  - Server starts successfully with all new subsystems wired
  - All integration tests pass with Docker services (PG, Redis, Meilisearch)
  - `moca backup create --encrypt` → `moca backup restore --decrypt` roundtrip works end-to-end

- **Risks / Unknowns:**
  - Worker subsystem needs a new job type registration for `notify_email` — verify the existing worker pool pattern supports this cleanly
  - Integration test reliability — SSO tests depend on httptest servers simulating IdP behavior

---

## Recommended Execution Order

1. **MS-22-T1** (Encryption) — No dependencies, provides crypto primitives for T2 and T3. ~4 days.
2. **MS-22-T2** (OAuth2/SAML/OIDC) + **MS-22-T3** (Notifications) — Can run **in parallel** after T1. Each ~4 days.
3. **MS-22-T4** (CLI + Wiring + E2E Tests) — Depends on T1, T2, T3. ~3 days.

```
T1: Encryption ──────┐
                     ├──→ T4: CLI + Wiring + E2E
T2: SSO (parallel) ──┤
T3: Notify (parallel)┘
```

## Open Questions

1. **Encryption key management**: Where should `MOCA_ENCRYPTION_KEY` be stored in production? Environment variable is standard, but should we also support a secrets manager (Vault, AWS Secrets Manager)? Recommendation: env var for now, secrets manager in a future milestone.

2. **SES dependency weight**: `aws-sdk-go-v2` pulls in significant dependencies. Should SES support be a compile-time flag or always included? Recommendation: include it — the Go module system handles unused code well, and SES is a common enterprise requirement.

3. **SAML binding support**: Start with POST binding only (most common), or also support Redirect binding? Recommendation: POST binding only for MS-22, add Redirect binding if a specific customer needs it.

4. **Notification real-time delivery**: The existing WebSocket hub + PubSub bridge can carry notification events. Should we define a specific message format now, or defer the Desk notification bell UI to MS-23? Recommendation: define the PubSub message format and backend push now; the Desk UI component can be minimal (bell + dropdown list).

## Out of Scope for This Milestone

- Push notifications (deferred per ROADMAP.md)
- SMS notifications (deferred per ROADMAP.md)
- OAuth2 **provider** mode (Moca acting as an OAuth2 authorization server for third-party apps)
- Visual SSO configuration wizard in Desk (configured via Settings > Authentication panel, which is basic form CRUD)
- Encryption key rotation migration tool
- Custom notification template CRUD UI
- SAML Redirect binding (POST binding only)
- Encrypted assertion support in SAML
