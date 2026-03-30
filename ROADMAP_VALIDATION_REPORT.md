# MOCA Roadmap Validation Report

## Executive Summary

| Metric | Value |
|--------|-------|
| **Overall Verdict** | **Needs Revision** |
| **Coverage Score** | **78 / 100** |
| **Readiness Verdict** | **Needs revision before execution** |
| **Framework Design Coverage** | 95% (excellent) |
| **CLI Design Coverage** | 73% (significant gaps) |
| **Dependency Logic** | 100% correct (no circular, no missing critical deps) |
| **Cross-Doc Contradictions** | 4 critical, 7 high, 6 medium findings |

The roadmap is architecturally sound and correctly sequenced. The framework system design (`MOCA_SYSTEM_DESIGN.md`) is covered at ~95%. However, the CLI system design (`MOCA_CLI_SYSTEM_DESIGN.md`) has **41 commands either missing or underspecified** across milestones. Three critical gaps must be fixed before execution: (1) `moca init` is completely absent, (2) 6 site management commands are orphaned with no milestone, and (3) notification CLI commands have zero coverage.

---

## Coverage Matrix

### Framework Design Coverage (`MOCA_SYSTEM_DESIGN.md`)

| Design Area | Source Reference | Roadmap Milestone(s) | Status | Notes |
|-------------|-----------------|---------------------|--------|-------|
| §1 Executive Summary | lines 9-43 | N/A (reference) | N/A | Context only |
| §2 High-Level Architecture | lines 47-130 | MS-00 through MS-26 | Fully covered | All layers mapped |
| §3.1 MetaType Registry | lines 133-296 | MS-03 | Fully covered | All 33 FieldTypes, NamingStrategy, ViewMeta, hot reload |
| §3.1.3 Hot Reload / Filesystem Watch | lines 271-296 | MS-03, MS-10 | Fully covered | Compiler in MS-03; fsnotify watcher in MS-10 |
| §3.2 Document Runtime | lines 300-462 | MS-04, MS-28 | Fully covered | DynamicDoc + 18 lifecycle events in MS-04; VirtualDoc deferred to MS-28 |
| §3.3 Customizable API Layer | lines 466-679 | MS-06, MS-18, MS-20 | Fully covered | REST (MS-06), Custom endpoints/webhooks (MS-18), GraphQL (MS-20) |
| §3.3.5 Transformer Pipeline | lines 616-633 | MS-06 | Fully covered | FieldRemapper, FieldFilter, ComputedInjector, Paginator, Expander, Localizer |
| §3.4 Permission Engine | lines 682-711 | MS-14, MS-18 | Fully covered | PermRule + RLS (MS-14); APIScopePerm (MS-18) |
| §3.5 Hook Registry | lines 714-781 | MS-08 | Fully covered | PrioritizedHandler, dependency resolution, all hook types |
| §3.6 Workflow Engine | lines 785-824 | MS-23 | Fully covered | State machine, transitions, SLA, approval quorum |
| §4.1-4.2 PostgreSQL Core | lines 828-887 | MS-02 | Fully covered | Schema-per-tenant, 3 system tables |
| §4.3 Per-Tenant Schema | lines 889-1006 | MS-03 | Partially covered | DDL generation covered; **tab_audit_log partitioning** not explicitly in acceptance criteria |
| §4.4 _extra JSONB Pattern | lines 1008-1022 | MS-03 | Fully covered | Standard column on all tables |
| §5.1 Caching Layer | lines 1026-1058 | MS-02, MS-12 | Fully covered | All key patterns, 4 Redis DBs |
| §5.1.1 Config Sync Contract | lines 1060-1072 | MS-11 | Fully covered | YAML-to-DB sync, dual-write |
| §5.2 Queue Layer | lines 1073-1115 | MS-02, MS-15 | Fully covered | Redis Streams, consumer groups, DLQ |
| §6.1 Kafka Topics (8 topics) | lines 1093-1120 | MS-15, MS-28 | Partially covered | 7 of 8 topics in MS-15; CDC deferred to MS-28 |
| §6.4 Transactional Outbox | lines 1205-1233 | MS-15 | Fully covered | Outbox table, poller, Kafka publisher |
| §6.5 Kafka-Optional Architecture | lines 1235-1260 | MS-15 | Fully covered | Redis pub/sub fallback, feature matrix |
| §7 App/Plugin System | lines 1263-1383 | MS-08, MS-13 | Fully covered | AppManifest, Migration DependsOn, Go workspace |
| §8 Multitenancy | lines 1388-1462 | MS-02, MS-12, MS-09 | Fully covered | 3 strategies, per-resource isolation, 9-step lifecycle |
| §9.1-9.2 React Desk | lines 1466-1568 | MS-17 | Fully covered | Providers, FormView, ListView, FieldRenderer |
| §9.3 Custom Field Type Registry | lines 1504-1514 | MS-19 | Fully covered | registerFieldType() API |
| §9.4 WebSocket Real-Time | lines 1516-1534 | MS-19 | Fully covered | WebSocket hub, Redis pub/sub bridge |
| §9.5 Portal / SSR | lines 1602-1619 | MS-27 | Deferred (post-v1.0) | Intentional deferral; documented |
| §9.6 Translation Architecture | lines 1621-1651 | MS-20 | Fully covered | tab_translation, .mo compilation, I18nProvider |
| §10 Query Engine | lines 1654-1725 | MS-05 | Fully covered | All 15 operators, _extra JSONB, Link auto-joins |
| §11 Observability | lines 1729-1770 | MS-24 | Fully covered | All 13 Prometheus metrics, structured logging, health checks |
| §12 Deployment Architecture | lines 1676-1838 | MS-10, MS-21 | Fully covered | 5 process types, single-instance and production |
| §13 Security | lines 1746-1880 | MS-14, MS-18, MS-22 | Fully covered | 5 auth methods, API keys, 7-layer defense, AES-256-GCM |
| §14 Request Lifecycle | lines 1884-1920 | MS-06, MS-14, MS-15 | Fully covered | Full 14-step flow |
| §15 Package Layout | lines 1924-2057 | MS-01 | Fully covered | All pkg/ directories |
| §16 ADR Summary (7 ADRs) | lines 2061-2105 | MS-00 | Fully covered | Validated by spikes |
| §17.1 Terminology Glossary | lines 2121-2132 | N/A (reference) | N/A | Documentation artifact |
| §17.2 Desk Composition Model | lines 2134-2143 | MS-17, MS-19 | Fully covered | 3-layer model, @moca/desk |
| §18 What to Revisit (5 items) | lines 2026-2040 | MS-28, MS-29 | Fully covered | All 5 items deferred to post-v1.0 |

