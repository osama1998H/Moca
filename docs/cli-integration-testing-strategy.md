# CLI Integration Testing Strategy

**Status:** Proposed
**Date:** 2026-04-05
**Author:** Osama Muhammed
**Version:** 1.0.0
**Companion Documents:** [MOCA_CLI_SYSTEM_DESIGN.md](../MOCA_CLI_SYSTEM_DESIGN.md), [ROADMAP.md](../ROADMAP.md)

---

## 1. Motivation

Unit tests validate individual functions in isolation. The existing 27 integration test files validate package-level behavior against real PostgreSQL, Redis, and Meilisearch instances. What they do **not** cover is the **developer experience as a sequence of CLI invocations** — the way a real person uses `moca` day to day.

This document defines a set of **story-driven CLI integration test flows**, each modeled as a chain of commands a developer would actually run. Every flow is a separate GitHub Actions workflow that can be triggered manually (`workflow_dispatch`) and optionally on a schedule. The flows exercise the binary end-to-end: they compile `moca`, invoke it as a subprocess, assert on stdout/stderr/exit codes, and inspect the resulting file system, database state, and service state.

### Goals

- Catch regressions that unit tests miss (flag parsing, context resolution, command chaining side effects).
- Validate the **contract between commands** — the output of one command is the precondition of the next.
- Test in both **predictable** environments (clean state, known seed data) and **unpredictable** ones (pre-existing sites, partial failures, resource limits, concurrent access).
- Keep each flow independently runnable so a contributor can re-run just the failing story.

---

## 2. Infrastructure Design

### 2.1 Infrastructure for Flow 01

Flow 01 uses **GitHub Actions `services:`** directly, matching the current CI setup in this repository. This keeps the first story self-contained and easy to debug.

`docker-compose.ci.yml` remains a sensible follow-up once there are multiple flows sharing the same infrastructure contract, but it is not introduced for Flow 01.

The base service set for Flow 01 is:

- PostgreSQL
- Redis
- Meilisearch

Additional services like Kafka or MinIO can be layered into later flows when they are actually exercised.

```yaml
# .github/workflows/cli-flow-01-project-bootstrap.yml
jobs:
  bootstrap-clean:
    services:
      postgres:
        image: postgres:16-alpine
        env:
          POSTGRES_DB: moca_test
          POSTGRES_USER: moca
          POSTGRES_PASSWORD: moca_test
      redis:
        image: redis:7-alpine
      meilisearch:
        image: getmeili/meilisearch:v1.12
        env:
          MEILI_MASTER_KEY: moca_test
          MEILI_NO_ANALYTICS: "true"
```

### 2.2 Reusable Workflow Skeleton

The workflow skeleton below is still useful as a design target, but the actual Flow 01 implementation stays in a single workflow file. Extracting a reusable workflow is deferred until at least a second CLI flow exists.

```yaml
# .github/workflows/_cli-test-setup.yml
name: CLI Test Setup (Reusable)

on:
  workflow_call:
    inputs:
      go_version:
        type: string
        default: "1.26.1"
      compose_profiles:
        type: string
        default: ""              # e.g. "kafka,storage"
      timeout_minutes:
        type: number
        default: 20

jobs:
  setup:
    runs-on: ubuntu-latest
    timeout-minutes: ${{ inputs.timeout_minutes }}

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: ${{ inputs.go_version }}
          cache: true

      - name: Build moca binary
        run: make build

      - name: Add bin to PATH
        run: echo "${{ github.workspace }}/bin" >> $GITHUB_PATH

      - name: Start infrastructure
        run: |
          PROFILES=""
          if [ -n "${{ inputs.compose_profiles }}" ]; then
            for p in $(echo "${{ inputs.compose_profiles }}" | tr ',' ' '); do
              PROFILES="$PROFILES --profile $p"
            done
          fi
          docker compose -f docker-compose.ci.yml up -d $PROFILES
          docker compose -f docker-compose.ci.yml ps

      - name: Wait for services
        run: |
          echo "Waiting for PostgreSQL..."
          until docker compose -f docker-compose.ci.yml exec -T postgres pg_isready -U moca; do sleep 1; done
          echo "Waiting for Redis..."
          until docker compose -f docker-compose.ci.yml exec -T redis redis-cli ping | grep -q PONG; do sleep 1; done
          echo "Waiting for Meilisearch..."
          until curl -sf http://localhost:7700/health; do sleep 1; done
          echo "All services ready."
```

### 2.3 Test Harness Shell Functions

Each flow sources a shared helper script that provides consistent assertion primitives:

```bash
# test/cli-harness.sh
# Sourced by every CLI integration flow.

set -euo pipefail

MOCA_BIN="${MOCA_BIN:-./bin/moca}"
TEST_PASS=0
TEST_FAIL=0
TEST_LOG="/tmp/cli-test-$(date +%s).log"

# ── Assertions ──────────────────────────────────────────────────────

assert_exit_code() {
  local description="$1" expected="$2"
  shift 2
  local actual=0
  "$@" >> "$TEST_LOG" 2>&1 || actual=$?
  if [ "$actual" -eq "$expected" ]; then
    echo "  ✓ $description (exit=$actual)"
    ((TEST_PASS++))
  else
    echo "  ✗ $description (expected exit=$expected, got exit=$actual)"
    echo "    Command: $*"
    tail -20 "$TEST_LOG" | sed 's/^/    | /'
    ((TEST_FAIL++))
  fi
}

assert_success() {
  assert_exit_code "$1" 0 "${@:2}"
}

assert_failure() {
  assert_exit_code "$1" 1 "${@:2}"
}

assert_stdout_contains() {
  local description="$1" pattern="$2"
  shift 2
  local output
  output=$("$@" 2>/dev/null) || true
  if echo "$output" | grep -qE "$pattern"; then
    echo "  ✓ $description (matched: $pattern)"
    ((TEST_PASS++))
  else
    echo "  ✗ $description (pattern '$pattern' not found in output)"
    echo "$output" | head -10 | sed 's/^/    | /'
    ((TEST_FAIL++))
  fi
}

assert_file_exists() {
  local description="$1" path="$2"
  if [ -e "$path" ]; then
    echo "  ✓ $description ($path exists)"
    ((TEST_PASS++))
  else
    echo "  ✗ $description ($path not found)"
    ((TEST_FAIL++))
  fi
}

assert_json_field() {
  local description="$1" field="$2" expected="$3"
  shift 3
  local output
  output=$("$@" --json 2>/dev/null) || true
  local actual
  actual=$(echo "$output" | jq -r "$field")
  if [ "$actual" = "$expected" ]; then
    echo "  ✓ $description ($field = $expected)"
    ((TEST_PASS++))
  else
    echo "  ✗ $description ($field: expected '$expected', got '$actual')"
    ((TEST_FAIL++))
  fi
}

assert_pg_query() {
  local description="$1" query="$2" expected="$3"
  local actual
  actual=$(PGPASSWORD=moca_test psql -h localhost -p 5433 -U moca -d moca_test \
    -t -A -c "$query" 2>/dev/null) || true
  if [ "$actual" = "$expected" ]; then
    echo "  ✓ $description"
    ((TEST_PASS++))
  else
    echo "  ✗ $description (expected '$expected', got '$actual')"
    ((TEST_FAIL++))
  fi
}

assert_redis_key_exists() {
  local description="$1" key="$2"
  local result
  result=$(redis-cli -h localhost -p 6380 EXISTS "$key" 2>/dev/null) || true
  if [ "$result" = "1" ]; then
    echo "  ✓ $description (key $key exists)"
    ((TEST_PASS++))
  else
    echo "  ✗ $description (key $key not found)"
    ((TEST_FAIL++))
  fi
}

# ── Summary ─────────────────────────────────────────────────────────

print_summary() {
  local total=$((TEST_PASS + TEST_FAIL))
  echo ""
  echo "═══════════════════════════════════════════════════"
  echo "  Results: $TEST_PASS/$total passed"
  if [ "$TEST_FAIL" -gt 0 ]; then
    echo "  ✗ $TEST_FAIL FAILURES"
    echo "  Full log: $TEST_LOG"
    echo "═══════════════════════════════════════════════════"
    exit 1
  else
    echo "  ✓ All passed"
    echo "═══════════════════════════════════════════════════"
    exit 0
  fi
}
```

