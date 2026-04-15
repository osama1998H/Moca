# MS-11 — CLI Operational Commands Plan

## Milestone Summary

- **ID:** MS-11
- **Name:** CLI Operational Commands — Site Ops, Database, Backup, Config, Cache
- **Roadmap Reference:** ROADMAP.md → MS-11 section (lines 656–711)
- **Goal:** Implement secondary site operations, backup/restore, configuration management, cache operations, and database utilities
- **Why it matters:** Essential operational commands for real development workflows. Marks the start of the Alpha release phase. Enables backup/restore safety net, config management, and database maintenance that all downstream milestones assume exist.
- **Position in roadmap:** Order #12 of 30 milestones (first Alpha milestone)
- **Upstream dependencies:** MS-09 (CLI Init, Site & App Commands — complete)
- **Downstream dependencies:** MS-13 (CLI App Scaffolding), MS-16 (Queue/Events/Monitor), MS-12 (Multitenancy benefits from operational commands)

## Vision Alignment

MS-11 transforms Moca from a framework with basic site CRUD into one with production-grade operational tooling. The backup system provides the safety net required by destructive operations (clone, reinstall, reset). The config sync contract (§5.1.1) establishes the dual-write invariant — YAML at rest, DB at runtime — that the entire server architecture depends on. Cache and DB utility commands give developers the maintenance tools needed for day-to-day development.

This milestone completes the CLI Stream B chain (MS-07 → MS-09 → **MS-11** → MS-13 → MS-16) at the operational layer. Without these commands, developers cannot safely manage sites, inspect database drift, or manage configuration — blocking practical use of the framework beyond toy examples.

As the first Alpha milestone, MS-11's backup and config infrastructure is load-bearing for the entire Alpha release: multitenancy (MS-12) needs config sync for per-site configuration, permissions (MS-14) needs config management for auth settings, and background jobs (MS-15) need cache operations for job state management.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| ROADMAP.md | MS-11 | 656–711 | Milestone definition, scope, acceptance criteria |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.2 Site Management | 763–843 | clone, reinstall, enable, disable, rename, browse specs |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.5 Database Operations | 1337–1495 | console, snapshot, seed, trim-tables, trim-database, export-fixtures, reset |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.6 Backup Operations | 1498–1653 | create, restore, list, verify specs |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.7 Configuration Management | 1657–1758 | get, set, remove, list, diff, export, import, edit specs |
| MOCA_SYSTEM_DESIGN.md | §5.1.1 Configuration Sync Contract | 1060–1072 | YAML ↔ DB sync rules, event publishing |
| MOCA_SYSTEM_DESIGN.md | §5.0 Redis key patterns | 1030–1055 | Cache key patterns for clear/warm commands |

## Research Notes

No web research was needed. The design documents and existing codebase provide sufficient detail for implementation. Key findings from codebase exploration:

- All 30+ target commands already have placeholder stubs registered via `newSubcommand()` in their respective command files
- `pkg/backup/` does not exist yet — entirely greenfield package
- `internal/config/` has YAML loading/validation but no YAML-to-DB sync or site-level config file helpers
- `pkg/tenancy/manager.go` has `CreateSite`, `DropSite`, `ListSites`, `GetSiteInfo` — needs new methods for `CloneSite`, `ReinstallSite`, `EnableSite`, `DisableSite`, `RenameSite`
- `db migrate`, `db rollback`, `db diff` are already fully implemented in `cmd/moca/db.go`
- Redis key patterns documented in System Design §5.0 (lines 1030–1055) define exactly which keys to clear per cache type
- `cmd/moca/services.go` provides full DI graph via `newServices()` returning `Services{DB, Redis, Migrator, Registry, Runner, Sites, Apps, Logger}`
- Existing helpers: `requireProject()`, `resolveSiteName()`, `confirmPrompt()`, `readPassword()`, `gatherMigrations()` in `cmd/moca/services.go`

## Milestone Plan

### Task 1