### CLI Design Coverage (`MOCA_CLI_SYSTEM_DESIGN.md`)

| Command Group | Total Cmds | Covered | Missing | Deferred | Coverage | Roadmap MS |
|---------------|-----------|---------|---------|----------|----------|------------|
| Project Init (`moca init`) | 1 | 0 | **1** | 0 | **0%** | **NONE** |
| `moca version` | 1 | 1 | 0 | 0 | 100% | MS-07 |
| Site Management | 12 | 6 | **6** | 0 | 50% | MS-09 |
| App Management | 11 | 9 | 1 | 1 | 82% | MS-09, MS-13 |
| Server/Process | 8 | 6 | **2** | 0 | 75% | MS-10, MS-16 |
| Scheduler | 8 | 2 | **6** | 0 | 25% | MS-16 (vague) |
| Database | 10 | 10 | 0 | 0 | 100% | MS-09, MS-11 |
| Backup/Restore | 8 | 4 | **4** | 0 | 50% | MS-11 |
| Configuration | 8 | 8 | 0 | 0 | 100% | MS-11 |
| Deployment | 6 | 5 | **1** | 0 | 83% | MS-21 |
| Infrastructure Gen | 7 | 6 | **1** | 0 | 86% | MS-21 |
| Developer Tools | 7 | 3 | 3 | 1 | 43% | MS-13, MS-28 |
| Testing | 5 | 3 | 2 | 0 | 60% | MS-25 |
| Build | 5 | 5 | 0 | 0 | 100% | MS-13, MS-17 |
| API Management | 10 | 7 | **3** | 0 | 70% | MS-18 |
| Monitoring/Diagnostics | 4 | 2 | **2** | 0 | 50% | MS-07, MS-24 |
| Queue/Events | 13 | 13 | 0 | 0 | 100% | MS-16 |
| User Management | 10 | 9 | **1** | 0 | 90% | MS-13 |
| Search | 3 | 3 | 0 | 0 | 100% | MS-16 |
| Cache | 5 | 2 | **3** | 0 | 40% | MS-11 |
| Log Management | 3 | 3 | 0 | 0 | 100% | MS-16 |
| Translation | 4 | 4 | 0 | 0 | 100% | MS-20 |
| Notifications | 2 | 0 | **2** | 0 | **0%** | **NONE** |
| Shell Completion | 1 | 1 | 0 | 0 | 100% | MS-07 |
| **TOTALS** | **152** | **111** | **38** | **3** | **73%** | |

