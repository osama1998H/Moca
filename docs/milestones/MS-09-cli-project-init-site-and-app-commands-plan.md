# MS-09 — CLI Project Init, Site, and App Commands Plan

## Milestone Summary

- **ID:** MS-09
- **Name:** CLI Project Init, Site, and App Commands (Init, Create, Drop, Install, Migrate)
- **Roadmap Reference:** `ROADMAP.md` lines 564–610
- **Goal:** Implement `moca init` (project bootstrapping), `moca site create/drop/list/use/info`, `moca app install/uninstall/list`, and `moca db migrate/rollback/diff` — making the framework usable from the command line for the first time.
- **Why it matters:** After MS-07 (CLI scaffold) and MS-08 (hooks + app system), the framework has infrastructure but no user-facing workflow. MS-09 is the first milestone where a developer can actually *use* Moca: initialize a project, create a tenant site, install apps, and manage database migrations. This is the developer experience unlock.
- **Position in roadmap:** Order #6 in the CLI workstream. Bridges the gap between internal framework plumbing (MS-00–MS-08) and developer-usable tooling (MS-10+ for hot reload, MS-11 for operational commands).
- **Upstream dependencies:** MS-07 (CLI Foundation — complete), MS-08 (Hook Registry & App System — in progress)
- **Downstream dependencies:** MS-10 (Dev Server — needs `moca init` + `moca site create` to work first), MS-11 (Operational CLI — extends site/db commands), MS-13 (App Scaffolding — extends app commands)

---

## Vision Alignment

Moca's core promise is that a single MetaType definition drives everything: schema, API, permissions, search, UI. But that promise is unreachable without a CLI that can bootstrap the infrastructure. MS-09 makes the MetaType-driven architecture tangible by providing the three essential workflows:

1. **`moca init`** — Creates the project structure and connects infrastructure (PostgreSQL, Redis). Establishes the `moca_system` schema that tracks sites and apps globally.
2. **`moca site create`** — Creates a tenant with its own PostgreSQL schema, system tables, and bootstrapped core app. This is the schema-per-tenant architecture (ADR-001) in action.
3. **`moca app install`** — Installs an app on a site by resolving dependencies, running migrations, creating MetaType tables, and seeding fixtures. This exercises the full MetaType → DDL → table pipeline.

After MS-09, a developer can: `moca init my-erp && moca site create acme.localhost && moca app install crm` — the complete onboarding experience.

---

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.1 moca init | 565–640 | Project initialization workflow, flags, directory structure |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.2 Site Management | 644–865 | All site commands: create (9-step lifecycle), drop, list, use, info, migrate |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.3 App Management | 869–1091 | App install/uninstall workflow, dependency resolution, fixture loading |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.5 Database Operations | 1327–1495 | db migrate/rollback/diff commands, migration version tracking |
| `MOCA_SYSTEM_DESIGN.md` | §8.3 Site Lifecycle | 1435–1462 | Canonical 9-step site creation and 6-step app installation sequences |
| `ROADMAP.md` | MS-09 | 564–610 | Scope, deliverables, acceptance criteria, risks |
| `pkg/orm/schema.go` | EnsureSystemSchema | 1–83 | Existing system schema DDL (sites, apps, site_apps tables) |
| `pkg/meta/migrator.go` | Migrator | 1–224 | Existing MetaType schema diffing and DDL application |
| `pkg/apps/loader.go` | ScanApps, ValidateDependencies | 1–200 | Existing app discovery, manifest validation, cycle detection |
| `pkg/builtin/core/bootstrap.go` | BootstrapCoreMeta | 1–95 | Core app's 8 MetaType definitions, self-referential DocType bootstrap |

---

## Research Notes

No web research was needed. The design documents, existing codebase, and ROADMAP provide sufficient detail for all implementation tasks. Key findings from codebase exploration:

