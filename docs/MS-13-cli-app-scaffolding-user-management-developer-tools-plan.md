# MS-13 — CLI App Scaffolding, User Management, and Developer Tools Plan

## Milestone Summary

- **ID:** MS-13
- **Name:** CLI App Scaffolding, User Management, and Developer Tools
- **Roadmap Reference:** ROADMAP.md → MS-13 section (lines 753-795)
- **Goal:** Implement `moca app new` (scaffold), `moca app get` (download from git), `moca user` commands, `moca dev execute/request`, and `moca build server/app`.
- **Why it matters:** Developers need to create apps, manage users, build binaries, and test APIs from the CLI. Without these, the framework requires manual setup of app directories, direct DB manipulation for users, and manual `go build` invocations.
- **Position in roadmap:** Order #8 of 30 milestones (parallel with MS-10, MS-11, MS-12, MS-14)
- **Upstream dependencies:** MS-09 (completed — site/app install/CLI), MS-08 (completed — hook registry & app system)
- **Downstream dependencies:** MS-15 (jobs/events/search), MS-17 (React Desk), MS-28 (app publish, dev console)

## Vision Alignment

MS-13 completes the developer CLI experience for app development. MS-09 delivered the ability to bootstrap projects, create sites, and install existing apps. MS-13 extends this with app creation (`moca app new`), external app acquisition (`moca app get`), dependency management (`moca app resolve`), and the build pipeline (`moca build server/app`). Together these form the full app development lifecycle.

User management commands bridge the gap between the existing User/Role DocTypes (defined in `pkg/builtin/core/`) and CLI operability. Currently, creating users requires direct document insertion. These commands expose the document CRUD system through ergonomic CLI commands.

The developer tools (`moca dev execute/request`) provide rapid feedback loops — executing framework functions and making authenticated API requests without writing test code or using external HTTP clients.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `ROADMAP.md` | MS-13 | 753-795 | Milestone definition |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.3 App Management | 869-1091 | app new/get/update/resolve/diff design |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.16 User Management | 2843-2974 | All user command specs |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §3.2 Lockfile | 315-345 | moca.lock YAML format |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.12 Build commands | 2267-2296 | build app/server specs |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.10 Dev tools | 2066-2098 | dev execute/request specs |
| `MOCA_SYSTEM_DESIGN.md` | §7.3 App Directory Structure | 1337-1382 | Canonical app layout |
| `MOCA_SYSTEM_DESIGN.md` | §7.1 AppManifest | 1261-1319 | Manifest struct definition |
| `docs/blocker-resolution-strategies.md` | Blocker 1 Phase 2 | 30-53 | ValidateAppDependencies for `moca app get` |
| `docs/moca-cross-doc-mismatch-report.md` | MISMATCH-013 | 466-499 | App directory structure reconciliation (resolved) |

## Research Notes

No web research was needed. All implementation patterns are well-documented in the design docs and validated by existing code:
- App manifest parsing/loading is mature in `pkg/apps/` (manifest.go, loader.go, installer.go)
- User DocType with bcrypt hashing exists in `pkg/builtin/core/user_controller.go`
- Go workspace composition was validated in `spikes/go-workspace/` (MS-00 Spike 3)
- Service wiring pattern is established in `cmd/moca/services.go`
- Placeholder command pattern is in `cmd/moca/placeholder.go`

**Key finding:** The `Services` struct in `cmd/moca/services.go` lacks `DocManager` from `pkg/document/`. User management commands need this — it must be wired into the service graph.

**Key finding:** `moca init` currently writes `moca.lock` as JSON. The design specifies YAML. The lockfile package should read both formats for backward compatibility but write YAML going forward.

## Milestone Plan

### Task 1