- **Task ID:** MS-11-T1
- **Title:** Backup Package and CLI Commands
- **Status:** Completed
- **Description:**
  Create the new `pkg/backup/` package with core backup/restore logic, then implement the `moca backup create`, `restore`, `list`, and `verify` CLI commands. The backup system wraps `pg_dump`/`psql` for schema-scoped PostgreSQL dumps, produces timestamped `.sql.gz` files in `sites/{site}/backups/`, and supports integrity verification.

  **Package structure:**
  - `pkg/backup/doc.go` — Package documentation
  - `pkg/backup/types.go` — `BackupInfo`, `CreateOptions`, `RestoreOptions`, `VerifyResult`
  - `pkg/backup/create.go` — `Create(ctx, opts) (*BackupInfo, error)`: `pg_dump` wrapper with `--schema=tenant_{site}`, gzip compression, backup ID generation (`bk_{site}_{YYYYMMDD}_{HHMMSS}`)
  - `pkg/backup/restore.go` — `Restore(ctx, opts) error`: Drop + recreate schema, pipe `.sql.gz` through psql
  - `pkg/backup/list.go` — `List(ctx, site, projectRoot) ([]BackupInfo, error)`: Scan `sites/{site}/backups/`, parse filenames
  - `pkg/backup/verify.go` — `Verify(ctx, backupPath) (*VerifyResult, error)`: Checksum validation, SQL syntax check via `pg_restore --list`, optional deep verify
  - `pkg/backup/deps.go` — `CheckDependencies() error`: Binary detection for `pg_dump`/`psql` via `exec.LookPath` (reusable by `moca doctor`)

  **CLI commands** (`cmd/moca/backup.go`): Replace 4 placeholders (create, restore, list, verify) with full implementations. Leave `schedule`, `upload`, `download`, `prune` as placeholders (out of scope — MS-21/MS-22).

  **Key implementation details:**
  - Schema-scoped dump: `pg_dump --schema=tenant_{site}` isolates tenant data
  - Connection parameters extracted from `config.DatabaseConfig` (host, port, user, dbname)
  - Backup directory: `{projectRoot}/sites/{site}/backups/`
  - Compression: gzip by default, `--compress` flag for zstd/none
  - Restore flow: confirm prompt → optional pre-restore backup → drop schema CASCADE → recreate schema → pipe SQL → optionally run migrations

- **Why this task exists:** Backup/restore is the safety net for all destructive operations in MS-11 (site clone, reinstall, db reset). It's greenfield work that must land first to unblock Tasks 3 and 4.
- **Dependencies:** None
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.6 (lines 1498–1653) — full command specs
  - `ROADMAP.md` MS-11 deliverables 6–8 (lines 670–672)
  - `pkg/tenancy/manager.go` — schema naming convention
  - `cmd/moca/services.go` — service DI pattern, config extraction
  - `internal/config/types.go` — `DatabaseConfig` for connection parameters
- **Deliverable:**
  - New package: `pkg/backup/` (7 files)
  - Modified: `cmd/moca/backup.go`
  - Tests: `pkg/backup/*_test.go` (unit), `pkg/backup/*_integration_test.go` (integration with real PG)
- **Acceptance Criteria:**
  - `moca backup create` produces timestamped backup in `sites/{site}/backups/`
  - `moca backup restore BACKUP_FILE --site X` restores site to backup state
  - `moca backup list` shows available backups with ID, size, timestamp
  - `moca backup verify BACKUP_ID` validates backup integrity (checksum + SQL syntax)
  - Missing `pg_dump` binary produces a clear CLIError with fix suggestion
  - All commands support `--json` output mode
  - Integration test: create → list → verify → restore round-trip succeeds
- **Risks / Unknowns:**
  - `pg_dump`/`psql` may not be installed — detect gracefully via `exec.LookPath` with helpful error message
  - Schema naming function in `pkg/tenancy/manager.go` may be unexported — export it or extract to shared utility

### Task 2

