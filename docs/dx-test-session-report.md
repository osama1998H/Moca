# Developer Experience Test Session Report

**Date:** 2026-04-08  
**Version tested:** v0.1.1-alpha.7  
**Method:** Clean install in `/tmp` using the install script + published packages  

## Test Steps Performed

1. Downloaded moca via `install.sh` (worked)
2. Ran `moca init myproject` (worked)
3. Ran `moca desk install` (failed - Issue #1)
4. Ran `moca app new library --doctype Book --desk` (failed - Issues #2, #3, #4)
5. Attempted `go mod tidy` on the app (failed - Issue #5)
6. From prior in-framework test: server doesn't load app hooks (Issue #6)
7. From prior in-framework test: Vite can't resolve app extension imports (Issue #7)

## Issues Found

### Issue #1: Desk package version mismatch (scaffold)

**Severity:** Blocker  
**File:** `internal/scaffold/desk_templates.go`  

The desk scaffold generates `"@osama1998h/desk": "^0.1.0"` but only alpha pre-release versions are published (`0.1.1-alpha.1` through `0.1.1-alpha.7`). npm semver does not resolve `^0.1.0` to pre-release versions on higher minor/patch.

**Fix options:**
- A) Scaffold should pin to the current release version (e.g., extract from `moca version` or a build-time constant)
- B) Publish a stable `0.1.0` release to npm
- C) Use a pre-release-aware range like `">=0.1.0-0"`

---

### Issue #2: `moca init` doesn't create `go.work`

**Severity:** Blocker  
**File:** `cmd/moca/init.go`  

`moca init` creates the project directory, `moca.yaml`, `.moca/`, `apps/`, `sites/`, `desk/` — but no `go.work` or `go.mod`. The `moca app new` scaffold calls `go work use` which requires a `go.work` to exist.

**Fix:** `moca init` should create a `go.work` (and optionally a `go.mod`) in the project root.

---

### Issue #3: App `go.mod` replace directive points to wrong location

**Severity:** Blocker  
**File:** `internal/scaffold/scaffold.go` (line 83, `readGoModulePath`)  

The scaffold reads `go.mod` from the project root to determine the module path. When the project root has no `go.mod`, it falls back to `github.com/osama1998H/moca`. The generated app `go.mod` then uses:

```
replace github.com/osama1998H/moca => ../..
```

This points to the project root (`myproject/`) which has no Go module. For standalone projects (not inside the framework repo), this should either:
- Point to the actual framework source (if cloned locally), or
- Use a version tag (no replace needed) for published framework releases

**Fix:** For standalone projects, generate a `go.mod` without a `replace` directive, using the published framework version:
```
require github.com/osama1998H/moca v0.1.1-alpha.7
```

---

### Issue #4: Missing `go.mod` in project root

**Severity:** Blocker (related to #2 and #3)  
**File:** `cmd/moca/init.go`  

The project root has no `go.mod`. This means:
- `readGoModulePath()` falls back to a hardcoded path
- The replace directive in app `go.mod` can't resolve
- `go work use` can't function

**Fix:** `moca init` should create a `go.mod` at the project root (even if it's just a workspace root module).

---

### Issue #5: Builtin core lived behind a separate Go module boundary

**Severity:** Blocker  
**File:** `apps/core/go.mod`  

The old `apps/core` directory had its own `go.mod`, making builtin core a separate Go module. When the root module was published to the Go proxy, `apps/core/` was excluded (Go module proxy strips directories with their own `go.mod`). External consumers could then fail `go mod tidy` even though builtin core is a framework-owned package.

**Implemented fix:** Fold builtin core into the root module at `pkg/builtin/core`, delete the nested `apps/core/go.mod`, remove the `go.work` entry for `./apps/core`, and switch release validation to a root-tag-only smoke test that imports `pkg/builtin/core`.

---

### Issue #6: Server doesn't load app `Initialize()` functions

**Severity:** Major  
**File:** `internal/serve/server.go` (line ~97)  

The HTTP server creates an empty `ControllerRegistry` and `HookRegistry` but never calls any app's `Initialize()` function. Controllers and hooks registered in `hooks.go` are never active at runtime.

`cmd/moca/services.go` hardcodes `core.Initialize()` but this only affects CLI commands, not the serve path.

**Impact:** Custom app controllers (validation hooks, lifecycle hooks) don't fire. The first test session required manually adding `library.Initialize()` to `server.go` and `services.go`.

**Fix:** The server startup should scan installed apps and call their `Initialize()` functions dynamically. This could use:
- Go plugin system
- A generated `app_registry.go` file (created by `moca build server`)
- `moca build server` already composes apps into a binary — it should also wire their Initialize calls

---

### Issue #7: Vite can't resolve imports from app extension files

**Severity:** Major  
**File:** `desk/src/vite-plugin.ts`, generated `.moca-extensions.ts`  

When `moca build desk` discovers app extensions (e.g., `apps/library/desk/pages/LibraryDashboard.tsx`), the generated imports work syntactically. But Vite's module resolution fails because:
- The app extension file is outside `desk/` (it's in `apps/library/desk/`)
- Vite resolves `@osama1998h/desk` and `react` relative to the file's location
- No `node_modules` exists in `apps/library/desk/`

**Fix:** The `mocaDeskPlugin()` Vite plugin should add `resolve.alias` entries for `@osama1998h/desk`, `react`, and `react-dom` pointing to `desk/node_modules/`. The workaround from the first test session:

```ts
resolve: {
  alias: {
    "@osama1998h/desk": path.resolve(__dirname, "node_modules/@osama1998h/desk"),
    "react": path.resolve(__dirname, "node_modules/react"),
    "react-dom": path.resolve(__dirname, "node_modules/react-dom"),
    "react/jsx-runtime": path.resolve(__dirname, "node_modules/react/jsx-runtime"),
    "react/jsx-dev-runtime": path.resolve(__dirname, "node_modules/react/jsx-dev-runtime"),
  },
}
```

---

## Summary

| # | Issue | Severity | Component | Status |
|---|-------|----------|-----------|--------|
| 1 | Desk version `^0.1.0` unresolvable | Blocker | scaffold/desk_templates.go | **Fixed** — release builds now pin to exact version |
| 2 | No `go.work` after `moca init` | Blocker | cmd/moca/init.go | **Fixed** — `initGoWorkspace()` creates go.work |
| 3 | Wrong replace path in app go.mod | Blocker | internal/scaffold/scaffold.go | Open |
| 4 | No `go.mod` in project root | Blocker | cmd/moca/init.go | **Fixed** — `initGoWorkspace()` creates go.mod |
| 5 | Builtin core nested-module release gap | Blocker | apps/core/go.mod, root go.mod | **Fixed** — builtin core moved into root module as `pkg/builtin/core` |
| 6 | Server doesn't load app hooks | Major | internal/serve/server.go | **Fixed** — app init registry + auto-loading via init() |
| 7 | Vite can't resolve app ext imports | Major | desk/src/vite-plugin.ts | **Fixed** — resolve aliases + fs.allow added to mocaDeskPlugin() |

**6 fixed**, 1 blocker remaining.

## Recommended Fix Priority

1. **Issue #3** (scaffold replace path) — Next priority. Fix the go.mod template to use published version for standalone projects.
2. **Issue #6** (server hooks) — Implement dynamic app loading in `moca build server` or server startup.
3. **Issue #7** (Vite resolve) — Add resolve aliases to `mocaDeskPlugin()`.
