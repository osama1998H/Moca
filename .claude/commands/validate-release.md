# Release Validation Audit

You are performing a comprehensive release validation audit of the Moca framework for the **$ARGUMENTS** release.

First, read `ROADMAP.md` fully to identify which milestones belong to the **$ARGUMENTS** release. The release groupings are defined in the "Suggested Release Grouping" section:

- **MVP** = MS-00 through MS-10
- **Alpha** = MS-00 through MS-17 (MVP + MS-11 through MS-17)
- **Beta** = MS-00 through MS-23 (Alpha + MS-18 through MS-23)
- **v1.0** = MS-00 through MS-26 (Beta + MS-24 through MS-26)

If the argument does not match any of these, treat it as a custom range and ask the user to confirm which milestones to validate.

Your goal is to produce a single deliverable: `docs/$ARGUMENTS-VALIDATION-REPORT.md`.

---

## Phase 1 — Read All Design Documents (do NOT skip any file)

Read these documents **in full**, line by line. Do not rely on memory — actually read them using the Read tool:

1. **`docs/MOCA_SYSTEM_DESIGN.md`** — the full framework architecture. Pay special attention to every section referenced by the milestones in scope.

2. **`docs/MOCA_CLI_SYSTEM_DESIGN.md`** — the CLI architecture. Pay special attention to every section referenced by the milestones in scope.

3. **`ROADMAP.md`** — the full roadmap. Read every milestone in scope in detail, including all Scope, Deliverables, Acceptance Criteria, and Design References.

4. **All milestone plan documents** in `docs/milestones/` for every milestone in scope (e.g., `docs/milestones/MS-XX-*-plan.md`).

5. **Supporting documents**:
   - `docs/blocker-resolution-strategies.md`
   - `docs/moca-cross-doc-mismatch-report.md`
   - `docs/moca-database-decision-report.md`
   - `docs/roadmap-gap-fix-summary.md`
   - `CLAUDE.md`

## Phase 2 — Read the Entire Codebase

After reading all documents, systematically read **every Go source file** in the project:

- Every `.go` file under `cmd/` (all 5 binaries)
- Every `.go` file under `pkg/` (all subpackages)
- Every `.go` file under `internal/` (all subpackages)
- Every `.go` file under `apps/core/` (core doctypes, controllers, manifest)
- Every `*_test.go` file across the entire project
- Every `.json` file under `apps/core/` (DocType definitions)
- `go.mod`, `go.work`, `go.sum`, `Makefile`, `docker-compose.yml`, `.golangci.yml`
- Any other source files you discover

Spawn multiple agents to read in parallel where possible.

Also run these commands and capture their output:

```bash
go build ./...
go vet ./...
make test 2>&1 | tail -80
make lint 2>&1 | tail -80
git rev-parse HEAD
```

## Phase 3 — Validate Each Milestone

For **each milestone in scope**, cross-reference:

1. Every **Deliverable** listed in `ROADMAP.md` — does corresponding code exist? Is it complete or stubbed?
2. Every **Acceptance Criterion** — does the code fulfill it? Can you find a test that verifies it?
3. Every **Design Reference** — does the implementation match what the design doc specifies? Note any deviations.
4. The **Scope IN** items — are they all implemented?
5. The detailed milestone plan doc in `docs/` — does the implementation match the plan? Are all tasks marked as completed?

Be extremely thorough. Check for:

- Functions/methods mentioned in design docs that don't exist in code
- Interfaces defined in design docs that aren't implemented
- Field types, constants, or enums specified but missing
- CLI commands that should exist but don't (check cobra registrations)
- Test coverage gaps (acceptance criteria without corresponding tests)
- `TODO` / `FIXME` / `HACK` comments indicating incomplete work
- Placeholder or stub implementations that need to be fleshed out
- Error handling patterns that don't match the design (e.g., rich errors in CLI)
- Missing integration tests specified in deliverables
- Configuration fields defined in design docs but not in the config struct
- Tasks in milestone plan files that are NOT marked as `Completed`

## Phase 4 — Generate the Report

Create `docs/$ARGUMENTS-VALIDATION-REPORT.md` with this structure:

```markdown
# Moca $ARGUMENTS Validation Report

**Generated:** [date]
**Release:** $ARGUMENTS
**Milestones in scope:** [list]
**Codebase state:** [git commit hash]
**Build status:** [pass/fail]
**Test status:** [pass/fail with summary]
**Lint status:** [pass/fail with summary]

## Executive Summary

[2-3 paragraphs: overall release completeness assessment, major findings, recommendation on readiness to proceed to next phase]

## Milestone-by-Milestone Audit

### MS-XX: [Title]
**Status:** [Complete / Partially Complete / Incomplete]
**Completeness:** [X/Y deliverables fulfilled, X/Y acceptance criteria met]
**Plan tasks:** [X/Y tasks marked completed in plan file]

#### Fulfilled
- [What IS implemented and verified, with file paths]

#### Gaps
- [What is missing, incomplete, or deviates from spec]
  - **Severity:** Critical / Major / Minor
  - **Design reference:** [specific section/line]
  - **Recommendation:** Fix now / Defer to backlog / Acceptable deviation

[Repeat for every milestone in scope]

## Cross-Cutting Concerns

### Code Quality
- [ ] All `go vet` warnings resolved
- [ ] All `golangci-lint` issues resolved
- [ ] No `TODO`/`FIXME` comments blocking release functionality
- [ ] Consistent error handling patterns
- [ ] Consistent naming conventions matching design docs

### Test Coverage
- [ ] Unit tests for all core packages
- [ ] Integration tests for DB operations
- [ ] Integration tests for API endpoints
- [ ] Integration tests for CLI commands
- [ ] All acceptance criteria have corresponding test coverage

### Design Doc Compliance
[Dynamically generate checklist items based on which milestones are in scope. Include every major specification point from the design docs that the in-scope milestones should have implemented.]

### Architectural Decisions (ADRs)
[Verify each ADR from the spikes is correctly reflected in production code:
- ADR-001 (pg-tenant): AfterConnect for search_path, per-site pool registry
- ADR-002 (redis-streams): go-redis v9, XAutoClaim for at-least-once delivery
- ADR-003 (go-workspace): MVS resolution, replace directives
- ADR-005 (cobra-ext): init() + blank imports, app:command namespace
- ADR-006 (meilisearch): index-per-tenant, tenant-token]

## Consolidated Backlog

| # | Item | Source MS | Severity | Category | Effort | Recommendation |
|---|------|-----------|----------|----------|--------|----------------|
| 1 | [description] | MS-XX | Critical/Major/Minor | Bug/Gap/Enhancement/Tech-Debt | S/M/L | Fix before next phase / Defer to MS-XX / Accept |

### Items Blocking Next Phase
[List only items that MUST be resolved before proceeding, with justification]

### Items Safe to Defer
[List items that can be addressed later without blocking progress]

## Pre-Next-Phase Checklist

### Must Do
- [ ] [Specific action items with file/function references]

### Should Do
- [ ] [Action items]

### Nice to Have
- [ ] [Action items]

## Feature Matrix

[Generate a complete feature matrix table covering every major feature from every milestone in scope. Each row should have: Feature | Design Doc Reference | Implemented (✅/❌/⚠️) | Tested (✅/❌) | Notes]
```

## Rules

1. **Do NOT modify any source code.** This is a read-only audit.
2. **Do NOT fabricate findings.** If you cannot verify something, say "Unable to verify — [reason]".
3. **Be specific.** Reference exact file paths, function names, line numbers, and design doc sections.
4. **Distinguish severity clearly:** Critical = blocks release claim, Major = significant gap but workaround exists, Minor = polish/enhancement.
5. **Check every single acceptance criterion** from the ROADMAP — do not skip any.
6. **Run the actual build and tests** — report real output, not assumptions.
7. **Cross-reference the mismatch report** (`docs/moca-cross-doc-mismatch-report.md`) — verify documented mismatches are resolved in code.
8. **Cross-reference blocker resolutions** (`docs/blocker-resolution-strategies.md`) — verify all blockers relevant to in-scope milestones are resolved.
9. **Spawn multiple agents** to parallelize reading and validation where possible.