---

## 3. Test Environment Categories

Every flow runs under one or both of these conditions:

### 3.1 Predictable Environment (Clean-Room)

- Fresh Docker containers with empty databases and caches.
- No pre-existing project directory — the flow starts from `moca init`.
- Deterministic seed data loaded by the flow itself.
- Network access only to localhost services.
- Fixed timezone (`UTC`), locale (`en_US.UTF-8`), and Go version.

**Purpose:** Catch regressions deterministically. Every run should produce identical results.

### 3.2 Unpredictable Environment (Chaos)

Applied as an overlay on top of a predictable run to simulate real-world conditions:

| Chaos Condition | How It Is Simulated |
|---|---|
| Pre-existing stale data | Run the flow twice without resetting containers between runs. The second run must handle "already exists" gracefully. |
| Partial infrastructure failure | Kill the Redis container mid-flow. Commands that depend on Redis should fail with clear error messages (not panics). Next command after Redis is restarted should recover. |
| Resource constraints | Run the Postgres container with `--memory=128m`. Verifies the CLI reports connection errors cleanly instead of crashing. |
| Concurrent access | Run two copies of the same flow in parallel against the same Postgres. Tests that distributed locks and schema creation handle races. |
| Filesystem permission issues | Make the `sites/` directory read-only mid-flow. Tests that `moca site create` reports a clear filesystem error. |
| Interrupted commands | Send `SIGTERM` to `moca serve` after 5 seconds. Tests graceful shutdown and PID file cleanup. |
| Stale PID files | Write a fake PID file before running `moca serve`. Tests that the CLI detects stale PIDs. |
| Clock skew | Set the container clock forward by 1 hour. Tests that backup timestamps and scheduler tick logic handle it. |

These chaos tests are run as separate jobs within the same workflow, gated behind a `chaos: true` input flag so they can be skipped during quick validation.

---

## 4. Developer Story Flows

Each flow below is a self-contained GitHub Actions workflow. The flow number, title, implementation status, and the services it requires are listed in the header.

---

### Flow 01 — First-Time Setup & Project Bootstrapping

**Story:** A new developer installs Moca and creates their first project with the core framework app.

**Services:** PostgreSQL, Redis, Meilisearch
**Status:** 🟢 Implementable now (MS-00 through MS-10 complete)

```
Workflow: .github/workflows/cli-flow-01-project-bootstrap.yml
Trigger:  workflow_dispatch, schedule (weekly)
```

**Command Chain:**

```bash
# Step 1 — Verify the binary works
moca version
moca version --json
moca version --short

# Step 1b — Verify shell completion generation
moca completion bash > /dev/null
moca completion zsh > /dev/null
moca completion fish > /dev/null

# Step 2 — Initialize a new project
mkdir /tmp/test-project && cd /tmp/test-project
moca init . --name test-erp \
  --db-host localhost --db-port 5433 \
  --redis-host localhost --redis-port 6380 \
  --minimal --no-kafka

# Step 3 — Verify project structure
test -f moca.yaml
test -f moca.lock
test -f go.work
test -d .moca
test -f apps/core/manifest.yaml

# Step 4 — Verify moca.yaml contents
moca config get project.name         # → "test-erp"
moca config get infrastructure.database.port  # → "5433"
moca config list --json

# Step 5 — Check system health
moca doctor
moca status
moca status --json
```

**Assertions:**

- `moca version` exits 0, output matches semver pattern.
- `moca version --json` keeps the existing JSON fields: `version`, `commit`, `build_date`, `go_version`, `os`, `arch`.
- `moca init` exits 0, creates all expected files/directories.
- `moca.yaml` contains correct project name and port overrides.
- `moca doctor` exits 0, reports PostgreSQL and Redis as healthy.
- `moca status --json` reports `active_site = "none"` immediately after bootstrap.

**Chaos Variant:**

- Run `moca init .` again in the same directory. Must exit with a clear "project already exists" error (not overwrite).
- Run `moca init .` with Postgres pointed at an unused port. Must exit non-zero with a connection error, not a panic.

---

### Flow 02 — Site Lifecycle (Create → Migrate → Use → Info → Drop)

**Story:** A developer creates their first tenant site, runs migrations, inspects it, and eventually tears it down.

**Services:** PostgreSQL, Redis, Meilisearch
**Status:** 🟢 Implementable now

```
Workflow: .github/workflows/cli-flow-02-site-lifecycle.yml
Trigger:  workflow_dispatch, schedule (weekly)
```

**Command Chain:**

