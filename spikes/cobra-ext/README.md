# Spike 5: Cobra CLI Extension Discovery

[![CI](https://github.com/osama1998H/Moca/actions/workflows/ci.yml/badge.svg?branch=main&event=push)](https://github.com/osama1998H/Moca/actions/workflows/ci.yml)

**Status:** Complete
**Task:** MS-00-T4
**Design Reference:** `MOCA_CLI_SYSTEM_DESIGN.md` §8 (lines 3363-3406)

## Objective

Validate that MOCA apps can register Cobra CLI commands via `init()` at build time,
across Go workspace module boundaries, so the commands appear in the root command tree.
Also test the explicit `NewCommand()` constructor pattern as an alternative.

## Key Questions to Answer

1. Does a blank import (`import _ "app/cmd/hooks"`) reliably trigger `init()` across module boundaries?
2. If two apps register a command with the same name, does the second silently overwrite the first?
3. Is the `init()` pattern or the explicit constructor pattern safer for MOCA's multi-app model?
4. Does command namespace prefixing (`app:command`) prevent name collisions?

## Expected Deliverables

- Framework root command definition (`framework/cmd/root.go`)
- `apps/stub-a/hooks.go` — registers `stub-a:hello` via `init()`
- `apps/stub-b/hooks.go` — registers `stub-b:greet` via `init()`
- Main binary that blank-imports both apps
- Collision test (two apps attempt to register the same command name)
- `ADR-005-cobra-cli-extension.md` — init() vs constructor recommendation

## Run

```bash
# From repo root:
make spike-cobra

# Or directly:
cd spikes/cobra-ext && go test -v -count=1 ./...

# Build and test the binary interactively:
cd spikes/cobra-ext && go build -o moca-spike . && ./moca-spike --help
```
