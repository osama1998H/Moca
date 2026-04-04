# MS-16 — CLI Queue, Events, Search, Monitor, and Log Commands Plan

## Milestone Summary

- **ID:** MS-16
- **Name:** CLI Queue, Events, Search, Monitor, and Log Commands
- **Roadmap Reference:** ROADMAP.md → MS-16 section (lines 894-930)
- **Goal:** Implement CLI commands for managing queues, events, search, monitoring, and logs — providing operators with full visibility and control over the async systems built in MS-15.
- **Why it matters:** MS-15 introduced Redis Streams workers, Kafka/Redis events, transactional outbox, cron scheduler, and Meilisearch sync. Without CLI tooling, operators have no way to inspect, debug, or manage these systems.
- **Position in roadmap:** Order #17 of 30 milestones (0-indexed from MS-00)
- **Upstream dependencies:** MS-15 (background jobs, scheduler, events, search sync), MS-07 (CLI foundation)
- **Downstream dependencies:** None directly — MS-17 (React frontend) is independent. MS-16 completes the backend operational tooling surface.

## Vision Alignment

MS-16 is a pure operational tooling milestone. Moca's design philosophy is "batteries included" — a complete framework where a single binary manages the entire application lifecycle. The CLI is the primary management interface (no web admin panel for infrastructure). Every async subsystem from MS-15 needs corresponding CLI commands for operators to inspect state, diagnose issues, rebuild indexes, tail events, and manage process lifecycle.

This milestone transforms MS-15's infrastructure from "running in the background" to "observable and controllable." Without it, debugging a stuck job, checking consumer lag, or rebuilding a corrupted search index would require raw Redis/Kafka CLI tools and direct Meilisearch API calls — defeating the "single tool" design goal.

The 8 command groups (queue, events, search, monitor, log, worker, scheduler, cache) follow the exact patterns established in MS-07 (CLI foundation) and MS-09 (site/app commands). All command groups already exist as placeholder files from earlier milestones; this milestone replaces every placeholder with a real implementation.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| ROADMAP.md | MS-16 | 894-930 | Milestone definition, acceptance criteria, risks |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.4a Scheduler Management | 1220-1336 | All 8 scheduler command specs |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.15 Queue & Events | 2611-2839 | Queue (8) and Events (5) command specs |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.14 Monitoring & Diagnostics | 2484-2607 | Monitor command specs |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.17 Search Management | 2978-3033 | Search command specs |
| MOCA_CLI_SYSTEM_DESIGN.md | §4.2.19 Log Management | 3065-3122 | Log command specs |
| MOCA_SYSTEM_DESIGN.md | §5.2 Queue Layer | 1073-1115 | Redis Streams key structure |
| MOCA_SYSTEM_DESIGN.md | §6 Kafka Event Streaming | 1089-1260 | Event topics, consumer groups |
| MOCA_SYSTEM_DESIGN.md | §6.5 Kafka-Optional Architecture | 1235-1260 | Fallback behavior |
| docs/moca-cross-doc-mismatch-report.md | MISMATCH-006 | 220-247 | Missing search-sync CLI surface |
| docs/blocker-resolution-strategies.md | Blocker 4 | 271-387 | Kafka-optional feature gates |

## Research Notes

No web research was needed. All implementation patterns are established in the codebase:
- `cmd/moca/cache.go` demonstrates the fully implemented command pattern (flags, RunE, output modes, spinners, CLIError)
- `pkg/queue/` provides all Redis Streams key naming and job structures
- `pkg/events/` provides Producer interface and DocumentEvent structure
- `pkg/search/` provides Client, Indexer, QueryService, and Syncer
- `internal/serve/subsystems.go` shows how worker/scheduler subsystems are constructed

## Current State

All 8 command groups already exist as placeholder files in `cmd/moca/`:
- `queue.go` — 5 subcommands + 3 dead-letter subcommands (all `newSubcommand()` placeholders)
- `events.go` — 5 subcommands (all placeholders)
- `search.go` — 3 subcommands (all placeholders)
- `monitor.go` — 3 subcommands (all placeholders, `live` TUI deferred)
- `log.go` — 3 subcommands (all placeholders)
- `worker.go` — 4 subcommands (all placeholders)
- `scheduler.go` — 8 subcommands (all placeholders)
- `cache.go` — 2 subcommands (**already implemented**: `clear` and `warm`)

**Total: ~32 placeholder commands to implement (cache is done).**

## Milestone Plan

### Task 1