```bash
# Precondition: Flow 01 has been run (or we run the init steps inline)
cd /tmp/test-project

# Step 1 — Create a site
moca site create acme.localhost \
  --admin-password secret123 \
  --timezone "UTC" \
  --language en

# Step 2 — Verify site exists
moca site list
moca site list --json
moca site info acme.localhost
moca site info acme.localhost --json

# Step 3 — Set as active site
moca site use acme.localhost
cat .moca/current_site   # → "acme.localhost"

# Step 4 — Run migrations
moca db migrate --site acme.localhost

# Step 5 — Verify database state
moca db diff --site acme.localhost   # Should show no pending changes

# Step 6 — Create a second site
moca site create globex.localhost --admin-password secret456

# Step 7 — List both sites
moca site list    # Should show 2 sites

# Step 8 — Disable and re-enable
moca site disable acme.localhost
moca site info acme.localhost    # Should show disabled
moca site enable acme.localhost
moca site info acme.localhost    # Should show enabled

# Step 9 — Drop a site
moca site drop globex.localhost --force --no-backup

# Step 10 — Verify cleanup
moca site list    # Should show 1 site
```

**Assertions:**

- PostgreSQL schema `acme_localhost` exists after create, gone after drop.
- `moca site list --json` returns valid JSON array with expected site names.
- `.moca/current_site` reflects the active site after `site use`.
- `db diff` reports zero pending changes after a fresh migration.
- `site drop --force` does not prompt, exits 0, removes schema and directory.

**Chaos Variant:**

- Create a site with the same name twice. Second call must fail gracefully.
- Drop a site that does not exist. Must fail with a descriptive error.
- Run `site create` while Postgres is at memory limit (128m). Verify timeout/error message quality.

---

### Flow 03 — App Development Workflow (New → Install → Migrate → Remove)

**Story:** A developer scaffolds a custom app, installs it on a site, verifies the schema changes, then removes it.

**Services:** PostgreSQL, Redis, Meilisearch
**Status:** 🟢 Implementable now

```
Workflow: .github/workflows/cli-flow-03-app-workflow.yml
Trigger:  workflow_dispatch, schedule (weekly)
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Scaffold a new app
moca app new my-crm --title "My CRM" --license MIT

# Step 2 — Verify scaffold structure
test -d apps/my-crm
test -f apps/my-crm/manifest.yaml
test -f apps/my-crm/go.mod

# Step 3 — Check go.work was updated
grep -q "my-crm" go.work

# Step 4 — Verify app appears in listing
moca app list
moca app list --json

# Step 5 — Install app on a site
moca app install my-crm --site acme.localhost

# Step 6 — Verify installation
moca app list --site acme.localhost   # Should include my-crm

# Step 7 — Run migration after install
moca db migrate --site acme.localhost

# Step 8 — App info
moca app info my-crm
moca app info my-crm --json

# Step 9 — Uninstall from site
moca app uninstall my-crm --site acme.localhost

# Step 10 — Remove from project
moca app remove my-crm --force

# Step 11 — Verify cleanup
test ! -d apps/my-crm
moca app list --json   # Should not include my-crm
```

**Assertions:**

- `app new` creates a valid Go module with `manifest.yaml`.
- `go.work` includes the new app directory after scaffold.
- `app install` exits 0, app appears in site-level listing.
- `app uninstall` removes app data, `app remove` deletes the directory.
- `go.work` no longer references the removed app.

---

### Flow 04 — Dev Server Lifecycle (Serve → Hot Reload → Stop → Restart)

**Story:** A developer starts the dev server, verifies hot reload detects file changes, stops it, and restarts it.

**Services:** PostgreSQL, Redis, Meilisearch
**Status:** 🟢 Implementable now (MS-10 complete)

```
Workflow: .github/workflows/cli-flow-04-dev-server.yml
Trigger:  workflow_dispatch, schedule (weekly)
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Start the dev server in background
moca serve --site acme.localhost &
SERVE_PID=$!
sleep 5

# Step 2 — Verify PID file exists
test -f .moca/process.pid

# Step 3 — Health check via HTTP
curl -sf http://localhost:8000/api/method/moca.ping

# Step 4 — Check status
moca status    # Should show server running

# Step 5 — Simulate a file change for hot reload
touch apps/core/core.go
sleep 3
# Verify the server restarted (check logs or PID changed)

# Step 6 — Graceful stop
moca stop
sleep 2

# Step 7 — Verify stopped
test ! -f .moca/process.pid
moca status    # Should show server stopped

# Step 8 — Restart
moca serve --site acme.localhost &
sleep 5
curl -sf http://localhost:8000/api/method/moca.ping
moca restart
sleep 5
curl -sf http://localhost:8000/api/method/moca.ping
moca stop
```

**Assertions:**

- `serve` starts within 5 seconds, PID file written.
- HTTP health endpoint responds 200.
- `stop` cleans up PID file, server process exits.
- `restart` results in a new PID; health endpoint responds after restart.

**Chaos Variant:**

- Kill the serve process with `kill -9`. Then run `moca serve`. Must detect stale PID and start cleanly.
- Send `SIGTERM` during startup. Must not leave dangling PID file.

---

### Flow 05 — Database Operations (Migrate → Seed → Snapshot → Rollback → Reset)

**Story:** A developer runs the full database management cycle: migration, seeding fixture data, taking a snapshot, rolling back, and resetting.

**Services:** PostgreSQL, Redis
**Status:** 🟡 Partially implementable (MS-11 adds `db seed`, `db snapshot`, `db rollback`, `db reset`)

```
Workflow: .github/workflows/cli-flow-05-database-ops.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Fresh migration
moca db migrate --site acme.localhost

# Step 2 — Check diff shows clean state
moca db diff --site acme.localhost

# Step 3 — Seed fixture data
moca db seed --site acme.localhost --app core

# Step 4 — Verify seed data exists
moca dev execute --site acme.localhost \
  "document.Count('User')"   # Should be > 0

# Step 5 — Snapshot current state
moca db snapshot --site acme.localhost --name "post-seed"

# Step 6 — Make a schema change (add a custom field via meta)
# (This would involve creating a DocType JSON and re-migrating)
moca db migrate --site acme.localhost

# Step 7 — Rollback
moca db rollback --site acme.localhost

# Step 8 — Verify rollback
moca db diff --site acme.localhost

# Step 9 — Nuclear reset
moca db reset --site acme.localhost --force

# Step 10 — Re-migrate from scratch
moca db migrate --site acme.localhost
```

**Assertions:**

- `db migrate` is idempotent — running it twice produces no errors.
- `db diff` shows zero pending changes after a clean migrate.
- `db seed` populates expected records.
- `db rollback` undoes the last migration batch.
- `db reset --force` drops and recreates the schema.

