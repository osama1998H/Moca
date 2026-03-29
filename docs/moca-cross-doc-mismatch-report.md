# MOCA Cross-Document Consistency Review
## Design Document vs. CLI Design Document — Mismatch Report

**Report Date:** 2026-03-29
**Reviewer:** Senior Software Architect (automated cross-analysis)
**Documents Analyzed:**
- **Document A:** `MOCA_SYSTEM_DESIGN.md` — Framework System Design & Architecture (2040 lines)
- **Document B:** `MOCA_CLI_SYSTEM_DESIGN.md` — CLI System Design & Architecture (3599 lines)

---

# Executive Summary

## Overall Assessment

Both documents describe a well-conceived, ambitious framework. They share strong thematic alignment on the core mission: a metadata-driven, multitenant, full-stack Go business application platform with a Go-native CLI. However, after exhaustive cross-analysis across all 20 dimensions, **30 distinct mismatches** were identified — ranging from critical architectural gaps to minor internal contradictions. The most dangerous issues concern the Kafka-optional architecture (defined in CLI, absent in framework), the configuration source-of-truth problem (YAML on disk vs. PostgreSQL JSONB), the missing app binary loading contract, and several process/binary naming inconsistencies that would cause build confusion immediately.

## Consistency Score: **54 / 100**

The score reflects that the high-level vision is aligned but the **integration contracts** — the specific interfaces, file paths, process names, and runtime behaviors that the CLI uses to operate the framework — are either underdefined, contradictory, or entirely missing in one document.

## Highest-Risk Areas

1. **App compilation and loading model** — `moca build app` implies independent compilation, but no plugin-loading mechanism is defined in the framework.
2. **Kafka-optional architecture** — CLI fully supports a Kafka-less Redis-fallback mode; the framework design does not acknowledge this mode exists.
3. **Configuration authority** — CLI reads YAML files; framework stores config in PostgreSQL JSONB. Neither document defines the sync relationship.
4. **`moca-search-sync` process** — A fifth production process defined in the framework has no CLI management surface at all.
5. **Binary naming inconsistency** — `cmd/moca-cli/` (framework doc) vs `cmd/moca/` (CLI doc) would cause immediate build failures.

---

# Issue Matrix

---

## MISMATCH-001

- **Severity:** High
- **Category:** Direct Contradiction
- **Title:** CLI binary directory named `moca-cli` in framework doc vs `moca` in CLI doc

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §15 "Framework Package Layout"
- Lines: 1835–1836
- Content: `cmd/moca-cli/  # CLI tool (create-site, install-app, bench, migrate)`

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §9 "CLI Internal Package Layout"
- Lines: 3312–3316
- Content: `cmd/moca/   # Main CLI entry point` with `main.go`, `init.go`, etc.

**Problem:**
The framework package layout names the CLI binary directory `moca-cli`, while the CLI design document places it at `cmd/moca/`. In Go, the directory name under `cmd/` determines the compiled binary name by convention (`go build ./cmd/moca-cli` produces `moca-cli`; `go build ./cmd/moca` produces `moca`). The CLI document consistently calls the binary `moca` throughout (installation: `curl -sSL https://get.moca.dev | sh`, usage examples: `moca init`, etc.).

**Why it matters:**
This would cause an immediate build inconsistency. A developer reading the framework design would create `cmd/moca-cli/main.go` and get a binary named `moca-cli`. The CLI design assumes the binary is named `moca`. All documentation, shell completions, and operator guides would fail.

**Recommended fix:**
Update `MOCA_SYSTEM_DESIGN.md` §15 to use `cmd/moca/` (not `cmd/moca-cli/`), consistent with the CLI design and the final binary name.

**Update target:** Document A (`MOCA_SYSTEM_DESIGN.md`)

---

## MISMATCH-002

- **Severity:** High
- **Category:** Terminology Mismatch / One-Sided Definition
- **Title:** Traefik listed as reverse proxy in framework doc; absent from all CLI commands

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §1 "Technology Stack" table
- Lines: 29
- Content: `Reverse Proxy: Caddy / Traefik — Automatic TLS, tenant-based routing`

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.1 Command Tree, §4.2.8 `moca deploy setup`, §4.2.9 `moca generate caddy/nginx`
- Lines: 439–445, 1742–1745, 1897–1985
- Content: `moca generate caddy`, `moca generate nginx`, `--proxy string "caddy" (default), "nginx"` — Traefik never appears.

**Problem:**
The framework design explicitly lists Traefik alongside Caddy as supported reverse proxies. The CLI design supports only Caddy and NGINX. `moca deploy setup --proxy` accepts only `"caddy"` or `"nginx"`. There is no `moca generate traefik` command. Traefik has fundamentally different configuration syntax and dynamic discovery model compared to Caddy/NGINX.

**Why it matters:**
Any operator who chooses Traefik (a common enterprise choice, especially on Kubernetes) has no CLI support. The framework design implies Traefik is a first-class option; the CLI makes it impossible.

**Recommended fix:**
Either: (a) add `moca generate traefik` command and `--proxy traefik` option to `moca deploy setup` in Document B; or (b) remove Traefik from the framework tech stack table in Document A and replace with NGINX as the second supported option. A decision must be made on whether Traefik is in scope for v1.0.

**Update target:** Both documents must align. Decide supported proxies, then update both.

---

## MISMATCH-003

- **Severity:** Critical
- **Category:** Ambiguous Behavior / Missing Integration Contract
- **Title:** Configuration authority undefined — YAML files on disk (CLI) vs. PostgreSQL JSONB (framework)

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §4.2 "Core System Tables", §8.1 "Tenant Resolution" (`SiteConfig` in `SiteContext`)
- Lines: 854–862, 1349–1353
- Content: `config JSONB NOT NULL DEFAULT '{}'` in `moca_system.sites`; `SiteContext.Config SiteConfig` loaded from DB at request time.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §3 Project Structure, §3.1 `moca.yaml`, §4.2.7 `moca config get`
- Lines: 161–200, 201–302, 1627–1645
- Content: `sites/{site}/site_config.yaml`, `sites/common_site_config.yaml`, config resolution order with YAML files as authoritative tiers 2–4.

**Problem:**
The framework design shows that site configuration is stored in `moca_system.sites.config` as JSONB and loaded from the database on every request via `SiteContext`. The CLI design creates YAML files on disk (`site_config.yaml`, `common_site_config.yaml`, `moca.yaml`) and defines a 5-tier config resolution order where these files are authoritative. Neither document defines:
- How YAML files are synced to the database (or whether they are at all)
- Which is the write path (`moca config set` writes to YAML — does this also update the DB?)
- What happens when YAML and DB configs diverge
- Whether the running server reads YAML files at all, or only the database

**Why it matters:**
This is a fundamental runtime contract. If the server reads only from the DB and the CLI writes only to YAML files, configuration changes via the CLI would have no effect on a running server. If `moca config set` is the only way to update config and it only writes YAML, then a new site's config never makes it to the DB. This would cause silent operational failures.

**Recommended fix:**
Define explicitly: (1) on `moca site create`, YAML config is written AND synced to the DB; (2) on `moca config set`, the value is written to YAML AND the DB is updated atomically; (3) the running server reads from DB (with Redis cache), and CLI updates trigger both DB write and cache invalidation via `meta.changed` or a config-changed event. Add a "Config Sync Contract" section to both documents.

