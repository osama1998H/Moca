# MVP Validation Prompt for Claude Code

> **When to use:** After completing MS-10 (Dev Server & Hot Reload), before starting MS-11.
> **How to use:** Copy everything below the `---` line and paste it as a single message into Claude Code inside the Moca project directory.

---

You are performing a comprehensive MVP validation audit of the Moca framework. Your goal is to read the entire codebase and all design documents, then produce a single deliverable: a markdown document called `docs/MVP-VALIDATION-REPORT.md` containing (1) a gap analysis / backlog of anything incomplete or missing from MS-00 through MS-10, and (2) a pre-release checklist of things to address before starting MS-11.

## Phase 1 — Read Everything (do NOT skip any file)

Read these documents **in full**, line by line. Do not summarize from memory — actually read them:

1. `MOCA_SYSTEM_DESIGN.md` — the full framework architecture (~2160 lines). Pay special attention to:
   - §3.1.1–3.1.3 MetaType, FieldDef, Metadata Lifecycle (lines ~133–296)
   - §3.2.1–3.2.3 Document Runtime (lines ~300–462)
   - §3.3 Customizable API Layer (lines ~466–679)
   - §3.5 Hook Registry (lines ~714–781)
   - §4.1–4.4 Database: schema-per-tenant, system tables, _extra JSONB (lines ~828–1022)
   - §5.1–5.2 Caching & Queue Layer (lines ~1026–1107)
   - §7.1–7.3 App System (lines ~1263–1383)
   - §8.3 Site Lifecycle (lines ~1371–1396)
   - §10.1–10.2 Query Engine & Report Builder (lines ~1654–1725)
   - §11.2–11.3 Logging & Health Checks (lines ~1749–1770)
   - §12.1 Single-Instance Development (lines ~1773–1791)
   - §14 Complete Request Lifecycle (lines ~1884–1920)
   - §15 Framework Package Layout (lines ~1924–2057)
   - ADR section (lines ~2061–2105)

2. `MOCA_CLI_SYSTEM_DESIGN.md` — the CLI architecture (~3699 lines). Pay special attention to:
   - §2.3 CLI Internal Architecture (lines ~92–150)
   - §3 Project Structure & §3.1 Project Manifest (lines ~155–313)
   - §3.2 Lockfile (lines ~315–345)
   - §4.1 Command Tree Overview (lines ~349–559)
   - §4.2.1 `moca init` (lines ~565–640)
   - §4.2.2 Site Management (lines ~644–865)
   - §4.2.3 App Management (lines ~869–1091)
   - §4.2.4 Server/Process Management (lines ~1094–1217)
   - §4.2.5 Database Operations (lines ~1327–1495)
   - §6 Context Detection & Resolution (lines ~3268–3293)
   - §7 Error Handling (lines ~3297–3359)
   - §8 Extension System (lines ~3363–3406)
   - §9 CLI Internal Package Layout (lines ~3410–3516)

3. `ROADMAP.md` — the full roadmap (~1450 lines). Read every milestone from MS-00 through MS-10 in detail, including all:
   - Scope (IN and OUT)
   - Deliverables (numbered lists)
   - Acceptance Criteria (every bullet)
   - Design References

4. **All milestone plan documents** in `docs/`:
   - `docs/MS-00-architecture-validation-spikes-plan.md`
   - `docs/MS-01-project-structure-configuration-plan.md`
   - `docs/MS-02-postgresql-foundation-redis-connection-layer-plan.md`
   - `docs/MS-03-metadata-registry-plan.md`
   - `docs/MS-04-document-runtime-plan.md`
   - `docs/MS-05-query-engine-and-report-foundation-plan.md`
   - `docs/MS-06-rest-api-layer-plan.md`
   - `docs/MS-07-cli-foundation-plan.md`
   - `docs/MS-08-hook-registry-and-app-system-foundation-plan.md`
   - `docs/MS-09-cli-project-init-site-and-app-commands-plan.md`

5. **Supporting documents**:
   - `docs/blocker-resolution-strategies.md`
   - `docs/moca-cross-doc-mismatch-report.md`
   - `docs/moca-database-decision-report.md`
   - `docs/roadmap-gap-fix-summary.md`
   - `CLAUDE.md`

## Phase 2 — Read the Entire Codebase

After reading all documents, systematically read **every Go source file** in the project. Use `find` and `cat` or read them methodically:

- Every `.go` file under `cmd/` (all 5 binaries)
- Every `.go` file under `pkg/meta/` (MetaType, FieldDef, compiler, registry, migrator, watcher)
- Every `.go` file under `pkg/document/` (Document, lifecycle, naming, validator, controller, CRUD)
- Every `.go` file under `pkg/orm/` (PostgreSQL adapter, query builder, transactions, schema DDL, migrations)
- Every `.go` file under `pkg/api/` (REST gateway, CRUD endpoints, transformers, versioning, rate limiter)
- Every `.go` file under `pkg/observe/` (logging, health checks)
- Every `.go` file under `pkg/hooks/` (HookRegistry, doc events, priority sorting, dependency resolution)
- Every `.go` file under `pkg/apps/` (AppManifest, loader, installer)
- Every `.go` file under `pkg/tenancy/` (site creation manager)
- Every `.go` file under `internal/` (config, drivers, context, output, process, scaffold, lockfile)
- Every `.go` file under `apps/core/` (core doctypes, controllers, manifest)
- Every `*_test.go` file across the entire project
- Every `.json` file under `apps/core/` (DocType definitions)
- `go.mod`, `go.work`, `go.sum`, `Makefile`, `docker-compose.yml`, `.golangci.yml`
- Any other source files you discover

Also run these commands to understand the project state:
```
go build ./...
go vet ./...
make test 2>&1 | tail -50
```

## Phase 3 — Validate Each Milestone

For **each milestone MS-00 through MS-10**, cross-reference:
1. Every **Deliverable** listed in `ROADMAP.md` — does corresponding code exist? Is it complete or stubbed?
2. Every **Acceptance Criterion** — does the code fulfill it? Can you find a test that verifies it?
3. Every **Design Reference** — does the implementation match what the design doc specifies? Note any deviations.
4. The **Scope IN** items — are they all implemented?
5. The detailed milestone plan doc in `docs/` — does the implementation match the plan?

Be extremely thorough. Check for:
- Functions/methods mentioned in design docs that don't exist in code
- Interfaces defined in design docs that aren't implemented
- Field types, constants, or enums specified but missing
- CLI commands that should exist but don't (check cobra registrations)
- Test coverage gaps (acceptance criteria without corresponding tests)
- TODO/FIXME/HACK comments in the code that indicate incomplete work
- Placeholder or stub implementations that need to be fleshed out
- Error handling patterns that don't match the design (e.g., rich errors in CLI)
- Missing integration tests specified in deliverables
- Configuration fields defined in design docs but not in the config struct

## Phase 4 — Generate the Report

Create `docs/MVP-VALIDATION-REPORT.md` with this exact structure:

```markdown
# Moca MVP Validation Report (MS-00 → MS-10)

**Generated:** [date]
**Codebase state:** [git commit hash from `git rev-parse HEAD`]
**Build status:** [pass/fail from `go build ./...`]
**Test status:** [pass/fail with summary from `make test`]

## Executive Summary

[2-3 paragraphs: overall MVP completeness assessment, major findings, recommendation on readiness for MS-11]

## Milestone-by-Milestone Audit

### MS-00: Architecture Validation Spikes
**Status:** [Complete / Partially Complete / Incomplete]
**Completeness:** [X/Y deliverables, X/Y acceptance criteria]

#### Fulfilled
- [List what IS implemented and verified]

#### Gaps
- [List what is missing, incomplete, or deviates from spec]
  - Severity: [Critical / Major / Minor]
  - Design reference: [specific section/line]
  - Recommendation: [fix now / defer to backlog / acceptable deviation]

[Repeat for MS-01 through MS-10]

## Cross-Cutting Concerns

### Code Quality
- [ ] All `go vet` warnings resolved
- [ ] All `golangci-lint` issues resolved
- [ ] No `TODO`/`FIXME` comments blocking MVP functionality
- [ ] Consistent error handling patterns
- [ ] Consistent naming conventions matching design docs

### Test Coverage
- [ ] Unit tests for all core packages
- [ ] Integration tests for DB operations
- [ ] Integration tests for API endpoints
- [ ] Integration tests for CLI commands
- [ ] All acceptance criteria have corresponding test coverage

### Design Doc Compliance
- [ ] Package layout matches §15 of MOCA_SYSTEM_DESIGN.md
- [ ] CLI command tree matches §4.1 of MOCA_CLI_SYSTEM_DESIGN.md
- [ ] MetaType struct matches §3.1.1 specification
- [ ] All 33 FieldTypes implemented with correct column mappings
- [ ] All 18 lifecycle events implemented in correct order
- [ ] All 6 naming strategies implemented
- [ ] All 15 query operators implemented
- [ ] REST API endpoints match §3.3 specification
- [ ] Hook priority and dependency resolution matches §3.5
- [ ] Config resolution matches §3.1 of CLI design doc

### Architectural Decisions
- [ ] AfterConnect (not BeforeAcquire) used for search_path — per ADR-001
- [ ] Per-site pool registry with idle eviction — per ADR-001
- [ ] go-redis v9 (not franz-go) for Redis Streams — per ADR-002
- [ ] XAutoClaim for at-least-once delivery — per ADR-002
- [ ] `app:command` namespace convention for CLI — per ADR-005
- [ ] Index-per-tenant for Meilisearch — per ADR-006

## Consolidated Backlog

| # | Item | Source MS | Severity | Category | Effort Est. | Recommendation |
|---|------|-----------|----------|----------|-------------|----------------|
| 1 | [description] | MS-XX | Critical/Major/Minor | Bug/Gap/Enhancement/Tech-Debt | S/M/L | Fix before MS-11 / Defer to MS-XX / Accept |

### Items Blocking MS-11 Start
[List only items that MUST be resolved before starting MS-11, with justification]

### Items Safe to Defer
[List items that can be addressed later without blocking progress]

## Pre-Release Checklist (Before Starting MS-11)

### Must Do
- [ ] [Action item with specific file/function reference]

### Should Do
- [ ] [Action item]

### Nice to Have
- [ ] [Action item]

## MVP Feature Matrix

| Feature | Design Doc Reference | Implemented | Tested | Notes |
|---------|---------------------|-------------|--------|-------|
| MetaType definition & compilation | SYS §3.1.1 | ✅/❌/⚠️ | ✅/❌ | |
| 33 FieldTypes with column mapping | SYS §3.1.2 | | | |
| Schema compiler (JSON → MetaType) | SYS §3.1.2 | | | |
| 3-tier metadata cache (mem/Redis/PG) | SYS §3.1.3 | | | |
| Schema migrator (diff → DDL) | SYS §4.4 | | | |
| Document interface & DynamicDoc | SYS §3.2.1 | | | |
| 18-event lifecycle engine | SYS §3.2.2 | | | |
| 6 naming strategies | SYS §3.2.1 | | | |
| Field-level validation | SYS §3.2.3 | | | |
| CRUD operations | SYS §3.2 | | | |
| Query builder (15 operators) | SYS §10.1 | | | |
| _extra JSONB transparency | SYS §4.4 | | | |
| Link field auto-joins | SYS §10.1 | | | |
| ReportDef & QueryReport | SYS §10.2 | | | |
| REST API auto-generation | SYS §3.3 | | | |
| API versioning | SYS §3.3 | | | |
| Rate limiting (Redis sliding window) | SYS §3.3 | | | |
| Request/response transformers | SYS §3.3 | | | |
| Audit log on mutations | SYS §3.3 | | | |
| HookRegistry (priority + deps) | SYS §3.5 | | | |
| DocEvent dispatcher integration | SYS §3.5 | | | |
| AppManifest parser & loader | SYS §7.1 | | | |
| apps/core with 5 DocTypes | SYS §7.2 | | | |
| Schema-per-tenant DB isolation | SYS §4.1 | | | |
| Transaction manager | SYS §4.2 | | | |
| Redis client factory (4 DBs) | SYS §5.1 | | | |
| System schema DDL (3 tables) | SYS §4.2 | | | |
| Health checks (live/ready) | SYS §11.3 | | | |
| Structured logging (slog) | SYS §11.2 | | | |
| CLI context resolver | CLI §6 | | | |
| CLI output formatters (TTY/JSON/Table) | CLI §7 | | | |
| CLI rich error format | CLI §7 | | | |
| 24 command groups registered | CLI §4.1 | | | |
| moca version/completion/doctor | CLI §4.2 | | | |
| moca init (project bootstrap) | CLI §4.2.1 | | | |
| moca site create/drop/list/use/info | CLI §4.2.2 | | | |
| moca app install/uninstall/list | CLI §4.2.3 | | | |
| moca db migrate/rollback/diff | CLI §4.2.5 | | | |
| Site creation 9-step lifecycle | SYS §8.3 | | | |
| Migration runner with version tracking | CLI §4.2.5 | | | |
| App installer (deps, migrate, fixtures) | SYS §7.1 | | | |
| Dev server (all-in-one process) | SYS §12.1 | | | |
| MetaType hot reload (fsnotify) | SYS §3.1.3 | | | |
| Goroutine supervisor | CLI §4.2.4 | | | |
| moca serve/stop/restart | CLI §4.2.4 | | | |
| PID file management | CLI §4.2.4 | | | |
| moca.yaml full schema parsing | CLI §3.1 | | | |
| Config resolution & env expansion | CLI §3.1 | | | |
| 5 cmd/ binaries build successfully | SYS §15 | | | |

[Fill every row with ✅ (implemented + tested), ⚠️ (partial/untested), or ❌ (missing)]
```

## Important Rules

1. **Do NOT modify any source code.** This is a read-only audit.
2. **Do NOT fabricate findings.** If you cannot verify something, say "Unable to verify — [reason]".
3. **Be specific.** Reference exact file paths, function names, line numbers, and design doc sections.
4. **Distinguish severity clearly:** Critical = blocks MVP claim, Major = significant gap but workaround exists, Minor = polish/enhancement.
5. **Check every single acceptance criterion** from the ROADMAP — do not skip any.
6. **Run the actual build and tests** — report real output, not assumptions.
7. **Cross-reference the mismatch report** (`docs/moca-cross-doc-mismatch-report.md`) — verify that all 30 documented mismatches have been resolved in code.
8. **Check the blocker resolutions** (`docs/blocker-resolution-strategies.md`) — verify all 4 blockers are resolved in the implementation.