- **Task ID:** MS-16-T1
- **Title:** Queue Commands and Shared Service Helpers
- **Status:** Completed
- **Description:**
  Replace all 8 placeholder subcommands in `cmd/moca/queue.go` (5 top-level + 3 dead-letter) with full implementations. Each command interacts directly with Redis Streams via `svc.Redis.Queue` using the key patterns from `pkg/queue/`:
  - `queue status` — `XLEN` + `XPENDING` on all streams per site/queue-type, table output with `--json` and `--watch` (polling loop)
  - `queue list` — `XRANGE` on target stream with `--queue`, `--status`, `--site`, `--limit` filters
  - `queue inspect JOB_ID` — Read specific message by ID, display full payload/metadata
  - `queue retry JOB_ID` — Re-enqueue failed job via `queue.Producer.Enqueue`; `--all-failed` + `--force`
  - `queue purge` — `XTRIM` on streams; requires `--queue` or `--all`; `--force` skips confirmation
  - `queue dead-letter list` — `XRANGE` on DLQ stream with `--site`, `--limit`
  - `queue dead-letter retry JOB_ID` — Move DLQ entry back to original queue; `--all` + `--force`
  - `queue dead-letter purge` — Truncate DLQ; `--older-than` filter; `--force`

  Also add shared helpers to `cmd/moca/services.go`:
  - `listActiveSites(ctx, svc)` — enumerate active sites from DB (reused by T2-T5)
  - `newQueueProducer(svc, site)` — construct queue producer for re-enqueue operations

- **Why this task exists:** Queue commands are the most direct operational need — debugging stuck/failed jobs, inspecting DLQ, and purging stale entries. The service helpers established here are reused by all subsequent tasks.
- **Dependencies:** None (first task)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.15 lines 2611-2742 (8 queue command specs)
  - `cmd/moca/queue.go` (current placeholder state)
  - `cmd/moca/cache.go` (reference implementation pattern)
  - `pkg/queue/job.go` (Job struct, key naming: `StreamKey`, `DLQKey`)
  - `pkg/queue/worker.go` (consumer group names, stream structure)
  - `cmd/moca/services.go` (service construction helpers)
- **Deliverable:**
  - Modified `cmd/moca/queue.go` with 8 implemented commands
  - Extended `cmd/moca/services.go` with shared helpers
  - New `cmd/moca/queue_test.go` verifying subcommand structure, flags, and handler wiring
- **Acceptance Criteria:**
  - `moca queue status` shows queue depths per queue type (default/long/critical) per site
  - `moca queue list --queue default --site acme.localhost` lists pending jobs
  - `moca queue inspect <JOB_ID>` displays job payload and retry history
  - `moca queue retry <JOB_ID>` re-enqueues a failed job
  - `moca queue dead-letter list` shows DLQ entries
  - All commands support `--json` output mode
  - Destructive commands (`purge`) require `--force` or interactive confirmation
- **Risks / Unknowns:**
  - `queue inspect` needs to scan multiple streams if queue type is unknown — may need a "search all streams" approach
  - `--watch` mode for `queue status` requires terminal cursor management

---

### Task 2

- **Task ID:** MS-16-T2
- **Title:** Worker and Scheduler Management Commands
- **Status:** Completed
- **Description:**
  Replace all 12 placeholder subcommands across `cmd/moca/worker.go` (4) and `cmd/moca/scheduler.go` (8).

  **Worker commands (4):**
  - `worker start` — Construct `WorkerSubsystem` from `internal/serve/subsystems.go` and run it. `--foreground` keeps attached; without it, re-exec as background process with PID file.
  - `worker stop` — Send SIGTERM to worker PID (read from `{project}/.moca/worker.pid`)
  - `worker status` — Query Redis `XINFO GROUPS` and `XINFO CONSUMERS` per stream to report active consumers, idle time, pending counts
  - `worker scale QUEUE COUNT` — Write desired scale to Redis key `moca:worker:scale:{queue}` for the running pool to pick up, or advise restart

  **Scheduler commands (8):**
  - `scheduler start` — Construct scheduler subsystem with leader election, run with PID file
  - `scheduler stop` — SIGTERM to scheduler PID
  - `scheduler status` — Read `moca:scheduler:leader` key, list registered entries with next fire times
  - `scheduler enable --site` — Remove site from `moca:scheduler:disabled-sites` set
  - `scheduler disable --site` — Add site to `moca:scheduler:disabled-sites` set
  - `scheduler trigger EVENT --site` — Manually enqueue a scheduled event job
  - `scheduler list-jobs --site` — Read registered cron entries from Redis hash `moca:scheduler:entries:{site}`
  - `scheduler purge-jobs` — Purge scheduler queue stream; `--force` required

  Add process lifecycle utilities to `services.go`: `writePIDFile()`, `readPIDFile()`, `stopProcess()`.