---

### Flow 06 — Backup & Restore Cycle

**Story:** A developer creates a backup of a production-like site, verifies the backup file, drops the site, restores from backup, and validates data integrity.

**Services:** PostgreSQL, Redis, MinIO (S3)
**Status:** 🟡 Partially implementable (MS-11 adds backup commands)

```
Workflow: .github/workflows/cli-flow-06-backup-restore.yml
Trigger:  workflow_dispatch
Compose profiles: storage
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Seed some data
moca db seed --site acme.localhost --app core
ORIGINAL_COUNT=$(moca dev execute --site acme.localhost "document.Count('User')")

# Step 2 — Create backup
moca backup create --site acme.localhost --with-files

# Step 3 — Verify backup exists
moca backup list --site acme.localhost
moca backup list --site acme.localhost --json
BACKUP_FILE=$(moca backup list --site acme.localhost --json | jq -r '.[0].path')

# Step 4 — Verify backup integrity
moca backup verify "$BACKUP_FILE"

# Step 5 — Upload to remote storage
moca backup upload "$BACKUP_FILE" --destination s3

# Step 6 — Drop the site
moca site drop acme.localhost --force --no-backup

# Step 7 — Restore from backup
moca site create acme.localhost --admin-password secret123
moca backup restore "$BACKUP_FILE" --site acme.localhost

# Step 8 — Verify data integrity
RESTORED_COUNT=$(moca dev execute --site acme.localhost "document.Count('User')")
test "$ORIGINAL_COUNT" = "$RESTORED_COUNT"

# Step 9 — Prune old backups
moca backup prune --site acme.localhost --keep 1
```

**Assertions:**

- Backup file is a valid archive (gzip/tar).
- `backup verify` exits 0.
- Restored site has identical record counts.
- `backup prune` reduces backup count.

---

### Flow 07 — User Management

**Story:** An admin creates users, assigns roles, tests password changes, disables/enables accounts.

**Services:** PostgreSQL, Redis
**Status:** 🟡 Partially implementable (MS-13 adds user commands beyond stubs)

```
Workflow: .github/workflows/cli-flow-07-user-management.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Set admin password
moca user set-admin-password --site acme.localhost --password new_admin_123

# Step 2 — Create a regular user
moca user add --site acme.localhost \
  --email dev@acme.com \
  --first-name Dev \
  --last-name User \
  --password user123

# Step 3 — List users
moca user list --site acme.localhost
moca user list --site acme.localhost --json

# Step 4 — Assign roles
moca user add-role --site acme.localhost --email dev@acme.com --role "System Manager"

# Step 5 — Change password
moca user set-password --site acme.localhost --email dev@acme.com --password new_pass

# Step 6 — Disable user
moca user disable --site acme.localhost --email dev@acme.com
moca user list --site acme.localhost --json   # Should show disabled=true

# Step 7 — Enable user
moca user enable --site acme.localhost --email dev@acme.com

# Step 8 — Remove role
moca user remove-role --site acme.localhost --email dev@acme.com --role "System Manager"

# Step 9 — Remove user
moca user remove --site acme.localhost --email dev@acme.com --force

# Step 10 — Verify removal
moca user list --site acme.localhost --json   # Should not contain dev@acme.com
```

**Assertions:**

- `user add` exits 0, user appears in listing.
- `user add-role` persists to the database (verifiable via `user list --json`).
- `user disable` / `user enable` toggle the user's active state.
- `user remove --force` deletes without prompt.

---

### Flow 08 — Configuration Management

**Story:** A developer manages config across environments, exports and imports config, and compares settings between dev and staging.

**Services:** PostgreSQL, Redis
**Status:** 🟡 Partially implementable (MS-11 adds config commands)

```
Workflow: .github/workflows/cli-flow-08-config-management.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Get/set values
moca config set maintenance_mode 1 --site acme.localhost
moca config get maintenance_mode --site acme.localhost   # → "1"

# Step 2 — Remove a key
moca config remove maintenance_mode --site acme.localhost
moca config get maintenance_mode --site acme.localhost   # Should error or return empty

# Step 3 — List all config
moca config list --site acme.localhost --json

# Step 4 — Export config
moca config export --site acme.localhost --format yaml > /tmp/config-acme.yaml
test -s /tmp/config-acme.yaml

# Step 5 — Create a second site for comparison
moca site create staging.localhost --admin-password secret123
moca config set maintenance_mode 0 --site staging.localhost

# Step 6 — Diff between sites
moca config diff --site acme.localhost --compare-site staging.localhost

# Step 7 — Import config
moca config import /tmp/config-acme.yaml --site staging.localhost

# Step 8 — Verify import
moca config list --site staging.localhost --json

# Cleanup
moca site drop staging.localhost --force --no-backup
```

**Assertions:**

- `config set` / `config get` round-trips a value.
- `config remove` actually removes the key.
- `config export` produces valid YAML.
- `config diff` exits 0 and shows differences between sites.
- `config import` applies all keys from the exported file.

---

### Flow 09 — Cache Operations

**Story:** A developer inspects cache state, clears specific caches, and pre-warms metadata caches.

**Services:** PostgreSQL, Redis
**Status:** 🟡 Future (MS-16 adds cache commands)

```
Workflow: .github/workflows/cli-flow-09-cache-ops.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Check cache stats
moca cache stats --site acme.localhost
moca cache stats --site acme.localhost --json

# Step 2 — Warm caches
moca cache warm --site acme.localhost

# Step 3 — Verify stats changed
moca cache stats --site acme.localhost --json

# Step 4 — Clear metadata cache only
moca cache clear-meta --site acme.localhost

# Step 5 — Clear sessions
moca cache clear-sessions --site acme.localhost

# Step 6 — Full cache clear
moca cache clear --site acme.localhost

# Step 7 — Verify Redis keys are gone
# (assert via redis-cli that the site's key namespace is empty)
```

**Assertions:**

- `cache stats` returns valid JSON with hit/miss counts.
- `cache warm` increases cached key count.
- `cache clear` removes all keys for the site in Redis.
- `cache clear-meta` only removes metadata keys, not session keys.

**Chaos Variant:**

- Kill Redis, run `cache clear`. Must fail with a clear connection error.
- Restart Redis, run `cache warm`. Must recover and populate.

---

### Flow 10 — Queue & Worker Management

**Story:** A developer starts workers, enqueues jobs, monitors queue depth, inspects failed jobs, and manages the dead letter queue.

