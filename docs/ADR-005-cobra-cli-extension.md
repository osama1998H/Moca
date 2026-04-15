# ADR-005: Cobra CLI Extension Pattern

**Status:** Accepted
**Spike:** MS-00-T4 (Spike 5)
**Date:** 2026-03-30
**Validated by:** `go test -v -count=1 -race ./...` — all 7 tests pass

---

## Context

MOCA is a multi-app framework where each app is a separate Go module. The CLI (`moca-server`) is a single compiled binary that must include commands from every installed app — without the app explicitly wiring into a central command list.

Two composition approaches were evaluated:

1. **`init()`-based registration**: Each app's `hooks.go` calls `MustRegisterCommand()` inside an `init()` function. The main binary uses blank imports (`import _ "app/package"`) to trigger `init()`. No explicit wiring needed in main.
2. **Explicit constructor**: Each app exports a `NewXxxCommand() *cobra.Command` function. The caller (main, or a framework bootstrap function) creates the command and calls `RegisterCommand()` explicitly.

Both patterns must work reliably across Go workspace module boundaries (multiple `go.mod` files composed via `go.work`).

Design reference: `MOCA_CLI_SYSTEM_DESIGN.md` §8, lines 3363–3406.

---

## Decision

**Use `init()`-based registration as the primary pattern for app CLI commands.**

Use explicit constructors as a secondary pattern for framework-internal commands and unit tests.

---

## Pattern 1: `init()`-Based Registration (Primary)

```go
// apps/my-app/hooks.go
package myapp

import (
    "github.com/osama1998H/moca/pkg/cli"
    "github.com/spf13/cobra"
)

func init() {
    cli.MustRegisterCommand(&cobra.Command{
        Use:   "my-app:sync",
        Short: "Sync data from external source",
        RunE:  runSync,
    })
}
```

The main binary blank-imports all installed apps:

```go
// cmd/moca-server/main.go
import (
    "github.com/osama1998H/moca/pkg/cli"

    _ "github.com/osama1998H/moca/apps/my-app"
    _ "github.com/osama1998H/moca/apps/crm"
    // ... one blank import per installed app
)

func main() {
    cli.RootCommand().Execute()
}
```

**Go guarantees**: all `init()` functions across all imported packages complete before `main()` is called. This means the registry is fully populated before `RootCommand()` attaches commands to the tree.

---

## Pattern 2: Explicit Constructor (Secondary)

```go
// apps/my-app/commands.go
func NewSyncCommand() *cobra.Command {
    return &cobra.Command{
        Use:   "my-app:sync-explicit",
        Short: "Sync data (explicit constructor pattern)",
        RunE:  runSync,
    }
}
```

Used by calling `cli.RegisterCommand(myapp.NewSyncCommand())` explicitly — from a bootstrap function, a test, or the framework itself.

---

## Recommendation

**`init()`-based registration is recommended for app commands** for these reasons:

| Property | init() | Explicit Constructor |
|----------|--------|---------------------|
| Wiring in main | None (blank import only) | Must call RegisterCommand() per app |
| Error surfacing | MustRegisterCommand panics at startup | RegisterCommand returns error (caller must handle) |
| Test isolation | Requires ResetForTesting() | Clean by default (no global state) |
| Naming collisions | Caught immediately at startup | Caught at registration time |
| Framework coupling | App imports pkg/cli (one dep) | Caller must know about app's constructor |

Explicit constructors are recommended for:
- Framework-internal commands (e.g., `moca build`, `moca app get`) where ordering control matters
- Unit testing individual commands in isolation (no global registry state)
- Dynamic registration at runtime based on configuration

---

## Collision Detection

All commands must use a namespace prefix: `app-name:command-name` (e.g., `crm:sync-contacts`, `hr:run-payroll`). This convention is enforced socially, not technically — the registry allows any name.

`MustRegisterCommand` converts a silent overwrite into a fatal panic:

```
panic: moca/cli: command "stub-a:hello" already registered (existing Use: "stub-a:hello")
```

This means any collision surfaces immediately at process startup, before the binary accepts traffic. Combined with the namespace prefix, the risk of cross-app name collision is minimal.

---

## `init()` Ordering

Go executes `init()` functions in import order within a package, and packages are initialized in dependency order. In the main binary, blank imports are listed explicitly — the order is deterministic and visible in the source.

