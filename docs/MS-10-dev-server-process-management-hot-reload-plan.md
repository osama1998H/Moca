# MS-10 — Dev Server, Process Management, and Hot Reload Plan

## Milestone Summary

- **ID:** MS-10
- **Name:** Dev Server, Process Management, and Hot Reload
- **Roadmap Reference:** `ROADMAP.md` lines 613–653
- **Goal:** Implement `moca serve` (single-process dev server), MetaType filesystem hot reload, PID management, and `moca stop/restart` — completing the developer experience loop: init → create site → serve → edit → see changes.
- **Why it matters:** After MS-09 gave developers CLI commands to bootstrap projects and create sites, MS-10 makes the framework *runnable*. This is the developer experience unlock — the first time someone can edit a MetaType JSON file and see the API update automatically.
- **Position in roadmap:** Bridges CLI tooling (MS-07–MS-09) with operational commands (MS-11+). Last milestone before MS-11 (operational CLI) and MS-12 (multitenancy).
- **Upstream dependencies:** MS-06 (REST API), MS-08 (Hook Registry & App System)
- **Downstream dependencies:** MS-11 (Operational CLI), MS-12 (Multitenancy — builds on request handling)

---

## Vision Alignment

Moca's promise is that a single MetaType definition drives everything: schema, API, permissions, search, UI. MS-10 makes this tangible in the development workflow. A developer edits a DocType JSON file, saves it, and within 2 seconds the API reflects the change — no restart, no manual migration. This is the "fast edit-save-refresh loop" described in MOCA_SYSTEM_DESIGN.md §3.1.3.

The architectural decision (ADR-CLI-006) to run everything in a single Go process for development — unlike Frappe/Bench which uses honcho to manage 4+ processes — means sub-second startup and no process coordination issues. The `internal/process/supervisor.go` goroutine supervisor is the foundation for this, managing HTTP server, workers, scheduler, file watcher, and outbox poller as named subsystems with graceful shutdown orchestration.

The existing `cmd/moca-server/main.go` already has a working HTTP server with the full middleware chain. MS-10 extracts this into a shared `internal/serve/` package so both the standalone binary and `moca serve` can reuse it, then layers on the supervisor, watcher, and PID management.

---

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `MOCA_SYSTEM_DESIGN.md` | §3.1.3 Metadata Lifecycle & Hot Reload | 271–296 | Hot reload pipeline: validate → diff → migrate → cache flush → event → route update |
| `MOCA_SYSTEM_DESIGN.md` | §12.1 Single-Instance Development | 1773–1791 | Dev architecture: single binary with HTTP + workers + scheduler + outbox |
| `MOCA_SYSTEM_DESIGN.md` | §12.3 Process Types | 1829–1837 | Process roles: moca-server, moca-worker, moca-scheduler, moca-outbox |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.4 Server/Process Management | 1094–1217 | `moca serve` flags, single-process diagram, `moca stop/restart` |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §3 Project Structure | 155–200 | `.moca/process.pid` location |
| `MOCA_CLI_SYSTEM_DESIGN.md` | ADR-CLI-006 | 3597–3601 | In-process dev server over multi-process Procfile |
| `ROADMAP.md` | MS-10 | 613–653 | Scope, deliverables, acceptance criteria, risks |
| `cmd/moca-server/main.go` | Full file | 1–167 | Existing HTTP server to extract into shared package |
| `pkg/meta/registry.go` | Register, Invalidate | — | Hot reload target: `Register()` does full diff-migrate-persist |
| `pkg/api/gateway.go` | Gateway, Handler, Mux | 1–159 | Middleware chain and route registration for static files |

---

## Research Notes

No web research needed. Key findings from codebase exploration:

1. **`cmd/moca-server/main.go` is a complete server** — loads config, creates DBManager/Redis/Registry/DocManager/Gateway, registers REST routes + health checks, handles graceful shutdown. This is the extraction source for `internal/serve/`.
2. **`pkg/meta/registry.go:Register()`** already handles the full hot reload pipeline internally (compile → diff → DDL → persist → cache). The watcher only needs to call it per changed file.
3. **Worker/scheduler/outbox binaries** (`cmd/moca-worker/`, `cmd/moca-scheduler/`, `cmd/moca-outbox/`) are stubs that only load config. For MS-10, these subsystems are simple context-blocking stubs in the supervisor.
4. **`fsnotify/fsnotify`** is the standard Go library for filesystem watching (used by cobra, viper, Hugo). Well-maintained with macOS kqueue support.
5. **PID file location** is `.moca/process.pid` per CLI design doc §3 (line 190). The `.moca/` directory already exists from `moca init`.

---

## Milestone Plan

### Task 1