- **Task ID:** MS-11-T2
- **Title:** Configuration Management Commands and Config Sync
- **Status:** Completed
- **Description:**
  Implement all 8 `moca config` subcommands and the YAML-to-DB config sync mechanism per the Config Sync Contract (System Design §5.1.1).

  **New files:**
  - `internal/config/sync.go` — `SyncToDatabase(ctx, siteName, cfg, db, redis)`: writes merged config to `moca_system.sites.config` JSONB column, updates Redis `config:{site}` key, publishes `config.changed` event on `pubsub:config:{site}` channel. If Redis unavailable, DB update succeeds alone.
  - `internal/config/site_config.go` — Site-level YAML file helpers: `LoadSiteConfig(projectRoot, site)`, `SaveSiteConfig(projectRoot, site, data)`, `LoadCommonSiteConfig(projectRoot)`, `SaveCommonSiteConfig(projectRoot, data)` — for `sites/{site}/site_config.yaml` and `sites/common_site_config.yaml`
  - `internal/config/keypath.go` — Dot-notation key path utilities: `GetByPath(cfg, "infra.db.host")`, `SetByPath(cfg, key, value)`, `RemoveByPath(cfg, key)` operating on `map[string]any` trees

  **Command implementations** (`cmd/moca/config_cmd.go`):
  - `config get KEY` — Read from YAML layers. `--resolved` merges project + common_site + site YAML. `--runtime` queries `moca_system.sites.config` from DB. Default reads project-level `moca.yaml`.
  - `config set KEY VALUE` — Auto-detect value type (int/bool/string/JSON). Write to appropriate YAML file (`--project`/`--common`/`--site`). Call `SyncToDatabase()`.
  - `config remove KEY` — Remove from YAML file, sync to DB.
  - `config list` — Load merged config, render as table/YAML/JSON per `--format`. Support `--filter` glob pattern.
  - `config diff SITE1 SITE2` — Deep key-by-key comparison of resolved configs.
  - `config export` — Marshal resolved config to YAML/JSON/env format. `--secrets` controls masking.
  - `config import FILE` — Parse input, merge into target config. `--overwrite`/`--dry-run`.
  - `config edit` — Resolve config file path, open `$EDITOR`, validate after save via `config.Validate()`.

  **Config sync contract enforcement (§5.1.1):**
  1. `config set` writes YAML AND updates DB atomically, publishes `config.changed` event
  2. If Redis unavailable, DB update succeeds; server picks up on next restart
  3. Server reads config only from DB (via Redis cache), never YAML directly
  4. `config get --resolved` merges YAML layers; `--runtime` queries DB

- **Why this task exists:** The config sync contract is a core system invariant. Every future milestone assumes `moca config set` updates both YAML and the running server. The enable/disable commands in Task 4 also rely on the config event mechanism.
- **Dependencies:** None
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.7 (lines 1657–1758) — all 8 command specs with flags and examples
  - `MOCA_SYSTEM_DESIGN.md` §5.1.1 (lines 1060–1072) — config sync contract rules
  - `MOCA_SYSTEM_DESIGN.md` §5.0 (lines 1050–1055) — pub/sub channel pattern `pubsub:config:{site}`
  - `internal/config/` — existing `LoadAndResolve()`, `ParseFile()`, `Validate()`, `ResolveInheritance()`
  - `internal/drivers/` — `RedisClients` with Cache and PubSub clients
  - `cmd/moca/services.go` — `newServices()` for DB/Redis access
- **Deliverable:**
  - New files: `internal/config/sync.go`, `internal/config/site_config.go`, `internal/config/keypath.go`
  - Modified: `cmd/moca/config_cmd.go`
  - Tests: `internal/config/sync_test.go`, `internal/config/keypath_test.go`, `internal/config/site_config_test.go`, integration test for full sync cycle
- **Acceptance Criteria:**
  - `moca config set KEY VALUE` updates YAML AND running server's config (via event)
  - `moca config get --resolved` shows merged config from all YAML layers
  - `moca config get --runtime` queries DB for active server config
  - `moca config edit` opens `$EDITOR` and validates on save
  - `moca config diff SITE1 SITE2` shows key-by-key differences
  - Config sync publishes `config.changed` on Redis pub/sub channel `pubsub:config:{site}`
  - All commands support `--json` output mode
- **Risks / Unknowns:**
  - YAML round-trip editing may lose comments — consider `gopkg.in/yaml.v3` node-level editing vs. simple marshal/unmarshal
  - Config sync event: if server not running, DB update queued until next start (acceptable per design)
  - `$EDITOR` may not be set — fall back to `vi` on Unix, provide CLIError on Windows

### Task 3