**Update target:** Both documents need a shared config sync specification.

---

## MISMATCH-004

- **Severity:** Critical
- **Category:** Direct Contradiction / Missing Integration Contract
- **Title:** Kafka is optional in CLI (Redis fallback) but mandatory and uncommented in framework design

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §1 Tech Stack, §6 "Kafka Event Streaming", §6.4 Transactional Outbox, §12.3 Process Types
- Lines: 25–26, 1089–1203, 1738–1741
- Content: Kafka listed as core infrastructure; outbox pattern described without conditions; `moca-outbox` process as always-present; all Kafka topics enumerated unconditionally.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.1 `moca init` flags, §3.1 `moca.yaml`
- Lines: 574–576, 244–249
- Content: `--kafka (default: true)`, `--no-kafka (Disable Kafka, Redis pub/sub will be used as fallback)`, `# Set false for small deployments that don't need event streaming. Redis pub/sub will be used as fallback`.

**Problem:**
The CLI explicitly supports a Kafka-disabled mode with Redis pub/sub as a fallback for "small deployments." The framework design never acknowledges this mode. Specifically:
- The transactional outbox pattern (§6.4) publishes to Kafka — what publishes to Redis pub/sub when Kafka is off?
- The `moca-outbox` process (§12.3) is Kafka-specific — does it not start when `kafka.enabled=false`?
- The five Kafka topics (§6.1) have defined consumers — where do those consumers go when Kafka is off?
- The audit log durability guarantees (90-day retention) would be lost with Redis pub/sub.
- WebSocket real-time (`doc:{site}:{doctype}:*` pub/sub) might work without Kafka, but CDC and integrations would not.

**Why it matters:**
A developer who deploys with `--no-kafka` and reads the framework design will have no idea what features are degraded, which processes to not start, or what the fallback architecture looks like. The framework will likely have conditional branches for Kafka vs non-Kafka that are completely invisible in the system design.

**Recommended fix:**
The framework design needs a §6.5 "Kafka-Optional Architecture" that explicitly defines: which features require Kafka, which fall back to Redis pub/sub, and which are unavailable in minimal mode. The CLI's `moca.yaml` comment says "Redis pub/sub will be used as fallback" — the framework must implement and document this fallback.

**Update target:** Primarily Document A (`MOCA_SYSTEM_DESIGN.md`); Document B should reference the limitations.

---

## MISMATCH-005

- **Severity:** Critical
- **Category:** Missing Integration Contract
- **Title:** `moca build app` implies independent app compilation but no app binary loading mechanism is defined

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §18 "What to Revisit" (Plugin sandboxing)
- Lines: 2034
- Content: "Plugin sandboxing — Currently, app code runs in the same process. For a marketplace model with untrusted plugins, consider WASM-based sandboxing for hook execution." (Implies apps run in-process, not as loadable binaries)

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.12 `moca build app`, §4.2.3 `moca app new` (go.mod/go.sum)
- Lines: 2222–2233, 882–891
- Content: `moca build app APP_NAME` — "Compile an app's Go code and verify it builds cleanly." Each app gets its own `go.mod` and `go.sum`.

**Problem:**
Go does not support hot-loading of compiled binaries into a running process (without CGo plugins or WASM). `moca build app` implies apps compile independently, but the framework design says "app code runs in the same process." If each app has its own `go.mod`, how are they composed into the single `moca-server` binary? There are only a few possible models (none of which are defined):
1. Go Workspaces (`go.work`) to build all apps together into one binary — not mentioned anywhere.
2. Go plugin system (`plugin.Open`) — works on Linux but fragile; not mentioned.
3. WASM sandboxing — listed as a *future* consideration, not current.
4. Each app is a dependency of the main module — but then why does each app have its own `go.mod`?

**Why it matters:**
This is the most critical unresolved architectural question. The entire app extensibility model is undefined at the Go compilation level. `moca build app` can verify syntax but cannot produce a loadable artifact without a defined integration model.

**Recommended fix:**
Add an ADR to the framework design defining the app compilation model. The most likely intent is a Go workspace (`go.work`) approach where `moca-server` imports all installed apps. Define: (1) how `go.work` is managed by the CLI; (2) whether `moca serve` triggers a recompile; (3) what `moca build app` actually produces and how it integrates. Update both documents with this ADR.

**Update target:** Both documents. A new ADR in Document A and a revised `moca build app` spec in Document B.

---

## MISMATCH-006

- **Severity:** Critical
- **Category:** Missing Integration Contract
- **Title:** `moca-search-sync` is a defined production process in framework doc with no CLI management surface

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §12.3 "Process Types"
- Lines: 1738–1741
- Content: `moca-search-sync | Kafka → Meilisearch | Horizontal, by topic partition`

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.9 `moca generate systemd`, §4.1 Command Tree
- Lines: 1921–1930
- Content: systemd generates: `moca-server@.service`, `moca-worker@.service`, `moca-scheduler.service`, `moca-outbox.service`, `moca.target` — `moca-search-sync` is absent.

**Problem:**
The framework design defines 5 process types. The CLI's `moca generate systemd` only generates 4 service files. `moca-search-sync` has no corresponding:
- systemd unit file generation
- `moca worker start/stop/status/scale` equivalent
- health check in `moca doctor` (which checks worker queues but not search sync lag)
- `moca monitor live` dashboard entry (no "search sync" row)

The search sync process is a critical production component — it keeps Meilisearch up to date. Its absence from the CLI means operators have no managed lifecycle for it.

**Why it matters:**
Without CLI management for `moca-search-sync`, operators must manually create systemd units, manually check process health, and have no way to use `moca doctor` to diagnose search sync lag. In the `moca monitor live` display, search sync lag would be invisible.

**Recommended fix:**
Add `moca-search-sync.service` to `moca generate systemd` output. Add `moca search-sync start/stop/status` commands (or incorporate into `moca worker` with a `--type search-sync` flag). Add search sync health to `moca doctor`. Update `moca monitor live` to show search sync consumer lag.

**Update target:** Document B (`MOCA_CLI_SYSTEM_DESIGN.md`)

---

## MISMATCH-007

- **Severity:** High
- **Category:** One-Sided Definition / Ambiguous Behavior
- **Title:** `HookDefs` type in `AppManifest` is declared but never defined; conflicts with code-based `hooks.go`

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §7.1 "App Manifest"
- Lines: 1228–1229
- Content: `Hooks HookDefs \`json:"hooks"\` // declarative hook config` — `HookDefs` type never defined in the document.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.3 `moca app new` scaffold, §8 Extension System
- Lines: 876–877, 3263–3290
- Content: `hooks.go # Hook registration code (with examples)` — hooks are Go code, not declarative JSON/YAML.

**Problem:**
The `AppManifest` struct has a `Hooks HookDefs` field that suggests hooks are declarable in the manifest (YAML/JSON). The CLI scaffolds a `hooks.go` file suggesting hooks are registered programmatically in Go code. These are two different extension models:
1. Declarative: `HookDefs` in manifest — framework reads JSON/YAML at boot.
2. Programmatic: `hooks.go` — app code registers handlers in Go `init()` or similar.