**Services:** PostgreSQL, Redis
**Status:** 🟡 Future (MS-15/MS-16)

```
Workflow: .github/workflows/cli-flow-10-queue-workers.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Start workers
moca worker start --site acme.localhost --concurrency 2 &
sleep 3

# Step 2 — Verify worker status
moca worker status --site acme.localhost
moca worker status --site acme.localhost --json

# Step 3 — Check queue status
moca queue status --site acme.localhost

# Step 4 — Enqueue a test job (via dev execute)
moca dev execute --site acme.localhost \
  "queue.Enqueue('default', 'test_job', map[string]any{\"key\": \"value\"})"

# Step 5 — Monitor queue
sleep 2
moca queue list --site acme.localhost --status completed

# Step 6 — Scale workers
moca worker scale --site acme.localhost --concurrency 4
moca worker status --site acme.localhost   # Should show 4 workers

# Step 7 — Inspect a job
JOB_ID=$(moca queue list --site acme.localhost --json | jq -r '.[0].id')
moca queue inspect "$JOB_ID" --site acme.localhost

# Step 8 — Dead letter queue
moca queue dead-letter list --site acme.localhost

# Step 9 — Stop workers
moca worker stop --site acme.localhost
```

**Assertions:**

- Workers start and register within 3 seconds.
- `queue status` reports depths for each queue.
- Enqueued job transitions from pending → active → completed.
- `worker scale` adjusts the pool size at runtime.
- `worker stop` shuts down all workers cleanly.

---

### Flow 11 — Kafka Event Streaming

**Story:** A developer enables Kafka, verifies topic creation, publishes a test event, tails the topic, and checks consumer lag.

**Services:** PostgreSQL, Redis, Kafka
**Status:** 🟡 Future (MS-15/MS-16)

```
Workflow: .github/workflows/cli-flow-11-kafka-events.yml
Trigger:  workflow_dispatch
Compose profiles: kafka
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Re-init with Kafka enabled
moca config set infrastructure.kafka.enabled true
moca config set infrastructure.kafka.brokers '["localhost:9092"]'

# Step 2 — List topics (should include system topics)
moca events list-topics --site acme.localhost

# Step 3 — Publish a test event
moca events publish --site acme.localhost \
  --topic "acme.localhost.document" \
  --payload '{"doctype": "User", "name": "test", "event": "on_update"}'

# Step 4 — Tail events (run for 5 seconds)
timeout 5 moca events tail --site acme.localhost \
  --topic "acme.localhost.document" \
  --from-beginning || true

# Step 5 — Check consumer status
moca events consumer-status --site acme.localhost

# Step 6 — Replay events from start
moca events replay --site acme.localhost \
  --topic "acme.localhost.document" \
  --from-beginning --dry-run
```

**Assertions:**

- Topic list includes expected system topics.
- Published event appears in tail output.
- Consumer status shows lag metrics.
- `--dry-run` on replay does not actually re-process events.

**Chaos Variant:**

- Stop Kafka mid-flow. `events list-topics` must return a clean connection error.
- Restart Kafka. Verify `events tail` reconnects.

---

### Flow 12 — Search Index Management

**Story:** A developer rebuilds search indices, queries from the CLI, and verifies index status.

**Services:** PostgreSQL, Redis, Meilisearch
**Status:** 🟡 Future (MS-15/MS-16)

```
Workflow: .github/workflows/cli-flow-12-search-index.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Check index status
moca search status --site acme.localhost
moca search status --site acme.localhost --json

# Step 2 — Rebuild index for a specific doctype
moca search rebuild --site acme.localhost --doctype User

# Step 3 — Wait for indexing to complete
sleep 3

# Step 4 — Query from CLI
moca search query --site acme.localhost --doctype User --q "admin"

# Step 5 — Rebuild all indices
moca search rebuild --site acme.localhost --all

# Step 6 — Verify status after rebuild
moca search status --site acme.localhost --json
```

**Assertions:**

- `search status` returns index counts and health.
- `search rebuild` triggers Meilisearch task, completes without error.
- `search query` returns matching documents.

---

### Flow 13 — Infrastructure Generation

**Story:** A developer generates production infrastructure configs (Docker, systemd, Caddy, Kubernetes) and validates them.

**Services:** PostgreSQL, Redis
**Status:** 🟡 Future (MS-21)

```
Workflow: .github/workflows/cli-flow-13-infra-generation.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Generate Docker Compose
moca generate docker
test -f config/docker/docker-compose.yml
docker compose -f config/docker/docker-compose.yml config --quiet

# Step 2 — Generate systemd units
moca generate systemd
test -f config/systemd/moca-server.service
test -f config/systemd/moca-worker.service
test -f config/systemd/moca-scheduler.service

# Step 3 — Generate Caddy config
moca generate caddy
test -f config/caddy/Caddyfile

# Step 4 — Generate Kubernetes manifests
moca generate k8s
test -f config/k8s/deployment.yaml
test -f config/k8s/service.yaml

# Step 5 — Generate .env file
moca generate env
test -f .env

# Step 6 — Validate generated K8s manifests (if kubectl is available)
kubectl apply --dry-run=client -f config/k8s/ || true
```

**Assertions:**

- Each `generate` command creates the expected files.
- Docker Compose config passes `docker compose config` validation.
- Systemd unit files are valid INI format.
- Generated .env file includes all `${VAR}` references from `moca.yaml`.

---

### Flow 14 — Deployment Workflow (Setup → Update → Rollback)

**Story:** A DevOps engineer sets up production deployment, runs an update cycle, then rolls back.

**Services:** PostgreSQL, Redis, MinIO
**Status:** 🟡 Future (MS-21)

```
Workflow: .github/workflows/cli-flow-14-deploy-cycle.yml
Trigger:  workflow_dispatch
Compose profiles: storage
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Deploy setup
moca deploy setup --process-manager docker --proxy caddy

# Step 2 — Verify deployment status
moca deploy status
moca deploy status --json

# Step 3 — Simulate an update
moca deploy update --site acme.localhost

# Step 4 — Check deployment history
moca deploy history
moca deploy history --json

# Step 5 — Rollback
moca deploy rollback --site acme.localhost

# Step 6 — Verify rollback
moca deploy status --json
moca deploy history --json   # Should show rollback entry
```

**Assertions:**

- `deploy setup` is idempotent.
- `deploy update` creates a backup, runs migrations, restarts services.
- `deploy history` records each operation with timestamps.
- `deploy rollback` restores the previous deployment state.

