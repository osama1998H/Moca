# MS-07 — CLI Foundation Plan

## Milestone Summary
- **ID:** MS-07
- **Name:** CLI Foundation — Context Resolver, Output Layer, Cobra Scaffold
- **Roadmap Reference:** `ROADMAP.md` lines 466–510
- **Goal:** Build CLI infrastructure: project/site/env detection, output formatting (TTY/JSON/Table/Progress), rich errors, and full Cobra command tree scaffold with all 24 command groups as placeholders.
- **Why it matters:** The CLI scaffold must exist before any real commands can be implemented (MS-09). This is the developer-facing entry point to the entire framework.
- **Position in roadmap:** Order #3 — runs in parallel with MS-02 (PostgreSQL & Redis Foundation). Both depend only on MS-01.
- **Upstream dependencies:** MS-01 (Project Structure & Config) — complete
- **Downstream dependencies:** MS-09 (CLI Site/App Commands), MS-11, MS-13, MS-16

## Vision Alignment

Moca is a metadata-driven, multitenant framework. The CLI (`moca` binary) is the primary developer interface — every project begins with `moca init` and every workflow (sites, apps, migrations, deployments) flows through it. MS-07 establishes the foundation all 152+ CLI commands will build on: context resolution (which project/site/env am I targeting?), output formatting (human-friendly TTY vs machine-readable JSON), and the extensible Cobra command tree that apps can hook into.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `ROADMAP.md` | MS-07 definition | 466–510 | Scope, deliverables, acceptance criteria |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §2.3 CLI Internal Architecture | 92–150 | 4-layer architecture diagram (Command, Context, Service, Driver + Output) |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.1 Command Tree Overview | 349–559 | All 24 command groups with subcommands |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §6 Context Detection & Resolution | 3268–3293 | 6-level priority pipeline for context resolution |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §7 Error Handling | 3297–3359 | Rich error format (Error/Context/Cause/Fix/Reference) with examples |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §8 Extension System | 3363–3406 | App command registration via `init()` + `cli.RegisterCommand()` |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §9 CLI Internal Package Layout | 3410–3516 | Directory structure for `cmd/moca/`, `internal/context/`, `internal/output/` |
| `docs/ADR-005-cobra-cli-extension.md` | Full ADR | — | Validated Cobra registration pattern |
| `spikes/cobra-ext/framework/cmd/root.go` | Registry implementation | 1–108 | Production-ready pattern to promote to `pkg/cli/` |
| `internal/config/load.go` | `LoadAndResolve()` | — | Config loading function the context resolver will call |
| `internal/config/types.go` | `ProjectConfig` struct | — | Type that `CLIContext` will carry |

## Research Notes

No web research was needed. All implementation patterns are well-defined in the design documents and validated by the Cobra spike (ADR-005). Key findings from codebase exploration:

- **Cobra is NOT in `go.mod` yet** — must be added as a dependency
- **`internal/config/`** is fully implemented with `LoadAndResolve()`, validation, env expansion, and inheritance merge — ready for the context resolver to use
- **Spike code** (`spikes/cobra-ext/framework/cmd/root.go`) provides a production-ready registry pattern with thread-safe registration, collision detection, and `ResetForTesting()` — ready to promote to `pkg/cli/`
- **`cmd/moca/main.go`** is a basic stub printing version info and loading config — needs complete rewrite as Cobra application
- **TTY detection:** `golang.org/x/term` is the recommended stdlib-adjacent package (avoids CGO issues of `mattn/go-isatty`)
- **Progress bars:** Defer `charmbracelet/bubbletea` to later milestones; a simple spinner/progress bar using `\r` is sufficient for MS-07's skeleton scope

---

## Milestone Plan

### Task 1: Cobra Root Command, `pkg/cli` Registry, `moca version`, `moca completion`