- **Task ID:** MS-10-T1
- **Title:** Goroutine Supervisor with Graceful Shutdown and PID File Utilities
- **Status:** Completed
- **Description:**
  Create `internal/process/` package with two components:

  **Supervisor (`supervisor.go`):**
  - `Subsystem` struct: `Name string`, `Run func(ctx context.Context) error`, `Critical bool`
  - `Supervisor` struct with `Add(Subsystem)` and `Run(ctx context.Context) error`
  - `Run` starts all subsystems as goroutines with a shared derived context
  - When ctx is cancelled (signal), waits for all subsystems to return (with configurable timeout, default 30s)
  - If a `Critical` subsystem fails, cancels the shared context (cascading shutdown)
  - Non-critical failures are logged but don't cascade
  - Returns the first critical error or nil

  **PID File (`pid.go`):**
  - `WritePID(dir string) error` — writes `os.Getpid()` to `{dir}/.moca/process.pid`, creates dirs
  - `ReadPID(dir string) (int, error)` — reads and parses PID from file
  - `RemovePID(dir string) error` — removes PID file
  - `IsRunning(pid int) bool` — sends `syscall.Kill(pid, 0)` to check process existence

- **Why this task exists:** Every other task depends on the supervisor to manage subsystem lifecycles. PID files are needed for `moca stop/restart`. This is the foundation layer.
- **Dependencies:** None
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.4 lines 1146–1165 (stop/restart behavior)
  - `MOCA_CLI_SYSTEM_DESIGN.md` §3 line 190 (`.moca/process.pid` location)
  - `MOCA_SYSTEM_DESIGN.md` §12.1 lines 1779–1790 (single-process dev architecture)
  - `cmd/moca-server/main.go` lines 44–45 (existing signal handling pattern to generalize)
- **Deliverable:**
  - `internal/process/supervisor.go` + `internal/process/supervisor_test.go`
  - `internal/process/pid.go` + `internal/process/pid_test.go`
- **Acceptance Criteria:**
  - Supervisor starts N subsystems concurrently and blocks until all exit
  - Context cancellation triggers orderly shutdown of all subsystems
  - Critical subsystem failure cascades shutdown to all others
  - Non-critical failure is logged without cascading
  - PID write/read/remove cycle works correctly
  - `IsRunning` correctly detects live vs dead processes
- **Risks / Unknowns:**
  - Shutdown ordering between subsystems (HTTP should stop accepting before workers drain). For MS-10, use a simple "cancel all, wait" approach. Ordered shutdown (HTTP first, then workers) can be added in MS-21 when production process separation ships.

---

### Task 2

- **Task ID:** MS-10-T2
- **Title:** Extract HTTP Server into Shared Package, Add Static Files and WebSocket Stub
- **Status:** Completed
- **Description:**
  Extract the HTTP server setup from `cmd/moca-server/main.go` into `internal/serve/` so both the standalone binary and `moca serve` can reuse it.

  **Server extraction (`internal/serve/server.go`):**
  - `ServerConfig` struct: `Config *config.ProjectConfig`, `Logger *slog.Logger`, `Port int`, `Host string`, `Version string`, `StaticDir string`
  - `Server` struct owning: `httpServer`, `gateway`, `registry`, `dbManager`, `redisClients`, `docManager`
  - `NewServer(ctx, cfg ServerConfig) (*Server, error)` — does everything in current `cmd/moca-server/main.go` run() up to but not including signal handling: create DBManager, Redis, Registry, DocManager, HookRegistry, Gateway, REST routes, health checks, static files
  - `Run(ctx context.Context) error` — starts HTTP listener, blocks until ctx cancelled, graceful shutdown. Matches `Subsystem.Run` signature
  - `Registry() *meta.Registry` — accessor for watcher
  - `DBManager() *orm.DBManager` — accessor for watcher site pool
  - `Close()` — close DB/Redis connections

  **Refactor `cmd/moca-server/main.go`:**
  - Replace inline server construction with `serve.NewServer()` + `srv.Run(ctx)` + `defer srv.Close()`

  **Static file serving (`internal/serve/static.go`):**
  - If `StaticDir` is set and exists, register `http.FileServer` on `/desk/` via `gw.Mux()`

  **WebSocket stub (`internal/serve/websocket.go`):**
  - Register handler on `/ws` returning HTTP 501 JSON

  **Dev mode stubs (`internal/serve/stubs.go`):**
  - `WorkerStub(ctx) error`, `SchedulerStub(ctx) error`, `OutboxStub(ctx) error` — log once, block on `<-ctx.Done()`