- **Task ID:** MS-13-T1
- **Title:** App Scaffolding Engine and `moca app new` Command
- **Status:** Completed
- **Description:**
  Create `internal/scaffold/` package with an app scaffolding engine and implement the `moca app new` command. The scaffold engine uses Go's `embed.FS` with `text/template` to generate all files for a new app. Three templates are supported: `standard` (full directory tree), `minimal` (manifest + hooks + module stub + go.mod), and `api-only` (like minimal but with an API controller stub).

  **New files to create:**
  - `internal/scaffold/scaffold.go` — `ScaffoldApp(opts ScaffoldOptions) error` with `ScaffoldOptions{AppName, Module, Title, Publisher, License, Template, DocType, ProjectRoot}`
  - `internal/scaffold/templates.go` — Embedded templates for: `manifest.yaml`, `hooks.go`, `go.mod`, `tests/setup_test.go`, `README.md`, `migrations/001_initial.sql`, starter DocType JSON (when `--doctype` is given)
  - `internal/scaffold/scaffold_test.go` — Unit tests verifying each template mode generates correct file tree

  **Files to modify:**
  - `cmd/moca/app.go` — Replace `newSubcommand("new", ...)` with full `newAppNewCmd()` implementing all flags from design (--module, --title, --publisher, --license, --doctype, --template)

  **Implementation details:**
  1. Validate app name (lowercase, hyphens, no special chars) — reuse pattern from `pkg/apps/manifest.go`
  2. Create `apps/{app_name}/` with full directory tree per `MOCA_SYSTEM_DESIGN.md` §7.3 (lines 1337-1382)
  3. Generate `go.mod` with module path derived from project or `--module` flag
  4. Append `./apps/{app_name}` to project root `go.work` using `golang.org/x/mod/modfile`
  5. Run `go mod tidy` in the new app directory
  6. If `--doctype` specified, generate starter MetaType JSON under `modules/{mod}/doctypes/{doctype}/`

  **Extract shared helper:** `func addToGoWork(projectRoot, appRelPath string) error` — used by both this task and Task 2.

- **Why this task exists:** App scaffolding is the primary developer workflow for creating new apps. Without it, developers must manually create 15+ files and directories to match the framework's expected layout.
- **Dependencies:** None
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.3 lines 871-909 (moca app new spec)
  - `MOCA_SYSTEM_DESIGN.md` §7.3 lines 1337-1382 (canonical app directory structure)
  - `pkg/builtin/core/` — builtin reference implementation of a framework-owned app package
  - `pkg/builtin/core/manifest.yaml` — manifest format reference
- **Deliverable:**
  - `internal/scaffold/scaffold.go`, `templates.go`, `scaffold_test.go`
  - Updated `cmd/moca/app.go` with working `moca app new` command
- **Acceptance Criteria:**
  - `moca app new my-app` creates valid scaffold that compiles cleanly (`go build ./apps/my-app/...`)
  - `moca app new my-app --template minimal` creates minimal scaffold
  - `moca app new my-app --doctype Task` creates app with starter DocType JSON
  - Generated `go.mod` has correct module path; `go.work` is updated
  - Generated `manifest.yaml` is valid and loadable by `apps.LoadApp()`
- **Risks / Unknowns:**
  - Template content must stay in sync with `AppManifest` struct changes. Mitigated by using `pkg/builtin/core/manifest.yaml` as the canonical builtin reference.

---

### Task 2

- **Task ID:** MS-13-T2
- **Title:** Lockfile Package, `moca app get`, and App Lifecycle Commands
- **Status:** Completed
- **Description:**
  Create `internal/lockfile/` package for reading/writing `moca.lock` and implement `moca app get`, `moca app update`, `moca app resolve`, and `moca app diff`.

  **New files to create:**
  - `internal/lockfile/lockfile.go` — `Lockfile` struct (GeneratedAt, MocaVersion, Apps map), `Read(path)`, `Write(path)`, `Resolve(appsDir, config)` functions. Read supports both JSON (backward compat) and YAML; Write outputs YAML per design spec.
  - `internal/lockfile/lockfile_test.go`
  - `pkg/apps/workspace.go` — `ValidateAppDependencies()` promoted from `spikes/go-workspace/` spike. Checks for major version conflicts before adding an app to `go.work`.

  **Files to modify:**
  - `cmd/moca/app.go` — Replace placeholders for `get`, `update`, `resolve`, `diff` with full implementations

  **`moca app get SOURCE` implementation:**
  1. Parse SOURCE: detect git URL (`github.com/...`), local path (`./...`), or registry name
  2. Git clone: `exec.Command("git", "clone", "--depth", depth, source, apps/{name})` with `--branch`/`--ref` support
  3. Validate manifest: `apps.LoadApp(targetDir)` — fail if no valid `manifest.yaml`
  4. Validate Go deps: `ValidateAppDependencies()` — warn/fail on major version conflicts
  5. Update `go.work` via shared helper from T1
  6. Regenerate `moca.lock` via `lockfile.Resolve()`
  7. Run `go mod download` in app directory

  **`moca app resolve`:** Scan all apps, validate inter-app dependencies, compute SHA256 checksums, write `moca.lock`.

  **`moca app update [APP]`:** For git-sourced apps, `git fetch` + check newer versions within semver constraints, show update table, confirm, checkout new version, re-resolve lockfile.

  **`moca app diff APP`:** Compare current app state against locked version — show MetaType JSON diffs, hook changes, pending migrations.