- **Task ID:** MS-11-T3
- **Title:** Cache Commands and DB Utility Commands
- **Status:** Completed
- **Description:**
  Implement `moca cache clear` and `moca cache warm`, plus all 7 DB utility commands: `console`, `seed`, `reset`, `snapshot`, `export-fixtures`, `trim-tables`, `trim-database`.

  **Cache commands** (`cmd/moca/cache.go`):
  - `cache clear` — Delete Redis keys by documented patterns (System Design §5.0): `meta:{site}:*`, `doc:{site}:*`, `config:{site}`, `perm:{site}:*:*`, `schema:{site}:version` on Cache DB (db 0); session keys `session:{site}:*` on Session DB (db 2). Flags: `--site` (required), `--type meta|doc|session|all` (default: all).
  - `cache warm` — Load all registered MetaTypes from L3 (PostgreSQL) via `Registry.Get()` to populate L1 (sync.Map) + L2 (Redis) caches. Report count of warmed entries.

  **DB commands** (`cmd/moca/db.go`):
  - `db console` — Detect `psql` via `exec.LookPath`, construct connection args from `DatabaseConfig` + `search_path=tenant_{site}`, use `syscall.Exec` on Unix for clean process replacement.
  - `db seed` — Scan `apps/{app}/fixtures/` for JSON fixture files, deserialize, insert into site schema via prepared statements. Flags: `--site`, `--app`, `--file`, `--force`.
  - `db reset` — Confirm prompt (or `--force`), optional backup via `pkg/backup.Create()` (warn if T1 not available), then `SiteManager.DropSite()` + `SiteManager.CreateSite()` preserving original config. Flags: `--site`, `--force`, `--no-backup`.
  - `db snapshot` — `pg_dump --schema-only --schema=tenant_{site}` to `sites/{site}/snapshots/{timestamp}.sql`. Flags: `--site`, `--include-data`.
  - `db export-fixtures` — Query data from specified DocType tables, marshal rows as JSON fixture files to `apps/{app}/fixtures/{doctype}.json`. Flags: `--site`, `--app`, `--doctype`, `--filters`.
  - `db trim-tables` — For each MetaType, query `information_schema.columns` for corresponding `tab_{doctype}` table, compare against MetaType field definitions + protected columns (`name`, `_extra`, `created_at`, `modified_at`, `owner`), report orphaned columns. `--dry-run` is the default safe behavior. Flags: `--site`, `--dry-run` (default true), `--doctype`.
  - `db trim-database` — Query `information_schema.tables` for all `tab_*` tables in tenant schema, compare against registered MetaType names. Report orphaned tables. `--dry-run` default. Flags: `--site`, `--dry-run` (default true).

- **Why this task exists:** These are the daily-use maintenance commands. Cache clear is essential for development iteration. DB console is the most-requested developer tool. trim-tables/trim-database maintain schema hygiene as MetaTypes evolve.
- **Dependencies:** MS-11-T1 (soft — `db reset` optionally uses backup; can warn and proceed without it)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.5 (lines 1337–1495) — DB command specs
  - `MOCA_SYSTEM_DESIGN.md` §5.0 (lines 1030–1055) — Redis key patterns for cache operations
  - `pkg/meta/registry.go` — `Registry.Get()`, MetaType field definitions
  - `pkg/orm/schema.go` — DDL generation, `information_schema` patterns
  - `pkg/tenancy/manager.go` — `DropSite()`, `CreateSite()` for `db reset`
  - `internal/drivers/` — `RedisClients.Cache`, `RedisClients.Session`
- **Deliverable:**
  - Modified: `cmd/moca/cache.go`, `cmd/moca/db.go`
  - Tests: unit tests for trim logic (mock information_schema results), fixture export/import, cache key pattern matching; integration tests for console arg construction, seed/export round-trip