The `HookDefs` type is referenced but never defined anywhere in either document. The `PrioritizedHandler` struct (§3.5) references `AppName string` but doesn't explain how the hook registry is populated from app code vs. manifest.

**Why it matters:**
App developers cannot write hooks without knowing whether to put them in `hooks.go` or `manifest.yaml` (or both). The `HookDefs` type being undefined means the declarative model is a phantom that could mislead developers.

**Recommended fix:**
Either define `HookDefs` fully (what fields it has, how it maps to `PrioritizedHandler`) or remove it from `AppManifest` and clarify that hooks are always registered programmatically via `hooks.go`. Document the `init()` registration pattern explicitly. Add a "Hook Registration Contract" section to the framework design.

**Update target:** Primarily Document A. Document B's `moca app new` scaffold examples should show a complete `hooks.go`.

---

## MISMATCH-008

- **Severity:** High
- **Category:** Direct Contradiction
- **Title:** `moca-bench` and `moca-migrate` as standalone tools in framework doc vs. embedded CLI subcommands

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §15 Framework Package Layout
- Lines: 1959–1961
- Content: `tools/ ├── moca-bench/ # benchmarking tool └── moca-migrate/ # standalone migration runner`

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.10 `moca dev bench`, §4.2.5 `moca db migrate`
- Lines: 2086–2098, 1327–1343
- Content: Benchmarking is `moca dev bench`; migrations are `moca db migrate` / `moca site migrate`.

**Problem:**
The framework design allocates `tools/moca-bench/` and `tools/moca-migrate/` as separate binary tools. The CLI design embeds their functionality as CLI subcommands (`moca dev bench`, `moca db migrate`). These cannot coexist consistently — either the CLI is the sole interface for these capabilities, or there are also standalone tools. If both exist, behavior must be identical and docs must define when to use which.

**Why it matters:**
Having two implementations of the same functionality creates maintenance burden and operator confusion. More critically, `moca-migrate` in the tools/ directory implies a different migration runner separate from the CLI — this could have different behavior, different flags, and cause schema drift if run independently.

**Recommended fix:**
Remove `tools/moca-bench/` and `tools/moca-migrate/` from the framework package layout. The CLI subcommands are the definitive interfaces. If standalone execution is needed for deployment scripts, document that you run `moca dev bench` and `moca db migrate` directly.

**Update target:** Document A (`MOCA_SYSTEM_DESIGN.md`) — remove tools/ entries.

---

## MISMATCH-009

- **Severity:** High
- **Category:** Ambiguous Behavior / Missing Integration Contract
- **Title:** MetaType hot reload triggered "through Desk UI or API" — no CLI/filesystem watch contract defined

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §3.1.3 "Metadata Lifecycle — Hot Reload"
- Lines: 288–296
- Content: "When a MetaType definition changes at runtime (through the Desk UI or API), Moca: 1. Validates the new schema... 2. Generates migration diff... 3. Invalidates Redis cache... 4. Publishes `meta.changed` event to Kafka..."

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.1 `moca serve` flags, §4.2.10 `moca dev watch`
- Lines: 1093, 2061–2071
- Content: `moca serve --no-watch` "Don't watch for file changes"; `moca dev watch` "Watch for file changes and rebuild assets automatically."

**Problem:**
The framework defines hot reload as triggered by "Desk UI or API." The CLI defines `--no-watch` and `moca dev watch` as controlling file-system watching. These are two different trigger mechanisms. The critical unresolved question: when a developer edits `sales_order.json` on disk during `moca serve`, does this:
a) Trigger the full framework hot reload (migration diff, cache invalidation, Kafka event)?
b) Only rebuild frontend assets (what `moca dev watch` suggests)?
c) Have no effect (MetaType changes only via API)?

If (a), then the framework design must define filesystem watching as a trigger. If (c), then developers must use the Desk UI or POST to the API every time they change a MetaType JSON file — a terrible DX.

**Why it matters:**
The developer experience for defining DocTypes depends entirely on this contract. If file-system-to-framework-reload is not defined, developers will be confused when their JSON changes don't take effect in the dev server.

**Recommended fix:**
Define explicitly in both documents: `moca serve` watches `*/doctypes/*.json` files for changes and, when changed, triggers the full MetaType hot reload pipeline (§3.1.3). `moca dev watch` watches only frontend assets (`.tsx`, `.css`). Add `--no-watch` to `moca serve` to disable BOTH. Document this "Development MetaType Edit Loop" explicitly.

**Update target:** Both documents.

---

## MISMATCH-010

- **Severity:** High
- **Category:** One-Sided Definition
- **Title:** OAuth2, SAML, and OIDC authentication defined in framework with no CLI configuration commands

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §13.1 Authentication Methods, §15 Package Layout (`auth/oauth2.go`, `auth/sso.go`)
- Lines: 1750–1754, 1875–1877
- Content: `OAuth2 — Third-party app authorization`, `SAML / OIDC — Enterprise SSO`; `auth/oauth2.go # OAuth2 provider + consumer`, `auth/sso.go # SAML / OIDC integration`

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.13 API Keys, §4.2.16 User Management
- Lines: 2303–2363, 2779–2911
- Content: CLI has API key management and user management but zero OAuth2, SAML, or OIDC configuration commands.

**Problem:**
The framework is designed to support OAuth2 (as both provider and consumer) and SAML/OIDC for enterprise SSO. But there are no CLI commands to: configure an OAuth2 client/provider, register SAML identity providers, configure OIDC settings, test SSO flows, or manage OAuth2 tokens. These are non-trivial configurations that typically require CLI-level setup.

**Why it matters:**
Enterprise customers who need SSO have no CLI path to configure it. Operators managing OAuth2 integration for external apps (where Moca acts as OAuth2 provider) have no tooling. This is a significant gap for the "enterprise" template that `moca init --template enterprise` implies.

**Recommended fix:**
Add an `moca auth` command group with subcommands: `moca auth oauth2 list/add/remove`, `moca auth saml configure`, `moca auth oidc configure`, `moca auth test-sso`. Alternatively, document that these are configured via the Desk UI only and note the CLI limitation explicitly.

**Update target:** Document B (`MOCA_CLI_SYSTEM_DESIGN.md`)

---

## MISMATCH-011

- **Severity:** High
- **Category:** One-Sided Definition
- **Title:** Translation system fully designed in CLI doc; framework design has no translation architecture

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §9.1 React Frontend (mention only: `I18nProvider`)
- Lines: 1437–1438
- Content: `I18nProvider → internationalization` — single line, no further detail.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.20 Translation Management (full section)
- Lines: 3062–3122
- Content: `moca translate export/import/status/compile` — full i18n CLI with PO/CSV/JSON formats, binary MO compilation, per-app language coverage reporting.

**Problem:**
The CLI has a fully designed translation workflow (export strings, import translations, compile to binary MO format). The framework design mentions `I18nProvider` on the React frontend but never defines:
- How translatable strings are extracted from MetaType labels, select options, etc.
- Where translations are stored (files? database table?)
- How the backend serves translated content in API responses
- What the `Accept-Language` flow looks like end-to-end
- Whether the `Localizer` transformer (§3.3.5 mentions `type Localizer struct`) reads from files or DB