- **Why this task exists:** Workers and scheduler are long-running processes that need start/stop/status lifecycle management. The scheduler's 8 commands provide granular control per acceptance criteria.
- **Dependencies:** MS-16-T1 (uses `listActiveSites` helper)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.4a lines 1220-1336 (8 scheduler commands)
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.15 (worker commands context)
  - `cmd/moca/worker.go`, `cmd/moca/scheduler.go` (current placeholders)
  - `internal/serve/subsystems.go` (WorkerSubsystem, scheduler construction)
  - `internal/process/` (existing supervisor/PID file management)
  - `pkg/queue/worker.go` (WorkerPool, consumer group structure)
- **Deliverable:**
  - Modified `cmd/moca/worker.go` with 4 implemented commands
  - Modified `cmd/moca/scheduler.go` with 8 implemented commands
  - Extended `cmd/moca/services.go` with PID file and process lifecycle helpers
  - `cmd/moca/worker_test.go` and `cmd/moca/scheduler_test.go`
- **Acceptance Criteria:**
  - `moca worker start --foreground` starts workers and blocks
  - `moca worker status` shows active consumers per queue
  - `moca worker scale default 4` sets desired scale for default queue
  - `moca scheduler list-jobs --site acme.localhost` shows registered cron jobs
  - `moca scheduler enable/disable --site acme.localhost` toggles scheduler for that site
  - `moca scheduler trigger hourly --site acme.localhost` fires the hourly event
  - All commands support `--json` output
- **Risks / Unknowns:**
  - Process daemonization on macOS/Linux differs from production systemd usage — `--foreground` is the primary mode, background launch is convenience only
  - `worker scale` runtime adjustment may require a worker pool enhancement to poll Redis for scale instructions
  - `scheduler list-jobs` depends on the scheduler persisting its entries to Redis — if this isn't done in MS-15, a small scheduler enhancement is needed

---

### Task 3

- **Task ID:** MS-16-T3
- **Title:** Search and Monitor Commands
- **Status:** Completed
- **Description:**
  Replace all 3 placeholders in `cmd/moca/search.go` and 2 of 3 in `cmd/moca/monitor.go` (`monitor live` stays as a "deferred" placeholder).

  **Search commands (3):**
  - `search rebuild` — Core acceptance criteria command. Creates `search.Client` + `search.Indexer`. For each searchable doctype: calls `EnsureIndex`, queries all rows from `tab_{doctype}` via `svc.DB.ForSite()`, converts to `map[string]any`, calls `IndexDocuments` in batches of `--batch-size` (default 1000). `--site` or `--all-sites`, `--doctype` optional. Progress bar for TTY.
  - `search status` — Lists Meilisearch indexes for the site prefix using SDK's `GetIndexes()`. Displays: index name, document count, index size, last update. `--site`, `--json`.
  - `search query QUERY` — Uses `search.QueryService.Search()` from `pkg/search/query.go`. `--site`, `--doctype` required. `--limit` default 10. Table output: name, score, key fields. `--json`.

  **Monitor commands (2 active):**
  - `monitor metrics` — HTTP GET to running server's `/metrics` endpoint (Prometheus format). Parses and displays key metrics or outputs raw text. Error with fix if server not running.
  - `monitor audit` — SQL query against `tab_audit_log` in site DB. Filters: `--site` (required), `--user`, `--doctype`, `--action`, `--since` (duration like "1h" or absolute timestamp), `--limit` default 50. Table: timestamp, user, action, doctype, docname. `--json`.

  **Monitor live (deferred):**
  - Replace placeholder with informative error: "TUI dashboard deferred — use `moca monitor metrics` or `moca queue status --watch`."

  May need to extend `pkg/search/client.go` with a `ListIndexes(ctx, prefix)` method if the existing narrow interface doesn't expose it.

- **Why this task exists:** `search rebuild` is a key acceptance criterion and essential for disaster recovery (corrupted index). `monitor audit` provides operational audit trail access. These are the highest-value operational commands after queue management.
- **Dependencies:** None (can run parallel with T1/T2)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.17 lines 2978-3033 (search commands)
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.14 lines 2592-2607 (monitor audit)
  - `cmd/moca/search.go`, `cmd/moca/monitor.go` (current placeholders)
  - `pkg/search/indexer.go` lines 40-131 (EnsureIndex, IndexDocuments)
  - `pkg/search/query.go` lines 49-120 (Search method)
  - `pkg/search/client.go` (Client wrapper, may need ListIndexes)
  - `cmd/moca/cache.go` lines 194-278 (cache warm pattern — iterates doctypes from DB)