### CLI Non-Command Sections

| Section | Source Reference | Roadmap MS | Status |
|---------|----------------|-----------|--------|
| §2 Architecture Overview | lines 45-151 | MS-07 | Fully covered (service layer, driver layer, output layer) |
| §3 Project Structure | lines 154-344 | MS-01 | Fully covered (moca.yaml, moca.lock, .moca/) |
| §5 Global Flags | lines 3248-3264 | MS-07 | Fully covered (--site, --json, --env, etc.) |
| §6 Context Detection | lines 3268-3293 | MS-07 | Fully covered (priority order for resolution) |
| §7 Error Handling | lines 3297-3359 | MS-07 | Fully covered (Context/Cause/Fix format) |
| §8 Extension System | lines 3363-3406 | MS-08 | Fully covered (cli.RegisterCommand in hooks.go) |
| §9 CLI Package Layout | lines 3410-3516 | MS-01, MS-07 | Fully covered |
| §11 ADR Summary (6 ADRs) | lines 3565-3601 | MS-00, MS-07 | Fully covered |

---

## Milestone Audit

### MS-01: Project Structure & Config
- **Covers well:** moca.yaml parsing, directory tree, 5 cmd/ stubs, env var expansion.
- **Missing:** `moca init` command. The CLI design (§4.2.1, lines 567-604) defines `moca init` as a project bootstrapping command with 12 flags (`--template`, `--minimal`, `--apps`, `--db-host`, etc.). No milestone implements this command.
- **Misplaced:** Nothing.
- **Recommended change:** Add `moca init` to MS-09 scope (where `moca site create` lives) or create a new deliverable in MS-07 (CLI Foundation) since `moca init` is the first command any user runs.

### MS-07: CLI Foundation
- **Covers well:** Context resolver, output layer, Cobra scaffold, version, completion, doctor skeleton.
- **Missing:** `moca init` is the natural home for the project bootstrapping command, since it's the first CLI interaction.
- **Recommended change:** Add `moca init` to MS-07 deliverables.

### MS-09: CLI Site & App Commands
- **Covers well:** site create/drop/list/use/info, app install/uninstall/list, db migrate/rollback/diff.
- **Missing:** `moca site clone`, `moca site rename`, `moca site reinstall`, `moca site enable`, `moca site disable`, `moca site browse`. These are marked as "OUT" with a reference to "MS-16", but MS-16's scope does not include them.
- **Recommended change:** Either (a) add a new milestone MS-09a for secondary site commands, or (b) expand MS-11 scope to include these, or (c) add them to the Deferred section explicitly.