Without the framework-side translation architecture, the CLI's export/import/compile workflow has no target to sync with.

**Why it matters:**
`moca translate export` must know where to find translatable strings. `moca translate compile` must know where to put the compiled `.mo` files so the framework can serve them. Without these contracts, the translation CLI commands cannot be implemented.

**Recommended fix:**
Add a "Translation Architecture" section to Document A defining: translation storage schema (likely a `tab_translation` table), string extraction from MetaType definitions, backend `Accept-Language` resolution, and how the `Localizer` transformer is implemented.

**Update target:** Document A (`MOCA_SYSTEM_DESIGN.md`) needs the server-side architecture.

---

## MISMATCH-012

- **Severity:** High
- **Category:** One-Sided Definition
- **Title:** Notification system (email, push, SMS) defined in framework; no CLI management surface

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §15 Package Layout (`notify/`), §6.1 Kafka Topics (`moca.notifications`)
- Lines: 1923–1928, 1117–1118
- Content: `notify/ ├── email.go ├── push.go ├── inapp.go └── sms.go`; `moca.notifications | User notification events | 6 partitions | 3 days`

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: Not present
- Lines: N/A

**Problem:**
The framework has a complete notification subsystem (email via SMTP/SES, push notifications, in-app notifications, SMS). The Kafka topic `moca.notifications` is enumerated. But the CLI has no commands to: configure SMTP settings, test email delivery, list notification templates, manage push notification subscriptions, or inspect the notifications Kafka topic consumer lag.

**Why it matters:**
Email configuration is typically the first thing an operator needs to set up after site creation. Without CLI support, operators must configure SMTP via the Desk UI or direct DB manipulation. In automated/headless deployments, this is a blocker.

**Recommended fix:**
Add a `moca notify` (or extend `moca config`) subcommand for notification configuration. At minimum: `moca notify test-email --to admin@site.com` for SMTP verification. Also ensure `moca events consumer-status` shows lag for `moca.notifications` consumer group, and `moca doctor` checks notification queue health.

**Update target:** Document B (`MOCA_CLI_SYSTEM_DESIGN.md`)

---

## MISMATCH-013

- **Severity:** Medium
- **Category:** Direct Contradiction
- **Title:** App directory structure differs in 5 specific ways between framework doc and CLI scaffold

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §7.3 "Directory Structure of a Moca App"
- Lines: 1280–1320
- Content: Shows per-DocType `.tsx` files (`sales_order_list.tsx`, `sales_order_form.tsx`), `pages/` and `reports/` directories inside modules, `templates/portal/` with both `.html` and `.go` files.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.3 `moca app new` scaffold
- Lines: 873–891
- Content: Scaffold creates: `modules/{module}/doctypes/.gitkeep`, `fixtures/`, `migrations/`, `templates/`, `public/`, `tests/setup_test.go`, `go.mod`, `go.sum`. **Missing**: per-DocType `.tsx` files, `pages/`, `reports/`, `templates/portal/` structure. **Present in CLI but not system design**: `tests/`, `go.mod`, `go.sum`.

**Specific diffs:**
1. Framework shows `sales_order_list.tsx` / `sales_order_form.tsx` co-located with DocType JSON. CLI scaffold creates no `.tsx` files.
2. Framework shows `modules/selling/pages/` and `modules/selling/reports/`. CLI creates neither.
3. Framework shows `templates/portal/order_status.html` + `.go`. CLI creates `templates/` (flat).
4. CLI creates `tests/setup_test.go`. Framework doc doesn't show this.
5. CLI creates `go.mod`/`go.sum` per-app. Framework doc doesn't show these.

**Why it matters:**
A developer who runs `moca app new crm` gets a scaffold that doesn't match the example structure in the framework design. The missing directories are not cosmetic — `pages/`, `reports/` are functional directories that the framework must discover to register pages and reports.

**Recommended fix:**
Reconcile both directory listings. The definitive structure should be in Document A §7.3. Document B's `moca app new` scaffold must generate exactly that structure plus the CLI-specific Go build files (`go.mod`, `go.sum`). Add `tests/` and Go build files to the framework's app directory reference.

**Update target:** Both documents — decide canonical structure, then both must match.

---

## MISMATCH-014

- **Severity:** Medium
- **Category:** One-Sided Definition / Missing Integration Contract
- **Title:** Per-app `go.mod` implies multi-module Go build; no build composition model defined

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §7.1 App Manifest, §15 Framework Package Layout
- Lines: 1211–1260, 1827–1962
- Content: Apps have `AppManifest` but the top-level `moca/` module structure doesn't define how apps integrate. Framework layout is a single Go module (`moca/`).

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.3 `moca app new`, §4.2.3 `moca app get`
- Lines: 882–891, 924–932
- Content: `moca app new` generates `go.mod`/`go.sum` per app; `moca app get` runs `go mod download`.

**Problem:**
If each app is a separate Go module with its own `go.mod`, building the single `moca-server` binary requires composing multiple modules. In Go, this requires either: (1) a Go workspace file (`go.work`), (2) the server `go.mod` replacing/requiring each app module, or (3) vendoring. The CLI never manages a `go.work` file. Neither document defines how `moca serve` or `moca build app` triggers recompilation of the server with the app included.

**Why it matters:**
Without this contract, `moca app install` cannot functionally install an app — the server binary won't include the new app's code unless rebuilt. The "zero downtime" rolling restart after `moca deploy update` depends on a new binary being built.

**Recommended fix:**
Define the multi-module build strategy (recommend Go workspaces). Add `go.work` to the project root directory structure. Specify that `moca app get` and `moca app install` update `go.work` and trigger a server rebuild. Add `moca build server` as a command that compiles all installed apps into the server binary.

**Update target:** Both documents need the multi-module build strategy.

---

## MISMATCH-015

- **Severity:** Medium
- **Category:** Ambiguous Behavior
- **Title:** `staging` environment referenced in CLI flags and `moca deploy promote` but absent from `moca.yaml` config sections

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: Not referenced
- Lines: N/A

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §5 Global Flags, §4.2.8 `moca deploy promote`, §3.1 `moca.yaml`
- Lines: 3153, 1882–1893, 201–302
- Content: `--env string    Override environment (dev/staging/prod)`; `moca deploy promote SOURCE_ENV TARGET_ENV (e.g., staging → production)`; `moca.yaml` only defines `development:` and `production:` sections — **no `staging:` section**.

**Problem:**
The global `--env` flag accepts `dev/staging/prod`, and `moca deploy promote` uses environment names as arguments. But `moca.yaml` only has `development:` and `production:` config sections. `staging` has no config section. Additionally, the system design only addresses single-instance vs. production architecture (§12.1, §12.2) — there's no staging tier defined at all.

**Why it matters:**
`moca deploy promote staging production` would fail to load `staging` config because there's no `staging:` section in `moca.yaml`. The CLI accepts `--env staging` but the config file can't define staging-specific settings.

**Recommended fix:**
Either (a) add a `staging:` section to the `moca.yaml` schema in Document B (and define how `moca init` creates it); or (b) redefine `--env` to accept arbitrary named environments that each map to a config section (more flexible but more complex); or (c) make staging a separate `moca.yaml` overlay file. Document A should acknowledge staging as a deployment tier.