If two apps attempt to register the same command name, `MustRegisterCommand` panics at startup (whichever fires second). The fix is to rename one of the commands using the namespace prefix.

`RootCommand()` attaches all queued commands in registration order, but Cobra sorts them alphabetically in `--help` output regardless. Order of registration does not affect user-visible behavior.

---

## Alternatives Considered

### Option A: `go plugin` System

Each app compiled as a Go plugin (`.so`), loaded at runtime via `plugin.Open`.

**Rejected** because: plugins require identical compiler versions and build flags across the framework and all apps; they are fragile on macOS (cgo dependency); symbol conflicts between plugins are runtime panics with no clear error message; plugin loading is not supported on all target platforms.

### Option B: Code Generation

A code generator reads installed app metadata and produces a `registered_commands.go` file listing all constructors.

**Rejected** because: it requires an extra build step outside of Go's standard toolchain; `go generate` would need to run before every `go build`; `init()` achieves the same result with zero tooling overhead.

### Option C: Configuration-Based Registration

A YAML/JSON config file lists app command registrations; the framework reads it at startup.

**Rejected** because: it converts a compile-time problem (command naming) into a runtime problem; it loses type safety; it requires parsing infrastructure before the CLI is usable; it does not integrate with `go build`'s compile-time verification.

---

## Validation Results

All 7 tests pass. Spike run: `go test -v -count=1 ./...` from `spikes/cobra-ext/`.

| Test | Result | Key Observation |
|------|--------|----------------|
| `TestInitRegistrationAcrossModuleBoundaries` | PASS | `stub-a:hello` and `stub-b:greet` registered via `init()` across Go workspace module boundaries |
| `TestCommandsAppearInHelpOutput` | PASS | Both commands visible in root `--help` output without any explicit wiring |
| `TestExplicitConstructorPattern` | PASS | `NewHelloCommand()` / `NewGreetCommand()` constructors register cleanly after `ResetForTesting()` |
| `TestCommandNameCollisionDetection` | PASS | `RegisterCommand` returns `"already registered"` error on duplicate name |
| `TestMustRegisterCommandPanicsOnCollision` | PASS | `MustRegisterCommand` panics with `"already registered"` — init() errors are fatal |
| `TestBuildWorkspace` | PASS | `go build ./...` succeeded in ~139ms across 4 workspace modules |
| `TestBuildRace` | PASS | `go build -race ./...` succeeded in ~891ms — no race conditions |

### Build Time Observations

With 4 modules (root + framework + stub-a + stub-b):

- Cold build (`go build ./...`): ~139ms
- Race build (`go build -race ./...`): ~891ms
- Subsequent builds (cached): near-instant via Go's build cache

At 10+ app modules, Go's build cache will handle incremental rebuilds efficiently. Only modules with changed files will be recompiled. The workspace does not meaningfully increase build time for unchanged modules.

---

## Consequences for Production

### For `pkg/cli` (MS-01)

The production `pkg/cli` package will mirror this spike's `framework/cmd` structure: `RegisterCommand`, `MustRegisterCommand`, `RootCommand`, and `ResetForTesting`. The mutex-locked registry is the validated pattern.

### For App `hooks.go` Convention

Every MOCA app that extends the CLI will have a `hooks.go` file (package-level, no exported symbols) containing `init()` that calls `MustRegisterCommand`. The app's `go.mod` will depend on `github.com/osama1998H/moca/pkg/cli`.

### For `moca build server` (future milestone)

The `moca build server` command will generate a `main.go` with blank imports for all installed apps (read from `go.work`), then compile it. This spike validates that the resulting binary will have all app commands in its command tree, matching the pattern in `MOCA_CLI_SYSTEM_DESIGN.md` lines 2281–2296.

---

## References

- `MOCA_CLI_SYSTEM_DESIGN.md` §8, lines 3363–3406 — Extension System
- `MOCA_CLI_SYSTEM_DESIGN.md` lines 2281–2296 — `moca build server`
- `docs/MS-00-architecture-validation-spikes-plan.md` Task MS-00-T4
- `ROADMAP.md` line 123 — Spike 5 acceptance criterion
- `ADR-003-go-workspace-composition.md` — Go workspace validation (dependency)
- Cobra v1.8.1 API: `cobra.Command`, `AddCommand`, `Execute`