### MS-11: CLI DB, Backup, Config, Cache
- **Covers well:** backup create/restore/list/verify, config get/set/remove/list, cache clear, db console/seed/reset.
- **Missing from deliverables:** `moca config edit` is in the IN scope but absent from numbered deliverables. `moca db trim-tables` and `moca db trim-database` are not mentioned. `moca backup schedule/upload/download/prune` are marked OUT but not assigned to any milestone that includes them.
- **Recommended change:** Add `config edit`, `db trim-tables`, `db trim-database` to MS-11 deliverables. Explicitly assign `backup schedule/upload/download/prune` to MS-21.

### MS-13: CLI App Scaffold, Users, Build
- **Covers well:** app new/get/resolve/update/diff, user add/remove/set-password/roles/list/disable/enable, build server/app.
- **Missing:** `moca user impersonate` (CLI doc lines 2963-2979) is absent from both scope and deliverables.
- **Recommended change:** Add `moca user impersonate` to MS-13 deliverables.

### MS-16: CLI Queue/Events/Search/Monitor
- **Covers well:** All 13 queue/events commands, search rebuild/status/query, log tail/search/export.
- **Missing:** Only 4 of 8 scheduler commands are mentioned (start/stop/status/scale). Missing: `scheduler enable`, `scheduler disable`, `scheduler trigger`, `scheduler list-jobs`, `scheduler purge-jobs`. Also missing: `moca worker scale` (only `moca worker start/stop/status` listed).
- **Misplaced:** MS-09 defers `moca site clone/rename` to "MS-16" but MS-16 has no site commands in scope.
- **Recommended change:** Add all 8 scheduler commands explicitly. Add `moca worker scale`. Remove the MS-09 → MS-16 deferral reference for site commands (they need their own home).

### MS-18: API Keys, Webhooks, Custom Endpoints
- **Covers well:** API key CRUD (create/revoke/list/rotate), webhooks (list/test/logs), custom endpoints, whitelisted methods.
- **Missing:** `moca api list` (endpoint introspection), `moca api test` (HTTP test), `moca api docs` (OpenAPI generation). These are in the CLI design (lines 2302-2365) but not in MS-18 deliverables.
- **Recommended change:** Add `moca api list/test/docs` to MS-18 deliverables.

### MS-20: GraphQL, Dashboard, Report, i18n, Storage
- **Covers well:** GraphQL, S3 storage, Dashboard/Report views, translation system.
- **Missing:** Translation CLI commands (`moca translate export/import/status/compile`) are mentioned in scope but not broken down in deliverables or acceptance criteria.
- **Recommended change:** Add specific acceptance criteria for each `moca translate` command.

### MS-21: Deployment & Infrastructure Gen
- **Covers well:** deploy setup/update/rollback, generate caddy/nginx/systemd/docker/k8s.
- **Missing:** `moca deploy promote` is not explicitly in deliverables (though the staging config was added by mismatch resolution). `moca generate supervisor` (legacy compat) is absent. `moca generate env` is in scope but not in deliverables. `moca backup schedule/upload/download/prune` are deferred from MS-11 but not picked up here.
- **Recommended change:** Add `deploy promote`, `generate supervisor` (or explicitly defer it), `generate env` to deliverables. Add backup schedule/upload/download/prune to scope.

### MS-22: Security Hardening
- **Covers well:** OAuth2, SAML/OIDC, field encryption, email notifications, backup encryption.
- **Missing:** `moca notify test-email` and `moca notify config` are in the CLI design (lines 3190-3221) but appear in NO milestone. MS-22 covers email notifications at the framework level but the CLI commands are absent.
- **Recommended change:** Add `moca notify test-email` and `moca notify config` to MS-22 deliverables.

### MS-24: Observability & Profiling
- **Covers well:** Prometheus metrics, OpenTelemetry tracing, moca doctor.
- **Missing:** `moca dev bench` and `moca dev profile` are mentioned in the design reference but not in deliverables. `moca monitor audit` is mentioned in MS-16 scope but not detailed.
- **Recommended change:** Add `moca dev bench` and `moca dev profile` to MS-24 deliverables explicitly.