---

### Flow 15 — Doctor & Diagnostics

**Story:** A developer runs `moca doctor` in various states — healthy, degraded (missing service), and broken (wrong config) — to verify the diagnostic quality.

**Services:** PostgreSQL, Redis, Meilisearch
**Status:** 🟢 Implementable now

```
Workflow: .github/workflows/cli-flow-15-doctor-diagnostics.yml
Trigger:  workflow_dispatch, schedule (weekly)
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Healthy state
moca doctor
moca doctor --json

# Step 2 — Degrade: stop Redis
docker compose -f docker-compose.ci.yml stop redis

# Step 3 — Doctor should report Redis as unhealthy
moca doctor
# Should exit non-zero with Redis connection failure

# Step 4 — Restart Redis
docker compose -f docker-compose.ci.yml start redis
sleep 3

# Step 5 — Doctor should recover
moca doctor

# Step 6 — Degrade: wrong Postgres password
moca config set infrastructure.database.password wrong_password
moca doctor   # Should report DB auth failure

# Step 7 — Fix config
moca config set infrastructure.database.password moca_test
moca doctor   # Should be healthy again
```

**Assertions:**

- `moca doctor` exits 0 when all services are healthy.
- `moca doctor` exits non-zero when a service is down, and the output names the failing service.
- `moca doctor --json` output includes per-service health status.
- After fixing the issue, `moca doctor` returns to healthy without restart.

---

### Flow 16 — Multitenancy Stress Test

**Story:** Create 10 sites rapidly, verify isolation, run migrations across all sites, then tear them all down.

**Services:** PostgreSQL, Redis, Meilisearch
**Status:** 🟡 Partially implementable (MS-12 adds full multitenancy)

```
Workflow: .github/workflows/cli-flow-16-multitenancy.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Create 10 sites in rapid succession
for i in $(seq 1 10); do
  moca site create "tenant${i}.localhost" --admin-password "pass${i}"
done

# Step 2 — List all sites
moca site list --json | jq length   # → 10 (plus any pre-existing)

# Step 3 — Migrate all sites
for i in $(seq 1 10); do
  moca db migrate --site "tenant${i}.localhost"
done

# Step 4 — Verify schema isolation
# Insert data into tenant1, verify it does not appear in tenant2
moca dev execute --site tenant1.localhost \
  "document.Insert('User', map[string]any{\"email\": \"only-in-t1@test.com\"})"
moca dev execute --site tenant2.localhost \
  "document.Count('User', map[string]any{\"email\": \"only-in-t1@test.com\"})"
# → Should be 0

# Step 5 — Concurrent operations
for i in $(seq 1 10); do
  moca cache warm --site "tenant${i}.localhost" &
done
wait   # All must succeed

# Step 6 — Teardown
for i in $(seq 1 10); do
  moca site drop "tenant${i}.localhost" --force --no-backup
done

moca site list --json | jq length   # → 0 (or pre-existing only)
```

**Assertions:**

- All 10 sites created without errors.
- Each site has its own PostgreSQL schema.
- Data in one tenant is not visible from another.
- Concurrent cache warm operations do not conflict.
- All sites drop cleanly.

---

### Flow 17 — Log & Monitor Commands

**Story:** A developer tails logs, searches for errors, exports a time range, and checks the live monitor.

**Services:** PostgreSQL, Redis
**Status:** 🟡 Future (MS-24)

```
Workflow: .github/workflows/cli-flow-17-log-monitor.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Start the server to generate logs
moca serve --site acme.localhost &
sleep 5

# Step 2 — Tail logs (5 seconds)
timeout 5 moca log tail --site acme.localhost || true

# Step 3 — Generate some traffic
for i in $(seq 1 20); do
  curl -sf http://localhost:8000/api/method/moca.ping > /dev/null
done

# Step 4 — Search logs
moca log search --site acme.localhost --pattern "ping"

# Step 5 — Export logs
moca log export --site acme.localhost --from "1 hour ago" --to "now" \
  --output /tmp/logs-export.json
test -s /tmp/logs-export.json

# Step 6 — Metrics dump
moca monitor metrics --site acme.localhost

# Step 7 — Audit log
moca monitor audit --site acme.localhost --limit 10

# Step 8 — Cleanup
moca stop
```

**Assertions:**

- `log tail` outputs log lines within 5 seconds.
- `log search` finds entries matching the pattern.
- `log export` produces a non-empty file.
- `monitor metrics` returns Prometheus-format metrics.

---

### Flow 18 — Build & Test Pipeline

**Story:** A developer builds all assets, runs tests, and generates a coverage report.

**Services:** PostgreSQL, Redis
**Status:** 🟡 Future (MS-25)

```
Workflow: .github/workflows/cli-flow-18-build-test.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Build Go server binary
moca build server

# Step 2 — Build app verification
moca build app --app core

# Step 3 — Run framework tests
moca test run --site acme.localhost

# Step 4 — Generate coverage
moca test coverage --site acme.localhost --output /tmp/coverage.html
test -f /tmp/coverage.html

# Step 5 — Load test fixtures
moca test fixtures --site acme.localhost --app core

# Step 6 — Generate factory data
moca test factory --site acme.localhost --doctype User --count 100
```

**Assertions:**

- `build server` produces a binary.
- `test run` exits 0 (all tests pass).
- `test coverage` generates an HTML report.
- `test factory` creates the specified number of records.

---

### Flow 19 — Translation Workflow

**Story:** A developer exports translatable strings, imports translations, checks coverage, and compiles for production.

**Services:** PostgreSQL, Redis
**Status:** 🟡 Future (MS-20)

```
Workflow: .github/workflows/cli-flow-19-translations.yml
Trigger:  workflow_dispatch
```

**Command Chain:**

```bash
cd /tmp/test-project

# Step 1 — Export strings
moca translate export --app core --output /tmp/strings.csv
test -s /tmp/strings.csv

# Step 2 — Check translation status
moca translate status --site acme.localhost

# Step 3 — Import translations
moca translate import --site acme.localhost \
  --language ar --file /tmp/translations-ar.csv

# Step 4 — Compile translations
moca translate compile --site acme.localhost

# Step 5 — Verify status updated
moca translate status --site acme.localhost --json
```

---

### Flow 20 — Full End-to-End Developer Journey

**Story:** This is the "golden path" — a developer goes from zero to a running multi-app project with users, data, and a backup, exercising the most common commands in sequence.