- **Deliverable:**
  - Modified `cmd/moca/search.go` with 3 implemented commands
  - Modified `cmd/moca/monitor.go` with 2 implemented + 1 deferred placeholder
  - Possibly extended `pkg/search/client.go` with `ListIndexes` method
  - `cmd/moca/search_test.go` and `cmd/moca/monitor_test.go`
- **Acceptance Criteria:**
  - `moca search rebuild --doctype SalesOrder --site acme.localhost` rebuilds the SalesOrder index from database
  - `moca search status --site acme.localhost` shows index document counts and sizes
  - `moca search query "widget" --site acme.localhost --doctype Product` returns matching results
  - `moca monitor audit --user admin --since 1h --site acme.localhost` shows recent audit entries
  - `moca monitor live` prints a "deferred" message with alternatives
  - All commands support `--json` output
- **Risks / Unknowns:**
  - `search rebuild` on large tables needs streaming/cursor-based DB reads to avoid OOM — use `LIMIT/OFFSET` or cursor pagination
  - `search status` may need SDK extension for `GetIndexes` with prefix filtering
  - `monitor audit` assumes `tab_audit_log` table exists and has a known schema from core migrations

---

### Task 4

- **Task ID:** MS-16-T4
- **Title:** Events Commands
- **Status:** Completed
- **Description:**
  Replace all 5 placeholders in `cmd/moca/events.go`.

  - `events list-topics` — Kafka enabled: create franz-go admin client, call `ListTopics`. Kafka disabled: list known topic constants from `pkg/events/event.go` (TopicDocumentEvents, TopicAuditLog, etc.) with a note they map to Redis pub/sub channels. `--json`, `--verbose` (partition count, retention).
  - `events tail TOPIC` — Subscribe in real-time. Kafka: create consumer with `kgo.ConsumeTopics(topic)` and a throwaway consumer group. Redis: `svc.Redis.PubSub.Subscribe(topic)`. Post-filter by `--site`, `--doctype`, `--event` on `DocumentEvent` fields. `--format` (json/short). `--since` for Kafka offset seeking. Handle Ctrl+C via signal context.
  - `events publish TOPIC` — Construct `DocumentEvent` from `--payload` JSON string or `--file` JSON file. Fill `--site`, `--doctype`, `--event` flags. Publish via `events.Producer`.
  - `events consumer-status` — Kafka: franz-go admin `DescribeConsumerGroups` showing group, topic, partition, offset, lag. `--group` filter. Redis: "consumer groups not applicable with Redis pub/sub" message. `--json`.
  - `events replay` — Kafka only (error if disabled). Seek to `--since` timestamp, read through `--until`, re-publish. `--consumer` resets consumer group offsets. `--dry-run` counts without publishing. Dangerous — requires `--force` confirmation.

- **Why this task exists:** Event observability is critical for debugging the document lifecycle pipeline (doc save -> outbox -> event -> search sync). `events tail` is a key acceptance criterion. `events replay` enables recovery from consumer failures.
- **Dependencies:** MS-16-T1 (uses shared service helpers)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.15 lines 2744-2839 (events commands)
  - `cmd/moca/events.go` (current placeholder)
  - `pkg/events/event.go` lines 14-27 (event types, topic constants)
  - `pkg/events/producer.go` (Producer interface, NewProducer factory)
  - `docs/blocker-resolution-strategies.md` lines 271-387 (Kafka-optional behavior)
  - `MOCA_SYSTEM_DESIGN.md` §6.5 lines 1235-1260 (Kafka-optional architecture)
- **Deliverable:**
  - Modified `cmd/moca/events.go` with 5 implemented commands
  - `cmd/moca/events_test.go`
- **Acceptance Criteria:**
  - `moca events list-topics` shows all Moca-managed topics with metadata
  - `moca events tail moca.doc.events --site acme.localhost` streams events in real-time
  - `moca events publish moca.doc.events --site acme.localhost --doctype Product --event doc.created --payload '{...}'` publishes a test event
  - `moca events consumer-status` shows consumer group lag per partition (Kafka mode)
  - `moca events replay --since "2h" --dry-run` reports count of events that would be replayed
  - Commands gracefully handle Kafka-disabled mode with informative messages
  - All commands support `--json` output
- **Risks / Unknowns:**
  - `events replay` with Kafka timestamp offset seeking is noted as a risk in ROADMAP.md — needs careful implementation with `--dry-run` safety
  - `events tail` with Redis pub/sub only receives messages published after subscription — no historical replay
  - Franz-go admin client API may differ from the producer client already used — verify API compatibility

---

### Task 5

