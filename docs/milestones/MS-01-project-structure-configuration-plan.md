# MS-01 — Project Structure, Configuration, and Go Module Layout Plan

## Milestone Summary
- **ID:** MS-01
- **Name:** Project Structure, Configuration, and Go Module Layout
- **Roadmap Reference:** `ROADMAP.md` lines 220-254
- **Goal:** Establish the canonical directory structure, `moca.yaml` parsing, configuration resolution, and the 5 `cmd/` entry points as empty stubs.
- **Why it matters:** Every file written from this point needs a home. The configuration system is read by nearly every other package. Without MS-01, no subsequent milestone can build production code.
- **Position in roadmap:** Order #2 on the critical path (MS-00 → **MS-01** → MS-02 → MS-03 → ...)
- **Upstream dependencies:** MS-00 (Architecture Validation Spikes) — completed
- **Downstream dependencies:** MS-02 (PostgreSQL & Redis Foundation), and transitively all milestones beyond

## Vision Alignment

MOCA is a metadata-driven, multitenant framework where a single `MetaType` definition drives everything. MS-01 lays the physical and logical foundation:

1. **Directory structure** — The 15 `pkg/` packages mirror the framework's modular architecture (meta, document, api, orm, auth, hooks, workflow, tenancy, queue, events, search, storage, ui, notify, observe). Establishing these early ensures every future milestone has a clear home.
2. **Configuration system** — `moca.yaml` is the single source of truth for a MOCA project. The parser, validator, env expansion, and inheritance/merge logic are used by the CLI, server, worker, scheduler, and outbox. Getting this right early prevents config-related bugs across the entire stack.
3. **5 cmd/ entry points** — MOCA ships 5 binaries. Stubbing them now establishes the build pipeline, CI validation, and the pattern for future implementation.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `MOCA_CLI_SYSTEM_DESIGN.md` | §3 Project Structure | 155-200 | Canonical directory tree created by `moca init` |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §3.1 Project Manifest (moca.yaml) | 202-313 | Full YAML schema with all top-level blocks |
| `MOCA_SYSTEM_DESIGN.md` | §15 Framework Package Layout | 1924-2057 | 15 pkg/ packages, 5 cmd/ binaries, desk/, apps/ |
| `ROADMAP.md` | MS-01 | 220-254 | Milestone scope, deliverables, acceptance criteria |
| `ROADMAP.md` | MS-02 | 257-295 | Downstream dependency — needs config system |
| `docs/milestones/MS-00-architecture-validation-spikes-plan.md` | Full | — | Context on completed MS-00 scaffolding |

## Research Notes

### Go YAML parsing
- `gopkg.in/yaml.v3` is the standard Go YAML library. It supports struct tags, custom unmarshalers, and `yaml.Node` for line/column error reporting.
- No alternative library is needed. `yaml.v3` is mature and widely used.

### Validation approach
- `go-playground/validator` was considered but rejected. It produces errors like `Key: 'ProjectConfig.Infrastructure.Database.Host' Error:Field validation for 'Host' failed on the 'required' tag` — not user-friendly.
- A custom validator using `ValidationError{Field, Message}` with dot-path field names (e.g., `infrastructure.database.host: required`) gives full control over error formatting and matches the acceptance criteria.

### Env var expansion
- Expanding `${VAR}` patterns in raw YAML bytes **before** parsing avoids custom unmarshalers on every field.
- Pattern: `\$\{([A-Za-z_][A-Za-z0-9_]*)\}` via `regexp.ReplaceAllStringFunc`.
- Missing env vars produce clear errors rather than silent empty strings.

### Config inheritance
- `staging.inherits: production` requires merging production defaults with staging overrides.
- Using pointer fields in `StagingConfig` (e.g., `Port *int`) to distinguish "not set" from zero value. Non-nil pointer = override; nil pointer = inherit from parent.
- Bespoke merge function (no third-party dependency like `mergo`) since the struct shape is known at compile time.

### go.work version mismatch
- Current `go.work` says `go 1.26.1` but `go.mod` says `go 1.26`. This should be reconciled during Task 1. The go.work directive should match the actual Go toolchain version in use.

**No web research was needed.** All implementation details were derivable from the design documents and standard Go ecosystem knowledge.

## Milestone Plan

### Task 1: Directory Scaffolding and cmd/ Stubs

- **Task ID:** MS-01-T1
- **Status:** Completed
- **Title:** Directory Scaffolding and cmd/ Stubs
- **Description:**
  Create the full `pkg/` directory tree with `doc.go` placeholder files for all 15 packages defined in §15. Create 4 new `cmd/` entry points (`moca-worker`, `moca-scheduler`, `moca`, `moca-outbox`) following the existing `moca-server` stub pattern (version print + exit). Update the `Makefile` to build all 5 binaries. Verify `go build ./cmd/...` produces 5 binaries.
- **Why this task exists:**
  Every subsequent milestone places code into `pkg/` subdirectories. The 5 binaries are the framework's primary artifacts. Without this scaffolding, nothing has a home. This is also the first acceptance criterion: `go build ./cmd/...` produces 5 binaries.
