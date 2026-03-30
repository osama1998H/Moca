# Spike 3: Go Workspace Multi-Module Composition

**Status:** Completed
**Task:** MS-00-T4
**Design Reference:** `docs/blocker-resolution-strategies.md` (Blocker 1, lines 7-63)
**ADR:** `ADR-003-go-workspace-composition.md`

## Result

All 7 tests pass with the `-race` flag. Multiple app modules with intentionally conflicting
dependency versions (`testify v1.8.0` in stub-a vs `v1.9.0` in stub-b) compile into a
single binary via `go.work`. MVS correctly selects v1.9.0 (the maximum) for all workspace
modules. `ValidateAppDependencies` correctly identifies major-version conflicts while
ignoring minor ones.

## Key Findings

| Question | Answer |
|----------|--------|
| Does MVS resolve minor conflicts automatically? | Yes — testify v1.8.0 (stub-a) + v1.9.0 (stub-b) → workspace selects v1.9.0 for all modules |
| Are major version "conflicts" actually conflicts in Go? | No — `pkg` and `pkg/v2` are distinct module paths; they coexist independently |
| Does `go build -race ./...` work across workspace modules? | Yes — all 4 modules build cleanly with the race detector in ~220ms |
| Can workspace-local modules be tidied without replace directives? | No — `go mod tidy` tries VCS lookup even in workspace mode; replace directives are required |
| What changes with `GOWORK=off`? | MVS scope reduces to per-module; stub-a sees testify v1.8.0 instead of workspace-upgraded v1.9.0 |

## Design Discovery: replace + use Pattern

Workspace-local module dependencies require BOTH a `replace` directive in go.mod AND a
`use` directive in go.work. The `replace` enables `go mod tidy` to run without hitting
the module proxy for unpublished modules. The `use` provides workspace-level resolution
during builds and tests.

## Deliverables

- `go.work` — local workspace composing root + framework + stub-a + stub-b
- `go.mod` — root module with replace directives for local deps
- `main.go` — `ValidateAppDependencies` function + `Conflict` struct + `majorVersion` helper
- `main_test.go` — 7 tests: `TestCrossModuleImport`, `TestMVSResolution`, `TestBuildAllModules`, `TestRaceBuild`, `TestMajorVersionCoexistence`, `TestValidateAppDependencies`, `TestGOWORKOffBehavior`
- `framework/` — stub framework module (`FrameworkVersion()`)
- `apps/stub-a/` — module requiring testify v1.8.0
- `apps/stub-b/` — module requiring testify v1.9.0
- `ADR-003-go-workspace-composition.md` — architecture decision record

## Running the Spike

```bash
cd spikes/go-workspace
go test -v -count=1 -race ./...
```

Or from the repo root:

```bash
make spike-gowork
```
