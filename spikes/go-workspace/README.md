# Spike 3: Go Workspace Multi-Module Composition

**Status:** Not started
**Task:** MS-00-T4
**Design Reference:** `docs/blocker-resolution-strategies.md` (Blocker 1, lines 7-63)

## Objective

Validate that multiple app modules with intentionally conflicting dependency versions
compile into a single binary via `go.work` and Go's Minimal Version Selection (MVS).
Document the resolution policy for minor conflicts and the failure mode for major conflicts.

## Key Questions to Answer

1. Does MVS correctly resolve `testify v1.8.0` (stub-a) vs `testify v1.9.0` (stub-b) to v1.9.0?
2. What is the correct way to document a `replace` directive strategy for irresolvable conflicts?
3. Does `go build -race ./...` work cleanly across all workspace modules?
4. How does Go handle a major version conflict (e.g., `pkg` vs `pkg/v2`)? Document clearly.

## Expected Deliverables

- `framework/` — stub framework module (`framework/go.mod`)
- `apps/stub-a/` — module requiring `testify v1.8.0`
- `apps/stub-b/` — module requiring `testify v1.9.0` (intentional minor conflict)
- `go.work` — local workspace composing all three modules
- `ADR-003-go-workspace-composition.md` — documents MVS, conflict policy, replace strategy