1. **Infrastructure readiness is high (~70%).** DBManager with per-tenant pools, Migrator with Diff/Apply, AppManifest parsing with dependency validation, CLI context/output layer, and core app bootstrap are all production-ready.
2. **`AppManifest` already declares `Migration` and `FixtureDef` struct types** (forward declarations from MS-08) — these are ready for MS-09 to consume.
3. **All CLI command groups are registered as placeholders** via `newSubcommand("name", "description")` with `notImplemented()` RunE — these need replacement with real implementations.
4. **`tab_version` table** (created by `EnsureMetaTables`) is designed for document change tracking, not migration tracking. A separate `tab_migration_log` table is needed to avoid conflating concerns.
5. **`SiteContext`** (`pkg/tenancy/site.go`) only holds `Name` and `Pool`. The `SiteManager` for lifecycle operations does not exist yet.
6. **Meilisearch and S3 integrations** are not implemented. The 9-step site lifecycle should stub these steps (log warning, continue) since they ship in later milestones.

---

## Milestone Plan

### Task 1: Migration Runner with Version Tracking

- **Task ID:** MS-09-T1
- **Status:** Completed
- **Title:** Migration Runner with Version Tracking and DependsOn Ordering
- **Description:**
  Build `pkg/orm/migrate.go` containing a `MigrationRunner` that executes SQL migration files against a site's database, tracks applied versions in a `tab_migration_log` table, supports DependsOn ordering via topological sort, and provides dry-run and rollback capabilities.

  Key types:
  - `MigrationRunner` struct (backed by `DBManager` and `slog.Logger`)
  - `AppMigration` struct: `AppName`, `Version`, `UpSQL`, `DownSQL`, `DependsOn []string`
  - `MigrateOptions` / `RollbackOptions` / `MigrateResult` structs

  Key methods:
  - `Pending(ctx, site, migrations) → []AppMigration` — queries `tab_migration_log`, returns unapplied migrations
  - `Apply(ctx, site, migrations, opts) → *MigrateResult` — topologically sorts by DependsOn, executes UP SQL in a transaction, records each in `tab_migration_log` with batch number
  - `Rollback(ctx, site, opts) → *MigrateResult` — finds latest batch, executes DOWN SQL in reverse order
  - `DryRun(ctx, site, migrations) → []DDLPreview` — returns SQL without executing

  Also extend `GenerateSystemTablesDDL()` in `pkg/meta/ddl.go` to include the `tab_migration_log` table (id, app, version, batch, up_sql, down_sql, applied_at). This table is created per-tenant alongside `tab_doctype`, `tab_singles`, etc.

  Reuse the topological sort pattern from `pkg/apps/loader.go:detectCycles()` for DependsOn ordering.