**Services:** PostgreSQL, Redis, Meilisearch
**Status:** 🟡 Partial (core init/site/app/serve flows work; db/backup/user commands need MS-11+)

```
Workflow: .github/workflows/cli-flow-20-golden-path.yml
Trigger:  workflow_dispatch, schedule (nightly)
```

**Command Chain:**

```bash
# ── Phase 1: Bootstrap ──
moca version
mkdir /tmp/golden && cd /tmp/golden
moca init . --name golden-erp --db-host localhost --db-port 5433 \
  --redis-host localhost --redis-port 6380 --minimal --no-kafka
moca doctor

# ── Phase 2: First Site ──
moca site create production.localhost --admin-password admin123
moca site use production.localhost
moca db migrate --site production.localhost

# ── Phase 3: Custom App ──
moca app new invoicing --title "Invoicing" --license MIT
moca app install invoicing --site production.localhost
moca db migrate --site production.localhost

# ── Phase 4: Users ──
moca user set-admin-password --site production.localhost --password admin_pass
moca user add --site production.localhost \
  --email accountant@company.com --first-name Jane --last-name Doe --password user123
moca user add-role --site production.localhost \
  --email accountant@company.com --role "System Manager"

# ── Phase 5: Dev Server ──
moca serve --site production.localhost &
sleep 5
curl -sf http://localhost:8000/api/method/moca.ping
moca status

# ── Phase 6: Data & Cache ──
moca db seed --site production.localhost --app core
moca cache warm --site production.localhost
moca search rebuild --site production.localhost --all

# ── Phase 7: Backup ──
moca backup create --site production.localhost
moca backup list --site production.localhost

# ── Phase 8: Config Snapshot ──
moca config export --site production.localhost --format yaml > /tmp/golden-config.yaml

# ── Phase 9: Cleanup ──
moca stop
moca site drop production.localhost --force --no-backup
```

**Assertions:**

- Every command in the chain exits 0.
- The final `site drop` leaves a clean state.
- This flow must pass end-to-end without manual intervention.

---

## 5. Workflow File Template

Below is a complete example workflow for **Flow 01** that can be copied and adapted for other flows:

```yaml
# .github/workflows/cli-flow-01-project-bootstrap.yml
name: "CLI Flow 01: Project Bootstrap"

on:
  workflow_dispatch:
    inputs:
      chaos:
        description: "Run chaos variants"
        type: boolean
        default: false
  schedule:
    - cron: "0 6 * * 1"   # Weekly on Monday at 06:00 UTC

permissions:
  contents: read

env:
  MOCA_DB_HOST: localhost
  MOCA_DB_PORT: 5433
  MOCA_DB_USER: moca
  MOCA_DB_PASSWORD: moca_test
  MOCA_DB_NAME: moca_test
  MOCA_REDIS_HOST: localhost
  MOCA_REDIS_PORT: 6380
  MOCA_MEILI_HOST: localhost
  MOCA_MEILI_PORT: 7700
  MOCA_MEILI_KEY: moca_test

jobs:
  bootstrap-clean:
    name: "Clean-Room Bootstrap"
    runs-on: ubuntu-latest
    timeout-minutes: 15

    services:
      postgres:
        image: postgres:16-alpine
        env:
          POSTGRES_USER: moca
          POSTGRES_PASSWORD: moca_test
          POSTGRES_DB: moca_test
        ports: ["5433:5432"]
        options: >-
          --health-cmd "pg_isready -U moca"
          --health-interval 5s
          --health-retries 10

      redis:
        image: redis:7-alpine
        ports: ["6380:6379"]
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 5s
          --health-retries 10

      meilisearch:
        image: getmeili/meilisearch:v1.12
        env:
          MEILI_MASTER_KEY: moca_test
          MEILI_NO_ANALYTICS: "true"
        ports: ["7700:7700"]
        options: >-
          --health-cmd "curl -sf http://localhost:7700/health"
          --health-interval 5s
          --health-retries 10

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.26.1"
          cache: true

      - name: Build moca
        run: make build

      - name: Add bin to PATH
        run: echo "${{ github.workspace }}/bin" >> $GITHUB_PATH

      - name: Source test harness
        run: echo "source ${{ github.workspace }}/test/cli-harness.sh" >> $GITHUB_ENV

      - name: "Test: moca version"
        run: |
          source test/cli-harness.sh
          echo "── moca version ──"
          assert_success "version exits 0" moca version
          assert_json_field "version --json exposes version field" '.version' '0.1.0' moca version
          assert_stdout_contains "version --short is semver" '^v[0-9]+\.[0-9]+\.[0-9]+' moca version --short
          print_summary

      - name: "Test: moca init"
        run: |
          source test/cli-harness.sh
          mkdir -p /tmp/test-project && cd /tmp/test-project
          echo "── moca init ──"
          assert_success "init creates project" moca init . \
            --name test-erp \
            --db-host localhost --db-port 5433 \
            --redis-host localhost --redis-port 6380 \
            --minimal --no-kafka
          assert_file_exists "moca.yaml created" /tmp/test-project/moca.yaml
          assert_file_exists "moca.lock created" /tmp/test-project/moca.lock
          assert_file_exists "go.work created" /tmp/test-project/go.work
          assert_file_exists ".moca dir created" /tmp/test-project/.moca
          assert_file_exists "core app present" /tmp/test-project/apps/core/manifest.yaml
          print_summary

      - name: "Test: moca config"
        run: |
          source test/cli-harness.sh
          cd /tmp/test-project
          echo "── config commands ──"
          assert_stdout_contains "project name correct" "test-erp" moca config get project.name
          assert_success "config list --json" moca config list --json
          print_summary

      - name: "Test: moca doctor"
        run: |
          source test/cli-harness.sh
          cd /tmp/test-project
          echo "── doctor ──"
          assert_success "doctor healthy" moca doctor
          assert_stdout_contains "doctor --json reports PostgreSQL" '"name": "PostgreSQL reachable"' moca doctor --json
          assert_success "status" moca status
          assert_json_field "status active site none" '.active_site' 'none' moca status
          print_summary

      - name: Upload test logs
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: cli-flow-01-logs
          path: /tmp/cli-test-*.log
          retention-days: 7

  bootstrap-chaos:
    name: "Chaos: Double Init & Degraded Infra"
    if: inputs.chaos == true
    needs: bootstrap-clean
    runs-on: ubuntu-latest
    timeout-minutes: 15

    services:
      postgres:
        image: postgres:16-alpine
        env:
          POSTGRES_USER: moca
          POSTGRES_PASSWORD: moca_test
          POSTGRES_DB: moca_test
        ports: ["5433:5432"]
        options: >-
          --health-cmd "pg_isready -U moca"
          --health-interval 5s
          --health-retries 10

      redis:
        image: redis:7-alpine
        ports: ["6380:6379"]
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 5s
          --health-retries 10

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.26.1"
          cache: true

      - name: Build moca
        run: make build

      - name: Add bin to PATH
        run: echo "${{ github.workspace }}/bin" >> $GITHUB_PATH

      - name: "Chaos: Double init"
        run: |
          source test/cli-harness.sh
          mkdir -p /tmp/chaos-project && cd /tmp/chaos-project
          assert_success "first init" moca init . --name chaos-test \
            --db-host localhost --db-port 5433 \
            --redis-host localhost --redis-port 6380 --minimal --no-kafka
          assert_failure "second init fails gracefully" moca init . --name chaos-test \
            --db-host localhost --db-port 5433 \
            --redis-host localhost --redis-port 6380 --minimal --no-kafka
          print_summary

      - name: Upload test logs
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: cli-flow-01-chaos-logs
          path: /tmp/cli-test-*.log
          retention-days: 7
```

