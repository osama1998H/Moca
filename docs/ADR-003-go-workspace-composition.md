# ADR-003: Go Workspace Multi-Module Composition via go.work

**Status:** Accepted
**Spike:** MS-00-T4 (Spike 3)
**Date:** 2026-03-30
**Validated by:** `go test -v -count=1 -race ./...` — all 7 tests pass

---

## Context

MOCA's build model requires multiple app modules (each a separate `go.mod`) to compile
into a single `moca-server` binary. Apps are installed independently and may have
conflicting dependency version requirements. Go workspaces (`go.work`) are the chosen
composition mechanism.

The fundamental risk: if dependency conflicts between installed apps are irresolvable,
or if the workspace build model is fragile, every subsequent milestone that adds a new
app module could break the entire build.

**Reference:** `docs/blocker-resolution-strategies.md` (Blocker 1, lines 7-63);
`MOCA_SYSTEM_DESIGN.md` lines 1380-1384 (Build Composition Model).

---

## Decision

Use Go workspaces (`go.work`) as the primary build composition mechanism. The workspace
composes the framework root module with all installed app modules. `go.work` acts as
the single source of truth for which app modules are active.

### Version Conflict Policy

| Conflict Type | Go Behavior | MOCA Policy |
|--------------|-------------|-------------|
| Minor/patch (v1.8 vs v1.9) | MVS selects the maximum automatically | Allow; no operator action needed |
| Major "conflict" (pkg vs pkg/v2) | Two distinct import paths; both coexist | Not a conflict; document import path change |
| True major conflict (v1.x vs v2.x same path) | Build fails — incompatible APIs | Block install; require operator resolution |

### Workspace-Local Module Dependencies

When app modules depend on workspace-local modules (e.g., the shared framework):

1. Add a `require` entry in the app's go.mod with version `v0.0.0`
2. Add a `replace` directive in the app's go.mod pointing to the local path
3. List the module in `go.work` with a `use` directive

The `replace` directive enables `go mod tidy` and standalone builds. The `use` directive
in `go.work` takes precedence during workspace builds, providing the canonical local path.

### Pre-Install Validation

The `ValidateAppDependencies` function (implemented in this spike's `main.go`) will be
integrated into `moca app get` in MS-13 to detect major-version conflicts before an app
is added to the workspace:

```go
func ValidateAppDependencies(appMod *modfile.File, workspaceMods []*modfile.File) []Conflict
```

Minor conflicts are intentionally not flagged — MVS resolves them automatically by
selecting the maximum version.

### GOWORK=off Behavior

Setting `GOWORK=off` disables workspace-wide MVS. Each module resolves its own `go.mod`
independently. This has two consequences:

1. Cross-module MVS upgrades are lost: a module requiring `testify v1.8.0` no longer
   gets upgraded to `v1.9.0` just because another workspace module requires it.
2. Builds still succeed if `replace` directives are present in go.mod (they handle
   local module paths).

**Implication for CI/Makefile:** Spike test runners should NOT use `GOWORK=off` if they
test workspace behavior. Only self-contained spikes (like redis-streams) use `GOWORK=off`
to avoid inheriting the root workspace.

---

## Alternatives Considered

### Option A: Single Monolithic go.mod

All apps in one root module with a single `go.mod`. One dependency graph, no conflicts.

**Why rejected:** Prevents independent app versioning. All apps must upgrade dependencies
together. Breaks the "install any app, any time" design goal. The entire codebase becomes
a single coupled unit.

### Option B: Replace Directives Only (No go.work)

Use `replace` directives in the root `go.mod` to map app module paths to local directories.
No `go.work` file.

**Why rejected:** `replace` directives are module-level and would require the root module
to know about every installed app. Adding or removing an app would require editing the root
`go.mod`. `go.work` handles this more cleanly via `go work use ./apps/new-app`.

### Option C: Build Tags for App Selection

Use `//go:build` tags in main packages to include/exclude apps at compile time.

**Why rejected:** Build tags don't solve dependency graph composition. All tagged packages
would still need to be in the same module. Doesn't enable independent app modules.

---

## Validation Results

| Test | Result | Key Observation |
|------|--------|----------------|
| TestCrossModuleImport | PASS | Cross-module imports (stub-a, stub-b → framework) resolve correctly within go.work workspace |
| TestMVSResolution | PASS | testify v1.8.0 (stub-a) + v1.9.0 (stub-b) → MVS selects v1.9.0 for all workspace modules |
| TestBuildAllModules | PASS | `go build ./...` compiles all 4 workspace modules together in ~140ms |
| TestRaceBuild | PASS | `go build -race ./...` succeeds; race detector instrumentation works across module boundaries |
| TestMajorVersionCoexistence | PASS | Go treats pkg and pkg/v2 as distinct module paths; both coexist without conflict |
| TestValidateAppDependencies | PASS | Minor conflicts (v1.8 vs v1.9) ignored; synthetic v2.0.0 conflict correctly detected |
| TestGOWORKOffBehavior | PASS | GOWORK=off: stub-a sees testify v1.8.0; workspace active: stub-a sees v1.9.0 (MVS upgrade from stub-b) |

### Key Observation: replace + use Pattern

The standard approach for workspace-local module dependencies requires BOTH:
- `replace` in go.mod — enables `go mod tidy` and standalone builds
- `use` in go.work — provides workspace-level module resolution

Without `replace`, `go mod tidy` attempts to fetch workspace-local modules from the VCS
(even when the workspace is active), causing build failures for unpublished modules.

### Key Observation: MVS Scope

The workspace's MVS applies across ALL modules in the `go.work`. A module installed into
the workspace gets its dependency versions upgraded by the workspace's maximum requirements.
Without the workspace (`GOWORK=off`), each module resolves only its own `go.mod`.

This is a **correctness requirement** for MOCA: apps installed into the workspace must be
built with the workspace-selected dependency versions, not their own isolated versions.

---

## Consequences for Production

### For `moca app get` (MS-13)

1. Parse the new app's `go.mod` before adding it to `go.work`
2. Run `ValidateAppDependencies(newAppMod, existingWorkspaceMods)`
3. If major conflicts are found, present them to the operator and require explicit override
4. If no conflicts, run `go work use ./apps/new-app` to add it to the workspace

### For App Module go.mod Files

- Use `replace` directives for all workspace-local module dependencies
- Use `v0.0.0` as the placeholder version for local modules
- Run `go mod tidy` in each module directory after initial setup

### For CI

- Build and test commands run from the workspace root (where `go.work` is present)
- Do NOT use `GOWORK=off` for integration tests that depend on cross-module MVS behavior
- Use `GOWORK=off` only for truly isolated spike tests that must not inherit the workspace

### Build Time

`go build -race ./...` across 4 modules (framework + 3 apps): ~220ms. Go's build cache
handles incremental workspace builds efficiently. Scaling to 10+ app modules should
remain fast for incremental builds.

---

## References

- `docs/blocker-resolution-strategies.md` (Blocker 1, lines 7-63)
- `MOCA_SYSTEM_DESIGN.md` lines 1380-1384 (Build Composition Model)
- `MOCA_SYSTEM_DESIGN.md` §15 lines 1924-2057 (Framework Package Layout)
- [Go Workspaces Tutorial](https://go.dev/doc/tutorial/workspaces)
- [Go Modules Reference: Workspaces](https://go.dev/ref/mod#workspaces)
- [Minimal Version Selection (MVS)](https://go.dev/ref/mod#minimal-version-selection)