- **Task ID:** MS-16-T5
- **Title:** Log Commands
- **Status:** Not Started
- **Description:**
  Replace all 3 placeholders in `cmd/moca/log.go`.

  - `log tail` — Read log files from project's log directory (`{project}/logs/` or configured path). Seek to end and poll for new lines (100ms interval). Parse each line as structured JSON (from `slog.NewJSONHandler`). Filter: `--process` (server/worker/scheduler), `--level` (error/warn/info/debug), `--site`, `--request-id`. `--json` outputs raw structured lines. `--no-color` disables ANSI. Handle Ctrl+C via signal context. `--follow` flag for continuous mode (default behavior).
  - `log search` — Open log files and search with filters. Parse JSON lines, apply `--process`, `--level`, `--since`, `--site`, `--request-id` filters. `--limit` caps output (default 100). Sort by timestamp descending.
  - `log export` — `--since` required. Read log files in time range. `--until` defaults to now. `--format` (json/text). `--output` file path (defaults to stdout). `--compress` wraps in gzip. `--process` and `--site` filter.

  Determine log directory convention: `{project-root}/logs/{process}.log` (e.g., `logs/server.log`, `logs/worker.log`, `logs/scheduler.log`). Add log directory config to `moca.yaml` as optional override.

- **Why this task exists:** Log commands complete the operational visibility surface. `log tail --follow --filter "level=error"` is a key acceptance criterion. Without CLI log access, operators must SSH and manually find/parse log files.
- **Dependencies:** None (fully independent)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.19 lines 3065-3122 (log commands)
  - `cmd/moca/log.go` (current placeholder)
  - `pkg/observe/` (structured logging setup, slog configuration)
  - `internal/config/types.go` (may need LogConfig addition)
- **Deliverable:**
  - Modified `cmd/moca/log.go` with 3 implemented commands
  - `cmd/moca/log_test.go`
  - Possibly extended `internal/config/types.go` with `LogConfig` struct
- **Acceptance Criteria:**
  - `moca log tail --follow --level error` streams error-level log lines in real-time
  - `moca log search --level error --since 1h --site acme.localhost` returns matching log entries
  - `moca log export --since "2024-01-01" --format json --output /tmp/logs.json` exports filtered logs
  - `moca log tail --process worker` shows only worker process logs
  - All commands support `--json` output
  - `--no-color` disables ANSI formatting
- **Risks / Unknowns:**
  - Log directory convention must be established and consistent with how `moca serve`, `moca worker start`, and `moca scheduler start` write logs (ROADMAP risk: "centralized vs per-process")
  - Log rotation handling — if a log file is rotated while tailing, need to detect and reopen
  - Large log files for `log search` and `log export` need streaming reads, not full file loads

## Recommended Execution Order

1. **MS-16-T1** (Queue + Service Helpers) — establishes Redis interaction patterns and shared helpers used by all subsequent tasks
2. **MS-16-T2** (Worker + Scheduler) — builds on T1's helpers, adds process lifecycle management
3. **MS-16-T3** (Search + Monitor) — search rebuild is a key acceptance criterion; monitor audit is high-value
4. **MS-16-T4** (Events) — includes the highest-risk command (`events replay`); benefits from patterns established in T1-T3
5. **MS-16-T5** (Log) — fully self-contained, lowest coupling, lowest risk

Note: T3, T4, and T5 are independent of each other and could be parallelized after T1 completes.

## Open Questions

- **Log directory convention:** Where do Moca processes write their log files? Need to establish `{project}/logs/{process}.log` convention and ensure `moca serve`, worker, and scheduler subsystems write there. This may require a small MS-15 follow-up.
- **Scheduler entry persistence:** Does the MS-15 scheduler persist its registered cron entries to Redis? If not, `scheduler list-jobs` and `scheduler status` need a small scheduler enhancement to write entries to `moca:scheduler:entries:{site}` on startup.
- **Worker scale runtime adjustment:** Can the MS-15 WorkerPool dynamically adjust consumer count? If not, `worker scale` may need to advise a restart rather than live-adjust.
- **`monitor audit` table schema:** Does `tab_audit_log` exist from core migrations? Need to verify the table and column names.

## Out of Scope for This Milestone

- `moca monitor live` TUI dashboard (deferred per roadmap scope)
- `moca cache stats` (cache commands `clear` and `warm` are already implemented; `stats` may be added as a follow-up)
- `moca search-sync start/stop/status` (identified in MISMATCH-006 but not in MS-16 scope)
- Systemd unit generation for worker/scheduler/search-sync processes
- WebSocket real-time event streaming to browser
- Log aggregation infrastructure (centralized logging service)