**Update target:** Primarily Document B (`moca.yaml` schema and promote command).

---

## MISMATCH-016

- **Severity:** Medium
- **Category:** Direct Contradiction (within Document B)
- **Title:** `--skip` migration flag used in error example but never declared in `moca db migrate`

**Document A reference:** N/A

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section A: §4.2.5 `moca db migrate` flags
- Lines: 1330–1342
- Content: Defined flags: `--site`, `--dry-run`, `--verbose`, `--step int` — no `--skip` flag.

- Section B: §7 Error Handling Philosophy, migration failure example
- Lines: 3246–3252
- Content: `Fix: 2. Skip migration: moca db migrate --site acme.localhost --skip 003`

**Problem:**
The error handling documentation tells operators to use `moca db migrate --skip 003` to skip a failed migration. But the command specification for `moca db migrate` does not define a `--skip` flag. This flag is undocumented and would fail with "unknown flag."

**Why it matters:**
An operator in a production crisis following the documented fix would run the exact command and get an error, making a bad situation worse.

**Recommended fix:**
Add `--skip string    Skip a specific migration by version/filename` to the `moca db migrate` flag list. Alternatively, replace the `--skip` example in error handling with the correct flag (`--step` combined with a database edit). Update both the command spec and the error example to be consistent.

**Update target:** Document B (`MOCA_CLI_SYSTEM_DESIGN.md`) — add `--skip` to flags, or fix the error example.

---

## MISMATCH-017

- **Severity:** Medium
- **Category:** Direct Contradiction (within Document B)
- **Title:** `moca site migrate` describes `--no-backup` behavior but never declares the flag

**Document A reference:** N/A

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.2 `moca site migrate`
- Lines: 726–748
- Content: Step 2 of "What it does" says "Backs up the site (unless --no-backup)." But the Flags section lists: `--site`, `--all`, `--dry-run`, `--skip-search`, `--skip-cache`, `--parallel` — **no `--no-backup` flag**.

**Problem:**
The behavioral description of `moca site migrate` references `--no-backup` to skip the pre-migration backup. But the flags list does not declare `--no-backup`. It is present on `moca site drop` (line 684), `moca deploy update` (line 1812), `moca app update` (line 949), and others — but not on `moca site migrate`.

**Why it matters:**
In CI/CD automation or development environments where backup is unnecessary overhead, operators would try `--no-backup` on `moca site migrate`, get "unknown flag," and be stuck.

**Recommended fix:**
Add `--no-backup  Skip automatic backup before migration` to `moca site migrate` flags. This is consistent with how it appears on all other destructive commands.

**Update target:** Document B (`MOCA_CLI_SYSTEM_DESIGN.md`)

---

## MISMATCH-018

- **Severity:** Medium
- **Category:** Direct Contradiction (within Document B)
- **Title:** `moca test run-ui` described as "Cypress tests" in command tree but "Playwright" in command spec

**Document A reference:** N/A (framework design never mentions testing framework)

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section A: §4.1 Command Tree
- Line: 459
- Content: `├── run-ui    # Run frontend/Cypress tests`

- Section B: §4.2.11 `moca test run-ui`
- Lines: 2143–2154
- Content: "Run frontend UI tests using **Playwright**." Flags include `--browser string "chromium" (default), "firefox", "webkit"` — these are Playwright browser engines, not Cypress.

**Problem:**
Two directly contradictory statements in the same document. The command tree says Cypress; the command spec says Playwright. Playwright and Cypress have entirely different APIs, configuration files, test syntax, and dependency trees. A developer setting up UI tests would use one or the other — they cannot be interchangeable here.

**Why it matters:**
App developers writing UI tests would write either Cypress or Playwright tests. The framework must pick one and generate the correct test harness in `moca app new`.

**Recommended fix:**
Decide: Playwright or Cypress (Playwright is the more modern choice and aligns with the browser flag choices). Update the command tree comment to match. Add the chosen test runner to `moca app new` scaffold. Note the test runner as a dependency.

**Update target:** Document B — fix command tree to say "Playwright."

---

## MISMATCH-019

- **Severity:** Medium
- **Category:** Direct Contradiction
- **Title:** Redis pub/sub for WebSocket uses key pattern colliding with document cache keys; no dedicated Redis DB defined

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §9.4 "Real-Time Updates via WebSocket", §5.1 "Caching Layer"
- Lines: 1518–1534, 1029–1037
- Content: WebSocket subscribes to `doc:{site}:{doctype}:*` (pub/sub); cache uses `doc:{site}:{doctype}:{name}` (string keys).

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §3.1 `moca.yaml` infrastructure section
- Lines: 237–242
- Content: `redis: db_cache: 0, db_queue: 1, db_session: 2` — only three Redis DBs defined; no `db_pubsub`.

**Problem:**
The WebSocket real-time system uses Redis Pub/Sub on channel pattern `doc:{site}:{doctype}:*`. The document cache uses keys with the same prefix `doc:{site}:{doctype}:{name}`. If both run on the same Redis DB (db_cache: 0), pub/sub channel subscriptions with wildcards (`PSUBSCRIBE doc:*`) could match cache key names. Additionally, there is no `db_pubsub` entry in `moca.yaml` — pub/sub presumably runs on `db_cache: 0` but this is never stated.

**Why it matters:**
Using `PSUBSCRIBE doc:*` on Redis DB 0 where document cache keys also start with `doc:` is technically valid (Redis key-space and pub/sub channels are separate namespaces) but creates confusion for operators monitoring Redis. More critically, the `moca.yaml` never tells the framework which Redis DB to use for pub/sub — the framework's WebSocket hub has an undefined Redis DB target.

**Recommended fix:**
Add `db_pubsub: 3` to the `moca.yaml` Redis config. Update the framework design's Redis key table to add the pub/sub channel patterns as distinct from cache key patterns. Alternatively, namespace pub/sub channels differently (e.g., `pubsub:doc:{site}:{doctype}:{name}`).

**Update target:** Both documents.

---

## MISMATCH-020

- **Severity:** Medium
- **Category:** Hidden Dependency / Likely Implementation Risk
- **Title:** `moca dev console` uses yaegi Go interpreter — dependency not validated against framework packages

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §15 Package Layout (implicitly — framework uses Go 1.22+, potential CGo, etc.)
- Lines: 1827–1962

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.10 `moca dev console`
- Lines: 2005–2025
- Content: "Uses yaegi (Go interpreter) for a Go REPL experience."

**Problem:**
Yaegi has known limitations: no CGo support, incomplete generics support (improving in recent versions), and limited support for some reflection patterns. The Moca framework uses: PostgreSQL driver `pgx` (likely uses CGo or assembly optimizations), Redis clients, Kafka clients (may use CGo). If any of these have CGo dependencies or use Go features yaegi doesn't support, `moca dev console` would fail to load framework packages.

Additionally, yaegi requires all packages to be pre-built or source-available. A compiled app binary (per MISMATCH-005) would not be interpretable by yaegi.

**Why it matters:**
The developer console is a key DX feature. If it silently fails to import framework packages, or if yaegi limitations cause subtle behavior differences from the production server, it becomes a liability rather than an asset.

