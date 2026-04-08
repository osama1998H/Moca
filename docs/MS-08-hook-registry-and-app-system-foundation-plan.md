# MS-08 - Hook Registry and App System Foundation Plan

## Milestone Summary
- **ID:** MS-08
- **Name:** Hook Registry and App System Foundation
- **Roadmap Reference:** `ROADMAP.md` lines 521-561
- **Goal:** Implement HookRegistry (priority-ordered, dependency-aware), AppManifest parser, app directory scanner, and the builtin `pkg/builtin/core` framework package with core DocTypes (User, Role, DocType, Module, SystemSettings).
- **Why it matters:** Hooks wire app-provided lifecycle behavior into Document Runtime. Core app provides the minimum MetaTypes for the system to function. Without this milestone, apps cannot extend framework behavior and the system has no built-in doctypes.
- **Position in roadmap:** MS-08 (Phase 2 -- Core Runtime). Follows MS-07 (CLI Foundation). Precedes MS-09 (CLI Project Init, Site, App Commands).
- **Upstream dependencies:** MS-04 (Document Runtime) -- complete
- **Downstream dependencies:** MS-09 (app install/uninstall CLI), MS-10 (Dev Server), MS-13 (CLI App Scaffold), MS-14 (Permission Engine)

## Vision Alignment

MS-08 is a critical inflection point in the Moca architecture. The framework's core proposition is that a single MetaType definition drives everything -- schema, validation, lifecycle, permissions, API, and UI. But until hooks exist, there is no mechanism for apps to inject behavior into the document lifecycle. And until the app system exists, there is no structure for packaging and loading those apps.

This milestone transforms Moca from an internal runtime into an extensible platform by providing:
1. **HookRegistry** -- the extension point through which apps customize document behavior
2. **App system** -- the packaging/loading mechanism for apps
3. **Core app** -- the minimum viable set of system doctypes (User, Role, DocType, Module, SystemSettings)

The explicit priority + dependency ordering in HookRegistry is a deliberate improvement over Frappe's implicit hook ordering, which caused subtle ordering bugs in production ERPNext deployments.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `ROADMAP.md` | MS-08 entry | 521-561 | Scope, deliverables, acceptance criteria, risks |
| `MOCA_SYSTEM_DESIGN.md` | S3.5 Hook Registry | 714-781 | HookRegistry struct, PrioritizedHandler, DocEvent constants, design rationale |
| `MOCA_SYSTEM_DESIGN.md` | S7.1 App Manifest | 1263-1319 | AppManifest struct, AppDep, ModuleDef, Migration |
| `MOCA_SYSTEM_DESIGN.md` | S7.2 App Installation Lifecycle | 1321-1335 | 5-step install flow |
| `MOCA_SYSTEM_DESIGN.md` | S7.3 App Directory Structure | 1337-1384 | Full app layout with manifests, hooks, doctypes |
| `MOCA_CLI_SYSTEM_DESIGN.md` | S8.1-8.3 Extension System | 3363-3406 | init() registration, blank imports, namespace conventions |
| `pkg/document/lifecycle.go` | DocEvent constants, DocLifecycle interface | 1-126 | 14 events + dispatch function that hooks integrate with |
| `pkg/document/controller.go` | ControllerRegistry | 1-126 | Thread-safe registry pattern to follow |
| `pkg/document/crud.go` | Insert/Update lifecycle | 604-696, 743-832 | Integration points for hook dispatch |
| `pkg/meta/stubs.go` | DocHookDefs placeholder | 79-83 | Empty struct to be fleshed out |
| `pkg/meta/registry.go` | Three-tier cache pattern | 1-143 | Registry design pattern reference |
| `internal/config/parse.go` | YAML parsing | 1-107 | Pattern for AppManifest parser |
| `pkg/cli/registry.go` | Command registration | full file | init() + blank import pattern (validated in ADR-005) |
| `pkg/builtin/core/core.go` | Package doc | 1-8 | Empty package to be populated |
| `spikes/cobra-ext/ADR-005-cobra-cli-extension.md` | Full ADR | all | Validated init() registration pattern |