- **Why this task exists:** `moca app get` is how external/third-party apps enter a project. The lockfile ensures reproducible installs. update/resolve/diff complete the dependency management story.
- **Dependencies:** MS-13-T1 (shares `addToGoWork` helper; can proceed in parallel since the helper is small)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.3 lines 911-1077 (app get/update/resolve/diff specs)
  - `MOCA_CLI_SYSTEM_DESIGN.md` §3.2 lines 315-345 (lockfile format)
  - `docs/blocker-resolution-strategies.md` lines 30-53 (ValidateAppDependencies)
  - `spikes/go-workspace/` — validated dependency conflict detection
- **Deliverable:**
  - `internal/lockfile/lockfile.go`, `lockfile_test.go`
  - `pkg/apps/workspace.go` with `ValidateAppDependencies()`
  - Updated `cmd/moca/app.go` with working get/update/resolve/diff commands
- **Acceptance Criteria:**
  - `moca app get github.com/moca-apps/crm --version "~1.2.0"` clones repo, validates manifest, adds to `go.work`
  - `moca app get ./local-apps/my-app` works with local paths
  - `moca app resolve` writes valid `moca.lock` in YAML format
  - `moca app resolve --dry-run` shows resolution without writing
  - `moca app get` with incompatible major version conflict warns/fails
  - Existing JSON `moca.lock` files are readable (backward compat)
- **Risks / Unknowns:**
  - Git authentication for private repos (SSH keys, HTTPS tokens) — document supported auth methods, defer complex auth to user's git config
  - Semver resolution complexity — start with simple constraint matching, not a full SAT solver

---

### Task 3

- **Task ID:** MS-13-T3
- **Title:** User Management Commands (All 10 Subcommands)
- **Status:** Completed
- **Description:**
  Implement all 10 `moca user` subcommands using the document CRUD system. Requires wiring `DocManager` into the `Services` struct.

  **Files to modify:**
  - `cmd/moca/services.go` — Add `DocManager *document.DocManager` to `Services` struct; wire `NamingEngine`, `Validator`, and `ControllerRegistry` in `newServices()`
  - `cmd/moca/user.go` — Replace all 10 `newSubcommand()` calls with full command implementations

  **Command implementations (all follow the established pattern: requireProject → resolveSiteName → newServices → operate):**

  | Command | Action | DocType Operation |
  |---------|--------|-------------------|
  | `user add EMAIL` | Create user with optional roles | `Insert("User", {...})` + `Insert("HasRole", ...)` per role |
  | `user remove EMAIL` | Delete user (with confirmation) | `Delete("User", email)` |
  | `user set-password EMAIL` | Update password field | `Update("User", email, {"password": pw})` — bcrypt handled by UserController |
  | `user set-admin-password` | Set Administrator password | Same as set-password for "Administrator" |
  | `user add-role EMAIL ROLE` | Add role to user | Load user, add HasRole child, save |
  | `user remove-role EMAIL ROLE` | Remove role from user | Load user, filter HasRole children, save |
  | `user list` | List users with optional filters | `GetList("User", filters)` |
  | `user disable EMAIL` | Set enabled=0 | `Update("User", email, {"enabled": 0})` |
  | `user enable EMAIL` | Set enabled=1 | `Update("User", email, {"enabled": 1})` |
  | `user impersonate EMAIL` | Generate one-time login URL | Store token in Redis with TTL, print URL |

  **Flags per command:** Follow exact spec from `MOCA_CLI_SYSTEM_DESIGN.md` lines 2843-2974. Key flags: `--site`, `--password` (prompted if not given via `readPassword()`), `--roles` (comma-separated), `--role`/`--status` filters for list, `--force` for remove, `--json`/`--verbose` for list.

  **DocManager wiring:** The `Services` struct needs:
  ```go
  DocManager *document.DocManager  // new field
  ```
  Construction requires `NamingEngine` and `ControllerRegistry`. Register `core.NewUserController` for User DocType. The existing `core.BootstrapCoreMeta` function already provides MetaType definitions.