- **Why this task exists:** Every downstream operation (site creation, app installation, db commands) depends on a migration runner. It is the foundation layer that must exist before anything else can execute schema changes reliably.
- **Dependencies:** None (foundation layer)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.5 lines 1327–1495 (db migrate/rollback behavior)
  - `pkg/meta/migrator.go` (existing `Apply()` pattern for transactional DDL execution)
  - `pkg/apps/loader.go:detectCycles()` (Kahn's algorithm to reuse for DependsOn sort)
  - `pkg/orm/transaction.go` (`WithTransaction` for atomic migration batches)
  - `pkg/meta/ddl.go:GenerateSystemTablesDDL()` (extend with tab_migration_log)
- **Deliverable:**
  - `pkg/orm/migrate.go` — MigrationRunner implementation
  - `pkg/orm/migrate_test.go` — unit tests for topological sort, pending calculation, dry-run
  - Updated `pkg/meta/ddl.go` — `tab_migration_log` DDL added to system tables
- **Risks / Unknowns:**
  - **Rollback strategy:** Full SQL DOWN vs snapshot-based. Design documents lean toward SQL DOWN. For MS-09, implement SQL DOWN rollback with a clear error when DOWN SQL is missing. Snapshot-based rollback can be added in MS-11.
  - **DependsOn format:** Cross-app references use `"appName:version"` format. Must handle the case where a dependency migration hasn't been loaded (error) vs already applied (skip).
  - **Concurrent migrations:** Two CLI sessions running `moca db migrate` simultaneously could conflict. Consider advisory locks (`pg_advisory_lock`) to serialize migrations per site.

---

### Task 2: Site Manager and App Installer Service Layer

- **Task ID:** MS-09-T2
- **Status:** Completed
- **Title:** SiteManager (9-Step Lifecycle) and AppInstaller (Install/Uninstall)
- **Description:**
  Build two service-layer packages that orchestrate the site and app lifecycles, consuming the infrastructure from Task 1 and existing packages.

  **`pkg/tenancy/manager.go` — SiteManager:**
  - `CreateSite(ctx, cfg SiteCreateConfig) error` — the 9-step lifecycle:
    1. Create PostgreSQL schema `tenant_{sanitized_name}` via SystemPool
    2. Call `Migrator.EnsureMetaTables(ctx, site)` for system tables
    3. Bootstrap core MetaTypes via `BootstrapCoreMeta()` → `Migrator.Diff(nil, mt)` → `Migrator.Apply()` for each
    4. Create Administrator user (INSERT into tab_user with bcrypt password)
    5. Create Redis key namespace (`config:{site}` key with initial config JSON)
    6. Stub: Meilisearch index creation (log warning, skip)
    7. Stub: S3 storage prefix creation (log warning, skip)
    8. Register site in `moca_system.sites` via SystemPool
    9. Warm metadata cache via `Registry.Register()` for each core MetaType
  - `DropSite(ctx, name string, opts SiteDropOptions) error` — DROP SCHEMA CASCADE, delete Redis keys, DELETE from moca_system.sites
  - `ListSites(ctx) ([]SiteInfo, error)` — query moca_system.sites JOIN site_apps
  - `GetSiteInfo(ctx, name string) (*SiteInfo, error)` — detailed site info with DB size
  - `SetActiveSite(projectRoot, siteName string) error` — write to `.moca/current_site`

  **`pkg/apps/installer.go` — AppInstaller:**
  - `Install(ctx, site, appName, appsDir string) error`:
    1. Load app via `ScanApps(appsDir)`, find target app
    2. Validate dependencies are installed on site (query moca_system.site_apps)
    3. Load app's MetaTypes from `modules/{module}/doctypes/` directories
    4. Compile via `meta.Compile()`, diff via `Migrator.Diff(nil, mt)`, apply via `Migrator.Apply()`
    5. Run app migrations via `MigrationRunner.Apply()` (if app has migration files)
    6. Load fixtures from `fixtures/` directory (JSON → INSERT via ORM)
    7. Register app hooks (if app provides `Initialize()`)
    8. INSERT into moca_system.site_apps
    9. Clear caches via `Registry.InvalidateAll()`
  - `Uninstall(ctx, site, appName string, opts UninstallOptions) error` — reverse dependency check, optionally drop tables, remove from site_apps
  - `ListInstalled(ctx, site string) ([]InstalledApp, error)` — query site_apps

  **Fixture loading:** Parse JSON files from `apps/{app}/fixtures/` directory. Each file contains an array of documents. Insert via direct SQL (not full document lifecycle) since we're bootstrapping. Handle ordering by inserting parent DocTypes before child tables.

- **Why this task exists:** CLI commands (Task 3) need a clean service layer to call. Separating orchestration from CLI concerns keeps the code testable and reusable (e.g., programmatic site creation in tests or future API endpoints).
- **Dependencies:** MS-09-T1 (MigrationRunner for Apply/Rollback)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §8.3 lines 1435–1462 (9-step create-site, 6-step install-app)
  - `pkg/orm/schema.go:EnsureSystemSchema()` (pattern for system schema DDL)
  - `pkg/meta/migrator.go:EnsureMetaTables()` (creates per-tenant system tables)
  - `pkg/builtin/core/bootstrap.go:BootstrapCoreMeta()` (8 core MetaTypes)
  - `pkg/builtin/core/user_controller.go` (bcrypt pattern for admin password)
  - `pkg/apps/loader.go` (ScanApps, LoadApp, ValidateDependencies)
  - `internal/drivers/redis.go` (RedisClients for cache namespace setup)
  - `pkg/meta/registry.go` (Register, Get, InvalidateAll for cache warming)
- **Deliverable:**
  - `pkg/tenancy/manager.go` — SiteManager with CreateSite, DropSite, ListSites, GetSiteInfo, SetActiveSite
  - `pkg/tenancy/manager_test.go` — unit tests (mocked DB for lifecycle step verification)
  - `pkg/apps/installer.go` — AppInstaller with Install, Uninstall, ListInstalled
  - `pkg/apps/installer_test.go` — unit tests
- **Risks / Unknowns:**
  - **Admin user creation without full document lifecycle:** During site bootstrap, the document CRUD engine may not be fully wired (no site context exists yet until the site is created). May need to insert the admin user via direct SQL rather than through the document lifecycle engine. This is acceptable for bootstrap.
  - **Fixture ordering:** Fixtures that reference other fixtures (e.g., HasRole references User and Role) need careful insertion order. Use the same topological sort on DocType dependencies (parent tables before child tables, Link targets before referencing documents).
  - **Core app MetaType registration before table creation:** `BootstrapCoreMeta()` returns compiled MetaTypes, but the Migrator needs the site pool (which requires the schema to exist first). The sequencing must be: create schema → create system tables → create MetaType tables → register MetaTypes in cache.

---

### Task 3: CLI Command Implementations

- **Task ID:** MS-09-T3
- **Status:** Completed
- **Title:** Implement moca init, site, app, and db CLI Commands
- **Description:**
  Replace all placeholder `newSubcommand()` / `notImplemented()` implementations with real Cobra commands that wire CLI flags, validate context, call the service layer, and format output.

  **Service construction helper (`cmd/moca/services.go`):**
  Create a shared factory that builds service dependencies (DBManager, RedisClients, MigrationRunner, Migrator, SiteManager, AppInstaller) from CLIContext. This avoids repeating construction logic in every command and ensures consistent initialization with proper `defer Close()`.

  **`moca init` (`cmd/moca/init.go`):**
  - Flags: `--name`, `--db-host`, `--db-port`, `--redis-host`, `--redis-port`, `--kafka/--no-kafka`, `--minimal`, `--template`, `--skip-assets`, `--apps`, `--json`
  - Workflow: create directory → generate moca.yaml → connect PG → `EnsureSystemSchema()` → connect Redis → register core app in moca_system.apps → generate moca.lock → `git init` → print summary
  - Use `output.Writer.NewSpinner()` for progress on each step

  **`moca site create` (`cmd/moca/site.go`):**
  - Args: SITE_NAME (required)
  - Flags: `--admin-password` (interactive prompt via `golang.org/x/term` if not provided), `--db-name`, `--install-apps`, `--timezone`, `--language`, `--currency`, `--no-cache-warmup`, `--json`
  - Validate project context exists (`CLIContext.Project != nil`)
  - Call `SiteManager.CreateSite()` with progress spinner per lifecycle step

  **`moca site drop`:** `--force`, `--no-backup`, `--keep-database`. Confirmation prompt unless `--force`.

  **`moca site list`:** `--json`, `--table`, `--verbose`, `--status` filter. Format via `output.Writer.PrintTable()`.

  **`moca site use`:** Write to `.moca/current_site`, print confirmation.

  **`moca site info`:** `--json`. Display site details.

  **`moca app install`:** Args: APP_NAME. `--site`, `--all-sites`. Call `AppInstaller.Install()`.

  **`moca app uninstall`:** `--site`, `--force`, `--keep-data`, `--dry-run`. Confirmation unless `--force`.

  **`moca app list`:** `--site`, `--project`, `--json`, `--verbose`. Show apps from ScanApps or ListInstalled depending on flags.

  **`moca db migrate`:** `--site`, `--dry-run`, `--step`, `--skip`, `--verbose`. Call `MigrationRunner.Pending()` then `Apply()`.

  **`moca db rollback`:** `--site`, `--step`, `--dry-run`. Call `MigrationRunner.Rollback()`.

  **`moca db diff`:** `--site`, `--doctype`, `--output` (text/sql/json). Load MetaTypes from registry, call `Migrator.Diff()` for each, display differences.

- **Why this task exists:** The CLI is the user-facing surface of MS-09. Without command implementations, the service layer has no consumer. This task turns the internal plumbing into a usable developer tool.
- **Dependencies:** MS-09-T2 (SiteManager, AppInstaller for service calls)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.1 lines 565–640 (moca init flags and workflow)
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.2 lines 644–865 (site command flags and behavior)
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.3 lines 869–1091 (app command flags and behavior)
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.5 lines 1327–1495 (db command flags and behavior)
  - `cmd/moca/init.go` — current placeholder to replace
  - `cmd/moca/site.go` — current placeholder subcommands to replace
  - `cmd/moca/app.go` — current placeholder subcommands to replace
  - `cmd/moca/db.go` — current placeholder subcommands to replace
  - `internal/output/` — Writer, Spinner, PrintTable, PrintSuccess, CLIError for output formatting
  - `internal/context/` — CLIContext for project/site resolution
  - `cmd/moca/placeholder.go` — `newSubcommand()` and `notImplemented()` helpers (to be replaced)
- **Deliverable:**
  - `cmd/moca/services.go` — shared service construction factory
  - `cmd/moca/init.go` — full moca init implementation
  - `cmd/moca/site.go` — create, drop, list, use, info implementations (other subcommands remain placeholders)
  - `cmd/moca/app.go` — install, uninstall, list implementations (others remain placeholders)
  - `cmd/moca/db.go` — migrate, rollback, diff implementations (others remain placeholders)
- **Risks / Unknowns:**
  - **Interactive password prompt:** `golang.org/x/term.ReadPassword()` requires a terminal. In non-TTY contexts (CI, scripts), must accept `--admin-password` flag or fail with a clear error. Check `os.Stdin` is a terminal before prompting.
  - **moca init vs site create boundary:** `moca init` creates the project and moca_system schema but does NOT create a site. Some users may expect `moca init` to also create a default site. The design doc is clear that these are separate, but consider a helpful "Next step: run `moca site create`" message.
  - **Service teardown:** DBManager and RedisClients open connections that must be closed. The services.go helper must ensure `defer manager.Close()` patterns are correct even when commands fail partway through.

---

### Task 4: Integration Tests (Full CLI Workflow)

- **Task ID:** MS-09-T4
- **Status:** Completed
- **Title:** Integration Tests for Migration Runner, Site Lifecycle, App Installation, and CLI Commands
- **Description:**
  Build integration tests that exercise the full MS-09 workflow end-to-end against real PostgreSQL and Redis instances (via Docker, using existing `docker-compose.yml`).

  **Test suites:**

  1. **MigrationRunner integration** (`pkg/orm/migrate_integration_test.go`):
     - Create test schema, run sample migrations, verify tables and tab_migration_log entries
     - Test DependsOn ordering (migration B depends on A, verify A runs first)
     - Test rollback (apply batch, rollback, verify tables removed and log entries updated)
     - Test dry-run (verify SQL returned without side effects)
     - Test `Pending()` correctly filters already-applied migrations

  2. **SiteManager integration** (`pkg/tenancy/manager_integration_test.go`):
     - `CreateSite` → verify schema exists, system tables exist, admin user in tab_user, site in moca_system.sites
     - `DropSite` → verify schema dropped, site removed from moca_system.sites
     - `ListSites` after creating 2+ sites → verify all returned with correct details
     - Duplicate site name → expect clear error
     - `SetActiveSite` → verify `.moca/current_site` file written correctly

  3. **AppInstaller integration** (`pkg/apps/installer_integration_test.go`):
     - Create site → install core app → verify MetaType tables created, site_apps entry
     - Install app with missing dependency → expect error
     - Uninstall app → verify site_apps entry removed
     - List installed apps → verify correct results

  4. **CLI end-to-end** (`cmd/moca/integration_test.go`):
     - Full workflow: `moca init` → `moca site create` → `moca app install` → `moca db migrate --dry-run` → `moca site list` → `moca site drop`
     - Execute commands programmatically via Cobra's `cmd.Execute()` (follow existing pattern in `cmd/moca/commands_test.go`)
     - Verify output format (JSON mode, table mode)
     - Error cases: init in existing project, create duplicate site

  All integration tests use the `integration` build tag and require Docker.

- **Why this task exists:** MS-09 is the first milestone where multiple subsystems interact end-to-end (CLI → service layer → ORM → PostgreSQL + Redis). Integration tests are essential to verify the full chain works and to prevent regressions as downstream milestones build on this foundation.
- **Dependencies:** MS-09-T1, MS-09-T2, MS-09-T3 (tests exercise all layers)
- **Inputs / References:**
  - `pkg/builtin/core/integration_test.go` — existing integration test pattern (setup helper, Docker PG/Redis)
  - `pkg/orm/integration_test.go` — existing ORM integration test pattern
  - `cmd/moca/commands_test.go` — existing CLI test pattern
  - `docker-compose.yml` — PostgreSQL on port 5433, Redis for test infrastructure
  - `ROADMAP.md` MS-09 acceptance criteria (lines 581–588) — the exact scenarios to verify
- **Deliverable:**
  - `pkg/orm/migrate_integration_test.go`
  - `pkg/tenancy/manager_integration_test.go`
  - `pkg/apps/installer_integration_test.go`
  - `cmd/moca/integration_test.go`
- **Risks / Unknowns:**
  - **Test isolation:** Each test must create and drop its own schema to avoid cross-test contamination. Use unique site names (e.g., `test_{uuid[:8]}`).
  - **Docker availability:** CI must have Docker. Local dev may not. Tests should skip gracefully when PostgreSQL/Redis are unreachable (existing pattern: check env var or connection before running).
  - **Test duration:** Full lifecycle tests may be slow (5–10s each for schema creation + migration). Keep the test count focused on critical paths, not exhaustive permutations.

---

## Recommended Execution Order

1. **MS-09-T1** — Migration Runner (foundation, no dependencies)
2. **MS-09-T2** — SiteManager + AppInstaller (consumes T1)
3. **MS-09-T3** — CLI Commands (consumes T2, wires everything to user-facing interface)
4. **MS-09-T4** — Integration Tests (validates T1–T3 end-to-end)

Tasks are strictly sequential due to layered dependencies. However, within T2, SiteManager and AppInstaller can be developed in parallel by two engineers since they share the same infrastructure but have independent logic.

---

## Open Questions

1. **Should `moca init` also create a default site?** The design docs keep them separate, but the acceptance criteria show `moca init my-erp` doing a lot of setup. Recommendation: keep them separate, print a "Next: run `moca site create`" message. Revisit if user feedback indicates friction.

2. **Fixture format for core app:** The core app needs fixture data (at minimum: "System Manager" role, "Administrator" module). Should these be JSON files in `pkg/builtin/core/fixtures/` or hardcoded in the SiteManager? Recommendation: JSON fixtures in `pkg/builtin/core/fixtures/` to establish the pattern that builtin and installable apps will follow.

3. **Advisory lock for concurrent migration safety:** Should the MigrationRunner acquire a PostgreSQL advisory lock before running migrations? This prevents two `moca db migrate` invocations from racing. Recommendation: yes, use `pg_advisory_xact_lock(hash)` inside the migration transaction. Low cost, high safety.

4. **`moca.lock` file format:** Referenced in deliverables but not specified in detail. Recommendation: JSON file recording each installed app's name, version, and resolved commit hash. Similar to `package-lock.json`. Define minimally for MS-09, extend in MS-13 when `moca app get` is added.

---

## Out of Scope for This Milestone

- `moca app new` — scaffolding a new app (MS-13)
- `moca app get` — downloading apps from registry or git (MS-13)
- `moca site clone/reinstall/enable/disable/rename/browse` — secondary site operations (MS-11)
- `moca db console/snapshot/seed/trim-tables/trim-database/reset/export-fixtures` — advanced DB operations (MS-11)
- Meilisearch index creation (stub only; real implementation in MS-16)
- S3 storage prefix creation (stub only; real implementation in MS-16)
- Backup before drop/rollback (referenced in flags but full backup system is MS-11)
- Production process management, hot reload (MS-10)
- Authentication/session management (MS-14)