## Research Notes

No web research was needed. All implementation patterns are well-defined in the existing design documents and validated spikes. Key findings from codebase exploration:

1. **DocEvent constants already exist** in `pkg/document/lifecycle.go` -- hooks reuse these, no duplication needed.
2. **ControllerRegistry** in `pkg/document/controller.go` provides the exact thread-safety and resolution pattern to follow.
3. **Import cycle avoidance** is the main architectural challenge: `pkg/hooks` needs `document.DocEvent` and `document.Document`, while `pkg/document/crud.go` needs to invoke hooks. Solution: define a `HookDispatcher` interface in the `document` package and have `hooks.DocEventDispatcher` implement it.
4. **Topological sort with priority** -- Kahn's algorithm with a min-heap as the zero-in-degree set gives deterministic priority-first ordering with O(V+E) complexity.
5. **Self-referential DocType bootstrap** -- the DocType MetaType describes itself. Solution: hard-code the DocType MetaType in Go (not loaded from JSON), seed it into the L1 cache before the registry starts, then load remaining core doctypes normally.

## Milestone Plan

### Task 1: Core HookRegistry with Priority Sorting and Dependency Resolution

- **Task ID:** MS-08-T1
- **Status:** Completed
- **Title:** Core HookRegistry with priority ordering and topological dependency sort
- **Description:**
  Implement the core `HookRegistry` in `pkg/hooks/registry.go` and the topological sort in `pkg/hooks/topo.go`.

  **registry.go:**
  - `HookRegistry` struct with `DocEvents map[string]map[DocEvent][]PrioritizedHandler`, `GlobalDocEvents map[DocEvent][]PrioritizedHandler`, and the other fields from MOCA_SYSTEM_DESIGN.md S3.5
  - `PrioritizedHandler` struct: `Handler DocEventHandler`, `Priority int` (default 500, lower = first), `AppName string`, `DependsOn []string`
  - `DocEventHandler` type: `func(ctx *document.DocContext, doc document.Document) error`
  - `Register(doctype string, event DocEvent, handler PrioritizedHandler)` -- thread-safe registration
  - `RegisterGlobal(event DocEvent, handler PrioritizedHandler)` -- for cross-cutting hooks
  - `Resolve(doctype string, event DocEvent) ([]PrioritizedHandler, error)` -- returns handlers sorted by priority + dependency order, merging per-doctype and global handlers
  - Thread-safe with `sync.RWMutex` (following ControllerRegistry pattern)

  **topo.go:**
  - Kahn's algorithm for topological sort with a min-heap (priority queue) as the zero-in-degree set
  - When multiple handlers have zero in-degree, the one with lowest `Priority` value goes first
  - Tie-breaking: alphabetical `AppName` for full determinism
  - `CircularDependencyError` type with the cycle path for clear error messages
  - `resolveOrder(handlers []PrioritizedHandler) ([]PrioritizedHandler, error)`

  **Tests** (`registry_test.go`, `topo_test.go`):
  - Priority ordering: p=100 before p=200
  - Dependency ordering: DependsOn=["crm"] runs after all "crm" app hooks
  - Circular dependency detection with clear error
  - Thread-safety under concurrent Register/Resolve
  - Empty registry returns empty slice
  - Global + per-doctype hooks merge correctly in Resolve