---

## 6. Flow Implementation Priority

Flows are grouped by when they become fully testable based on the roadmap.

### Tier 1 — Implementable Now (MS-00 through MS-10)

| Flow | Name | Schedule |
|------|------|----------|
| 01 | Project Bootstrap | Weekly + on dispatch |
| 02 | Site Lifecycle | Weekly + on dispatch |
| 03 | App Development Workflow | Weekly + on dispatch |
| 04 | Dev Server Lifecycle | Weekly + on dispatch |
| 15 | Doctor & Diagnostics | Weekly + on dispatch |

### Tier 2 — Implementable After MS-11/MS-12/MS-13

| Flow | Name | Depends On |
|------|------|------------|
| 05 | Database Operations | MS-11 |
| 06 | Backup & Restore | MS-11 |
| 07 | User Management | MS-13 |
| 08 | Configuration Management | MS-11 |
| 16 | Multitenancy Stress | MS-12 |
| 20 | Golden Path (partial) | MS-11, MS-13 |

### Tier 3 — Implementable After MS-15/MS-16

| Flow | Name | Depends On |
|------|------|------------|
| 09 | Cache Operations | MS-16 |
| 10 | Queue & Worker Management | MS-15, MS-16 |
| 11 | Kafka Event Streaming | MS-15, MS-16 |
| 12 | Search Index Management | MS-15, MS-16 |

### Tier 4 — Implementable After MS-20+

| Flow | Name | Depends On |
|------|------|------------|
| 13 | Infrastructure Generation | MS-21 |
| 14 | Deployment Workflow | MS-21 |
| 17 | Log & Monitor | MS-24 |
| 18 | Build & Test Pipeline | MS-25 |
| 19 | Translation Workflow | MS-20 |

---

## 7. Naming Conventions & Organization

```
.github/workflows/
  ci.yml                                    # Existing: unit + integration tests
  release.yml                               # Existing: release builds
  nightly.yml                               # Existing: nightly builds
  benchmark.yml                             # Existing: performance benchmarks
  cli-flow-01-project-bootstrap.yml         # NEW: Flow 01
  cli-flow-02-site-lifecycle.yml            # NEW: Flow 02
  cli-flow-03-app-workflow.yml              # NEW: Flow 03
  ...
  cli-flow-20-golden-path.yml              # NEW: Flow 20

test/
  cli-harness.sh                            # NEW: shared assertion functions
```

---

## 8. CI Dashboard & Reporting

Each flow uploads its log file as a GitHub Actions artifact. For a unified view:

- **Badge per flow:** Add a status badge for each workflow to the project README or a `docs/cli-test-status.md` page.
- **Summary comment:** For the Golden Path flow (Flow 20), post a GitHub Actions Job Summary with a table of pass/fail per phase.
- **Failure alerts:** Configure GitHub Actions notifications to the team's Slack/Discord channel when any scheduled flow fails.

Example badge syntax:

```markdown
| Flow | Status |
|------|--------|
| 01 — Bootstrap | ![Flow 01](https://github.com/osama1998H/moca/actions/workflows/cli-flow-01-project-bootstrap.yml/badge.svg) |
| 02 — Site Lifecycle | ![Flow 02](https://github.com/osama1998H/moca/actions/workflows/cli-flow-02-site-lifecycle.yml/badge.svg) |
| ... | ... |
| 20 — Golden Path | ![Flow 20](https://github.com/osama1998H/moca/actions/workflows/cli-flow-20-golden-path.yml/badge.svg) |
```

---

## 9. Guidelines for Writing New Flows

When adding a new flow, follow these rules:

1. **One story, one workflow.** Each flow tests a coherent developer journey, not a grab-bag of commands.
2. **Self-contained setup.** A flow must not depend on another flow having run first. If it needs a project and site, create them inline.
3. **Assert on behavior, not output format.** Use `--json` for machine-readable assertions. Avoid brittle regex on human-readable output.
4. **Include a chaos variant.** At minimum, test the "do it twice" case (idempotency) and the "service is down" case (error quality).
5. **Tag the implementation status.** Use 🟢 (implementable), 🟡 (partially/future), 🔴 (blocked) in the flow header.
6. **Keep flows under 15 minutes.** If a flow takes longer, split it.
7. **Upload logs as artifacts.** Always include the `upload-artifact` step with `if: always()`.
8. **Use `workflow_dispatch`.** Every flow must be manually triggerable for debugging.

---

## 10. Relationship to Existing Tests

This strategy complements, not replaces, the existing test layers:

| Layer | What It Tests | Where It Lives | When It Runs |
|---|---|---|---|
| **Unit tests** | Individual functions, pure logic | `*_test.go` (no build tags) | Every push (CI) |
| **Package integration tests** | Package behavior against real services | `*_integration_test.go` (build tag: `integration`) | Every push (CI) |
| **CLI flow tests** (this document) | End-to-end developer stories as command chains | `.github/workflows/cli-flow-*.yml` | Manual dispatch + weekly/nightly schedule |
| **Benchmarks** | Performance regression detection | `*_bench_test.go` | PR comments (benchmark.yml) |

The CLI flow tests are the **outermost ring** — they exercise the compiled binary the same way a developer would. If a unit test passes but a flow test fails, the bug is in the wiring between components (flag parsing, context resolution, service initialization, error propagation).