- **Acceptance Criteria:**
  - `moca cache clear --site X` removes all Redis keys for site
  - `moca cache clear --site X --type meta` selectively clears only metadata cache
  - `moca cache warm --site X` populates L1+L2 caches for all MetaTypes
  - `moca db console` opens psql connected to correct tenant schema with correct search_path
  - `moca db trim-tables --dry-run` shows orphaned columns without removing them
  - `moca db trim-database --dry-run` shows orphaned tables without removing them
  - `moca db reset --force` drops and recreates site schema (with pre-reset backup by default)
  - `moca db seed --app core` loads fixture data from app's fixtures directory
  - `moca db snapshot` saves schema DDL to `sites/{site}/snapshots/`
  - `moca db export-fixtures --doctype User` exports user data as JSON fixture
  - `_extra` JSONB column is never flagged as orphaned by `trim-tables`
- **Risks / Unknowns:**
  - `psql` binary dependency — same detection pattern as `pg_dump` in T1
  - `trim-tables` must correctly handle protected columns (`name`, `_extra`, timestamps) — these should never be flagged as orphaned
  - `db reset` is destructive — confirmation UX must be very clear, especially without `--force`
  - Fixture format: need to define/document the JSON fixture file format (array of objects keyed by DocType name)

### Task 4

- **Task ID:** MS-11-T4
- **Title:** Site Operational Commands
- **Status:** Completed
- **Description:**
  Implement `moca site clone`, `reinstall`, `enable`, `disable`, `rename`, and `browse` by extending `pkg/tenancy/manager.go` with new methods and replacing the placeholder commands in `cmd/moca/site.go`.

  **New SiteManager methods** (`pkg/tenancy/manager.go`):
  - `CloneSite(ctx, source, target string, opts CloneOptions) error` — (a) create backup of source via `pkg/backup`, (b) create target site schema, (c) restore backup into target schema, (d) if `opts.Anonymize`, run anonymization queries on cloned data, (e) register target in system table
  - `ReinstallSite(ctx, siteName string, opts ReinstallOptions) error` — (a) backup (unless opts.NoBackup), (b) get installed app list, (c) drop site, (d) recreate with new admin password, (e) reinstall all apps via `AppInstaller`
  - `EnableSite(ctx, siteName string) error` — update `sites.status='active'`, clear maintenance Redis keys, publish `config.changed` event
  - `DisableSite(ctx, siteName, message string, allowIPs []string) error` — update `sites.status='disabled'`, store maintenance metadata in site config, publish `config.changed` event
  - `RenameSite(ctx, oldName, newName string) error` — in transaction: `ALTER SCHEMA tenant_{old} RENAME TO tenant_{new}`, update `sites.name` and `sites.db_schema`, rename Redis keys, rename `sites/{old}/` directory, update `.moca/current_site` if needed

  **CLI commands** (`cmd/moca/site.go`):
  - `site clone SOURCE NEW --anonymize --data-only --exclude DOCTYPES` — calls `CloneSite()`
  - `site reinstall [SITE] --admin-password --force --no-backup` — calls `ReinstallSite()`
  - `site enable [SITE]` — calls `EnableSite()`
  - `site disable [SITE] --message STRING --allow IPS` — calls `DisableSite()`
  - `site rename OLD NEW --no-proxy-reload` — calls `RenameSite()`
  - `site browse [SITE] --user --print-url` — construct URL from config (`http://{site}:{dev_port}`), open via platform-specific browser command (`open` on macOS, `xdg-open` on Linux), or print URL with `--print-url`

  **Anonymization for clone:** Hardcoded rules for core DocTypes:
  - `tab_user`: randomize email (user_{id}@example.com), hash password, clear full_name
  - `tab_contact`: randomize phone, email
  - `tab_address`: randomize street address
  - Extensible via app-level `anonymize.json` files in future

- **Why this task exists:** These commands complete the site lifecycle. Clone enables staging/testing workflows. Enable/disable provides maintenance mode. Reinstall provides clean reset. These are essential for the Alpha release operational story.
- **Dependencies:** MS-11-T1 (clone and reinstall use backup/restore), MS-11-T2 (enable/disable publish config events via `SyncToDatabase`/pub/sub)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.2 (lines 763–843) — all 6 command specs with flags
  - `pkg/tenancy/manager.go` — existing `CreateSite()`, `DropSite()`, `ListSites()`, `GetSiteInfo()` patterns
  - `pkg/backup/` (from T1) — `Create()`, `Restore()` for clone/reinstall safety
  - `internal/config/sync.go` (from T2) — event publishing for enable/disable
  - `pkg/apps/installer.go` — `AppInstaller.Install()` for reinstall
