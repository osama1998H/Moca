# MS-21 — Deployment, Infrastructure Generation, and Production Processes Plan

## Milestone Summary

- **ID:** MS-21
- **Name:** Deployment, Infrastructure Generation, and Production Processes
- **Roadmap Reference:** ROADMAP.md → MS-21 section (lines 1088-1127)
- **Goal:** Implement `moca deploy setup/update/rollback`, infrastructure config generation (Caddy, NGINX, systemd, Docker, K8s), backup automation (schedule/upload/download/prune), and 5-process production architecture.
- **Why it matters:** The system is functionally complete but not deployable to production. This milestone bridges development and production, making Moca a self-deploying framework — described in the CLI design as "the biggest improvement over bench" (Frappe's tool).
- **Position in roadmap:** Order #10 of 30 milestones (Beta phase, parallel with MS-18 through MS-24)
- **Upstream dependencies:** MS-10 (Dev Server & Hot Reload), MS-15 (Background Jobs, Scheduler, Kafka/Redis Events, Search Sync)
- **Downstream dependencies:** MS-22 (Security Hardening — backup encryption extends backup commands), MS-24 (Observability — K8s HPA depends on Prometheus metrics)

## Vision Alignment

Moca's vision is a metadata-driven, multitenant framework that handles the full application lifecycle — from MetaType definition through production deployment. MS-21 completes the operational story by making the framework self-deploying. Where Frappe's `bench` tool requires manual configuration of NGINX, supervisor, and cron jobs, Moca's `deploy setup` is a single idempotent command that takes a project from development to production.

The 5-process architecture (server, worker, scheduler, outbox, search-sync) defined in MOCA_SYSTEM_DESIGN.md §12.3 provides clear separation of concerns for horizontal scaling. Infrastructure generation ensures these processes are correctly configured for systemd, Docker, or Kubernetes — the three primary deployment targets. The template-driven approach means generated configs always reflect the current `moca.yaml` state.

Backup automation (schedule, S3 upload/download, retention pruning) completes the data safety story. Combined with `deploy update`'s auto-rollback and pre-update backups, this gives operators confidence to deploy updates to production without risk of data loss.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| ROADMAP.md | MS-21 | 1088-1127 | Milestone definition, deliverables, acceptance criteria |
| MOCA_SYSTEM_DESIGN.md | §12 Deployment Architecture | 1773-1838 | Single-instance vs production topology, 5 process types, scaling model |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.8 Deployment Operations | 1762-1928 | Full spec for deploy setup/update/rollback/promote/history |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.9 Infrastructure Generation | 1931-2031 | Full spec for generate caddy/nginx/systemd/docker/k8s/env |
| internal/config/types.go | ProductionConfig, BackupConfig | 127-201 | Config types driving generation and deployment |
| internal/scaffold/scaffold.go | Template rendering pattern | 1-50 | Reference for text/template approach |
| pkg/storage/s3.go | S3Storage | 1-123 | Existing MinIO client to reuse for backup upload/download |
| pkg/backup/ | Existing backup package | all | Foundation to extend with remote, schedule, prune |
| internal/process/supervisor.go | Supervisor pattern | 1-120 | Process management for deploy status/restart |
| cmd/moca/deploy.go | Placeholder stubs | 1-24 | 6 stubs to replace |
| cmd/moca/generate.go | Placeholder stubs | 1-26 | 7 stubs to replace |
| cmd/moca/backup.go | Placeholder stubs | 26-29 | 4 stubs to replace |

## Research Notes

No web research was needed. All implementation details are fully specified in the design documents. Key patterns to reuse:

- **Template rendering:** `internal/scaffold/` uses `text/template` with embedded constants and `renderToFile()` — same pattern applies to infrastructure generation.
- **S3 client:** `pkg/storage/s3.go` already wraps minio-go v7 with Upload/Download/Delete/Exists/EnsureBucket — backup remote storage is a thin adapter on top.
- **CLI patterns:** Existing backup commands (`cmd/moca/backup.go:39-326`) demonstrate the exact Cobra + output.Writer + spinner + CLIError patterns for new commands.
- **Process management:** `internal/process/supervisor.go` and `cmd/moca/serve.go` show the subsystem orchestration pattern that `deploy status` needs to inspect.

## Milestone Plan

### Task 1

- **Task ID:** MS-21-T1
- **Title:** Infrastructure Template Engine and Generation Commands
- **Status:** Completed
- **Description:** Create `internal/generate/` package with a template engine that renders infrastructure configs from `ProjectConfig`. Implement all 7 `moca generate` subcommands (caddy, nginx, systemd, docker, k8s, supervisor, env). Templates are Go constants rendered via `text/template`, following the `internal/scaffold/` pattern. Each generator reads config from `moca.yaml` and writes to `config/{type}/` by default. Systemd units use `@.service` template pattern for multi-instance processes. Conditional generation omits Kafka-dependent units when Kafka is disabled.

  **New files:**
  - `internal/generate/generate.go` — `GenerateOptions`, `TemplateData`, `NewTemplateData()`, `renderToFile()`
  - `internal/generate/templates.go` — all template string constants
  - `internal/generate/caddy.go` — Caddyfile (per-site routing, TLS, WebSocket, reverse_proxy, static files)
  - `internal/generate/nginx.go` — NGINX upstream + server block with TLS and WebSocket
  - `internal/generate/systemd.go` — 6 files: server@.service, worker@.service, scheduler.service, outbox.service, search-sync.service, moca.target
  - `internal/generate/docker.go` — docker-compose.yml, docker-compose.prod.yml, Dockerfile, .dockerignore
  - `internal/generate/k8s.go` — 7 manifests: deployment, service, ingress, configmap, secret, hpa, pdb
  - `internal/generate/env.go` — .env file in dotenv/docker/systemd formats
  - `internal/generate/generate_test.go` — unit tests

  **Modified files:**
  - `cmd/moca/generate.go` — replace 7 `newSubcommand` placeholders with real command constructors

- **Why this task exists:** Every deployment command depends on generated infrastructure configs. `deploy setup` calls `generate caddy/systemd`, templates must exist first. This is the foundation layer.
- **Dependencies:** None (can start immediately)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.9 (lines 1931-2031) — all generate command specs
  - `MOCA_SYSTEM_DESIGN.md` §12.3 (lines 1829-1838) — 5 process types and their roles
  - `internal/config/types.go` (lines 127-149) — ProductionConfig, ProxyConfig, TLSConfig
  - `internal/scaffold/scaffold.go` + `templates.go` — reference pattern for template rendering
- **Deliverable:** New `internal/generate/` package (9 files), updated `cmd/moca/generate.go`, unit tests
- **Acceptance Criteria:**
  - `moca generate systemd` produces 5+ unit files with correct `[Unit]`, `[Service]`, `ExecStart`
  - `moca generate docker` produces parseable docker-compose.yml
  - `moca generate k8s` produces valid YAML with correct apiVersion/kind
  - `moca generate caddy` produces Caddyfile with domain, TLS, reverse_proxy directives
  - `moca generate env --format dotenv` produces valid KEY=VALUE from moca.yaml
  - Kafka-disabled config omits outbox and search-sync units
- **Risks / Unknowns:**
  - K8s HPA depends on Prometheus metrics adapter (MS-24) — generate with comment, not functional until then
  - Systemd socket activation for zero-downtime deferred to post-v1.0

### Task 2

- **Task ID:** MS-21-T2
- **Title:** Deployment Command Orchestration
- **Status:** Completed
- **Description:** Create `internal/deploy/` package implementing the deployment lifecycle. Implement all 6 `moca deploy` subcommands. `deploy setup` is a 14-step idempotent pipeline that calls into generate and backup packages. `deploy update` is a 4-phase atomic update with auto-rollback on failure. Deployment history is YAML-based in `.moca/deployments/`.

  **New files:**
  - `internal/deploy/types.go` — `DeploymentRecord` (ID format `dp_YYYYMMDD_HHMMSS`, status, duration, apps), `SetupOptions`, `UpdateOptions`, `RollbackOptions`, `PromoteOptions`
  - `internal/deploy/history.go` — YAML history in `.moca/deployments/history.yaml`, load/save/record. Snapshots in `.moca/deployments/{id}/` with moca.yaml + moca.lock + binary checksums
  - `internal/deploy/setup.go` — 14-step pipeline: validate requirements → switch to prod mode → build binaries + frontend → generate proxy config → generate process manager → Redis prod config → logrotate → backup schedule → firewall → fail2ban → TLS → start services → health check → record deployment
  - `internal/deploy/update.go` — 4-phase: Prepare (check config, resolve versions) → Backup (parallel per-site, verify) → Update (pull, build, migrate — auto-rollback on failure) → Activate (rolling restart, health check, record)
  - `internal/deploy/rollback.go` — Find target deployment by ID/step, restore config + DB from snapshot, restart services
  - `internal/deploy/promote.go` — Read source env config, backup target, copy lockfile, migrate target, restart
  - `internal/deploy/status.go` — Process states for 5 binaries, current deployment ID, uptime, sites count
  - `internal/deploy/deploy_test.go` — unit tests

  **Modified files:**
  - `cmd/moca/deploy.go` — replace 6 `newSubcommand` placeholders with real commands

- **Why this task exists:** This is the core user-facing value of MS-21 — turning Moca from a dev-only framework into a production-deployable system.
- **Dependencies:** MS-21-T1 (generate commands for setup steps 4-5), MS-21-T3 (backup for setup step 8, update Phase 2)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.8 (lines 1762-1928) — full spec for all 6 deploy commands
  - `MOCA_SYSTEM_DESIGN.md` §12.1-12.3 (lines 1773-1838) — deployment topology
  - `internal/process/supervisor.go` — process management
  - `cmd/moca/serve.go` — reference for multi-subsystem orchestration
  - `cmd/moca/build.go` — reference for exec.Command + spinner patterns
  - `docs/blocker-resolution-strategies.md` Blocker 3 (lines 180-267) — config sync during deploy update
- **Deliverable:** New `internal/deploy/` package (8 files), updated `cmd/moca/deploy.go`, `.moca/deployments/` directory structure
- **Acceptance Criteria:**
  - `moca deploy setup` on fresh Ubuntu creates working production deployment
  - `moca deploy setup --dry-run` prints all 14 steps without executing
  - `moca deploy update` with migration failure auto-rolls back
  - `moca deploy promote staging production --dry-run` shows environment diff
  - `moca deploy history` prints deployment table (ID, timestamp, status, duration, apps)
  - `moca deploy rollback` restores config + DB from snapshot
- **Risks / Unknowns:**
  - `deploy setup` touches system-level resources (systemd, firewall, fail2ban) — needs sudo detection
  - Rolling restart zero-downtime depends on systemd socket activation (deferred post-v1.0)
  - Multi-site parallel migration needs careful error handling for partial failure

### Task 3

- **Task ID:** MS-21-T3
- **Title:** Backup Automation: Schedule, Remote Storage, and Pruning
- **Status:** Completed
- **Description:** Extend `pkg/backup/` with S3 remote storage, cron scheduling, and retention-based pruning. Implement 4 remaining `moca backup` subcommands.

  **New files:**
  - `pkg/backup/remote.go` — S3 adapter wrapping `pkg/storage/s3.go` with backup-specific key naming (`{prefix}/backups/{site}/{filename}`). `Upload()`, `Download()`, `ListRemote()`.
  - `pkg/backup/schedule.go` — System crontab management (`crontab -l` / `crontab -`). `InstallCronSchedule()`, `RemoveCronSchedule()`, `ShowSchedule()`. Cron entry: `cd {projectRoot} && moca backup create --compress`.
  - `pkg/backup/prune.go` — Retention pruning. Classify backups by age (daily <7d, weekly <30d, monthly >30d). Keep newest N per category per `RetentionConfig`. Delete rest from local + remote. `--dry-run` support.

  **Modified files:**
  - `pkg/backup/types.go` — add `RemoteBackupInfo` (extends BackupInfo with RemoteKey, RemoteURL)
  - `cmd/moca/backup.go` — replace 4 placeholders at lines 26-29 with real commands

- **Why this task exists:** Production systems need scheduled backups with off-site storage and automatic cleanup. `deploy update` depends on backup upload for pre-update safety.
- **Dependencies:** None (can start immediately, parallel with T1)
- **Inputs / References:**
  - ROADMAP.md MS-21 deliverables 11-13 (lines 1107-1109)
  - `pkg/backup/` existing package (types.go, create.go, list.go)
  - `pkg/storage/s3.go` — existing S3 client (lines 1-123)
  - `internal/config/types.go` BackupConfig (lines 173-201) — Schedule, Retention, Destination
- **Deliverable:** 3 new files in `pkg/backup/`, extended types.go, updated `cmd/moca/backup.go`, unit + integration tests
- **Acceptance Criteria:**
  - `moca backup schedule --cron "0 2 * * *"` configures daily backups
  - `moca backup schedule --show` displays current schedule
  - `moca backup upload --destination s3://moca-backups` uploads to S3/MinIO
  - `moca backup download --backup-id bk_xxx --output /tmp/` downloads with checksum verification
  - `moca backup prune --dry-run` shows what would be deleted
  - Prune with retention {Daily: 7, Weekly: 4, Monthly: 3} keeps correct set
- **Risks / Unknowns:**
  - Crontab manipulation requires careful parsing to avoid corrupting existing user entries
  - Large backup uploads may need multipart (MinIO client handles this automatically)
  - Backup encryption deferred to MS-22

### Task 4

- **Task ID:** MS-21-T4
- **Title:** Integration Wiring and End-to-End Validation
- **Status:** Not Started
- **Description:** Wire generate, deploy, and backup packages together. Write integration tests (`//go:build integration`) validating all 8 roadmap acceptance criteria. Ensure cross-package calls work correctly (deploy setup → generate + backup schedule, deploy update → backup upload).

  **Wiring points:**
  - `deploy setup` step 4 → `generate.GenerateCaddy()` / `generate.GenerateNginx()`
  - `deploy setup` step 5 → `generate.GenerateSystemd()` / `generate.GenerateDocker()`
  - `deploy setup` step 8 → `backup.InstallCronSchedule()`
  - `deploy update` Phase 2 → `backup.Create()` + `backup.Upload()` (if S3 configured)
  - `deploy status` → checks generated config existence + process states

  **Integration tests:**
  - Generate systemd → validate unit file structure
  - Generate docker → validate with `docker compose config`
  - Generate k8s → validate YAML structure
  - Deploy setup --dry-run → all 14 steps print
  - Deploy update with simulated failure → auto-rollback
  - Backup upload/download roundtrip with MinIO
  - Backup prune with known dataset
  - Deployment history CRUD

- **Why this task exists:** Individual tasks produce isolated packages. This task validates they compose correctly and satisfies the end-to-end acceptance criteria.
- **Dependencies:** MS-21-T1, MS-21-T2, MS-21-T3
- **Inputs / References:**
  - ROADMAP.md MS-21 Acceptance Criteria (lines 1110-1118)
  - docker-compose.yml (MinIO service for integration tests)
- **Deliverable:** Integration test files, any cross-package wiring code, updated CLAUDE.md
- **Acceptance Criteria:**
  - All 8 roadmap acceptance criteria pass
  - Integration tests pass with `make test-integration`
  - No regressions in existing tests
- **Risks / Unknowns:**
  - Full `deploy setup` E2E on Ubuntu requires VM/container — scope integration tests to dry-run + template validation

## Recommended Execution Order

1. **MS-21-T1** (Generate Templates) and **MS-21-T3** (Backup Automation) — **in parallel**, no dependencies
2. **MS-21-T2** (Deploy Orchestration) — after T1 and T3 complete
3. **MS-21-T4** (Integration & E2E) — after all three complete

## Open Questions

1. **Supervisor support:** `cmd/moca/generate.go` lists a `supervisor` subcommand not in the design docs. Implement supervisord config generator or defer?
2. **moca-search-sync binary:** Design lists 5 process types including search-sync, but codebase embeds it in `moca-outbox`. Generate separate unit or treat as part of outbox?
3. **Deploy promote specifics:** Cross-environment promotion between two projects on different servers, or within a single project's staging/production configs?

## Out of Scope for This Milestone

- Blue/green deployment
- Canary deployment
- CI/CD generation (GitHub Actions, GitLab CI)
- Backup encryption (MS-22)
- Prometheus metrics for K8s HPA (MS-24)
- Systemd socket activation for zero-downtime restarts (post-v1.0)