- **Dependencies:** None (first task)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §15 lines 1924-2057 (package layout)
  - `cmd/moca-server/main.go` (existing stub pattern)
  - `Makefile` (existing build target)
- **Deliverable:**
  - 15 `pkg/*/doc.go` files (meta, document, api, orm, auth, hooks, workflow, tenancy, queue, events, search, storage, ui, notify, observe)
  - 4 new `cmd/*/main.go` files
  - Updated `Makefile` with multi-binary build target
  - `go build ./cmd/...` succeeds and produces 5 binaries in `bin/`
- **Risks / Unknowns:**
  - Minimal risk. Pure file creation.
  - The `go.work` / `go.mod` Go version mismatch (1.26.1 vs 1.26) should be reconciled here.

---

### Task 2: moca.yaml Typed Structs and YAML Parser

- **Task ID:** MS-01-T2
- **Status:** Completed
- **Title:** moca.yaml Typed Structs and YAML Parser
- **Description:**
  Implement the full `ProjectConfig` struct hierarchy in `internal/config/types.go` mapping 1:1 to the moca.yaml schema from §3.1. Implement YAML parsing in `internal/config/parse.go` using `gopkg.in/yaml.v3`. Implement environment variable expansion in `internal/config/envexpand.go` — a pre-processing step that replaces `${VAR_NAME}` patterns in raw YAML bytes before decoding. Implement user-friendly error wrapping in `internal/config/errors.go`.
- **Why this task exists:**
  `ProjectConfig` is the typed representation of `moca.yaml` — the single source of truth for every MOCA project. Every binary, every CLI command, and every runtime component reads this config. The parser + env expansion are the foundation that validation and merging build upon.
- **Dependencies:** MS-01-T1 (needs `internal/config/` directory)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §3.1 lines 202-313 (full YAML schema)
  - `gopkg.in/yaml.v3` documentation
- **Deliverable:**
  - `internal/config/types.go` — `ProjectConfig` and all sub-structs (`ProjectInfo`, `AppConfig`, `InfrastructureConfig`, `DatabaseConfig`, `RedisConfig`, `KafkaConfig`, `SearchConfig`, `StorageConfig`, `DevelopmentConfig`, `ProductionConfig`, `StagingConfig`, `SchedulerConfig`, `BackupConfig`, `RetentionConfig`, `BackupDestination`, `TLSConfig`, `ProxyConfig`)
  - `internal/config/parse.go` — `ParseFile(path) (*ProjectConfig, error)` and `Parse(io.Reader) (*ProjectConfig, error)`
  - `internal/config/envexpand.go` — `ExpandEnvVars([]byte) ([]byte, error)`
  - `internal/config/errors.go` — `ConfigError` type with file/line context
  - `go.mod` updated with `gopkg.in/yaml.v3`
- **Risks / Unknowns:**
  - `StagingConfig` uses pointer fields to distinguish "set" vs "unset" — adds verbosity but is necessary for correct inheritance in T3.
  - The `KafkaConfig.Enabled` field should be `*bool` (not `bool`) to distinguish explicit `false` from absent.
  - Schema may evolve in future milestones. Design structs for extensibility (consider an `Extras map[string]interface{}` escape hatch, but defer to when actually needed).

---

### Task 3: Validation and Config Inheritance/Merging

- **Task ID:** MS-01-T3
- **Status:** Completed
- **Title:** Validation and Config Inheritance/Merging
- **Description:**
  Implement config validation in `internal/config/validate.go` that walks the `ProjectConfig` struct and accumulates `[]ValidationError` with dot-path field names. Implement config merging in `internal/config/merge.go` for two scenarios: (1) `staging.inherits: production` overlay and (2) three-layer cascade (moca.yaml → common_site_config.yaml → site_config.yaml).
- **Why this task exists:**
  Validation with clear field-path errors is an explicit acceptance criterion. The inheritance mechanism (`staging.inherits: production`) and the three-layer config cascade are core to how MOCA resolves configuration across environments and sites. Without merging, the config system is incomplete.
- **Dependencies:** MS-01-T2 (needs typed structs and parser)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §3.1 lines 286-291 (staging inherits production)
  - `MOCA_CLI_SYSTEM_DESIGN.md` §3 lines 155-200 (common_site_config.yaml, site_config.yaml)
  - `ROADMAP.md` MS-01 acceptance criteria lines 233-239
- **Deliverable:**
  - `internal/config/validate.go` — `Validate(*ProjectConfig) []ValidationError`
  - `internal/config/merge.go` — `ResolveInheritance(*ProjectConfig)` and `MergeLayers(project, commonSite, site) *ProjectConfig`
  - Validation rules:
    - `project.name`: required, non-empty
    - `project.version`: required, valid semver
    - `moca`: required, valid semver constraint
    - `apps.core`: required (core must be present)
    - `infrastructure.database.host`: required
    - `infrastructure.database.port`: required, range 1-65535
    - `infrastructure.redis.host`: required
    - `infrastructure.redis.port`: required, range 1-65535
    - Port fields: valid range if set
    - `scheduler.tick_interval`: valid Go duration if set
  - Merge: staging pointer fields overlay production base; three-layer cascade with higher-priority layers overriding lower