- **Deliverable:**
  - Modified: `cmd/moca/site.go`, `pkg/tenancy/manager.go`
  - New types: `CloneOptions`, `ReinstallOptions` in `pkg/tenancy/`
  - Tests: `pkg/tenancy/manager_test.go` (new method tests), integration tests for clone, enable/disable status transitions
- **Acceptance Criteria:**
  - `moca site clone acme.localhost staging.localhost --anonymize` creates anonymized copy
  - `moca site reinstall` resets site to fresh state with all apps reinstalled
  - `moca site disable acme.localhost --message "Maintenance"` sets status to disabled, returns 503
  - `moca site enable acme.localhost` restores site to active status
  - `moca site rename old.localhost new.localhost` renames schema, directory, and system records
  - `moca site browse` opens site URL in default browser (or prints with `--print-url`)
  - Clone with `--exclude User` skips the User table data
- **Risks / Unknowns:**
  - `site rename` touches multiple systems (PG schema, Redis, filesystem, system table) — partial failure needs rollback strategy. Recommend: PG operations in transaction, filesystem/Redis best-effort with error logging.
  - `--anonymize` scope: core doctypes are clear, but app-specific PII fields need a hook or convention for future extensibility
  - `site browse --user` impersonation requires auth token generation — stub until MS-14 (Auth). For now, `--user` flag can be accepted but warn "requires auth module (MS-14)"

## Recommended Execution Order

1. **MS-11-T1** (Backup Package) — Greenfield package with no dependencies. Unblocks T3 (`db reset` backup) and T4 (`site clone`, `site reinstall`). Start here.
2. **MS-11-T2** (Config Management + Sync) — No dependencies. Establishes the config sync contract used by T4 (`site enable/disable`). Can run in parallel with T1.
3. **MS-11-T3** (Cache + DB Utils) — Soft dependency on T1 for `db reset` backup integration. Can start in parallel with T1/T2 by stubbing the backup call initially.
4. **MS-11-T4** (Site Ops) — Depends on T1 (backup/restore for clone/reinstall) and T2 (config events for enable/disable). Must run last.

**Parallelization opportunity:** Tasks 1, 2, and 3 are largely independent and can be developed simultaneously by different engineers. Task 4 is the integration task that ties everything together.

```
Week 1: T1 (Backup) + T2 (Config) in parallel
Week 2: T3 (Cache + DB Utils) + begin T4 (Site Ops)
Week 3: Complete T4 + cross-task integration testing
```

## Open Questions

1. **`schemaNameForSite()` visibility:** This function in `pkg/tenancy/manager.go` appears to be unexported. Tasks T1, T3, and T4 all need the schema naming convention. Should we export it as `SchemaNameForSite()`, or extract it to a shared utility in `pkg/tenancy/site.go`?

2. **`site browse --user` impersonation:** Auth is not implemented until MS-14. Options: (a) accept the flag but warn "requires auth module", (b) stub it completely, (c) implement a simple dev-mode-only temporary token. Recommend option (a).

3. **YAML comment preservation:** `config set` and `config import` modify YAML files. Using `gopkg.in/yaml.v3` node-level editing preserves comments but adds complexity. Simple marshal/unmarshal loses comments. Which approach is preferred?

4. **`trim-tables` default safety:** Should `--dry-run` be the implicit default (requiring `--execute` to actually drop columns), or follow the standard pattern where no flag means execute and `--dry-run` opts into preview mode? Recommend: default to dry-run for destructive schema operations.

## Out of Scope for This Milestone

- `moca backup schedule` — deferred to MS-21 (Ops & Infrastructure)
- `moca backup upload/download/prune` — deferred to MS-21 (requires S3/MinIO adapter)
- Backup encryption (AES-256-GCM) — deferred to MS-22 (Security)
- `moca deploy *` commands — separate milestone (MS-21)
- Full authentication and permissions — MS-14
- `moca doctor` and `moca status` — could be addressed as bonus but not in MS-11 scope
- `moca dev console/execute/request` — developer tools (MS-13)
- `moca queue/events/log/monitor` — operational monitoring (MS-16)