- **Why this task exists:** User management is a fundamental operational need. Without CLI commands, operators must write custom scripts or use direct SQL to manage users, which bypasses validation and lifecycle hooks.
- **Dependencies:** None (document CRUD system is complete from MS-04/MS-09)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.16 lines 2843-2974 (all user command specs)
  - `pkg/builtin/core/user_controller.go` — UserController with BeforeSave bcrypt hashing
  - `pkg/builtin/core/modules/core/doctypes/user/user.json` — User DocType definition
  - `pkg/document/crud.go` — DocManager Insert/Update/Delete/Get/GetList
  - `cmd/moca/services.go` — Service wiring pattern to extend
- **Deliverable:**
  - Updated `cmd/moca/services.go` with `DocManager` wiring
  - Updated `cmd/moca/user.go` with all 10 working commands
  - Tests for user command workflows
- **Acceptance Criteria:**
  - `moca user add admin@test.com --password secret --site acme.localhost` creates User document
  - `moca user add john@test.com --roles "Sales Manager,Sales User"` creates user with roles
  - `moca user set-password admin@test.com` prompts for password securely
  - `moca user list --site acme.localhost --role "Sales User"` filters correctly
  - `moca user disable admin@test.com` sets enabled=0; `enable` reverses it
  - `moca user impersonate admin@test.com --site acme.localhost` generates valid one-time URL
  - All commands support `--json` output mode
- **Risks / Unknowns:**
  - `DocManager` wiring complexity — may need to construct several dependencies (NamingEngine, Validator, ControllerRegistry). Mitigate by keeping CLI wiring minimal.
  - `impersonate` requires auth token generation — may need a simplified token mechanism if full JWT isn't available yet (MS-14 delivers JWT). Can use a Redis-stored random token with TTL as a stopgap.

---

### Task 4

- **Task ID:** MS-13-T4
- **Title:** Build Commands (`moca build server` and `moca build app`)
- **Status:** Completed
- **Description:**
  Implement the two build commands that wrap `go build` with Go workspace awareness.

  **Files to modify:**
  - `cmd/moca/build.go` — Replace placeholders for `app` and `server` with full implementations

  **`moca build app APP_NAME`:**
  1. Verify app exists in `apps/` via `apps.LoadApp(filepath.Join(projectRoot, "apps", appName))`
  2. Run `go build` from project root: `exec.Command("go", "build", flags..., "./apps/"+appName+"/...")`
  3. Set `GOWORK` env to ensure workspace mode
  4. If `--race`, add `-race` flag
  5. Stream stdout/stderr to the CLI output writer
  6. This is verification only — no binary produced

  **`moca build server`:**
  1. Determine output path: `--output` flag or default `bin/moca-server`
  2. Ensure output directory exists (`os.MkdirAll`)
  3. Run: `go build -o <output> ./cmd/moca-server/`
  4. Inject ldflags: `-X main.Version=<from moca.yaml> -X main.BuildTime=<now>`
  5. If `--race`, add `-race`; if `--ldflags`, append to default ldflags
  6. If `--verbose`, print full command before execution
  7. Report binary size and path on success

  Both commands use `exec.Command` similar to `initGit()` in `cmd/moca/init.go`.