- **Risks / Unknowns:**
  - Nested struct merging (e.g., `TLSConfig` inside `ProductionConfig`) requires field-by-field handling. Tedious but correct.
  - The three-layer cascade applies to infrastructure and runtime config — need to clarify which fields are per-site overridable vs project-global. For MS-01, assume all fields are overridable; restrict in future milestones if needed.
  - Semver constraint parsing (for the `moca:` field) may need a lightweight parser. `golang.org/x/mod/semver` handles versions but not constraint ranges. Consider a simple regex or defer full constraint evaluation to CLI app resolution (MS-09). For MS-01, validate it's a non-empty string that looks like a semver constraint.

---

### Task 4: cmd/ Wiring, Integration, and Test Suite

- **Task ID:** MS-01-T4
- **Status:** Completed
- **Title:** cmd/ Wiring, Integration, and Test Suite
- **Description:**
  Wire all 5 `cmd/*/main.go` binaries to load `moca.yaml` from the current directory, validate it, resolve inheritance, and print the resolved config. Build a comprehensive test suite covering parsing, env expansion, validation, and merging. Create test fixture YAML files in `internal/config/testdata/`.
- **Why this task exists:**
  This task proves all acceptance criteria end-to-end: valid YAML parses correctly, missing fields produce clear errors, env vars expand, staging inherits production, and all 5 binaries can load config. The test suite ensures the config system remains correct as it evolves in future milestones.
- **Dependencies:** MS-01-T2, MS-01-T3
- **Inputs / References:**
  - `ROADMAP.md` MS-01 acceptance criteria lines 233-239
  - `cmd/moca-server/main.go` (existing pattern)
- **Deliverable:**
  - 5 updated `cmd/*/main.go` files that load and print resolved config
  - `internal/config/parse_test.go` — tests for valid/invalid YAML, env expansion, malformed input
  - `internal/config/validate_test.go` — tests for each validation rule, multiple-error accumulation
  - `internal/config/merge_test.go` — tests for staging inheritance, three-layer cascade, empty layers
  - `internal/config/testdata/` with 6+ fixture files:
    - `valid_full.yaml` — complete moca.yaml with all fields
    - `valid_minimal.yaml` — only required fields
    - `missing_fields.yaml` — several required fields absent
    - `malformed.yaml` — invalid YAML syntax
    - `with_env_vars.yaml` — `${...}` patterns for expansion
    - `with_staging.yaml` — staging inherits production
  - `go test ./internal/config/...` passes
  - `go vet ./...` and `golangci-lint run` clean
- **Risks / Unknowns:**
  - Test fixtures with env vars need `t.Setenv()` to inject test values (available in Go 1.17+, fine for 1.26+).
  - The cmd/ wiring should handle "no moca.yaml found" gracefully (print message and exit 0, not crash). This is important because during development, engineers may run binaries outside a project directory.

## Recommended Execution Order

1. **MS-01-T1** — Directory scaffolding and cmd/ stubs (unblocks everything)
2. **MS-01-T2** — Typed structs and YAML parser (core data model)
3. **MS-01-T3** — Validation and merging (builds on parser)
4. **MS-01-T4** — Integration wiring and tests (proves all acceptance criteria)

All tasks are sequential. Each builds on the previous.

## Open Questions

1. **go.work Go version:** `go.work` says `go 1.26.1` but `go.mod` says `go 1.26`. Which version should be canonical? Recommend aligning both to the actual toolchain version in use.
2. **Semver constraint validation depth:** Should MS-01 fully parse semver constraint ranges (e.g., `>=1.0.0, <2.0.0`) or just validate they're non-empty strings? Full parsing requires a library or custom parser. Recommendation: validate non-empty + basic format check; defer full constraint evaluation to MS-09 (CLI App Commands).
3. **Three-layer cascade scope:** Which config fields are overridable per-site vs project-global? The design docs show `common_site_config.yaml` and `site_config.yaml` but don't enumerate which fields they contain. Recommendation: for MS-01, implement the merge mechanism for all fields; restrict per-field overridability in future milestones when site management is implemented (MS-09/MS-12).
4. **Existing moca-server binary:** There's a pre-built `moca-server` binary (2.5 MB) at the project root. Should it be removed or added to `.gitignore`? It appears to be an artifact from MS-00 spike work.

## Out of Scope for This Milestone

- No actual functionality in any binary (server, worker, scheduler, outbox do nothing beyond config loading)
- No CLI commands (Cobra integration is MS-07)
- No database connections (MS-02)
- No MetaType definitions (MS-03)
- No hot reload or dev server (MS-10)
- No lockfile (`moca.lock`) parsing — mentioned in §3.2 but not in MS-01 scope
- No `moca init` command that creates the project structure (MS-07/MS-09)
- No site creation or site_config.yaml generation (MS-09)