**Recommended fix:**
Validate yaegi compatibility with all framework packages before committing to this approach. Document any limitations. Consider an alternative: a `moca dev console` that starts a local server process and provides an HTTP-based REPL (like Frappe's `bench console` which starts a Python shell with the framework imported). This avoids the interpreter compatibility problem entirely.

**Update target:** Document B — add a note on yaegi limitations; Document A — no yaegi constraint should be placed on framework packages.

---

## MISMATCH-021

- **Severity:** Medium
- **Category:** Terminology Mismatch
- **Title:** "Plugin" term used in multiple incompatible contexts across both documents

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §9.3 "Custom Field Type Registry", §18 "What to Revisit"
- Lines: 1506–1513, 2034
- Content: "In an app's desk plugin: `import { registerFieldType } from '@moca/desk'`"; "Plugin sandboxing — Currently, app code runs in the same process..."

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §11 ADR-CLI-001
- Lines: 3470–3471
- Content: "For truly dynamic extensions, apps can register as plugins that the CLI discovers."

**Problem:**
"Plugin" appears in three different senses:
1. **React Desk plugin**: An app's frontend components that register field types/custom views (§9.3, System Design).
2. **WASM sandbox plugin**: Future runtime plugin model for untrusted app code (§18, System Design).
3. **CLI plugin**: A dynamic extension that the CLI discovers at runtime (ADR-CLI-001, CLI Design).

These are three completely different things with different interfaces, lifecycles, and security models. Using the same word for all three causes confusion. The CLI design's ADR-CLI-001 says apps "register as plugins" for dynamic CLI extension — but this conflicts with apps being compiled Go code (they can't be "dynamically discovered" as Go binaries without a plugin loading mechanism).

**Why it matters:**
Inconsistent terminology will confuse app developers who need to know: "am I building an app, a module, or a plugin?" Each term implies different contracts.

**Recommended fix:**
Establish a terminology glossary section in both documents:
- **App**: A Moca application (has `manifest.yaml`, modules, DocTypes, hooks)
- **Module**: A logical grouping of DocTypes within an App
- **Desk Extension**: App-provided React components (not "Desk plugin")
- **CLI Extension**: App-registered custom CLI commands via `cli.RegisterCommand()`
- Reserve "Plugin" only for the future WASM sandbox model.

**Update target:** Both documents — add a glossary; replace "plugin" with consistent terms.

---

## MISMATCH-022

- **Severity:** Medium
- **Category:** Direct Contradiction (within Document B)
- **Title:** `moca scale` command advertised in comparison table but never defined

**Document A reference:** N/A

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section A: §2 "Why Not Just Copy Bench?" comparison table
- Lines: 31
- Content: `| No horizontal scaling support | \`moca scale\` — built-in orchestration for multi-process deployment |`

- Section B: §4.1 Full Command Tree and all command specs
- Lines: 342–547 (entire command tree)
- Content: No `moca scale` command exists anywhere in the command tree or command reference.

**Problem:**
The design rationale table claims `moca scale` as a capability vs. bench. But the command is never defined in the command tree or command specifications. No flags, behavior, or examples are given.

**Why it matters:**
Any reader of the comparison table would expect `moca scale` to be a real command. It creates a false promise in the positioning section.

**Recommended fix:**
Either: (a) define `moca scale` (likely a shorthand for `moca worker scale` + `moca generate k8s --replicas`); or (b) update the comparison table to reference the actual commands that provide horizontal scaling (`moca worker scale`, `moca generate k8s`).

**Update target:** Document B (`MOCA_CLI_SYSTEM_DESIGN.md`)

---

## MISMATCH-023

- **Severity:** Medium
- **Category:** One-Sided Definition
- **Title:** `moca generate supervisor` exists in CLI but supervisor is never mentioned as a supported process manager in framework design

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §12 Deployment Architecture
- Lines: 1676–1742
- Content: Defines deployment with systemd service units, Docker Compose, and Kubernetes. No mention of supervisor.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.1 Command Tree, `moca generate`
- Lines: 443
- Content: `├── supervisor    # Generate supervisor config (legacy compat)`

**Problem:**
The CLI generates supervisor configuration "for legacy compat" but the framework design never acknowledges supervisor as a supported process manager. The `moca deploy setup --process` flag only accepts "systemd" or "docker" — supervisor is not an option. This creates a partial support scenario: you can generate supervisor config but cannot deploy using it.

**Why it matters:**
Some production environments still use supervisord. If the CLI generates config for it, operators will expect it to work. But since `moca deploy setup` doesn't support `--process supervisor`, the full deployment workflow breaks.

**Recommended fix:**
Either add `--process supervisor` to `moca deploy setup` (and document it in both docs), or remove `moca generate supervisor` and note in the CLI doc that supervisor is not a supported deployment manager. The "legacy compat" note suggests the latter.

**Update target:** Document B should clarify supervisor support scope; Document A's deployment section should explicitly list all supported process managers.

---

## MISMATCH-024

- **Severity:** Medium
- **Category:** Missing Integration Contract
- **Title:** `moca site create` step list differs from framework's Site Lifecycle definition (S3 init missing; Administrator creation missing)

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §8.3 "Site Lifecycle" (`create-site` steps)
- Lines: 1374–1396
- Content: Steps 1–7: Create PG schema, create system tables, run migrations, create Redis namespace, **create S3 storage bucket/prefix** (step 5), create Meilisearch index (step 6), register in moca_system.sites (step 7). No "create Administrator user" step.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.2 `moca site create` "What it does"
- Lines: 665–676
- Content: Steps 1–10: Create PG schema, run system tables, create `site_config.yaml`, run migrations, **create Administrator user** (step 5), install apps, create Redis namespace, create Meilisearch index, **create S3 storage prefix** (step 9), warm metadata cache. No S3 step in framework's site lifecycle; no Administrator step in framework design.

**Specific gaps:**
- Framework §8.3 step 5: "Create S3 storage bucket/prefix" — present in CLI steps (step 9) ✓ but ordering differs
- CLI step 5: "Creates the Administrator user" — absent from framework's site lifecycle
- CLI step 10: "Warms metadata cache" — absent from framework's site lifecycle

**Why it matters:**
If someone implements `moca site create` following Document A's lifecycle (§8.3), they'd miss creating the Administrator user. If they follow Document B, the steps differ from the authoritative framework definition.

**Recommended fix:**
Reconcile both step lists. The canonical site creation lifecycle should be in Document A §8.3 and must include: PG schema, system tables, migrations, **Administrator user creation**, Redis namespace, S3 prefix, Meilisearch index, site registry, **cache warmup**. Document B's CLI command should mirror this exactly.

**Update target:** Both documents — Document A needs the missing steps; Document B ordering/wording should match A.

---

## MISMATCH-025

- **Severity:** Medium
- **Category:** Missing Integration Contract
- **Title:** `@moca/desk` npm package used in system design but never declared or versioned anywhere

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §9.3 "Custom Field Type Registry"
- Lines: 1509–1513
- Content: `import { registerFieldType } from '@moca/desk';` — implies an npm package `@moca/desk`.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.12 `moca build desk`, §3 Project Structure (`desk/package.json`)
- Lines: 2178–2196, 192–194
- Content: `moca build desk` "Resolves all app desk/ component registrations" and "Generates route manifest from installed apps" — but never names `@moca/desk` as a dependency.