- **Why this task exists:** Build commands are essential for the deployment pipeline. `moca build server` produces the binary that `moca deploy` ships. `moca build app` validates that new/modified app code compiles cleanly before committing.
- **Dependencies:** None (can run in parallel with all other tasks)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` lines 2267-2296 (build app/server specs)
  - `cmd/moca/init.go` — `initGit()` as reference for exec.Command pattern
  - `go.work` at project root — workspace composition
- **Deliverable:**
  - Updated `cmd/moca/build.go` with working `app` and `server` subcommands
  - Tests verifying flag parsing and command construction
- **Acceptance Criteria:**
  - `moca build server` produces working binary at `bin/moca-server`
  - `moca build server --output /tmp/test-server --race` respects flags
  - `moca build app crm` verifies an installable app compiles cleanly
  - `moca build app nonexistent` fails with clear error message
  - Build uses Go workspace mode (respects `go.work`)
- **Risks / Unknowns:**
  - Go toolchain must be available on PATH. Commands should check for `go` binary and provide clear error if missing.

---

### Task 5

- **Task ID:** MS-13-T5
- **Title:** Developer Tools (`moca dev execute` and `moca dev request`)
- **Status:** Completed
- **Description:**
  Implement the two in-scope dev commands. `moca dev request` is straightforward HTTP client work. `moca dev execute` is more complex due to Go's compiled nature.

  **Files to modify:**
  - `cmd/moca/dev.go` — Replace placeholders for `execute` and `request` with full implementations

  **`moca dev request METHOD URL`:**
  1. Resolve server URL from project config (default `http://localhost:{port}` where port comes from `moca.yaml`)
  2. Resolve `--user` flag (default: Administrator)
  3. Generate internal auth: construct a request header that the running `moca-server` will accept. Options: (a) use a shared secret/API key from config, or (b) generate a short-lived Redis-stored token similar to impersonate
  4. Build HTTP request: method, full URL (prefix server base if URL starts with `/`), optional `--data` JSON body, `--headers`
  5. Send via `net/http.Client`, capture response
  6. Display: status code, response body (pretty-print JSON), headers if `--verbose`

  **`moca dev execute EXPRESSION`:**

  Use the **code-gen + exec** approach (simpler and more reliable than Yaegi for initial implementation):
  1. Create temp directory inside project (e.g., `.moca/tmp/exec-{random}/`)
  2. Generate `main.go` that:
     - Imports framework packages (`document`, `meta`, `orm`, etc.)
     - Bootstraps service connections from `moca.yaml`
     - Executes the user's expression within a `func main()`
     - Prints the result to stdout
  3. Run `go run .moca/tmp/exec-{random}/main.go` within the workspace
  4. Capture and display output
  5. Clean up temp directory

  This approach works because `go run` respects `go.work`, so all framework and app packages are available. The limitation is that expressions must be valid Go statements (not arbitrary expressions), but this matches the design doc examples like `document.Count(ctx, "SalesOrder", nil)`.