- **Why this task exists:** The HookRegistry is the core extension mechanism. Everything else (doc event dispatch, app loading, core app) depends on being able to register and resolve hooks.
- **Dependencies:** None (first task)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` S3.5 (lines 714-781) -- full struct and type definitions
  - `pkg/document/controller.go` -- thread-safe registry pattern
  - `pkg/document/lifecycle.go` -- DocEvent type and constants
- **Deliverable:**
  - `pkg/hooks/registry.go` -- HookRegistry with Register/RegisterGlobal/Resolve
  - `pkg/hooks/topo.go` -- topological sort with priority-aware min-heap
  - `pkg/hooks/registry_test.go` -- unit tests for all acceptance criteria
  - `pkg/hooks/topo_test.go` -- unit tests for sort algorithm edge cases
- **Risks / Unknowns:**
  - Import direction: `pkg/hooks` imports `pkg/document` for DocEvent/DocContext/Document types. This is fine as long as `pkg/document` does not import `pkg/hooks` -- verified by the interface approach in T2.
  - The full HookRegistry struct from S3.5 has many fields (APIMiddleware, CronJobs, etc.) -- for MS-08, only implement DocEvents, GlobalDocEvents, TypeOverrides, and TypeExtensions. Leave other fields as declared but unused (no registration/resolve methods yet). This keeps scope manageable.

---

### Task 2: DocEvent Dispatcher Integration with Document Lifecycle

- **Task ID:** MS-08-T2
- **Status:** Completed
- **Title:** Wire HookRegistry into document CRUD lifecycle via DocEventDispatcher
- **Description:**
  Create the bridge between HookRegistry and the document lifecycle engine.

  **pkg/hooks/docevents.go:**
  - `DocEventDispatcher` struct holding a `*HookRegistry`
  - `Dispatch(doctype string, event DocEvent, ctx *DocContext, doc Document) error` -- resolves hooks from registry and invokes them in order. Errors from before/on/after hooks are fatal (abort operation). Errors from OnChange hooks are logged (best-effort), consistent with existing controller behavior in crud.go.

  **pkg/document/hooks.go** (new file in document package):
  - `HookDispatcher` interface: `Dispatch(doctype string, event DocEvent, ctx *DocContext, doc Document) error`
  - This interface lives in the `document` package to avoid import cycles. `hooks.DocEventDispatcher` implements it.

  **Modify pkg/document/crud.go:**
  - Add `HookDispatcher` field to `DocManager`
  - At each lifecycle dispatch point in Insert/Update/Delete flows, after the controller method call, invoke `hookDispatcher.Dispatch(...)` if set
  - Hook dispatch is optional (nil check) so existing tests pass without hooks configured
  - Ordering: controller method fires first, then hooks. This follows the Frappe pattern where the controller is the "primary" behavior and hooks are "extensions."

  **Tests:**
  - Hook fires after controller method at each lifecycle point
  - Hook error aborts operation (for before_save etc.)
  - OnChange hook error is logged not propagated
  - Nil dispatcher is a no-op (backward compatibility)
  - Multiple hooks fire in resolved order

- **Why this task exists:** Without wiring hooks into the CRUD lifecycle, the HookRegistry is just a data structure with no runtime effect. This task makes hooks actually fire during document operations.
- **Dependencies:** MS-08-T1 (needs HookRegistry and Resolve)
- **Inputs / References:**
  - `pkg/document/crud.go` lines 604-696 (Insert), 743-832 (Update) -- integration points
  - `pkg/document/lifecycle.go` -- dispatchEvent pattern to follow
  - `MOCA_SYSTEM_DESIGN.md` S3.5 -- hook execution semantics
- **Deliverable:**
  - `pkg/hooks/docevents.go` -- DocEventDispatcher
  - `pkg/document/hooks.go` -- HookDispatcher interface
  - Modified `pkg/document/crud.go` -- hook dispatch calls at lifecycle points
  - `pkg/hooks/docevents_test.go` -- integration tests
- **Risks / Unknowns:**
  - Transaction boundary: hooks that fire inside a transaction (BeforeSave, AfterSave) must receive the same `pgx.Tx` via DocContext. This is already the case since DocContext carries TX.
  - Performance: hook resolution happens on every CRUD call. For hot paths, consider caching resolved handler lists. Can defer to optimization if benchmarks show it matters.

---

### Task 3: AppManifest Parser and App Directory Loader

- **Task ID:** MS-08-T3
- **Status:** Completed
- **Title:** AppManifest YAML parser/validator and app directory scanner
- **Description:**
  Implement the app packaging infrastructure.

  **pkg/apps/manifest.go:**
  - `AppManifest` struct: Name, Title, Version (semver), Publisher, License, Description, MocaVersion, Dependencies ([]AppDep), Modules ([]ModuleDef)
  - `AppDep` struct: Name, Version (semver constraint)
  - `ModuleDef` struct: Name, Label, DocTypes []string
  - `ParseManifest(path string) (*AppManifest, error)` -- reads YAML, validates required fields, validates semver format
  - `ValidateManifest(m *AppManifest) error` -- checks: Name non-empty and valid identifier, Version valid semver, MocaVersion valid semver constraint, no duplicate module names, no duplicate doctype names across modules
  - Follow `internal/config/parse.go` pattern for error handling with file path context

  **pkg/apps/loader.go:**
  - `AppLoader` struct with base path (typically project root `apps/` directory)
  - `ScanApps(appsDir string) ([]AppInfo, error)` -- walks `apps/*/manifest.yaml`, returns list of discovered apps
  - `AppInfo` struct: Name, Path, Manifest *AppManifest
  - `LoadApp(appDir string) (*AppInfo, error)` -- loads and validates a single app
  - `ValidateDependencies(apps []AppInfo) error` -- checks all inter-app dependencies are satisfiable (no missing deps, no version conflicts)
  - Dependency validation uses DAG -- detect cycles in app dependency graph

  **Tests:**
  - Valid manifest parses correctly
  - Missing required fields produce clear errors
  - Invalid semver is rejected
  - Directory scan finds all apps with manifest.yaml
  - Directory without manifest.yaml is skipped with warning
  - Circular app dependencies detected
  - Missing dependency produces clear error

- **Why this task exists:** The app system needs a way to define app metadata (manifest) and discover installed apps (loader). MS-09 builds the installation lifecycle on top of this foundation.
- **Dependencies:** None (can be developed in parallel with T1)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` S7.1 (lines 1265-1319) -- AppManifest struct definition
  - `MOCA_SYSTEM_DESIGN.md` S7.3 (lines 1337-1384) -- app directory structure
  - `internal/config/parse.go` -- YAML parsing pattern
  - `internal/config/types.go` -- AppConfig struct for reference
- **Deliverable:**
  - `pkg/apps/manifest.go` -- AppManifest types, ParseManifest, ValidateManifest
  - `pkg/apps/loader.go` -- AppLoader, ScanApps, LoadApp, ValidateDependencies
  - `pkg/apps/manifest_test.go` -- unit tests
  - `pkg/apps/loader_test.go` -- unit tests with temp directory fixtures
- **Risks / Unknowns:**
  - Semver validation: use a well-known Go semver library or stdlib. `golang.org/x/mod/semver` requires `v` prefix. Recommend `github.com/Masterminds/semver/v3` for flexible constraint matching (e.g., `>=0.1.0`).
  - Fields like Fixtures, Migrations, StaticAssets, PortalPages are in the design doc but OUT of scope for MS-08 (MS-09 handles installation). Define them in the struct for forward compatibility but don't validate their contents yet.

---

### Task 4: Core App -- Manifest, DocType Definitions, and Controllers

- **Task ID:** MS-08-T4
- **Status:** Completed
- **Title:** pkg/builtin/core with manifest.yaml, 5 DocType JSON definitions, User controller, and bootstrap sequence
- **Description:**
  Build the builtin `pkg/builtin/core` framework package -- the minimum set of system doctypes every Moca deployment needs.

  **pkg/builtin/core/manifest.yaml:**
  ```yaml
  name: core
  title: Moca Core
  version: 0.1.0
  publisher: Moca Framework
  license: MIT
  description: Core framework doctypes and system configuration
  moca_version: ">=0.1.0"
  modules:
    - name: Core
      label: Core
      doctypes:
        - User
        - Role
        - DocType
        - Module
        - SystemSettings
  ```

  **DocType JSON definitions** (in `pkg/builtin/core/modules/core/doctypes/<name>/`):

  1. **User** (`user.json`): Fields -- email (unique, required), full_name, password (Password type, hidden in API), enabled (Check, default 1), language, time_zone, user_type (Select: System/Website), roles (Table child linking to Role). `naming_rule: "field:email"`.

  2. **Role** (`role.json`): Fields -- role_name (unique, required), disabled (Check). Simple doctype. `naming_rule: "field:role_name"`.

  3. **DocType** (`doctype.json`): Self-referential -- this MetaType describes the structure of all MetaTypes. Fields -- name, module, fields (Table child), permissions (Table child), is_submittable, is_single, is_tree, track_changes, etc. This is the most complex definition. Bootstrap strategy: hard-code in Go as a fallback, load from JSON for validation.

  4. **Module** (`module.json`): Fields -- module_name (unique, required), label, app_name, icon, color. `naming_rule: "field:module_name"`.

  5. **SystemSettings** (`system_settings.json`): `is_single: true` (uses `tab_singles` table pattern -- one row per field, not one row per document). Fields -- site_name, default_language, time_zone, date_format, enable_password_policy, password_min_length, session_expiry.

  **pkg/builtin/core/modules/core/doctypes/user/user.go** -- User controller:
  - Embeds `document.BaseController`
  - `BeforeSave`: hash password with bcrypt if modified and not already hashed (check `$2a$`/`$2b$` prefix)
  - Uses `golang.org/x/crypto/bcrypt` (add to the root `go.mod`)

  **pkg/builtin/core/hooks.go:**
  - `init()` function that registers the User controller override with `document.ControllerRegistry`
  - Registers any core doc event hooks with `hooks.HookRegistry`

  **Bootstrap sequence** (`pkg/builtin/core/bootstrap.go`):
  - `BootstrapCoreMeta() []*meta.MetaType` -- returns hard-coded DocType MetaType and other core MetaTypes
  - Solves the self-referential problem: DocType MetaType is built in Go code, seeded into the L1 cache before normal registry loading
  - Other 4 doctypes are loaded from their JSON files using the standard MetaType compiler

- **Why this task exists:** The core app is the foundation every Moca site needs. Without User, Role, and DocType, the system cannot function. This task also solves the self-referential bootstrap problem.
- **Dependencies:** MS-08-T1 (hook registration), MS-08-T2 (dispatcher integration), MS-08-T3 (manifest validation)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` S7.3 (lines 1337-1384) -- app directory structure
  - `pkg/meta/metatype.go` -- MetaType struct definition
  - `pkg/meta/compiler.go` -- MetaType compilation from JSON
  - `pkg/meta/stubs.go` line 83 -- DocHookDefs placeholder
  - `pkg/document/controller.go` -- BaseController pattern
  - `ROADMAP.md` acceptance criteria -- User DDL correctness, SystemSettings tab_singles
- **Deliverable:**
  - `pkg/builtin/core/manifest.yaml`
  - `pkg/builtin/core/hooks.go` -- init() with controller and hook registration
  - `pkg/builtin/core/bootstrap.go` -- BootstrapCoreMeta()
  - `pkg/builtin/core/modules/core/doctypes/user/user.json` + `user.go`
  - `pkg/builtin/core/modules/core/doctypes/role/role.json`
  - `pkg/builtin/core/modules/core/doctypes/doctype/doctype.json`
  - `pkg/builtin/core/modules/core/doctypes/module/module.json`
  - `pkg/builtin/core/modules/core/doctypes/system_settings/system_settings.json`
  - Updated root `go.mod` with the `golang.org/x/crypto` dependency required by builtin core
- **Risks / Unknowns:**
  - **Self-referential DocType bootstrap** is the biggest risk. The hard-coded Go fallback must stay in sync with doctype.json. Mitigation: add a test that compiles doctype.json and compares it field-by-field with the Go-built MetaType.
  - **tab_singles pattern**: Need to verify that the MetaType compiler and DDL generator handle `is_single: true` correctly (generates `tab_singles` key-value table instead of a regular columnar table). If not implemented in MS-03/MS-04, this becomes a hidden dependency.
  - **bcrypt as placeholder**: The User controller hashes passwords but the full auth flow (login, session, JWT) is MS-14. The bcrypt hashing here is forward-compatible -- MS-14 will use the same hash format.

---

### Task 5: End-to-End Integration Tests

- **Task ID:** MS-08-T5
- **Status:** Completed
- **Title:** Integration tests verifying all acceptance criteria
- **Description:**
  Write integration tests that verify the full MS-08 deliverable against real infrastructure.

  **pkg/hooks/integration_test.go** (build tag: `integration`):
  - Register hooks with priorities 100 and 200, verify execution order
  - Register hook with DependsOn, verify it runs after dependency
  - Circular dependency produces `CircularDependencyError`
  - Hook fires during actual document Insert/Update via DocManager

  **pkg/builtin/core/integration_test.go** (build tag: `integration`):
  - `pkg/builtin/core/manifest.yaml` parses and validates successfully via AppManifest parser
  - All 5 core DocType JSONs compile via MetaType compiler
  - User MetaType generates correct DDL (columns, constraints, naming)
  - SystemSettings MetaType generates tab_singles DDL
  - User controller bcrypt-hashes password on BeforeSave
  - App directory scan discovers core app

  **Smoke test:**
  - Full lifecycle: load core app -> register hooks -> create User document -> verify password hashed -> query User back

- **Why this task exists:** Acceptance criteria must be verified end-to-end. Unit tests in T1-T4 cover individual components; this task verifies they work together correctly.
- **Dependencies:** MS-08-T1, MS-08-T2, MS-08-T3, MS-08-T4
- **Inputs / References:**
  - `ROADMAP.md` acceptance criteria (lines 537-543)
  - Existing integration test patterns in the codebase
- **Deliverable:**
  - `pkg/hooks/integration_test.go`
  - `pkg/builtin/core/integration_test.go`
  - All tests pass with `make test-integration`
- **Risks / Unknowns:**
  - Integration tests require Docker (PostgreSQL + Redis). CI must have Docker available.
  - Self-referential DocType bootstrap test may be fragile if MetaType compiler changes in future milestones.

## Recommended Execution Order

1. **MS-08-T1** (HookRegistry) and **MS-08-T3** (AppManifest/Loader) -- in parallel, no dependencies between them
2. **MS-08-T2** (DocEvent Dispatcher) -- depends on T1
3. **MS-08-T4** (Core App) -- depends on T1, T2, T3
4. **MS-08-T5** (Integration Tests) -- depends on all above

```
T1 (HookRegistry) ───────┐
                          ├──> T2 (Dispatcher) ──┐
T3 (AppManifest/Loader) ──┤                      ├──> T4 (Core App) ──> T5 (Integration Tests)
                          └──────────────────────-┘
```

## Open Questions

1. **tab_singles DDL**: Does the existing MetaType compiler + DDL generator handle `is_single: true`? If not, this is a hidden prerequisite for SystemSettings. Need to check `pkg/meta/compiler.go` and `pkg/orm/ddl.go`.
2. **Semver library choice**: Use `golang.org/x/mod/semver` (requires `v` prefix) or a third-party like `github.com/Masterminds/semver/v3` (more flexible)? Recommend Masterminds/semver for manifest version constraints.
3. **HookRegistry scope for MS-08**: The design doc lists many hook types (APIMiddleware, CronJobs, FormContextHooks, etc.). MS-08 should implement only DocEvents and GlobalDocEvents. Should TypeOverrides/TypeExtensions fields be included in HookRegistry or remain solely in ControllerRegistry?
4. **DocHookDefs stub**: Should `pkg/meta/stubs.go` DocHookDefs be fleshed out in MS-08, or deferred? It's currently empty. If hooks can be declared in MetaType JSON (not just Go code), this struct needs fields.

## Out of Scope for This Milestone

- **App installation lifecycle** (MS-09) -- install, uninstall, dependency resolution at install time
- **Migration runner** (MS-09) -- database migration execution
- **Fixtures** (MS-09) -- seed data loading
- **API middleware hooks** -- declared in HookRegistry struct but not wired
- **CronJob hooks** -- declared but not wired (needs queue infrastructure)
- **Form/List context hooks** -- declared but not wired (needs React frontend)
- **Full auth flow** -- login, session, JWT, OAuth2 (MS-14)
- **Permission enforcement** -- RLS, role-based access (MS-14)
- **App CLI commands** -- `moca app install/new/get` (MS-09, MS-13)
- **VirtualFields, APIMethods** -- hook types that depend on later milestones