### MS-25: Testing Framework
- **Covers well:** moca test run, test factory, comprehensive integration tests.
- **Missing:** `moca test coverage` and `moca test fixtures` are not explicitly listed.
- **Sequencing note:** Dependency on MS-23 is correct for comprehensive testing, but unit test utilities could start earlier (after MS-06). This is acceptable as-is.
- **Recommended change:** Add `moca test coverage` and `moca test fixtures` to deliverables.

---

## Gaps and Risks

### GAP-001: `moca init` — Project Bootstrapping Command Missing
- **Severity:** Critical
- **Description:** `moca init` (CLI doc §4.2.1, lines 567-604) is the first command any user runs. It creates the project directory, generates `moca.yaml`, connects to infrastructure, creates the `moca_system` schema, installs the core app, and initializes git. This command appears in NO milestone scope or deliverables.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` lines 567-604
- **Impact:** Users cannot create new projects. The entire getting-started workflow is broken.
- **Recommended Fix:** Add `moca init` to **MS-09** (alongside `moca site create`) or to **MS-07** (CLI Foundation). MS-09 is more logical since `moca init` depends on database connectivity (MS-02) and core app (MS-08).

### GAP-002: 6 Site Management Commands Orphaned
- **Severity:** Critical
- **Description:** `moca site clone/reinstall/enable/disable/rename/browse` (CLI doc lines 763-842) are excluded from MS-09 with a reference to "MS-16", but MS-16 contains no site commands. These 6 commands have no milestone home.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` lines 763-842; `ROADMAP.md` MS-09 OUT clause
- **Impact:** Operational site management is incomplete. Cannot clone sites for staging, cannot disable sites for maintenance.
- **Recommended Fix:** Add these to **MS-11** (operational CLI commands) or create **MS-11a** specifically for secondary site/app operations.

### GAP-003: `moca notify` Commands — Zero Coverage
- **Severity:** High
- **Description:** `moca notify test-email` and `moca notify config` (CLI doc lines 3190-3221) appear in no milestone. The framework design includes a notification subsystem (`pkg/notify/`), and MS-22 implements email sending at the framework level, but the CLI surface is absent.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` lines 3190-3221
- **Impact:** Operators cannot verify SMTP configuration or manage notification settings from CLI.
- **Recommended Fix:** Add to **MS-22** (Security Hardening) deliverables.

### GAP-004: Scheduler Commands Severely Underspecified
- **Severity:** High
- **Description:** 8 scheduler commands designed (CLI doc lines 1222-1336); only 2 (start/stop) clearly scheduled. Missing: `enable`, `disable`, `trigger`, `list-jobs`, `purge-jobs`, `status`.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` lines 1222-1336
- **Impact:** Scheduler job lifecycle management missing from v1.0.
- **Recommended Fix:** Expand **MS-16** to explicitly list all 8 scheduler commands.

### GAP-005: Backup Automation Commands Orphaned
- **Severity:** High
- **Description:** `moca backup schedule/upload/download/prune` (CLI doc lines 1604-1653) are deferred from MS-11 to "MS-21/MS-22" but neither milestone includes them.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` lines 1604-1653; `ROADMAP.md` MS-11 OUT clause
- **Impact:** No automated backup scheduling, no S3 upload, no retention policies in v1.0.
- **Recommended Fix:** Add to **MS-21** deliverables explicitly.

### GAP-006: `moca user impersonate` Missing
- **Severity:** High
- **Description:** Admin user impersonation (CLI doc lines 2963-2979) is absent from MS-13 despite all other user commands being present.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` lines 2963-2979
- **Impact:** Support/debugging workflow incomplete.
- **Recommended Fix:** Add to **MS-13** deliverables.