- **Why this task exists:** Developer tools accelerate the development feedback loop. `moca dev request` replaces curl/Postman for API testing with built-in auth. `moca dev execute` provides a way to run framework functions directly from the terminal.
- **Dependencies:** `moca dev request` requires a running server (`moca serve`). `moca dev execute` has no runtime dependencies.
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` lines 2066-2098 (dev execute/request specs)
  - `cmd/moca/services.go` — service wiring for execute bootstrap
  - `internal/config/` — project config loading for server URL resolution
- **Deliverable:**
  - Updated `cmd/moca/dev.go` with working `execute` and `request` subcommands
  - Tests for request command flag parsing and URL construction
- **Acceptance Criteria:**
  - `moca dev request GET /api/v1/resource/User --site acme.localhost` returns response from running server
  - `moca dev request POST /api/v1/resource/User --data '{"email":"test@test.com"}' --user Administrator` sends authenticated POST
  - `moca dev request --verbose` shows full request/response headers
  - `moca dev execute 'fmt.Println("hello")'` prints "hello"
  - `moca dev execute` with invalid expression shows compilation error
  - `--json` output mode works for `moca dev request`
- **Risks / Unknowns:**
  - **Auth for `dev request`:** Full JWT auth is in MS-14. For now, use a simpler mechanism — e.g., a `X-Moca-Internal` header with a shared secret from config, or bypass auth in dev mode. Document the limitation.
  - **`dev execute` code-gen:** Template must handle imports correctly. Consider using `goimports` on the generated file, or require the user to write full import paths.

---

### Task 6

- **Task ID:** MS-13-T6
- **Title:** Builtin Core Folded into the Root Module
- **Status:** Completed
- **Description:**
  Replace the temporary submodule-tagging workaround from the earlier alpha line by folding builtin core into the root module. `pkg/builtin/core` is now framework-owned code inside `github.com/osama1998H/moca`, while only installable apps under `apps/` remain separate modules composed via `go.work`.

  **Files to create:**
  - `pkg/builtin/core/` — builtin core package containing runtime/bootstrap code, manifest, and embedded doctypes
  - `internal/releaseverify/main.go` — `go run` verifier that validates the single-module builtin-core invariants
  - `internal/releaseverify/main_test.go` — unit tests for legacy-module detection and builtin layout validation

  **Files to modify:**
  - `go.mod` — remove the legacy `github.com/osama1998H/moca/apps/core` require/replace entries
  - `go.work` — stop composing a separate builtin core module
  - `.github/workflows/release.yml` — verify the new invariant set and run an external `go mod tidy` smoke check against the root tag only

  **Implementation details:**
  1. Move bootstrap/runtime code, manifest, and embedded doctypes from `apps/core` to `pkg/builtin/core`.
  2. Delete the legacy nested module files and remove the `go.work` entry for `./apps/core`.
  3. Keep `moca app new` standalone behavior unchanged: pin the released root framework version, but do not add a direct builtin-core dependency unless generated code imports it.
  4. Verify an external temp module can `go mod tidy` and `go build` after importing `pkg/document`, `pkg/hooks`, and `pkg/builtin/core` from the released root module.

- **Why this task exists:** The multi-module workspace design from MS-00 only covered local composition. The short-term submodule-tagging workaround fixed alpha releases, but it kept builtin core on the wrong architectural boundary. Folding builtin core into the root module removes the release-engineering debt before beta.
- **Dependencies:** MS-13-T1 (completed), MS-00-T1 (completed), MS-00-T4 (completed)
- **Inputs / References:**
  - `docs/dx-test-session-report.md` — Issue #5 root cause and recommended fix
  - `docs/ADR-003-go-workspace-composition.md` — local replace + go.work policy
  - `.github/workflows/release.yml` — current root-tag-only release pipeline
- **Deliverable:**
  - Builtin core package lives under `pkg/builtin/core`
  - Verified release workflow that enforces the single-module builtin-core contract
- **Acceptance Criteria:**
  - Root `go.mod` does not require or replace `github.com/osama1998H/moca/apps/core`
  - `go.work` does not reference `./apps/core`
  - `pkg/builtin/core` contains the builtin core runtime/bootstrap package and embedded doctypes
  - Release workflow runs an external `go mod tidy` smoke test against the tagged release after importing `pkg/document`, `pkg/hooks`, and `pkg/builtin/core`
  - Existing scaffold tests continue to pass without adding a direct builtin-core dependency to generated apps

## Recommended Execution Order

1. **MS-13-T1** (App Scaffold) — Foundation for T2; establishes `addToGoWork` helper and `internal/scaffold/` package
2. **MS-13-T4** (Build Commands) — Independent, small scope, quick win
3. **MS-13-T3** (User Management) — Independent, high-value, exercises document CRUD from CLI
4. **MS-13-T2** (Lockfile + App Get/Update/Resolve/Diff) — Largest task, depends on T1's go.work helper
5. **MS-13-T5** (Dev Tools) — Most experimental, lowest priority within milestone

Tasks T1, T3, and T4 can proceed in parallel. T2 should start after T1's shared helper is ready. T5 can start anytime.

## Open Questions

1. **`moca dev execute` approach:** Should we invest in Yaegi integration now, or is the code-gen approach sufficient for the initial release? (Recommendation: code-gen first, Yaegi upgrade later)
2. **`moca dev request` auth:** Without MS-14's JWT system, how should we authenticate requests? Options: (a) shared internal secret, (b) Redis-stored token, (c) dev-mode auth bypass. (Recommendation: dev-mode bypass with `X-Moca-Dev-User` header, guarded by a config flag)
3. **`moca user impersonate` token:** Same auth question — should this generate a JWT (MS-14) or a simpler Redis-stored token? (Recommendation: Redis token with 5min TTL, upgrade to JWT when MS-14 lands)

## Out of Scope for This Milestone

- `moca app publish` — deferred to MS-28
- `moca dev console` (REPL) — deferred to MS-28
- `moca build desk/portal/assets` — frontend build commands, deferred to MS-17+
- OAuth2/SAML/OIDC user commands — deferred to MS-22
- API key management — deferred to MS-18
- Full JWT auth system — MS-14 (but MS-13 may use simplified token mechanism)