- **Why this task exists:** The HTTP server logic currently lives in `cmd/moca-server/main.go` and cannot be reused by `moca serve`. Extracting it eliminates duplication and ensures both paths use identical server configuration.
- **Dependencies:** None (parallel with T1)
- **Inputs / References:**
  - `cmd/moca-server/main.go` lines 1–167 (extraction source)
  - `pkg/api/gateway.go` lines 49–70 (middleware chain to preserve)
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.4 lines 1121–1141 (single-process diagram)
  - `cmd/moca/services.go` (existing service construction patterns)
- **Deliverable:**
  - `internal/serve/server.go`, `internal/serve/static.go`, `internal/serve/websocket.go`, `internal/serve/stubs.go`
  - `internal/serve/server_test.go`
  - Modified `cmd/moca-server/main.go`
- **Acceptance Criteria:**
  - `cmd/moca-server/main.go` still works identically after refactoring
  - `serve.NewServer()` creates a functional HTTP server with full middleware chain
  - `srv.Run(ctx)` blocks and responds to context cancellation with graceful shutdown
  - Static files served from `desk/dist/` when directory exists
  - `/ws` returns 501 JSON response
  - Stub subsystems block on context and log startup message
- **Risks / Unknowns:**
  - Extraction must preserve the exact middleware chain and all Gateway accessors. The refactoring should be mechanical, not structural.

---

### Task 3