### GAP-007: `moca api list/test/docs` Missing from MS-18
- **Severity:** High
- **Description:** API introspection (`moca api list`), testing (`moca api test`), and documentation (`moca api docs`) from CLI doc lines 2302-2365 are not in MS-18 deliverables despite being in the same command group as API keys/webhooks.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` lines 2302-2365
- **Impact:** Developers cannot list endpoints, test APIs, or generate OpenAPI specs from CLI.
- **Recommended Fix:** Add to **MS-18** deliverables.

### GAP-008: `moca deploy promote` Missing from MS-21
- **Severity:** Medium
- **Description:** Environment promotion command (CLI doc lines 1882-1893) is not explicitly in MS-21 deliverables. The staging config section added by the mismatch resolution also lacks a roadmap milestone.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` lines 1882-1893
- **Impact:** Multi-environment promotion workflow unavailable.
- **Recommended Fix:** Add to **MS-21** deliverables.

### GAP-009: `moca dev bench/profile` Not Assigned
- **Severity:** Medium
- **Description:** Developer benchmarking and profiling tools (CLI doc lines 2126-2153) are referenced in MS-24 design references but not in its deliverables.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` lines 2126-2153
- **Impact:** Performance profiling tools unavailable for developers.
- **Recommended Fix:** Add to **MS-24** deliverables explicitly.

### GAP-010: `moca generate supervisor` Unresolved
- **Severity:** Medium
- **Description:** Supervisor config generation (CLI doc command tree line 444) exists in the design but is not in MS-21. The mismatch report marked it as "legacy compat, not a supported process manager" but the roadmap neither includes it nor explicitly defers it.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` line 444; mismatch report MISMATCH-023
- **Impact:** Legacy supervisor users have no migration path.
- **Recommended Fix:** Either add to **MS-21** as low-priority deliverable, or add to Deferred section explicitly.

### GAP-011: `tab_audit_log` Partitioning Not in Acceptance Criteria
- **Severity:** Low
- **Description:** The framework design (lines 967-979) specifies `PARTITION BY RANGE(timestamp)` for the audit log table. MS-03 covers DDL generation but the acceptance criteria don't mention partitioning.
- **Source References:** `MOCA_SYSTEM_DESIGN.md` lines 967-979
- **Impact:** Audit log performance at scale if partitioning is missed.
- **Recommended Fix:** Add partitioning to **MS-03** acceptance criteria.

### GAP-012: Staging Config Section Not in Any Milestone
- **Severity:** Low
- **Description:** The mismatch report resolution added a `staging:` section to `moca.yaml` (with `inherits: production`), but no roadmap milestone implements or tests environment config inheritance.
- **Source References:** `MOCA_CLI_SYSTEM_DESIGN.md` moca.yaml staging section; mismatch report MISMATCH-015
- **Impact:** `moca deploy promote staging production` may fail without staging config.
- **Recommended Fix:** Add staging config inheritance to **MS-01** (config parsing) acceptance criteria.

---

## Milestone Dependency Verification

All 30 milestones were audited for dependency correctness:

| Result | Count | Details |
|--------|-------|---------|
| Correct dependencies | 29 | All declared dependencies verified against technical requirements |
| Overstated dependency | 1 | MS-25 depends on "MS-23 (all features)" — correct for comprehensive testing, but unit test utils could start after MS-06 |
| Missing dependencies | 0 | No milestone uses capabilities from undeclared prerequisites |
| Circular dependencies | 0 | Dependency graph is a valid DAG |
| Premature dependencies | 0 | No milestone lists dependencies it doesn't need |

**Critical path verified:** MS-00 → MS-01 → MS-02 → MS-03 → MS-04 → MS-06 → MS-12 → MS-15 → MS-23 → MS-25 → MS-26