**Problem:**
Apps that extend the Desk UI must import `@moca/desk` (the React component library and extension API). But:
- The package name `@moca/desk` is never formally declared with a version, npm scope, or distribution method.
- The `desk/package.json` in the project root doesn't show `@moca/desk` as a dependency.
- `moca app new` doesn't create a `package.json` in app directories (since apps put `.tsx` files in the DocType directory without a separate React project).
- `moca build desk` must somehow resolve these `.tsx` files, but the import path `@moca/desk` must be available.

**Why it matters:**
Without a defined npm package, app developers cannot build the TypeScript components that extend the Desk. The Vite build process needs to resolve `@moca/desk`. If it's a local workspace package, `desk/package.json` must reference it. If it's published to npm, it must have a version.

**Recommended fix:**
Add `@moca/desk` to the `desk/package.json` in Document B's project structure section. Define whether it's published to npm or a local workspace package. Add it to the framework's React package layout in Document A.

**Update target:** Both documents.

---

## MISMATCH-026

- **Severity:** Medium
- **Category:** Ambiguous Behavior
- **Title:** `desk/` directory exists in three different locations with undefined relationships

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §15 Framework Package Layout
- Lines: 1938–1953
- Content: `moca/desk/` — React frontend source at the framework level.

- Section: §7.3 App Directory Structure
- Lines: 1291–1294
- Content: Per-DocType `.tsx` files: `sales_order_list.tsx`, `sales_order_form.tsx`.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §3 Project Structure
- Lines: 190–194
- Content: `desk/` at project root: `src/`, `package.json`, `vite.config.ts` — "React frontend source (if customized)."

**Problem:**
There are effectively three `desk/` locations:
1. **Framework-level** (`moca/desk/`): The base React application — MetaProvider, FormView, ListView, etc.
2. **Project-level** (`my-project/desk/`): "If customized" — but what can be customized here? Override components? Add routes?
3. **App-level** (`.tsx` files within app DocType directories): Per-DocType form/list overrides.

The relationships between these three are never defined:
- Does the project-level `desk/` override the framework-level `desk/`?
- How does `moca build desk` compose framework + project + multiple app `.tsx` files?
- What happens if two apps define `sales_order_form.tsx` for the same DocType?
- Is the project `desk/` compiled separately or merged with the framework `desk/`?

**Why it matters:**
The entire frontend customization model depends on these relationships. Without them, app developers cannot reliably extend the UI.

**Recommended fix:**
Define the frontend composition model explicitly in Document A §9 and Document B §4.2.12. Specify: framework desk is the base; apps contribute components via a registration API; project-level `desk/` is for project-specific overrides. Define conflict resolution (e.g., app alphabetical order, explicit priority). Add this to `moca build desk` documentation.

**Update target:** Both documents.

---

## MISMATCH-027

- **Severity:** Low
- **Category:** Missing Integration Contract
- **Title:** Migration `DependsOn` cross-app dependency resolution never addressed by CLI migration runner

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §7.1 App Manifest, `Migration` struct
- Lines: 1255–1260
- Content: `DependsOn []string \`json:"depends_on"\` // other migrations that must run first`

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.5 `moca db migrate`
- Lines: 1327–1343
- Content: Flags include `--step int Run only N migrations`. No mention of dependency resolution.

**Problem:**
Migrations can declare `DependsOn` to specify that another migration (potentially from a different app) must run first. The CLI's `moca db migrate --step int` flag simply runs N migrations by count. If `--step 3` would skip a migration that another app's migration depends on, the dependency goes unresolved. The CLI never describes how it handles `DependsOn`.

**Why it matters:**
In multi-app environments, cross-app migration dependencies are likely. The `--step` flag could leave a site in an invalid state if it stops before a dependency is satisfied.

**Recommended fix:**
Add a note to `moca db migrate` that it respects `DependsOn` and will refuse to skip a migration that other pending migrations depend on. Define whether `--step` applies to the dependency-resolved order or the raw file order.

**Update target:** Document B (`MOCA_CLI_SYSTEM_DESIGN.md`)

---

## MISMATCH-028

- **Severity:** Low
- **Category:** One-Sided Definition
- **Title:** `moca.notifications` Kafka topic defined in framework; no CLI consumer monitoring for it

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §6.1 Kafka Topic Design
- Lines: 1117–1118
- Content: `moca.notifications | User notification events | 6 partitions | 3 days retention`

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §4.2.15 `moca events list-topics` example
- Lines: 2714–2724
- Content: Lists: `moca.doc.events`, `moca.audit.log`, `moca.meta.changes`, `moca.integration.outbox`, `moca.workflow.transitions`, `moca.search.indexing` — `moca.notifications` is **absent** from the example output.

**Problem:**
The framework defines `moca.notifications` as a Kafka topic. The CLI's `moca events list-topics` example output omits it. This is likely an oversight in the example, but may indicate the notifications topic is not managed through the same Kafka tooling as other topics.

**Why it matters:**
Operators using `moca events tail` to debug notification delivery issues would not know to look for `moca.notifications`. `moca events consumer-status` would not show notification consumer lag.

**Recommended fix:**
Add `moca.notifications` to the `moca events list-topics` example output. Ensure `moca events tail moca.notifications` works and is documented as useful for debugging notification delivery.

**Update target:** Document B (`MOCA_CLI_SYSTEM_DESIGN.md`) — add to example output.

---

## MISMATCH-029

- **Severity:** Low
- **Category:** Missing Integration Contract
- **Title:** `moca deploy setup --process docker` behavior and relationship to `moca generate docker` undefined

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §12.2 Production Architecture diagram
- Lines: 1697–1730
- Content: Shows load balancer, API servers, worker pools, and infrastructure clusters — no Docker Compose or Docker-specific deployment mentioned.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section A: §4.2.8 `moca deploy setup`
- Lines: 1742–1745
- Content: `--process string  Process manager: "systemd" (default), "docker"`

- Section B: §4.2.9 `moca generate docker`
- Lines: 1932–1949
- Content: Generates `docker-compose.yml`, `docker-compose.prod.yml`, `Dockerfile`

**Problem:**
When `moca deploy setup --process docker` is used, it's unclear whether it internally calls `moca generate docker` or has its own Docker setup logic. The deploy setup step 4 says "Generates process manager unit files" — for docker, this would presumably be `docker-compose.yml` files. But the relationship (same code path? separate?) is never defined.

**Why it matters:**
If `moca deploy setup --process docker` and `moca generate docker` have different behavior or generate different files, a Docker-based deployment could be inconsistent. If they share code, this should be documented.

**Recommended fix:**
State explicitly in Document B that `moca deploy setup --process docker` calls `moca generate docker` internally (or equivalent logic) as step 4, and then starts the Docker Compose stack.

**Update target:** Document B (`MOCA_CLI_SYSTEM_DESIGN.md`)

---

## MISMATCH-030

- **Severity:** Low
- **Category:** One-Sided Definition
- **Title:** `moca.yaml` `moca_system` database name hardcoded in CLI but system DB naming undefined in framework