- **Task ID:** MS-07-T1
- **Status:** Completed
- **Title:** Cobra Root Command + Command Registry + Version & Completion Commands
- **Description:**
  Add `github.com/spf13/cobra` to `go.mod`. Promote the validated spike registry from `spikes/cobra-ext/framework/cmd/root.go` into `pkg/cli/registry.go`. Rewrite `cmd/moca/main.go` as a Cobra application with persistent global flags (`--site`, `--env`, `--project`, `--json`, `--table`, `--no-color`, `--verbose`). Implement `moca version` (CLI version, Go version, OS/Arch from ldflags) and `moca completion bash|zsh|fish|powershell` (using Cobra's built-in generation).
- **Why this task exists:** Every other task depends on the Cobra root command and registry. Version and completion are zero-dependency commands that validate the scaffold works end-to-end.
- **Dependencies:** None (first task)
- **Inputs / References:**
  - `spikes/cobra-ext/framework/cmd/root.go` (lines 1–108) — registry pattern to promote
  - `spikes/cobra-ext/framework/cmd/main_test.go` — tests to port
  - `cmd/moca/main.go` (current stub) — ldflags pattern to preserve
  - `Makefile` line 8 — ldflags injection for Version/Commit/BuildDate
  - `MOCA_CLI_SYSTEM_DESIGN.md` §8 (lines 3363–3406) — extension system design
- **Deliverable:**
  - `pkg/cli/registry.go` — `RegisterCommand()`, `MustRegisterCommand()`, `RootCommand()`, `ResetForTesting()`, collision detection
  - `pkg/cli/registry_test.go` — ported spike tests
  - `cmd/moca/main.go` — Cobra-based entry point with `SilenceErrors`, `SilenceUsage`, `PersistentPreRunE` placeholder
  - `cmd/moca/version.go` — `moca version` command
  - `cmd/moca/completion.go` — `moca completion bash|zsh|fish|powershell`
  - Updated `go.mod` with Cobra dependency
- **Risks / Unknowns:**
  - Spike uses package path `spikes/cobra-ext/framework/cmd`; production uses `pkg/cli` — mechanical rename but verify `go.work` resolves correctly after adding Cobra
  - `PersistentPreRunE` must be a no-op placeholder here; Task 2 wires context resolution into it. Ensure the hook signature accepts the `CLIContext` wiring without requiring a rewrite

---

### Task 2: Context Resolver (Project, Site, Environment)

- **Task ID:** MS-07-T2
- **Status:** Completed
- **Title:** Context Detection & Resolution Pipeline
- **Description:**
  Implement `internal/context/` with three resolvers following the 6-level priority pipeline: (1) CLI flags, (2) env vars, (3) local state files, (4) project config, (5) directory-tree auto-detection, (6) defaults. The `CLIContext` struct carries `*config.ProjectConfig`, resolved site name, environment, and project root path. Wire the resolver into the root command's `PersistentPreRunE` using Go's `context.WithValue()` on `cmd.SetContext()` — no global state.
- **Why this task exists:** Context detection is the core UX innovation of the CLI — it eliminates repetitive flags. Every command downstream (MS-09+) depends on resolved context.
- **Dependencies:** MS-07-T1 (needs root command with persistent flags)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §6 (lines 3268–3293) — priority pipeline
  - `internal/config/load.go` — `LoadAndResolve()` function to call
  - `internal/config/types.go` — `ProjectConfig` type for `CLIContext`
- **Deliverable:**
  - `internal/context/context.go` — `CLIContext` struct + `Resolve(cmd *cobra.Command) (*CLIContext, error)` + `FromCommand(cmd *cobra.Command) *CLIContext` helper
  - `internal/context/project.go` — walk up directory tree to find `moca.yaml`, load via `config.LoadAndResolve()`
  - `internal/context/site.go` — resolve from `--site` flag > `MOCA_SITE` env > `.moca/current_site` file
  - `internal/context/environment.go` — resolve from `--env` flag > `MOCA_ENV` env > `.moca/environment` file > `"development"` default
  - `internal/context/context_test.go` — tests using `t.TempDir()` for mock project directories
  - Updated `cmd/moca/main.go` `PersistentPreRunE` to call `context.Resolve()`
- **Risks / Unknowns:**
  - Project detection must be optional: `moca version` and `moca completion` must work outside a project directory. `PersistentPreRunE` should set `CLIContext.Project = nil` when no `moca.yaml` is found, not error. Commands requiring project context check `ctx.Project != nil` themselves.
  - `config.LoadAndResolve()` takes a file path string. Verify it works correctly when cwd is a subdirectory of the project (the project detector finds the directory, then passes `<dir>/moca.yaml`).

---

### Task 3: Output Layer (TTY, JSON, Table, Rich Errors)

- **Task ID:** MS-07-T3
- **Status:** Completed
- **Title:** Output Formatting Layer + Rich Error System
- **Description:**
  Implement `internal/output/` with: (a) a `Writer` that reads `--json`, `--table`, `--no-color` flags and dispatches to the correct formatter, (b) TTY-aware color output respecting `NO_COLOR` env var, (c) JSON output mode via `encoding/json`, (d) table output via stdlib `text/tabwriter`, (e) simple spinner/progress using `\r` line clearing, and (f) rich `CLIError` type implementing the `Error/Context/Cause/Fix/Reference` format from the design doc. The `CLIError` implements Go's `error` interface so `RunE` can return it; `main.go` checks for `*CLIError` in a custom error handler.
- **Why this task exists:** Every command needs consistent output formatting. The rich error system is a core UX requirement — errors must be actionable, not cryptic.
- **Dependencies:** MS-07-T1 (needs root command flags `--json`, `--table`, `--no-color`)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §7 (lines 3297–3359) — error format with two detailed examples
  - `MOCA_CLI_SYSTEM_DESIGN.md` §2.3 (lines 142–149) — output layer architecture (TTY/JSON/Table/Progress)
  - `ROADMAP.md` line 477–478 — output formatter deliverables
- **Deliverable:**
  - `internal/output/output.go` — `Writer` struct with `NewWriter(cmd *cobra.Command)`, mode detection, `Print()`, `PrintJSON()`, `PrintTable()` methods
  - `internal/output/color.go` — TTY detection via `golang.org/x/term`, `NO_COLOR` support, color helpers (success/warning/error/info/muted)
  - `internal/output/json.go` — JSON marshaling wrapper
  - `internal/output/table.go` — table formatting using `text/tabwriter`
  - `internal/output/progress.go` — simple `Spinner` (Start/Stop) and `ProgressBar` (Update/Finish) using `\r`
  - `internal/output/error.go` — `CLIError` struct with `Error`, `Context`, `Cause`, `Fix`, `Reference` fields + `Format(w io.Writer)` with color
  - `internal/output/output_test.go`, `error_test.go` — unit tests
  - Updated `go.mod` with `golang.org/x/term`
  - Updated `cmd/moca/main.go` with custom error handler for `CLIError`
- **Risks / Unknowns:**
  - Progress bar and spinner are skeleton implementations without real consumers until MS-09+. Keep the API minimal (`Start`/`Update`/`Finish`) to allow refactoring when real usage patterns emerge.
  - `text/tabwriter` may be too basic for some table layouts later. Starting simple; can upgrade to a richer library if needed.

---

### Task 4: Register All 24 Command Groups + `moca doctor` Skeleton

- **Task ID:** MS-07-T4
- **Status:** Completed
- **Title:** Command Group Scaffolding + Doctor Command
- **Description:**
  Create placeholder command files for all 24 command groups plus top-level commands (`init`, `status`). Each group registers a parent command with subcommand stubs matching the design doc's command tree (§4.1). Each placeholder `RunE` returns a `CLIError` with `Error: "not implemented"` and `Fix: "This command will be available in a future release."`. Framework-internal commands use the explicit constructor pattern (`NewXxxCommand()`) called from a wiring file, reserving the `init()` pattern for app-contributed commands per ADR-005. Implement `moca doctor` skeleton with a `DoctorCheck` interface and 3–4 skeleton checks (project detected, config valid, PG reachable placeholder, Redis reachable placeholder) that print a status table.
- **Why this task exists:** `moca help` must show all 24 command groups to validate the scaffold is complete. The doctor skeleton establishes the health-check framework that later milestones will populate with real checks.
- **Dependencies:** MS-07-T1 (registry), MS-07-T3 (CLIError type, table output for doctor)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.1 (lines 349–559) — complete command tree with all subcommands
  - `MOCA_CLI_SYSTEM_DESIGN.md` §9 (lines 3410–3474) — directory layout for command files
  - `spikes/cobra-ext/apps/stub-a/hooks.go` — `init()` registration pattern (for app commands)
  - `ROADMAP.md` line 482 — `moca doctor` skeleton deliverable
- **Deliverable:**
  - `cmd/moca/commands.go` — wiring file that calls `NewXxxCommand()` for all 24 groups and registers them
  - One file per command group under `cmd/moca/`: `site.go`, `app.go`, `db.go`, `backup.go`, `config_cmd.go`, `deploy.go`, `generate.go`, `dev.go`, `test_cmd.go`, `build.go`, `worker.go`, `scheduler.go`, `api.go`, `user.go`, `search.go`, `cache.go`, `queue.go`, `events.go`, `translate.go`, `log.go`, `monitor.go`, `serve.go`, `stop.go`, `restart.go`
  - `cmd/moca/init.go` — `moca init` placeholder
  - `cmd/moca/status.go` — `moca status` placeholder
  - `cmd/moca/doctor.go` — `DoctorCheck` interface, skeleton checks, table-formatted output
  - Tests verifying `moca help` output contains all 24 groups
- **Risks / Unknowns:**
  - 24+ files is significant boilerplate — establish a template with the first 2–3 commands, then replicate consistently. Each file is small (~30–50 lines).
  - Package naming: `config_cmd.go` avoids collision with `internal/config` package name. Similarly `test_cmd.go` avoids Go's `test` reserved package name.
  - Some groups have nested subcommands (e.g., `api keys create`, `queue dead-letter list`). For MS-07, register only the parent and immediate children — deeper nesting can be added when the real commands are implemented.

---

## Recommended Execution Order

```
1. MS-07-T1  Cobra Root + pkg/cli Registry + version + completion
      |
      +----> 2a. MS-07-T2  Context Resolver        (parallel)
      |
      +----> 2b. MS-07-T3  Output Layer             (parallel)
                  |
                  +----> 3. MS-07-T4  24 Command Groups + Doctor
```

1. **MS-07-T1** first — everything depends on the Cobra root and registry
2. **MS-07-T2** and **MS-07-T3** in parallel — independent of each other, both depend only on T1
3. **MS-07-T4** last — needs both the registry (T1) and the CLIError/table output (T3)

## Verification Plan

1. `make build-moca` — binary compiles successfully
2. `bin/moca --help` — shows all 24 command groups + version/completion/doctor/init/status
3. `bin/moca version` — prints version, commit, build date, Go version, OS/Arch
4. `bin/moca completion bash` — outputs valid bash completion script
5. `bin/moca doctor` — runs skeleton checks and prints status table
6. Run from a directory with `moca.yaml` — context resolver detects project
7. Run from outside a project — `moca version` works, site-requiring commands show appropriate message
8. `bin/moca --site acme.localhost site info` — resolves site from flag (returns "not implemented")
9. `MOCA_SITE=acme.localhost bin/moca site info` — resolves from env
10. `bin/moca site list --json` — outputs JSON format
11. `bin/moca site list --table` — outputs table format
12. `bin/moca --no-color site info` — no ANSI codes in output
13. `make test` — all new and existing tests pass with race detector

## Open Questions

- **Flat vs nested command files:** The design doc shows subdirectories (`cmd/moca/site/create.go`), but for MS-07 placeholders, flat files (`cmd/moca/site.go` containing all site subcommand stubs) are simpler. Recommend flat for now, split into subdirectories when real implementations land in MS-09. Should we start with subdirectories to match the design doc layout?

## Out of Scope for This Milestone

- Real command implementations (deferred to MS-09+)
- Driver connections (PostgreSQL, Redis) — doctor skeleton uses placeholder checks only
- Interactive TUI (`charmbracelet/bubbletea`) — simple spinner/progress is sufficient
- Go REPL for `moca dev console` (deferred to MS-28)
- App command discovery/loading (deferred to MS-08/MS-09)
- `moca.lock` management (`internal/lockfile/`)
- Process management (`internal/process/`)
- Code generation templates (`internal/scaffold/`)