**Parallel workstreams verified:**
- Stream A (Backend): MS-03 → MS-04 → MS-05 → MS-06 → MS-08 → MS-10 → MS-12 → MS-14 → MS-15
- Stream B (CLI): MS-07 → MS-09 → MS-11 → MS-13 → MS-16 (syncs at MS-09)
- Stream C (Frontend): MS-17 → MS-19 → MS-20 (starts after MS-06 + MS-14)

All sync points between streams are correct.

---

## Required Roadmap Changes

### Critical (must fix before execution)

- [ ] **Add `moca init` to MS-09** scope and deliverables (or MS-07 if preferred). Include all 12 flags from CLI doc lines 567-604. Add acceptance criteria: `moca init my-erp` creates project dir, generates moca.yaml, connects to PG/Redis, creates moca_system schema.
- [ ] **Assign 6 orphaned site commands** (`clone`, `reinstall`, `enable`, `disable`, `rename`, `browse`) to MS-11 or a new milestone. Remove the incorrect MS-09 → "MS-16" deferral reference.
- [ ] **Add `moca notify test-email` and `moca notify config`** to MS-22 deliverables.

### High (should fix before execution)

- [ ] **Expand MS-16** to explicitly list all 8 scheduler commands and `moca worker scale`.
- [ ] **Add to MS-21** deliverables: `moca deploy promote`, `moca backup schedule/upload/download/prune`, `moca generate supervisor` (or explicitly defer it).
- [ ] **Add `moca user impersonate`** to MS-13 deliverables.
- [ ] **Add `moca api list/test/docs`** to MS-18 deliverables.
- [ ] **Add `moca db trim-tables` and `moca db trim-database`** to MS-11 deliverables (they're in the CLI design but missing from MS-11).

### Medium (recommended before execution)

- [ ] **Add `moca dev bench` and `moca dev profile`** to MS-24 deliverables explicitly.
- [ ] **Add `moca deploy promote`** to MS-21 deliverables.
- [ ] **Add staging config inheritance** (`staging:` section with `inherits: production`) to MS-01 acceptance criteria.
- [ ] **Add `tab_audit_log` partitioning** to MS-03 acceptance criteria.
- [ ] **Add `moca test coverage` and `moca test fixtures`** to MS-25 deliverables.
- [ ] **Clarify MS-20** translation CLI deliverables with specific acceptance criteria for each `moca translate` command.

### Low (can defer)

- [ ] **Add `moca generate supervisor`** to Deferred section if not adding to MS-21.
- [ ] **Add `moca app pin`** to either MS-13 or Deferred section.
- [ ] **Add `moca dev playground`** to MS-28 (post-v1.0) explicitly.
- [ ] **Add `moca cache clear-meta`, `clear-sessions`, `stats`** to MS-11 deliverables (they're in scope text but not in numbered deliverables).

---

## Final Verdict

### Does the roadmap fully fulfill both design docs?

**No — but it is close.** The framework design is covered at ~95% (excellent). The CLI design is covered at ~73% (needs work). The architecture, sequencing, and dependency logic are sound (100% verified). The gaps are concentrated in:

1. **One missing critical command** (`moca init`) that blocks the entire getting-started experience.
2. **6 orphaned site commands** that have no milestone assignment.
3. **~30 commands** that are in milestone scope text but missing from numbered deliverables or acceptance criteria — meaning they might be implemented but aren't formally tracked.
4. **2 command groups** (`moca notify`, `moca init`) with zero coverage.

### What must be fixed before execution starts?

1. **Add `moca init`** to a milestone (MS-07 or MS-09). This is the single most critical gap — without it, no user can start a project.
2. **Resolve the 6 orphaned site commands.** Either assign to MS-11, create MS-09a, or explicitly defer with justification.
3. **Add `moca notify` commands** to MS-22.
4. **Expand MS-16** to cover all scheduler commands.
5. **Add backup automation** (`schedule/upload/download/prune`) to MS-21.

After these 5 changes, the roadmap coverage score would rise to **~90/100**, which is sufficient to begin execution. The remaining ~10% consists of deliverable-level specificity improvements that can be resolved during sprint planning.