**Document A reference:**
- File: `MOCA_SYSTEM_DESIGN.md`
- Section: §4.2 Core System Tables
- Lines: 853–883
- Content: `CREATE TABLE moca_system.sites (...)` — uses schema name `moca_system` in SQL examples. No formal definition of whether this name is configurable.

**Document B reference:**
- File: `MOCA_CLI_SYSTEM_DESIGN.md`
- Section: §3.1 `moca.yaml`
- Lines: 234
- Content: `system_db: moca_system` — the system DB name is a configurable field in `moca.yaml`.

**Problem:**
The CLI makes `system_db` a configurable parameter. The framework design hardcodes `moca_system` in all SQL examples without acknowledging it as configurable. If someone sets `system_db: my_company_moca`, the framework must use that name everywhere — but the framework design treats it as a constant.

**Why it matters:**
In enterprise deployments with naming conventions, the ability to customize `system_db` is valuable. But if the framework hard-codes `moca_system` internally, the `system_db` config option would have no effect.

**Recommended fix:**
Document A should note that the system schema name is configurable via `moca.yaml:infrastructure.database.system_db` and show the SQL examples using a placeholder name. Document B should note the default value and constraints (must be a valid PostgreSQL schema name).

**Update target:** Document A (`MOCA_SYSTEM_DESIGN.md`)

---

# Top Priority Fix Order

1. **MISMATCH-001** — Fix `cmd/moca-cli/` → `cmd/moca/` naming before any code is written. A trivial change that prevents build confusion.

2. **MISMATCH-005** — Resolve the app binary loading/composition model (Go workspaces or plugin system). This blocks ALL app development and is the most fundamental unresolved architectural question.

3. **MISMATCH-004** — Define the Kafka-optional fallback architecture in the framework design. Many CLI capabilities (`--no-kafka`, minimal deployments) are currently undefined from the framework's perspective.

4. **MISMATCH-003** — Define the configuration sync contract between YAML files and PostgreSQL. Without this, `moca config set` has undefined effects on a running server.

5. **MISMATCH-006** — Add `moca-search-sync` to the CLI's systemd generation and management commands. A missing process in production with no management tooling.

6. **MISMATCH-009** — Define the MetaType hot reload trigger (filesystem vs API) and document the development edit loop explicitly.

7. **MISMATCH-007** — Define `HookDefs` in the framework design or remove it and document the programmatic `hooks.go` registration model clearly.

8. **MISMATCH-013** — Reconcile the app directory structure. The scaffold must generate what the framework expects to discover.

9. **MISMATCH-002** — Decide on Traefik support. Update both documents to reflect the actual supported proxy options.

10. **MISMATCH-016** & **MISMATCH-017** — Fix the undeclared `--skip` and `--no-backup` flags immediately. These are operator traps in production crisis scenarios.

---

# Unification Checklist

## Binary & Build Model
- [ ] Rename `cmd/moca-cli/` to `cmd/moca/` in framework package layout (MISMATCH-001)
- [ ] Remove `tools/moca-bench/` and `tools/moca-migrate/` from framework layout — these are CLI subcommands (MISMATCH-008)
- [ ] Define Go module composition model (Go workspaces? per-app `go.mod` strategy) (MISMATCH-005, MISMATCH-014)
- [ ] Add `moca build server` command that compiles all installed apps into server binary (MISMATCH-005)
- [ ] Validate yaegi compatibility with all framework dependencies before committing to dev console approach (MISMATCH-020)

## Process & Deployment
- [ ] Add `moca-search-sync.service` to `moca generate systemd` output (MISMATCH-006)
- [ ] Add `moca search-sync` management commands (or integrate into `moca worker`) (MISMATCH-006)
- [ ] Decide Traefik support: add `moca generate traefik` OR remove Traefik from framework tech stack (MISMATCH-002)
- [ ] Decide supervisor support: document limitations OR add `--process supervisor` to `moca deploy setup` (MISMATCH-023)
- [ ] Define `staging` environment config section in `moca.yaml` schema (MISMATCH-015)
- [ ] Clarify `moca deploy setup --process docker` vs `moca generate docker` relationship (MISMATCH-029)

## Configuration
- [ ] Define the YAML ↔ PostgreSQL config sync contract (MISMATCH-003)
- [ ] Document which is the authoritative config source at runtime (DB) vs. at rest (YAML) (MISMATCH-003)
- [ ] Add `db_pubsub` Redis DB entry to `moca.yaml` schema (MISMATCH-019)
- [ ] Document `system_db` as configurable in framework design (MISMATCH-030)

## Architecture Gaps
- [ ] Add §6.5 "Kafka-Optional Architecture" to framework design (MISMATCH-004)
- [ ] Define MetaType hot reload trigger from CLI/filesystem (MISMATCH-009)
- [ ] Define `HookDefs` type or remove from `AppManifest` and document `hooks.go` pattern (MISMATCH-007)
- [ ] Add Translation Architecture section to framework design (MISMATCH-011)
- [ ] Add notification CLI commands (`moca notify test-email` etc.) (MISMATCH-012)
- [ ] Add OAuth2/SAML/OIDC configuration CLI commands or document UI-only path (MISMATCH-010)

## CLI Command Fixes (Internal Contradictions)
- [ ] Add `--skip string` flag to `moca db migrate` (MISMATCH-016)
- [ ] Add `--no-backup` flag to `moca site migrate` (MISMATCH-017)
- [ ] Fix `moca test run-ui` command tree comment: "Cypress" → "Playwright" (MISMATCH-018)
- [ ] Define `moca scale` command or remove from comparison table (MISMATCH-022)

## Directory Structure & Scaffolding
- [ ] Reconcile app directory structure between §7.3 (framework) and `moca app new` scaffold (MISMATCH-013)
- [ ] Add `pages/`, `reports/` dirs and per-DocType `.tsx` stubs to `moca app new` scaffold (MISMATCH-013)
- [ ] Add `tests/setup_test.go` and `go.mod`/`go.sum` to framework §7.3 app structure example (MISMATCH-013)
- [ ] Define the three `desk/` locations and their composition model (MISMATCH-026)
- [ ] Formally declare `@moca/desk` npm package name, version, and distribution method (MISMATCH-025)

## Lifecycle & Integration Contracts
- [ ] Reconcile `moca site create` steps with framework §8.3 site lifecycle (add Administrator creation, cache warmup) (MISMATCH-024)
- [ ] Document migration `DependsOn` handling in `moca db migrate` (MISMATCH-027)
- [ ] Add `moca.notifications` to `moca events list-topics` example (MISMATCH-028)

## Terminology
- [ ] Create a terminology glossary in both documents (App, Module, Desk Extension, CLI Extension, Plugin) (MISMATCH-021)
- [ ] Replace "desk plugin" usage in framework §9.3 with "Desk Extension" (MISMATCH-021)

---

*Report generated by cross-document analysis of MOCA_SYSTEM_DESIGN.md (2040 lines) and MOCA_CLI_SYSTEM_DESIGN.md (3599 lines). Total issues found: 30. Issues with direct evidence: 30. Issues labeled "unclear" due to insufficient evidence: 0.*