- **Task ID:** MS-10-T3
- **Title:** fsnotify Watcher with Debounce and Hot Reload via Registry.Register
- **Status:** Completed
- **Description:**
  Implement filesystem watching of MetaType JSON files with 500ms debounce that triggers the hot reload pipeline.

  **Watcher (`pkg/meta/watcher.go`):**
  - `WatcherConfig` struct: `AppsDir string`, `Debounce time.Duration` (default 500ms)
  - `Watcher` struct: holds `Registry`, `Migrator`, `*slog.Logger`, debounce timers, fsnotify instance
  - `NewWatcher(registry, migrator, logger, cfg) *Watcher`
  - `Run(ctx context.Context) error` — matches `Subsystem.Run` signature:
    1. Discover all `*/modules/*/doctypes/` directories under AppsDir
    2. Add fsnotify watches on each directory
    3. Event loop: receive events, filter to `*.json` files, debounce, trigger reload
    4. Block until ctx cancelled, then close fsnotify watcher

  **Debounce logic:**
  - `map[string]*time.Timer` keyed by file path
  - On Create/Write/Rename event for `*.json`: reset or create 500ms timer
  - When timer fires, call `reload(path)`
  - Ignore Remove events, dotfiles, non-JSON files

  **Reload function:**
  1. Read JSON file from disk
  2. Compile via `meta.Compile(jsonBytes)` — if fails, log error and return (don't crash)
  3. Query sites from system DB (`SELECT name FROM sites`)
  4. For each site, call `registry.Register(ctx, site, jsonBytes)` — full diff-migrate-persist pipeline
  5. Log success with timing

  **New dependency:** `github.com/fsnotify/fsnotify` added to `go.mod`

- **Why this task exists:** Hot reload is the core developer experience feature of MS-10. Without it, developers must restart the server after every MetaType change.
- **Dependencies:** None (parallel with T1 and T2)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §3.1.3 lines 288–296 (hot reload pipeline)
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.4 lines 1115–1119 (file watching behavior)
  - `pkg/meta/registry.go` — `Register()` method
  - `pkg/meta/compiler.go` — `Compile()` for validation
  - `ROADMAP.md` MS-10 risks line 637 (fsnotify macOS + vim)
- **Deliverable:**
  - `pkg/meta/watcher.go` + `pkg/meta/watcher_test.go`
- **Acceptance Criteria:**
  - Watcher detects `*.json` file changes in `apps/*/modules/*/doctypes/` directories
  - Changes are debounced at 500ms (multiple rapid saves trigger only one reload)
  - Invalid JSON is logged as error but does not crash the server
  - Hot reload completes within 2 seconds of file save
  - `Run(ctx)` blocks until context cancelled, then cleans up fsnotify resources
  - Vim-style saves (write to temp → rename) are handled correctly via debounce
- **Risks / Unknowns:**
  - **Site resolution:** Query all sites from DB, reload all. If no sites exist, log warning and skip.
  - **Concurrent reloads:** Registry uses internal locking, so concurrent Register calls should be safe.

---

### Task 4

- **Task ID:** MS-10-T4
- **Title:** Implement moca serve/stop/restart Commands and End-to-End Integration Tests
- **Status:** Completed
- **Description:**
  Wire the CLI commands that compose all previous tasks, then validate the full workflow with integration tests.

  **`moca serve` (`cmd/moca/serve.go`):**
  - Flags: `--port` (8000), `--host` (0.0.0.0), `--workers` (2), `--no-workers`, `--no-scheduler`, `--no-watch`, `--profile`
  - Workflow:
    1. Require project context
    2. Check PID file — error if server already running, clean up stale PID
    3. Create `serve.Server` from project config
    4. Create `process.Supervisor`, add subsystems: HTTP (critical), worker stub, scheduler stub, outbox stub, watcher (unless `--no-watch`)
    5. Write PID file, defer removal
    6. Set up signal handler (SIGINT/SIGTERM → cancel context)
    7. Print startup banner with URL, enabled subsystems, PID
    8. Call `supervisor.Run(ctx)` — blocks until shutdown
    9. Close server resources

  **`moca stop` (`cmd/moca/stop.go`):**
  - Flags: `--graceful` (true), `--timeout` (30s), `--force`
  - Workflow: read PID → check running → send SIGTERM (or SIGKILL if --force) → poll IsRunning with timeout → remove PID file

  **`moca restart` (`cmd/moca/restart.go`):**
  - Workflow: run stop logic → run serve logic inline

  **Integration Tests (`cmd/moca/serve_integration_test.go`):**
  Build tag: `//go:build integration`
  1. Server lifecycle: start supervisor with HTTP, verify health 200, cancel, verify PID cleanup
  2. Hot reload: start with watcher, write MetaType JSON, wait 2s, verify registry has new type
  3. Invalid JSON: write bad JSON, verify error logged, server still healthy
  4. `--no-watch`: write JSON, verify NOT auto-loaded
  5. Static files: serve `desk/dist/index.html`, verify HTTP response
  6. Stop via PID: start in goroutine, SIGTERM, verify clean exit

- **Why this task exists:** The CLI commands are the user-facing surface. Integration tests prove the "edit MetaType → see API change" acceptance criterion.
- **Dependencies:** MS-10-T1 (Supervisor, PID), MS-10-T2 (Server, stubs), MS-10-T3 (Watcher)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.4 lines 1096–1165 (serve/stop/restart flags)
  - `cmd/moca/serve.go`, `stop.go`, `restart.go` — current stubs to replace
  - `cmd/moca/services.go` — helpers (`requireProject`, `resolveSiteName`)
  - `ROADMAP.md` MS-10 acceptance criteria lines 629–635
- **Deliverable:**
  - `cmd/moca/serve.go` — full implementation
  - `cmd/moca/stop.go` — full implementation
  - `cmd/moca/restart.go` — full implementation
  - `cmd/moca/serve_integration_test.go` — integration tests
- **Acceptance Criteria:**
  - `moca serve` starts HTTP server, prints URL, serves API endpoints
  - `moca serve` writes PID file, cleaned up on shutdown
  - `moca serve --no-watch` starts without file watcher
  - `moca stop` sends SIGTERM, waits for graceful shutdown, removes PID
  - `moca stop --force` sends SIGKILL
  - `moca restart` stops then starts
  - Modifying `*.json` MetaType triggers hot reload within 2 seconds
  - Invalid JSON logged but server continues running
- **Risks / Unknowns:**
  - Integration test timing: use generous 2s timeout for hot reload assertions.
  - Port conflicts in CI: use random ports (`:0`) in tests.

---

## Recommended Execution Order

1. **MS-10-T1** — Supervisor + PID (foundation, no dependencies)
2. **MS-10-T2** — Server extraction + stubs (parallel with T1)
3. **MS-10-T3** — File watcher + hot reload (parallel with T1/T2)
4. **MS-10-T4** — CLI commands + integration tests (consumes T1–T3)

T1, T2, and T3 are independent and can be developed in parallel. T4 depends on all three.

---

## Open Questions

1. **Site resolution during hot reload:** When the watcher detects a change, which site(s) should it reload? Recommendation: query `SELECT name FROM sites` and reload all. If no sites exist, log warning and skip.

2. **React dev server integration:** The design spec shows `moca serve` launching a separate React dev server. For MS-10, defer this — the React frontend doesn't exist yet (MS-17+). Default `--no-desk` to true.

3. **Worker/scheduler stub behavior:** Recommendation: log once at startup ("worker: stub (not yet implemented)") then stay silent to avoid cluttering dev output.

---

## Out of Scope for This Milestone

- Production process separation (separate moca-server, moca-worker binaries) — MS-21
- systemd unit generation — MS-21
- Real background worker implementation (Redis Streams consumer) — MS-15
- Real scheduler implementation (cron trigger) — MS-15
- WebSocket real-time updates — MS-19
- React dev server / frontend hot reload — MS-17+
- `moca dev watch` for frontend assets — MS-17+
- Rolling restart / zero-downtime restart — MS-21
- Site clone/reinstall/enable/disable/rename/browse — MS-11
- Backup before operations — MS-11
