# Moca CLI — System Design & Architecture

**Status:** Proposed
**Date:** 2026-03-29
**Author:** Osama Muhammed
**Version:** 1.0.0
**Companion Document:** [MOCA_SYSTEM_DESIGN.md](./MOCA_SYSTEM_DESIGN.md)

---

## 1. Executive Summary

The Moca CLI is the **single command-line interface** for developing, managing, deploying, and operating applications built on the Moca framework. It replaces the concept of Frappe's "bench" — but with a fundamentally different design philosophy.

**One-sentence definition:**
`moca` is a statically-compiled, context-aware CLI tool built in Go that manages the full lifecycle of Moca projects, sites, apps, infrastructure, and operations through a unified command tree with built-in diagnostics, developer experience tooling, and production orchestration.

### Why Not Just Copy Bench?

Frappe's bench is a Python wrapper that shells out to system commands, manages virtualenvs, generates Procfiles, and delegates most real work to the Frappe framework. It works, but has well-documented pain points:

| Bench Pain Point | Moca CLI Solution |
|---|---|
| Python CLI managing a Python framework (fragile deps, virtualenv issues) | Single Go binary — zero runtime dependencies |
| No version pinning for apps (`get-app` always clones HEAD) | Built-in app version constraints, lockfiles, and release channels |
| Cryptic error messages; failures dump raw stack traces | Context-aware errors with suggested fixes and diagnostic commands |
| Production setup requires many manual steps (nginx, supervisor, ssl) | `moca deploy` — one command, declarative config, idempotent |
| `bench update` is all-or-nothing; partial failures leave dirty state | Atomic operations with automatic rollback on failure |
| No built-in health checks or diagnostics | `moca doctor` — comprehensive system health analysis |
| Three separate Redis instances (cache, queue, socketio) | Unified Redis with logical separation (key prefixes + streams) |
| No horizontal scaling support | `moca worker scale` + `moca generate k8s` — built-in orchestration for multi-process deployment |
| Module caching bugs after app changes | No module cache — Go binaries + metadata registry, no stale state |
| Whitespace in paths breaks commands | Robust path handling throughout |

### Design Principles

1. **Single binary, zero dependencies.** `moca` is distributed as one compiled Go binary. No Python, no pip, no virtualenv, no Node.js required on the host.
2. **Context-aware.** The CLI detects whether it's inside a project directory, which site is active, and what environment (dev/staging/prod) it's targeting.
3. **Idempotent operations.** Every command that changes state can be run repeatedly without causing harm.
4. **Explicit over implicit.** No hidden globals, no magic environment detection. When `moca` needs to know something, it reads from a well-defined config file or explicit flag.
5. **Progressive disclosure.** Simple tasks are simple commands. Complex operations have subcommands and flags, but sensible defaults make the common path short.
6. **Diagnostics-first.** Every failure suggests the next step. `moca doctor` can diagnose any state.

---

## 2. Architecture Overview

### 2.1 How Bench Works (For Contrast)

```
bench (Python CLI)
  │
  ├── shells out to → git, pip, npm, yarn, redis-cli, mysql/psql
  ├── generates → Procfile, nginx.conf, supervisor.conf
  ├── manages → Python virtualenv (env/), node_modules
  ├── delegates to → frappe.commands.* (framework CLI)
  └── orchestrates → honcho (dev), supervisor/systemd (prod)
```

Bench is essentially a **shell script organizer** written in Python. It doesn't have a deep understanding of the framework — it coordinates external tools.

### 2.2 How Moca CLI Works

```
moca (single Go binary)
  │
  ├── EMBEDS ──────────────────────────────────────────────────────────┐
  │   ├── Project Manager     (init, config, lockfile)                │
  │   ├── Site Manager        (create, drop, migrate, backup)         │
  │   ├── App Manager         (new, get, install, remove, publish)    │
  │   ├── Schema Engine       (migrate, diff, snapshot, seed)         │
  │   ├── Process Supervisor  (start, stop, restart, scale)           │
  │   ├── Service Checker     (doctor, status, health)                │
  │   ├── Deploy Engine       (deploy, rollback, promote)             │
  │   ├── Config Manager      (get, set, diff, export, import)        │
  │   ├── Infra Generator     (caddy, systemd, docker, k8s)          │
  │   ├── Dev Toolkit         (console, shell, watch, test, bench)    │
  │   ├── Backup Engine       (backup, restore, schedule, verify)     │
  │   └── Extension Manager   (plugin hooks, custom commands)         │
  └────────────────────────────────────────────────────────────────────┘
       │              │              │              │
       ▼              ▼              ▼              ▼
   PostgreSQL       Redis         Kafka        Meilisearch
                                                   │
                                               MinIO / S3
```

The key difference: Moca CLI **embeds** the logic rather than shelling out. It speaks PostgreSQL wire protocol directly, manages Redis connections natively, understands Kafka topics, and generates configs from Go templates — all within a single binary.

### 2.3 CLI Internal Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│  moca binary                                                      │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │  Command Layer (cobra)                                        │ │
│  │                                                               │ │
│  │  moca init    moca site    moca app    moca serve            │ │
│  │  moca deploy  moca doctor  moca db     moca config           │ │
│  │  moca backup  moca worker  moca test   moca generate         │ │
│  │  ...                                                          │ │
│  └──────────────────────┬───────────────────────────────────────┘ │
│                          │                                         │
│  ┌──────────────────────▼───────────────────────────────────────┐ │
│  │  Context Resolver                                             │ │
│  │                                                               │ │
│  │  ┌─────────────┐ ┌──────────────┐ ┌───────────────────────┐ │ │
│  │  │  Project     │ │  Environment │ │  Active Site           │ │ │
│  │  │  Detector    │ │  Resolver    │ │  Resolver              │ │ │
│  │  │  (moca.yaml) │ │  (dev/prod)  │ │  (flag/env/config)    │ │ │
│  │  └─────────────┘ └──────────────┘ └───────────────────────┘ │ │
│  └──────────────────────┬───────────────────────────────────────┘ │
│                          │                                         │
│  ┌──────────────────────▼───────────────────────────────────────┐ │
│  │  Service Layer                                                │ │
│  │                                                               │ │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌───────────┐ │ │
│  │  │  Project    │ │  Site      │ │  App       │ │  Schema   │ │ │
│  │  │  Service    │ │  Service   │ │  Service   │ │  Service  │ │ │
│  │  └────────────┘ └────────────┘ └────────────┘ └───────────┘ │ │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌───────────┐ │ │
│  │  │  Process   │ │  Deploy    │ │  Backup    │ │  Config   │ │ │
│  │  │  Service   │ │  Service   │ │  Service   │ │  Service  │ │ │
│  │  └────────────┘ └────────────┘ └────────────┘ └───────────┘ │ │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌───────────┐ │ │
│  │  │  Infra     │ │  Health    │ │  Dev       │ │  Ext.     │ │ │
│  │  │  Service   │ │  Service   │ │  Service   │ │  Service  │ │ │
│  │  └────────────┘ └────────────┘ └────────────┘ └───────────┘ │ │
│  └──────────────────────┬───────────────────────────────────────┘ │
│                          │                                         │
│  ┌──────────────────────▼───────────────────────────────────────┐ │
│  │  Driver Layer                                                 │ │
│  │                                                               │ │
│  │  PostgreSQL    Redis    Kafka    Meilisearch    S3/MinIO     │ │
│  │  Driver        Driver   Driver   Driver         Driver       │ │
│  │  (pgx)         (go-     (franz-  (meilisearch   (minio-go)  │ │
│  │                redis)   go)      -go)                        │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │  Output Layer                                                 │ │
│  │                                                               │ │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌─────────────────┐ │ │
│  │  │  TTY     │ │  JSON    │ │  Table   │ │  Progress       │ │ │
│  │  │  (color, │ │  (--json │ │  (--table│ │  (bars, spinners│ │ │
│  │  │  emoji)  │ │  output) │ │  output) │ │  for long ops)  │ │ │
│  │  └──────────┘ └──────────┘ └──────────┘ └─────────────────┘ │ │
│  └──────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
```

---

## 3. Project Structure Managed by the CLI

When you run `moca init`, the CLI creates this structure:

```
my-project/                          ← project root
├── moca.yaml                        ← project manifest (replaces bench's implicit structure)
├── moca.lock                        ← lockfile for app versions (NEW — not in bench)
├── apps/                            ← installed Moca applications
│   ├── core/                        ← framework core app (always present)
│   ├── crm/                         ← example: CRM app
│   └── accounting/                  ← example: accounting app
├── sites/                           ← all site data
│   ├── common_site_config.yaml      ← shared config across all sites
│   ├── acme.localhost/
│   │   ├── site_config.yaml         ← per-site config
│   │   ├── private/                 ← private files (attachments)
│   │   ├── public/                  ← public files (images, assets)
│   │   └── backups/                 ← site backups
│   └── globex.localhost/
│       ├── site_config.yaml
│       ├── private/
│       ├── public/
│       └── backups/
├── config/                          ← generated infrastructure configs
│   ├── caddy/                       ← reverse proxy config
│   ├── systemd/                     ← systemd unit files
│   ├── redis/                       ← Redis config
│   ├── kafka/                       ← Kafka topic configs
│   └── docker/                      ← Docker Compose files
├── logs/                            ← all log files
│   ├── moca-server.log
│   ├── moca-worker.log
│   └── moca-scheduler.log
├── storage/                         ← local file storage (dev mode)
├── desk/                            ← React frontend (scaffolded by moca init)
│   ├── package.json                 ← depends on @moca/desk (GitHub Packages)
│   ├── index.html                   ← SPA entry point
│   ├── vite.config.ts               ← uses mocaDeskPlugin() from @moca/desk/vite
│   ├── tsconfig.json                ← composite project references
│   ├── tsconfig.app.json            ← React app config (includes .moca-extensions.ts)
│   ├── tsconfig.node.json           ← Node build tools config
│   ├── .moca-extensions.ts          ← auto-generated by moca build desk (gitignored)
│   ├── .gitignore
│   └── src/
│       ├── main.tsx                 ← calls createDeskApp().mount("#root")
│       └── overrides/
│           ├── index.ts             ← project-level override entry point
│           └── theme.ts             ← theme/branding overrides
├── go.work                          ← Go workspace: composes framework + installed apps
└── .moca/                           ← CLI state directory
    ├── current_site                  ← active site pointer
    ├── environment                   ← dev/staging/prod
    ├── process.pid                   ← PID file for dev server
    └── cache/                        ← CLI-level caches
```

### 3.1 Project Manifest — `moca.yaml`

```yaml
# moca.yaml — the single source of truth for a Moca project
project:
  name: my-erp
  version: "1.0.0"

# Framework version constraint
moca: ">=1.0.0, <2.0.0"

# Installed applications with version constraints
apps:
  core:
    source: builtin
  crm:
    source: github.com/moca-apps/crm
    version: "~1.2.0"                    # semver range
    branch: main                          # optional: pin to branch
    ref: "a1b2c3d"                        # optional: pin to exact commit
  accounting:
    source: github.com/moca-apps/accounting
    version: "^2.0.0"
  custom-hr:
    source: ./local-apps/custom-hr       # local path for development
    version: "*"

# Infrastructure configuration
infrastructure:
  database:
    driver: postgres
    host: localhost
    port: 5432
    system_db: moca_system
    pool_size: 25

  redis:
    host: localhost
    port: 6379
    db_cache: 0
    db_queue: 1
    db_session: 2
    db_pubsub: 3

  kafka:
    enabled: true
    brokers:
      - localhost:9092
    # Set false for small deployments that don't need event streaming
    # Redis pub/sub will be used as fallback

  search:
    engine: meilisearch
    host: localhost
    port: 7700
    api_key: "${MOCA_MEILI_KEY}"

  storage:
    driver: s3                            # "local" for development
    endpoint: localhost:9000
    bucket: moca-files
    access_key: "${MOCA_S3_ACCESS_KEY}"
    secret_key: "${MOCA_S3_SECRET_KEY}"

# Development settings
development:
  port: 8000
  workers: 2
  auto_reload: true
  desk_dev_server: true                   # run React dev server with HMR
  desk_port: 3000

# Production settings
production:
  port: 443
  workers: auto                           # auto = num_cpu * 2
  tls:
    provider: acme                        # automatic Let's Encrypt
    email: admin@example.com
  proxy:
    engine: caddy                         # caddy or nginx
  process_manager: systemd                # systemd or docker
  log_level: warn

# Staging settings (optional — extends production with overrides)
staging:
  inherits: production                    # starts from production settings
  port: 8443
  log_level: info
  tls:
    provider: acme
    email: admin@example.com

# Scheduler configuration
scheduler:
  enabled: true
  tick_interval: 60s

# Backup configuration
backup:
  schedule: "0 2 * * *"                   # daily at 2 AM
  retention:
    daily: 7
    weekly: 4
    monthly: 12
  destination:
    driver: s3
    bucket: moca-backups
    prefix: "${PROJECT_NAME}/"
  encrypt: true
  encryption_key: "${MOCA_BACKUP_KEY}"
```

### 3.2 Lockfile — `moca.lock`

```yaml
# Auto-generated by `moca app resolve`. Do not edit manually.
# This file ensures reproducible installs across environments.
generated_at: "2026-03-29T14:30:00Z"
moca_version: "1.0.0"

apps:
  core:
    version: "1.0.0"
    source: builtin
    checksum: "sha256:abc123..."

  crm:
    version: "1.2.3"
    source: github.com/moca-apps/crm
    ref: "a1b2c3d4e5f6"
    checksum: "sha256:def456..."
    dependencies:
      - core: ">=1.0.0"

  accounting:
    version: "2.1.0"
    source: github.com/moca-apps/accounting
    ref: "f6e5d4c3b2a1"
    checksum: "sha256:789abc..."
    dependencies:
      - core: ">=1.0.0"
      - crm: ">=1.0.0"
```

---

## 4. Complete Command Reference

### 4.1 Command Tree Overview

```
moca
├── init                              # Initialize a new Moca project
├── version                           # Show Moca CLI and framework version
├── doctor                            # Diagnose system health
├── status                            # Show project/site/service status
│
├── site                              # Site management
│   ├── create                        # Create a new site
│   ├── drop                          # Delete a site
│   ├── list                          # List all sites
│   ├── use                           # Set active site
│   ├── info                          # Show site details
│   ├── browse                        # Open site in browser
│   ├── reinstall                     # Reset site to fresh state
│   ├── migrate                       # Run pending migrations
│   ├── enable                        # Enable a disabled site
│   ├── disable                       # Disable a site (maintenance mode)
│   ├── rename                        # Rename a site
│   └── clone                         # Clone a site (schema + data)
│
├── app                               # Application management
│   ├── new                           # Scaffold a new Moca app
│   ├── get                           # Download and install an app
│   ├── remove                        # Remove an app from project
│   ├── install                       # Install an app on a site
│   ├── uninstall                     # Uninstall an app from a site
│   ├── list                          # List apps (project or site level)
│   ├── update                        # Update apps (all or specific)
│   ├── resolve                       # Resolve and lock dependency versions
│   ├── publish                       # Publish app to registry
│   ├── info                          # Show app manifest details
│   ├── diff                          # Show changes since last install
│   └── pin                           # Pin app to exact version/commit
│
├── serve                             # Start the development server
├── start                             # Alias for `serve` (bench compat)
├── stop                              # Stop all running Moca processes
├── restart                           # Restart all running Moca processes
│
├── worker                            # Background worker management
│   ├── start                         # Start background workers
│   ├── stop                          # Stop background workers
│   ├── status                        # Show worker pool status
│   └── scale                         # Adjust worker pool size at runtime
│
├── scheduler                         # Scheduler management
│   ├── start                         # Start the scheduler process
│   ├── stop                          # Stop the scheduler
│   ├── status                        # Show scheduler status
│   ├── enable                        # Enable scheduler for a site
│   ├── disable                       # Disable scheduler for a site
│   ├── trigger                       # Manually trigger a scheduled event
│   ├── list-jobs                     # List registered scheduled jobs
│   └── purge-jobs                    # Purge pending jobs from queue
│
├── db                                # Database operations
│   ├── console                       # Open interactive psql session
│   ├── migrate                       # Run pending schema migrations
│   ├── rollback                      # Rollback last migration batch
│   ├── diff                          # Show schema diff (meta vs actual DB)
│   ├── snapshot                      # Save current schema as snapshot
│   ├── seed                          # Load seed/fixture data
│   ├── trim-tables                   # Remove orphaned columns
│   ├── trim-database                 # Remove orphaned tables
│   ├── export-fixtures               # Export data as fixture files
│   └── reset                         # Drop and recreate site schema
│
├── backup                            # Backup operations
│   ├── create                        # Backup a site (or all sites)
│   ├── restore                       # Restore a site from backup
│   ├── list                          # List available backups
│   ├── schedule                      # Configure automated backups
│   ├── verify                        # Verify backup integrity
│   ├── upload                        # Upload backup to remote storage
│   ├── download                      # Download backup from remote storage
│   └── prune                         # Delete old backups per retention policy
│
├── config                            # Configuration management
│   ├── get                           # Get a config value
│   ├── set                           # Set a config value
│   ├── remove                        # Remove a config key
│   ├── list                          # List all effective config (merged)
│   ├── diff                          # Compare config between environments
│   ├── export                        # Export full config as YAML/JSON
│   ├── import                        # Import config from file
│   └── edit                          # Open config in $EDITOR
│
├── deploy                            # Deployment operations
│   ├── setup                         # One-command production setup
│   ├── update                        # Production update (backup → pull → migrate → restart)
│   ├── rollback                      # Rollback to previous deployment
│   ├── promote                       # Promote staging to production
│   ├── status                        # Show deployment status
│   └── history                       # Show deployment history
│
├── generate                          # Infrastructure config generation
│   ├── caddy                         # Generate Caddy reverse proxy config
│   ├── nginx                         # Generate NGINX config (for those who prefer it)
│   ├── systemd                       # Generate systemd unit files
│   ├── docker                        # Generate Docker Compose files
│   ├── k8s                           # Generate Kubernetes manifests
│   ├── supervisor                    # Generate supervisor config (legacy compat, not a supported process manager)
│   └── env                           # Generate .env file from moca.yaml
│
├── dev                               # Developer tools
│   ├── console                       # Interactive Go REPL with framework loaded
│   ├── shell                         # Open a shell with Moca env vars set
│   ├── execute                       # Run a one-off Go function/expression
│   ├── request                       # Make an HTTP request as a user
│   ├── bench                         # Run microbenchmarks on queries/operations
│   ├── profile                       # Profile a request or operation
│   ├── watch                         # Watch and rebuild assets on change
│   └── playground                    # Start interactive API playground (Swagger/GraphiQL)
│
├── test                              # Testing
│   ├── run                           # Run tests (Go tests + framework tests)
│   ├── run-ui                        # Run frontend/Playwright tests
│   ├── coverage                      # Generate test coverage report
│   ├── fixtures                      # Load test fixture data
│   └── factory                       # Generate test data from MetaType definitions
│
├── build                             # Build operations
│   ├── desk                          # Build React Desk frontend
│   ├── portal                        # Build portal/website assets
│   ├── assets                        # Build all static assets
│   ├── app                           # Verify an app's Go code compiles
│   └── server                        # Compile server binary with all installed apps
│
├── desk                              # Desk frontend management
│   ├── install                       # Install desk npm dependencies
│   ├── update                        # Update @moca/desk + regenerate extensions
│   └── dev                           # Start Vite dev server for desk development
│
├── user                              # User management
│   ├── add                           # Create a new user on a site
│   ├── remove                        # Remove a user from a site
│   ├── set-password                  # Set user password
│   ├── set-admin-password            # Set Administrator password
│   ├── add-role                      # Assign a role to a user
│   ├── remove-role                   # Remove a role from a user
│   ├── list                          # List all users on a site
│   ├── disable                       # Disable a user account
│   ├── enable                        # Enable a user account
│   └── impersonate                   # Generate login URL as any user (dev only)
│
├── api                               # API management (NEW — not in bench)
│   ├── list                          # List all registered API endpoints
│   ├── test                          # Test an API endpoint
│   ├── docs                          # Generate OpenAPI/Swagger spec
│   ├── keys                          # Manage API keys
│   │   ├── create                    # Create a new API key
│   │   ├── revoke                    # Revoke an API key
│   │   ├── list                      # List all API keys
│   │   └── rotate                    # Rotate an API key's secret
│   └── webhooks                      # Manage webhooks
│       ├── list                      # List configured webhooks
│       ├── test                      # Send a test webhook
│       └── logs                      # Show webhook delivery logs
│
├── search                            # Search index management (NEW)
│   ├── rebuild                       # Rebuild search index for a site/doctype
│   ├── status                        # Show search index status
│   └── query                         # Query search index from CLI
│
├── cache                             # Cache management
│   ├── clear                         # Clear all caches for a site
│   ├── clear-meta                    # Clear metadata cache only
│   ├── clear-sessions                # Clear all sessions (logout all users)
│   ├── stats                         # Show cache hit/miss statistics
│   └── warm                          # Pre-warm caches (metadata, hot docs)
│
├── queue                             # Queue management (NEW)
│   ├── status                        # Show queue depths and worker status
│   ├── list                          # List pending/active/failed jobs
│   ├── retry                         # Retry a failed job
│   ├── purge                         # Purge all pending jobs
│   ├── inspect                       # Inspect a specific job's payload/history
│   └── dead-letter                   # Manage dead letter queue
│       ├── list                      # List dead letter entries
│       ├── retry                     # Retry a dead letter job
│       └── purge                     # Purge dead letter queue
│
├── events                            # Kafka event management (NEW)
│   ├── list-topics                   # List all Kafka topics
│   ├── tail                          # Tail events from a topic in real-time
│   ├── publish                       # Publish a test event
│   ├── consumer-status               # Show consumer group lag
│   └── replay                        # Replay events from a time offset
│
├── translate                         # Translation management
│   ├── export                        # Export translatable strings
│   ├── import                        # Import translations
│   ├── status                        # Show translation coverage
│   └── compile                       # Compile translations to binary format
│
├── log                               # Log viewing (NEW)
│   ├── tail                          # Tail logs in real-time (with filters)
│   ├── search                        # Search through log files
│   └── export                        # Export logs for a time range
│
├── monitor                           # Monitoring (NEW)
│   ├── live                          # Live dashboard (TUI) showing requests, workers, queues
│   ├── metrics                       # Dump current Prometheus metrics
│   └── audit                         # Query audit log
│
└── completion                        # Shell completion
    ├── bash                          # Generate bash completions
    ├── zsh                           # Generate zsh completions
    ├── fish                          # Generate fish completions
    └── powershell                    # Generate PowerShell completions
```

---

### 4.2 Detailed Command Reference

#### 4.2.1 Project Initialization

##### `moca init`

Initialize a new Moca project. This is the starting point for everything.

```
Usage: moca init [PATH] [flags]

Arguments:
  PATH    Directory to create the project in (default: current directory)

Flags:
  --name string           Project name (default: directory name)
  --moca-version string   Framework version to use (default: latest stable)
  --apps strings          Comma-separated list of apps to pre-install
  --db-host string        PostgreSQL host (default: localhost)
  --db-port int           PostgreSQL port (default: 5432)
  --redis-host string     Redis host (default: localhost)
  --redis-port int        Redis port (default: 6379)
  --kafka                 Enable Kafka integration (default: true)
  --no-kafka              Disable Kafka (Redis pub/sub fallback)
  --minimal               Minimal setup (PostgreSQL + Redis only)
  --template string       Project template: "standard", "minimal", "enterprise"
  --skip-assets           Skip building frontend assets
  --json                  Output result as JSON

Examples:
  # Standard project
  moca init my-erp

  # Minimal project for small deployments
  moca init my-app --minimal --no-kafka

  # Pre-install apps
  moca init my-erp --apps crm,accounting

  # Enterprise template with all services
  moca init my-erp --template enterprise
```

**What it does:**
1. Creates the project directory structure.
2. Generates `moca.yaml` with infrastructure config.
3. Connects to PostgreSQL and creates the `moca_system` schema.
4. Connects to Redis and verifies connectivity.
5. Optionally creates Kafka topics.
6. Installs the `core` framework app.
7. Generates initial `moca.lock`.
8. Initializes git repository.

##### `moca version`

```
Usage: moca version [flags]

Flags:
  --short      Print only the version number
  --json       Output as JSON (includes all app versions)
  --check      Check for available updates

Examples:
  $ moca version
  Moca CLI:        v1.0.0
  Moca Framework:  v1.0.0
  Go:              go1.26.0
  PostgreSQL:      16.2
  Redis:           7.2.4
  Kafka:           3.7.0
  Meilisearch:     1.7.0

  Apps:
    core          v1.0.0  (builtin)
    crm           v1.2.3  (github.com/moca-apps/crm @ a1b2c3d)
    accounting    v2.1.0  (github.com/moca-apps/accounting @ f6e5d4c)
```

---

#### 4.2.2 Site Management

##### `moca site create`

```
Usage: moca site create SITE_NAME [flags]

Arguments:
  SITE_NAME    The site identifier (e.g., "acme.localhost", "acme.myerp.com")

Flags:
  --admin-password string   Administrator password (prompted if not provided)
  --db-name string          Custom database/schema name (default: auto-generated)
  --install-apps strings    Apps to install immediately after site creation
  --template string         Site template for pre-configured settings
  --timezone string         Site timezone (default: UTC)
  --language string         Default language (default: en)
  --currency string         Default currency (default: USD)
  --no-cache-warmup         Skip initial cache warming
  --json                    Output result as JSON

Examples:
  # Basic site
  moca site create acme.localhost --admin-password secret123

  # Site with apps pre-installed
  moca site create acme.localhost \
    --admin-password secret123 \
    --install-apps crm,accounting \
    --timezone "Asia/Baghdad" \
    --currency IQD
```

**What it does:**
1. Creates PostgreSQL schema for the tenant.
2. Runs system table creation (tab_singles, tab_version, tab_audit_log, etc.).
3. Creates the `sites/{site_name}/` directory with `site_config.yaml`.
4. Runs core framework migrations.
5. Creates the Administrator user.
6. Installs specified apps (runs their migrations + fixtures).
7. Creates Redis key namespace.
8. Creates Meilisearch index.
9. Creates S3 storage prefix.
10. Warms metadata cache.

##### `moca site drop`

```
Usage: moca site drop SITE_NAME [flags]

Flags:
  --force              Skip confirmation prompt
  --no-backup          Skip automatic backup before dropping
  --archived           Move site dir to archived_sites/ instead of deleting
  --keep-database      Don't drop the database schema
```

##### `moca site list`

```
Usage: moca site list [flags]

Flags:
  --json         Output as JSON
  --table        Output as formatted table (default)
  --verbose      Show additional details (DB size, last backup, etc.)
  --status       Filter by status (active, disabled, migrating)

Example output:
  SITE                    STATUS    APPS                  DB SIZE    LAST BACKUP
  acme.localhost          active    core, crm, accounting 245 MB     2h ago
  globex.localhost        active    core, crm             128 MB     2h ago
  test.localhost          disabled  core                  12 MB      1d ago

  Active site: acme.localhost (set with `moca site use`)
```

##### `moca site use`

```
Usage: moca site use SITE_NAME

Sets the active site for all subsequent commands. Equivalent to
Frappe's `bench use` but also supports the MOCA_SITE environment
variable for override.

Priority order:
  1. --site flag on any command (highest)
  2. MOCA_SITE environment variable
  3. moca site use setting (stored in .moca/current_site)
```

##### `moca site migrate`

```
Usage: moca site migrate [SITE_NAME] [flags]

Runs all pending migrations for the specified site (or active site).

Flags:
  --site string      Target site (default: active site)
  --all              Migrate all sites
  --dry-run          Show what would be migrated without executing
  --no-backup        Skip automatic backup before migration
  --skip-search      Skip search index rebuild after migration
  --skip-cache       Skip cache clear after migration
  --parallel int     Parallel migration workers for --all (default: 4)

What it does:
  1. Checks readiness (no pending background jobs blocking migration)
  2. Backs up the site (unless --no-backup)
  3. Runs pending schema migrations (ALTER TABLE, new tables, etc.)
  4. Runs pending data patches
  5. Syncs MetaType definitions to metadata cache
  6. Rebuilds search indexes for changed DocTypes
  7. Clears all caches
  8. Reports success/failure with timing
```

##### `moca site clone`

```
Usage: moca site clone SOURCE_SITE NEW_SITE [flags]

Creates a copy of an existing site (schema + data).

Flags:
  --data-only        Only copy data, not files/attachments
  --anonymize        Anonymize PII data in the clone (for staging/testing)
  --exclude strings  DocTypes to exclude from clone

Examples:
  # Clone production to staging
  moca site clone acme.myerp.com staging.myerp.com --anonymize
```

##### `moca site reinstall`

```
Usage: moca site reinstall [SITE_NAME] [flags]

Completely resets a site by dropping all data and re-installing all
currently installed apps from scratch. Equivalent to bench's `reinstall`.

Flags:
  --site string       Target site (default: active site)
  --admin-password    New admin password (prompted if not provided)
  --force             Skip confirmation prompt
  --no-backup         Skip automatic backup before reinstall
```

##### `moca site enable`

```
Usage: moca site enable [SITE_NAME] [flags]

Re-enable a site that was previously disabled (maintenance mode).

Flags:
  --site string       Target site
```

##### `moca site disable`

```
Usage: moca site disable [SITE_NAME] [flags]

Put a site into maintenance mode. All requests will receive a
503 Service Unavailable response with a maintenance page.

Flags:
  --site string       Target site
  --message string    Custom maintenance message
  --allow strings     IP addresses to allow through during maintenance
```

##### `moca site rename`

```
Usage: moca site rename OLD_NAME NEW_NAME [flags]

Rename a site. Updates the database, config files, proxy configs,
and S3 prefixes.

Flags:
  --no-proxy-reload   Skip reloading the reverse proxy
```

##### `moca site browse`

```
Usage: moca site browse [SITE_NAME] [flags]

Opens the site URL in the default browser.

Flags:
  --user string       Login as a specific user (dev mode only)
  --print-url         Print the URL instead of opening browser
```

##### `moca site info`

```
Usage: moca site info [SITE_NAME] [flags]

Flags:
  --json              Output as JSON

Example output:
  Site:               acme.localhost
  Status:             active
  Created:            2026-03-15 14:30:00 UTC
  DB Schema:          tenant_acme
  DB Size:            245 MB
  Installed Apps:     core (v1.0.0), crm (v1.2.3), accounting (v2.1.0)
  Users:              42 (38 active)
  Documents:          125,432
  Last Backup:        2026-03-29 02:00:00 UTC (verified ✓)
  Last Migration:     2026-03-28 09:15:00 UTC
  Scheduler:          enabled
  Search Index Size:  18 MB (125,432 documents)
```

---

#### 4.2.3 App Management

##### `moca app new`

```
Usage: moca app new APP_NAME [flags]

Scaffolds a new Moca application with full directory structure.

Flags:
  --module string       Initial module name (default: based on app name)
  --title string        Human-readable app title
  --publisher string    Publisher/organization name
  --license string      License (default: MIT)
  --doctype string      Create an initial DocType with the app
  --template string     App template: "standard", "minimal", "api-only"

What it generates:
  apps/{app_name}/
  ├── manifest.yaml           # AppManifest
  ├── hooks.go                # Hook registration (with examples)
  ├── modules/
  │   └── {module}/
  │       ├── doctypes/
  │       │   └── .gitkeep
  │       ├── pages/           # Custom page components (.tsx)
  │       │   └── .gitkeep
  │       └── reports/         # Report definitions (.json + .go)
  │           └── .gitkeep
  ├── fixtures/
  ├── migrations/
  │   └── 001_initial.sql
  ├── templates/
  │   └── portal/
  ├── public/
  ├── tests/
  │   └── setup_test.go
  ├── go.mod
  ├── go.sum
  └── README.md
```

##### `moca app get`

```
Usage: moca app get SOURCE [flags]

Downloads and installs an app into the project.

Arguments:
  SOURCE    Git URL, registry name, or local path

Flags:
  --version string      Version constraint (semver range)
  --branch string       Git branch to clone
  --ref string          Exact git ref (commit/tag)
  --depth int           Git clone depth (default: 1 for shallow)
  --no-resolve          Skip dependency resolution
  --no-install          Download only, don't install on any site

Examples:
  # From Moca app registry (shortest form)
  moca app get crm

  # From Git with version constraint
  moca app get github.com/moca-apps/crm --version "~1.2.0"

  # Pin to exact version
  moca app get github.com/moca-apps/crm --ref v1.2.3

  # From local path (for development)
  moca app get ./local-apps/my-custom-app

What it does:
  1. Resolves the app source (registry lookup / git URL / path)
  2. Checks version constraints against moca.yaml and existing apps
  3. Resolves transitive dependencies
  4. Downloads/clones the app to apps/
  5. Updates moca.yaml and regenerates moca.lock
  6. Installs Go dependencies (go mod download)
  7. Builds frontend assets if the app has desk/ components
```

##### `moca app update`

```
Usage: moca app update [APP_NAME] [flags]

Update one or all apps, respecting version constraints.

Flags:
  --all                 Update all apps
  --dry-run             Show what would be updated
  --migrate             Run site migrations after update (default: true)
  --no-migrate          Skip migrations
  --backup              Backup before updating (default: true in production)
  --no-backup           Skip backup
  --force               Force update even with local modifications

Example output:
  Resolving updates...

  APP           CURRENT    AVAILABLE    CONSTRAINT
  crm           v1.2.3     v1.2.5       ~1.2.0       ✓ compatible
  accounting    v2.1.0     v2.3.0       ^2.0.0       ✓ compatible

  Proceed? [Y/n] y

  Updating crm v1.2.3 → v1.2.5...
    ✓ Downloaded (0.8s)
    ✓ Dependencies resolved
    ✓ Assets built (2.1s)
    ✓ Site acme.localhost migrated (1.2s)
    ✓ Site globex.localhost migrated (0.9s)
    ✓ Lockfile updated

  Updating accounting v2.1.0 → v2.3.0...
    ✓ Downloaded (1.1s)
    ✓ Dependencies resolved
    ✓ Assets built (3.4s)
    ✓ Site acme.localhost migrated (2.5s)
    ✓ Lockfile updated

  All updates complete.
```

##### `moca app install`

```
Usage: moca app install APP_NAME [flags]

Install an app on a site (the app must already be in the project).

Flags:
  --site string       Target site (default: active site)
  --all-sites         Install on all sites

What it does:
  1. Validates the app is in apps/ directory
  2. Resolves dependencies (ensures required apps are installed first)
  3. Runs the app's migrations on the target site
  4. Creates MetaType tables
  5. Seeds fixture data
  6. Registers hooks
  7. Clears caches
  8. Rebuilds search indexes for new DocTypes
```

##### `moca app uninstall`

```
Usage: moca app uninstall APP_NAME [flags]

Uninstall an app from a site (destructive — removes all app data).

Flags:
  --site string       Target site (default: active site)
  --force             Skip confirmation
  --no-backup         Skip automatic backup
  --keep-data         Remove app registration but keep database tables
  --dry-run           Show what would be removed
```

##### `moca app list`

```
Usage: moca app list [flags]

Flags:
  --site string       Show apps installed on this site
  --project           Show apps in the project (default)
  --json              Output as JSON
  --verbose           Show version, source, dependencies

Example:
  PROJECT APPS:                                     SITES INSTALLED
  core          v1.0.0   builtin                    acme, globex, test
  crm           v1.2.3   github.com/moca-apps/crm   acme, globex
  accounting    v2.1.0   github.com/moca-apps/acc    acme
  custom-hr     v0.1.0   ./local-apps/custom-hr     (none)
```

##### `moca app resolve`

```
Usage: moca app resolve [flags]

Resolves all app dependencies and regenerates moca.lock.

Flags:
  --dry-run          Show resolution without writing lockfile
  --update           Allow updating locked versions within constraints
  --strict           Fail if any constraint cannot be satisfied

This is the equivalent of `npm install` / `go mod tidy` for Moca apps.
```

##### `moca app diff`

```
Usage: moca app diff APP_NAME [flags]

Show changes in an app since the locked version.

Flags:
  --schema           Show only schema/MetaType changes
  --hooks            Show only hook changes
  --migrations       Show only pending migrations
```

##### `moca app publish`

```
Usage: moca app publish APP_NAME [flags]

Publish an app to the Moca app registry.

Flags:
  --registry string   Registry URL (default: registry.moca.dev)
  --tag string        Release tag (auto-detected from manifest)
  --dry-run           Validate without publishing
```

---

#### 4.2.4 Server & Process Management

##### `moca serve`

```
Usage: moca serve [flags]

Start the Moca development server. This is the primary dev command.

Flags:
  --port int          HTTP port (default: 8000)
  --host string       Bind address (default: 0.0.0.0)
  --workers int       Number of worker goroutines (default: 2)
  --no-workers        Don't start background workers
  --no-scheduler      Don't start the scheduler
  --no-desk           Don't start the React dev server
  --desk-port int     React dev server port (default: 3000)
  --no-watch          Don't watch for file changes (disables both MetaType
                      JSON watching and frontend asset watching)
  --profile           Enable pprof profiling endpoints

File watching behavior:
  - The server watches `*/doctypes/*.json` for MetaType changes and triggers
    the full hot reload pipeline (schema diff, cache invalidation, event broadcast).
  - `moca dev watch` watches only frontend assets (.tsx, .css) for rebuilds.
  - `--no-watch` disables BOTH MetaType and frontend watching.

What it starts (single process):
  ┌────────────────────────────────────────────┐
  │  moca serve                                │
  │                                            │
  │  ┌──────────────────┐  ┌────────────────┐ │
  │  │  HTTP Server      │  │  WebSocket Hub │ │
  │  │  (port 8000)      │  │  (port 8000/ws)│ │
  │  └──────────────────┘  └────────────────┘ │
  │  ┌──────────────────┐  ┌────────────────┐ │
  │  │  Background       │  │  Scheduler     │ │
  │  │  Workers (2)      │  │  (in-process)  │ │
  │  └──────────────────┘  └────────────────┘ │
  │  ┌──────────────────┐  ┌────────────────┐ │
  │  │  File Watcher     │  │  Outbox Poller │ │
  │  │  (auto-reload)    │  │  (if Kafka on) │ │
  │  └──────────────────┘  └────────────────┘ │
  │  ┌──────────────────┐                     │
  │  │  React Dev Server │  (separate process)│
  │  │  (port 3000, HMR) │                    │
  │  └──────────────────┘                     │
  └────────────────────────────────────────────┘
```

Unlike bench, which runs multiple processes via honcho/Procfile, `moca serve` runs everything in a **single Go process** (except the React dev server). This makes dev startup sub-second and eliminates process coordination issues.

##### `moca stop`

```
Usage: moca stop [flags]

Flags:
  --graceful          Wait for in-flight requests to complete (default: true)
  --timeout duration  Graceful shutdown timeout (default: 30s)
  --force             Force kill immediately
```

##### `moca restart`

```
Usage: moca restart [flags]

Flags:
  --graceful          Rolling restart (zero downtime) in production
  --process string    Restart only a specific process (server, worker, scheduler)
```

##### `moca worker start`

```
Usage: moca worker start [flags]

Start background workers as a separate process (production use).

Flags:
  --queues strings    Queues to consume (default: all)
  --concurrency int   Number of concurrent workers (default: auto)
  --burst             Process all pending jobs then exit
```

##### `moca worker scale`

```
Usage: moca worker scale QUEUE COUNT [flags]

Dynamically adjust worker pool size for a specific queue.

Examples:
  moca worker scale default 8
  moca worker scale long 4
  moca worker scale critical 2
```

##### `moca worker stop`

```
Usage: moca worker stop [flags]

Stop background worker processes.

Flags:
  --graceful          Wait for in-flight jobs to complete (default: true)
  --timeout duration  Graceful shutdown timeout (default: 60s)
  --queue string      Stop workers for a specific queue only
```

##### `moca worker status`

```
Usage: moca worker status [flags]

Show current worker pool status, active jobs, and throughput.

Flags:
  --json              Output as JSON
  --watch             Refresh continuously (like `top`)
```

---

#### 4.2.4a Scheduler Management

##### `moca scheduler start`

```
Usage: moca scheduler start [flags]

Start the scheduler as a separate process (production).
In development mode, the scheduler runs inside `moca serve`.

Flags:
  --foreground        Run in foreground (default: background)
```

##### `moca scheduler stop`

```
Usage: moca scheduler stop [flags]

Stop the scheduler process.
```

##### `moca scheduler status`

```
Usage: moca scheduler status [flags]

Show scheduler status and next scheduled run times.

Flags:
  --json              Output as JSON

Example:
  Scheduler: running (PID 12345)

  JOB                               SITE              NEXT RUN           LAST RUN
  all (every 60s)                   acme.localhost    in 42s             58s ago ✓
  hourly                            acme.localhost    in 52m             8m ago ✓
  daily                             acme.localhost    in 10h 52m         13h ago ✓
  backup.auto                       acme.localhost    in 10h 52m         13h ago ✓
  crm.sync_contacts (custom)        acme.localhost    in 22m             38m ago ✗ (error)
```

##### `moca scheduler enable`

```
Usage: moca scheduler enable [flags]

Enable the scheduler for a specific site.

Flags:
  --site string       Target site (default: active site)
  --all-sites         Enable for all sites
```

##### `moca scheduler disable`

```
Usage: moca scheduler disable [flags]

Disable the scheduler for a specific site. Background jobs will
stop being enqueued but already-enqueued jobs will complete.

Flags:
  --site string       Target site (default: active site)
  --all-sites         Disable for all sites
```

##### `moca scheduler trigger`

```
Usage: moca scheduler trigger EVENT [flags]

Manually trigger a scheduled event immediately.

Arguments:
  EVENT    Event name: "all", "hourly", "daily", "weekly", "monthly",
           "cron", or a custom event name

Flags:
  --site string       Target site
  --all-sites         Trigger for all sites

Examples:
  moca scheduler trigger daily --site acme.localhost
  moca scheduler trigger crm.sync_contacts --site acme.localhost
```

##### `moca scheduler list-jobs`

```
Usage: moca scheduler list-jobs [flags]

List all registered scheduled jobs from all installed apps.

Flags:
  --site string       Target site
  --app string        Filter by app
  --json              Output as JSON
```

##### `moca scheduler purge-jobs`

```
Usage: moca scheduler purge-jobs [flags]

Purge pending scheduled jobs from the queue.

Flags:
  --site string       Target site
  --event string      Purge only jobs for a specific event
  --all               Purge all pending jobs (default: only stale)
  --force             Skip confirmation
```

---

#### 4.2.5 Database Operations

##### `moca db console`

```
Usage: moca db console [flags]

Opens an interactive PostgreSQL session for the active site's schema.

Flags:
  --site string       Target site
  --system            Connect to the moca_system schema instead
  --readonly          Open in read-only mode
```

##### `moca db migrate`

```
Usage: moca db migrate [flags]

Run pending schema migrations. This is the lower-level form of
`moca site migrate` — it only handles database schema, not the
full migration lifecycle (cache clear, search rebuild, etc.).

Flags:
  --site string       Target site (default: active site)
  --dry-run           Show SQL that would be executed
  --verbose           Show each migration as it runs
  --step int          Run only N migrations
  --skip string       Skip a specific migration by version/filename

Note: The migration runner respects `DependsOn` declarations in app manifests.
If `--step` would stop before a depended-upon migration completes, an error
is raised. `--skip` will refuse to skip a migration that other pending
migrations depend on.
```

##### `moca db rollback`

```
Usage: moca db rollback [flags]

Rollback the last migration batch.

Flags:
  --site string       Target site
  --step int          Number of batches to rollback (default: 1)
  --dry-run           Show what would be rolled back
```

##### `moca db diff`

```
Usage: moca db diff [flags]

Compare MetaType definitions against the actual database schema
and show the differences. This is extremely useful for diagnosing
schema drift.

Flags:
  --site string       Target site
  --doctype string    Check a specific DocType only
  --output string     Output format: "text" (default), "sql", "json"

Example output:
  Schema diff for site: acme.localhost

  DocType: SalesOrder
    ✓ Table tab_sales_order exists
    ✗ Column "delivery_notes" defined in MetaType but missing in DB
    ✗ Column "old_status" exists in DB but not in MetaType
    ✗ Index idx_sales_order_customer expected but not found

  DocType: Customer
    ✓ Schema matches MetaType definition

  Summary: 3 differences found. Run `moca db migrate` to apply.
```

##### `moca db snapshot`

```
Usage: moca db snapshot [NAME] [flags]

Save the current database schema state as a named snapshot.
Useful before risky operations.

Flags:
  --site string       Target site
  --include-data      Include row data (not just schema)
```

##### `moca db seed`

```
Usage: moca db seed [flags]

Load seed/fixture data from app fixture files.

Flags:
  --site string       Target site
  --app string        Seed data from a specific app only
  --file string       Seed from a specific fixture file
  --force             Overwrite existing data
```

##### `moca db trim-tables`

```
Usage: moca db trim-tables [flags]

Remove database columns that no longer exist in MetaType definitions.

Flags:
  --site string       Target site
  --dry-run           Show what would be removed
  --doctype string    Target a specific DocType
```

##### `moca db trim-database`

```
Usage: moca db trim-database [flags]

Remove database tables that don't correspond to any MetaType definition.

Flags:
  --site string       Target site
  --dry-run           Show what would be removed
```

##### `moca db reset`

```
Usage: moca db reset [flags]

Drop and recreate the site's database schema. This is destructive
and will delete ALL data.

Flags:
  --site string       Target site
  --force             Skip confirmation (required for non-interactive use)
  --no-backup         Skip automatic backup before reset
```

##### `moca db export-fixtures`

```
Usage: moca db export-fixtures [flags]

Export site data as fixture JSON files to an app's fixtures/ directory.

Flags:
  --site string       Target site
  --app string        Target app to save fixtures into
  --doctype string    Export a specific DocType
  --filters string    JSON filter to limit exported records
```

---

#### 4.2.6 Backup & Restore

##### `moca backup create`

```
Usage: moca backup create [flags]

Flags:
  --site string         Target site (default: active site)
  --all-sites           Backup all sites
  --with-files          Include private and public files
  --only-db             Database only (no files)
  --compress string     Compression: "gzip" (default), "zstd", "none"
  --encrypt             Encrypt backup with configured key
  --output string       Custom output path
  --upload              Upload to configured remote storage after creating
  --parallel int        Parallel table dump for large databases (default: 4)
  --json                Output result as JSON

Example output:
  Backing up site: acme.localhost

  ✓ Database backup   245 MB → 42 MB (gzip)    [3.2s]
  ✓ Private files     1.2 GB → 890 MB (gzip)   [8.5s]
  ✓ Public files      340 MB → 280 MB (gzip)    [2.1s]
  ✓ Encrypted with AES-256-GCM
  ✓ Uploaded to s3://moca-backups/acme/2026-03-29_020000/

  Backup saved:
    DB:      sites/acme.localhost/backups/2026-03-29_020000_db.sql.gz.enc
    Private: sites/acme.localhost/backups/2026-03-29_020000_private.tar.gz.enc
    Public:  sites/acme.localhost/backups/2026-03-29_020000_public.tar.gz.enc

  Backup ID: bk_20260329_020000
  Verify with: moca backup verify bk_20260329_020000
```

##### `moca backup restore`

```
Usage: moca backup restore TARGET [flags]

Arguments:
  TARGET    Backup ID, file path, or S3 URL

Flags:
  --site string         Target site to restore into
  --new-site string     Create a new site from backup
  --db-only             Restore database only
  --with-files          Restore files (default: true when available)
  --decrypt             Decrypt backup (prompted for key if not configured)
  --force               Overwrite existing site data
  --no-migrate          Skip running migrations after restore
  --parallel int        Parallel table restore (default: 4)
  --progress            Show detailed progress (default: true)

Examples:
  # Restore from backup ID
  moca backup restore bk_20260329_020000 --site acme.localhost

  # Restore from file
  moca backup restore ./backups/acme_db.sql.gz --site acme.localhost

  # Restore from S3 into a new site
  moca backup restore s3://moca-backups/acme/2026-03-29/ --new-site staging.localhost

  # Restore with automatic decryption
  moca backup restore bk_20260329_020000 --site acme.localhost --decrypt
```

##### `moca backup list`

```
Usage: moca backup list [flags]

Flags:
  --site string       Filter by site
  --remote            List backups on remote storage
  --local             List local backups only (default: both)
  --json              Output as JSON
  --limit int         Number of backups to show (default: 20)

Example output:
  BACKUP ID               SITE              TYPE      SIZE      AGE       VERIFIED
  bk_20260329_020000      acme.localhost    full      1.2 GB    2h ago    ✓
  bk_20260328_020000      acme.localhost    full      1.1 GB    1d ago    ✓
  bk_20260329_020000      globex.localhost  db-only   42 MB     2h ago    ✗ not verified
  bk_20260327_020000      acme.localhost    full      1.1 GB    2d ago    ✓
```

##### `moca backup verify`

```
Usage: moca backup verify BACKUP_ID [flags]

Verifies backup integrity by:
  1. Checking file checksums
  2. Attempting to decrypt (if encrypted)
  3. Validating SQL syntax of database dump
  4. Checking for completeness (all expected files present)
  5. Optionally restoring to a temporary database to verify

Flags:
  --deep              Full restore to temp DB for verification (slow but thorough)
```

##### `moca backup schedule`

```
Usage: moca backup schedule [flags]

Configure and manage automated backups.

Flags:
  --cron string       Cron expression (e.g., "0 2 * * *")
  --show              Show current backup schedule
  --disable           Disable automated backups
  --enable            Enable automated backups
```

##### `moca backup upload`

```
Usage: moca backup upload BACKUP_ID [flags]

Upload a local backup to configured remote storage (S3/MinIO).

Flags:
  --destination string  Override remote storage destination
  --delete-local        Delete local copy after successful upload
```

##### `moca backup download`

```
Usage: moca backup download BACKUP_ID [flags]

Download a backup from remote storage to local disk.

Flags:
  --output string     Local download path
  --source string     Override remote storage source
```

##### `moca backup prune`

```
Usage: moca backup prune [flags]

Delete old backups according to retention policy.

Flags:
  --dry-run           Show what would be deleted
  --force             Skip confirmation
  --site string       Prune for a specific site
```

---

#### 4.2.7 Configuration Management

##### `moca config get`

```
Usage: moca config get KEY [flags]

Flags:
  --site string       Get site-level config
  --project           Get project-level config (default)
  --resolved          Show the effective value after merging all layers

Config resolution order (highest priority first):
  1. Environment variable: MOCA_{KEY} (uppercased, dots → underscores)
  2. Site config: sites/{site}/site_config.yaml
  3. Common config: sites/common_site_config.yaml
  4. Project config: moca.yaml
  5. Default value
```

##### `moca config set`

```
Usage: moca config set KEY VALUE [flags]

Flags:
  --site string       Set in site config
  --common            Set in common_site_config.yaml
  --project           Set in moca.yaml (default)
  --type string       Value type hint: "string", "int", "bool", "json" (auto-detected)

Examples:
  moca config set development.port 9000
  moca config set --site acme.localhost scheduler.enabled false
  moca config set --common backup.schedule "0 3 * * *"
```

##### `moca config list`

```
Usage: moca config list [flags]

Flags:
  --site string       Show resolved config for a site
  --format string     Output format: "table" (default), "yaml", "json"
  --filter string     Filter keys (e.g., "database.*", "*.enabled")
```

##### `moca config diff`

```
Usage: moca config diff SITE1 SITE2 [flags]

Compare configuration between two sites, or between environments.

Examples:
  moca config diff acme.localhost globex.localhost
  moca config diff --site acme.localhost --env production --env staging
```

##### `moca config export`

```
Usage: moca config export [flags]

Export the full resolved configuration to a file.

Flags:
  --site string       Export for a specific site
  --format string     "yaml" (default), "json", "env"
  --output string     Output file path (default: stdout)
  --secrets           Include secret values (default: masked)
```

##### `moca config import`

```
Usage: moca config import FILE [flags]

Import configuration from a YAML/JSON file, merging with existing config.

Flags:
  --site string       Import into site config
  --common            Import into common_site_config.yaml
  --project           Import into moca.yaml (default)
  --overwrite         Overwrite conflicting keys (default: skip conflicts)
  --dry-run           Show what would change
```

##### `moca config edit`

```
Usage: moca config edit [flags]

Open the configuration file in $EDITOR for manual editing.
Validates the config after saving.

Flags:
  --site string       Edit site config
  --common            Edit common_site_config.yaml
  --project           Edit moca.yaml (default)
```

---

#### 4.2.8 Deployment Operations

##### `moca deploy setup`

**This is the biggest improvement over bench.** One idempotent command that takes a project from development to production.

```
Usage: moca deploy setup [flags]

Flags:
  --domain string       Primary domain for the deployment
  --email string        Admin email for TLS certificates
  --proxy string        Reverse proxy: "caddy" (default), "nginx"
  --process string      Process manager: "systemd" (default), "docker"
                        When "docker" is selected, step 5 internally runs
                        `moca generate docker --profile production` to produce
                        the Compose files, then starts the stack via docker compose.
  --workers int         Number of HTTP worker processes (default: auto)
  --background int      Number of background workers (default: 2)
  --tls string          TLS mode: "acme" (default), "custom", "none"
  --tls-cert string     Custom TLS certificate path
  --tls-key string      Custom TLS key path
  --firewall            Configure firewall rules (default: true)
  --fail2ban            Setup fail2ban (default: true)
  --logrotate           Configure log rotation (default: true)
  --dry-run             Show what would be configured
  --yes                 Skip confirmation prompts

What it does:
  1. Validates system requirements (PostgreSQL, Redis, etc.)
  2. Switches project to production mode
  3. Builds frontend assets (optimized, minified)
  4. Generates reverse proxy configuration
  5. Generates process manager unit files
  6. Generates Redis production config
  7. Configures log rotation
  8. Configures automated backups
  9. Sets up firewall rules (if --firewall)
  10. Sets up fail2ban (if --fail2ban)
  11. Obtains TLS certificates (if --tls=acme)
  12. Starts all services
  13. Runs health checks
  14. Reports deployment status

Example:
  moca deploy setup \
    --domain myerp.example.com \
    --email admin@example.com \
    --proxy caddy \
    --process systemd \
    --workers 4 \
    --background 3

  ┌─────────────────────────────────────────────────┐
  │  Production Deployment: myerp.example.com        │
  │                                                  │
  │  ✓ System requirements validated                 │
  │  ✓ Project set to production mode                │
  │  ✓ Frontend assets built (12.3s)                 │
  │  ✓ Caddy config generated → /etc/caddy/moca.conf│
  │  ✓ systemd units generated (4 services)          │
  │  ✓ Redis config optimized for production         │
  │  ✓ Log rotation configured                       │
  │  ✓ Automated backups scheduled (daily at 2 AM)   │
  │  ✓ Firewall configured (80, 443, 22)             │
  │  ✓ fail2ban configured                           │
  │  ✓ TLS certificate obtained (Let's Encrypt)      │
  │  ✓ All services started                          │
  │  ✓ Health check passed                           │
  │                                                  │
  │  Your site is live at https://myerp.example.com  │
  └─────────────────────────────────────────────────┘
```

##### `moca deploy update`

```
Usage: moca deploy update [flags]

Safe, atomic production update. The single most important production command.

Flags:
  --apps strings      Update specific apps only (default: all)
  --no-backup         Skip pre-update backup
  --no-migrate        Skip migrations
  --no-build          Skip frontend asset build
  --no-restart        Skip process restart
  --dry-run           Show update plan without executing
  --parallel int      Parallel site migration (default: 2)

Execution flow:
  ┌─────────────────────────────────────────────────┐
  │  moca deploy update                              │
  │                                                  │
  │  Phase 1: Prepare                                │
  │    ✓ Check for pending changes in moca.yaml      │
  │    ✓ Resolve app versions from lockfile           │
  │    ✓ Validate migration compatibility             │
  │                                                  │
  │  Phase 2: Backup                                 │
  │    ✓ Backup all sites (parallel)                  │
  │    ✓ Verify backups                               │
  │    ✓ Create deployment snapshot                   │
  │                                                  │
  │  Phase 3: Update                                 │
  │    ✓ Pull app updates                             │
  │    ✓ Build assets                                 │
  │    ✓ Migrate site: acme (2.1s)                    │
  │    ✓ Migrate site: globex (1.8s)                  │
  │                                                  │
  │  Phase 4: Activate                               │
  │    ✓ Rolling restart (zero downtime)              │
  │    ✓ Health checks passed                         │
  │    ✓ Deployment recorded                          │
  │                                                  │
  │  Update complete. Rollback available:             │
  │    moca deploy rollback dp_20260329_150000        │
  └─────────────────────────────────────────────────┘

On failure at any phase:
  - Phase 1-2 failure: abort cleanly, nothing changed
  - Phase 3 failure: auto-rollback database from backup
  - Phase 4 failure: rollback to previous binary + restart
```

##### `moca deploy rollback`

```
Usage: moca deploy rollback [DEPLOYMENT_ID] [flags]

Rollback to a previous deployment state.

Flags:
  --step int          Rollback N deployments (default: 1)
  --force             Skip confirmation
  --no-backup         Skip backup of current state before rollback
```

##### `moca deploy history`

```
Usage: moca deploy history [flags]

Flags:
  --limit int         Number of entries (default: 20)
  --json              Output as JSON

Example:
  DEPLOYMENT            TIMESTAMP              STATUS     DURATION   APPS UPDATED
  dp_20260329_150000    2026-03-29 15:00:00    success    45s        crm, accounting
  dp_20260328_100000    2026-03-28 10:00:00    success    38s        crm
  dp_20260325_090000    2026-03-25 09:00:00    rolled_back 52s       accounting
```

##### `moca deploy promote`

```
Usage: moca deploy promote SOURCE_ENV TARGET_ENV [flags]

Promote a deployment from one environment to another
(e.g., staging → production).

Flags:
  --dry-run           Show what would be promoted
  --skip-backup       Skip backup of target environment
```

---

#### 4.2.9 Infrastructure Generation

##### `moca generate caddy`

```
Usage: moca generate caddy [flags]

Generate Caddy reverse proxy configuration.

Flags:
  --output string     Output path (default: config/caddy/Caddyfile)
  --multitenant       Generate wildcard config for subdomain multitenancy
  --reload            Reload Caddy after generating
```

##### `moca generate systemd`

```
Usage: moca generate systemd [flags]

Generate systemd unit files for all Moca processes.

Flags:
  --user string       System user to run as
  --output string     Output directory (default: config/systemd/)
  --install           Install units to /etc/systemd/system/

Generates:
  moca-server@.service      (template for N instances)
  moca-worker@.service      (template for worker processes)
  moca-scheduler.service    (single instance)
  moca-outbox.service       (single instance, if Kafka enabled)
  moca-search-sync.service  (Kafka → Meilisearch sync, if Kafka enabled)
  moca.target               (group target for all services)
```

##### `moca generate docker`

```
Usage: moca generate docker [flags]

Generate Docker Compose configuration for the full stack.

Flags:
  --output string     Output directory (default: config/docker/)
  --profile string    "development", "production" (default: development)
  --include strings   Extra services: "kafka", "meilisearch", "minio"

Generates:
  docker-compose.yml        (main compose file)
  docker-compose.prod.yml   (production overrides)
  Dockerfile                (Moca server image)
  .dockerignore
```

##### `moca generate k8s`

```
Usage: moca generate k8s [flags]

Generate Kubernetes manifests.

Flags:
  --output string     Output directory (default: config/k8s/)
  --namespace string  Kubernetes namespace
  --replicas int      Server replicas (default: 3)
  --helm              Generate as Helm chart instead of raw manifests

Generates:
  deployment.yaml           (server + worker + scheduler)
  service.yaml              (ClusterIP for server)
  ingress.yaml              (with TLS)
  configmap.yaml            (moca.yaml as ConfigMap)
  secret.yaml               (template for secrets)
  hpa.yaml                  (Horizontal Pod Autoscaler)
  pdb.yaml                  (PodDisruptionBudget)
```

##### `moca generate nginx`

```
Usage: moca generate nginx [flags]

For users who prefer NGINX over Caddy.

Flags:
  --output string     Output path
  --multitenant       Subdomain-based multitenancy config
  --reload            Reload NGINX after generating
```

##### `moca generate env`

```
Usage: moca generate env [flags]

Generate a .env file from moca.yaml configuration.

Flags:
  --output string     Output path (default: .env)
  --format string     "dotenv" (default), "docker", "systemd"
```

---

#### 4.2.10 Developer Tools

##### `moca dev console`

```
Usage: moca dev console [flags]

Start an interactive console with the Moca framework loaded.
Uses yaegi (Go interpreter) for a Go REPL experience.

> **Known limitations:** Yaegi does not support CGo or all reflection patterns.
> If a framework package fails to load in the console (e.g., packages with
> native dependencies), use `moca dev execute` for one-off expressions instead,
> which runs compiled Go code in the actual server process.

Flags:
  --site string       Target site context
  --user string       Impersonate user (default: Administrator)
  --autoreload        Auto-reload on file changes

Example session:
  moca> site := ctx.Site()
  moca> doc, _ := document.Get(ctx, "SalesOrder", "SO-0001")
  moca> fmt.Println(doc.Get("grand_total"))
  42500.00
  moca> doc.Set("status", "Completed")
  moca> doc.Save(ctx)
  moca> // Tab-complete works on MetaType fields
```

##### `moca dev execute`

```
Usage: moca dev execute EXPRESSION [flags]

Run a one-off expression in the framework context.

Flags:
  --site string       Target site

Examples:
  moca dev execute 'document.Count(ctx, "SalesOrder", nil)'
  moca dev execute 'auth.SetPassword(ctx, "admin@acme.com", "newpass")'
```

##### `moca dev request`

```
Usage: moca dev request METHOD URL [flags]

Make an HTTP request to the Moca API as a specific user.

Flags:
  --site string       Target site
  --user string       Request as user (default: Administrator)
  --data string       Request body (JSON)
  --headers strings   Extra headers
  --verbose           Show full request/response

Examples:
  moca dev request GET /api/v1/resource/SalesOrder
  moca dev request POST /api/v1/resource/SalesOrder --data '{"customer_name":"Acme"}'
```

##### `moca dev watch`

```
Usage: moca dev watch [flags]

Watch for file changes and rebuild assets automatically.

Flags:
  --desk              Watch React desk source only
  --portal            Watch portal templates only
  --all               Watch everything (default)
```

##### `moca dev playground`

```
Usage: moca dev playground [flags]

Start an interactive API playground.

Flags:
  --port int          Playground port (default: 8001)
  --swagger           Enable Swagger UI
  --graphiql          Enable GraphiQL (default: true)
```

##### `moca dev bench`

```
Usage: moca dev bench [flags]

Run microbenchmarks on framework operations.

Flags:
  --site string       Target site
  --doctype string    Benchmark operations on a specific DocType
  --operation string  "read", "write", "query", "all" (default: all)
  --iterations int    Number of iterations (default: 1000)
  --concurrent int    Concurrent goroutines (default: 10)
```

##### `moca dev profile`

```
Usage: moca dev profile URL [flags]

Profile a request and generate flamegraph.

Flags:
  --site string       Target site
  --output string     Output file (default: profile.svg)
  --duration string   Profile duration for continuous profiling
  --type string       "cpu" (default), "mem", "goroutine", "block"
```

---

#### 4.2.11 Testing

##### `moca test run`

```
Usage: moca test run [flags]

Run Go tests for apps installed in the project.

Flags:
  --site string       Test site (auto-created if not exists)
  --app string        Test a specific app only
  --module string     Test a specific module
  --doctype string    Test a specific DocType's tests
  --parallel int      Parallel test execution (default: num_cpu)
  --verbose           Verbose test output
  --coverage          Generate coverage report
  --failfast          Stop on first failure
  --filter string     Run tests matching pattern

Examples:
  moca test run --app crm
  moca test run --doctype SalesOrder --verbose
  moca test run --filter TestSalesOrderSubmit
```

##### `moca test run-ui`

```
Usage: moca test run-ui [flags]

Run frontend UI tests using Playwright.

Flags:
  --app string        Test a specific app
  --headed            Run in headed mode (visible browser)
  --browser string    "chromium" (default), "firefox", "webkit"
```

##### `moca test factory`

```
Usage: moca test factory DOCTYPE [COUNT] [flags]

Generate realistic test data from MetaType field definitions.

Flags:
  --site string       Target site
  --locale string     Locale for generated data (default: en)
  --seed int          Random seed for reproducibility
  --with-children     Generate child table data too

Examples:
  moca test factory SalesOrder 100
  moca test factory Customer 500 --locale ar
```

---

#### 4.2.12 Build

##### `moca build desk`

```
Usage: moca build desk [flags]

Build the React Desk frontend for production.

Flags:
  --verbose           Show Vite build output

What it does:
  1. Validates desk/package.json exists
  2. Scans apps/*/desk/desk-manifest.json for extension declarations
  3. Generates .moca-extensions.ts with typed imports and registration calls
  4. Runs 'npx vite build' with NODE_ENV=production
  5. Outputs to desk/dist/ directory

  If two apps register a component for the same DocType view, the app
  with higher priority in moca.yaml wins. Legacy apps without a manifest
  fall back to desk/setup.ts for bare side-effect imports.
```

##### `moca desk install`

```
Usage: moca desk install [flags]

Install npm dependencies for the desk/ frontend project.

Flags:
  --verbose           Show npm output

Runs 'npm install' in the desk/ directory and reports the installed
node_modules size.
```

##### `moca desk update`

```
Usage: moca desk update [flags]

Update @moca/desk to the latest compatible version and regenerate
desk extension imports.

Flags:
  --verbose           Show npm output

Runs 'npm update @moca/desk' in desk/, then regenerates
.moca-extensions.ts from all discovered app desk manifests.
```

##### `moca desk dev`

```
Usage: moca desk dev [flags]

Start the Vite development server for desk/ frontend development.
Enables hot module replacement (HMR) for rapid UI iteration.

Flags:
  --port int          Dev server port (default: from config or 3000)

Port resolution: --port flag > development.desk_port in moca.yaml > 3000.
Regenerates .moca-extensions.ts before starting. Forwards SIGINT/SIGTERM
to the Vite process for clean shutdown.
```

##### `moca build portal`

```
Usage: moca build portal [flags]

Build portal/website templates and static assets.

Flags:
  --app string        Build for a specific app only
  --watch             Watch for changes and rebuild
```

##### `moca build assets`

```
Usage: moca build assets [flags]

Build all static assets (desk + portal + app public files).

Flags:
  --force             Rebuild even if no changes detected
  --no-minify         Skip minification (for debugging)
```

##### `moca build app`

```
Usage: moca build app APP_NAME [flags]

Verify that an app's Go code compiles cleanly within the workspace.
Does not produce a standalone binary — apps are composed into the
server binary via `moca build server`.

Flags:
  --race              Enable race detector
  --verbose           Show compiler output
```

##### `moca build server`

```
Usage: moca build server [flags]

Compile the moca-server binary with all installed apps included.
Uses the Go workspace (go.work) to compose the framework and all
app modules into a single binary. This is called automatically by
`moca serve` and `moca deploy update`.

Flags:
  --output string     Output binary path (default: bin/moca-server)
  --race              Enable race detector
  --verbose           Show compiler output
  --ldflags string    Additional linker flags
```

---

#### 4.2.13 API Management (New — Not in Bench)

##### `moca api list`

```
Usage: moca api list [flags]

List all registered API endpoints.

Flags:
  --site string       Target site
  --doctype string    Filter by DocType
  --method string     Filter by HTTP method
  --format string     "table" (default), "json", "openapi"

Example:
  METHOD   PATH                                    SOURCE          AUTH
  GET      /api/v1/resource/SalesOrder              auto-generated  session/jwt/key
  GET      /api/v1/resource/SalesOrder/:name        auto-generated  session/jwt/key
  POST     /api/v1/resource/SalesOrder              auto-generated  session/jwt/key
  POST     /api/v1/resource/SalesOrder/:name/approve custom         session/jwt
  GET      /api/v1/orders/dashboard/summary          custom         session/jwt/key
  POST     /api/method/send_email                    whitelisted    session
  ...
  GET      /api/graphql                              auto-generated  session/jwt/key
```

##### `moca api test`

```
Usage: moca api test ENDPOINT [flags]

Test an API endpoint with authentication and timing.

Flags:
  --site string       Target site
  --method string     HTTP method (default: GET)
  --user string       Authenticate as user (default: Administrator)
  --api-key string    Authenticate with API key
  --data string       Request body (JSON)
  --repeat int        Repeat N times and show timing stats
  --verbose           Show full request/response headers

Example:
  $ moca api test /api/v1/resource/SalesOrder --site acme.localhost

  Status:    200 OK
  Time:      12ms
  Size:      4.2 KB
  Records:   20 of 1,245
```

##### `moca api docs`

```
Usage: moca api docs [flags]

Generate OpenAPI 3.0 specification from MetaType + APIConfig definitions.

Flags:
  --site string       Target site
  --output string     Output path (default: stdout)
  --format string     "json" (default), "yaml"
  --serve             Start a Swagger UI server for browsing
  --port int          Swagger UI port (default: 8002)
```

##### `moca api keys create`

```
Usage: moca api keys create [flags]

Create a new API key.

Flags:
  --site string       Target site
  --user string       Associated user
  --label string      Human-readable label
  --scopes strings    Permission scopes (e.g., "orders:read", "orders:write")
  --expires string    Expiry duration (e.g., "90d", "1y", "never")
  --ip-allow strings  IP allowlist
```

##### `moca api keys revoke`

```
Usage: moca api keys revoke KEY_ID [flags]

Revoke an API key immediately. All requests using this key will
be rejected.

Flags:
  --site string       Target site
  --force             Skip confirmation
```

##### `moca api keys list`

```
Usage: moca api keys list [flags]

List all API keys for a site.

Flags:
  --site string       Target site
  --user string       Filter by associated user
  --status string     "active", "revoked", "expired", "all" (default: active)
  --json              Output as JSON

Example:
  KEY ID           LABEL              USER              SCOPES              LAST USED     EXPIRES
  key_abc123       Mobile App         api@acme.com      orders:read,write   2h ago        2027-03-29
  key_def456       Partner Sync       sync@acme.com     all:read            5d ago        never
  key_ghi789       Reporting          report@acme.com   reports:read        12m ago       2026-06-30
```

##### `moca api keys rotate`

```
Usage: moca api keys rotate KEY_ID [flags]

Generate a new secret for an existing API key. The old secret
is immediately invalidated.

Flags:
  --site string       Target site
  --grace-period      Keep old secret valid for a duration (e.g., "1h")
```

##### `moca api webhooks list`

```
Usage: moca api webhooks list [flags]

List all configured webhooks.

Flags:
  --site string       Target site
  --doctype string    Filter by DocType
  --json              Output as JSON

Example:
  NAME                 DOCTYPE        EVENT        URL                                    STATUS
  order-notify         SalesOrder     on_submit    https://ext.example.com/order-hook     active ✓
  payment-sync         Payment        after_insert https://pay.example.com/webhook        active ✓
  erp-sync             SalesInvoice   on_submit    https://erp.old.com/api/invoice        failing ✗
```

##### `moca api webhooks logs`

```
Usage: moca api webhooks logs [WEBHOOK_NAME] [flags]

Show webhook delivery history and responses.

Flags:
  --site string       Target site
  --status string     "success", "failed", "all" (default: all)
  --limit int         Max entries (default: 50)
  --json              Output as JSON

Example:
  TIMESTAMP              WEBHOOK          EVENT        STATUS   RESPONSE   DURATION
  2026-03-29 15:04:18    order-notify     on_submit    200 OK   accepted   120ms
  2026-03-29 15:02:05    erp-sync         on_submit    timeout  -          30000ms
  2026-03-29 14:58:42    payment-sync     after_insert 200 OK   ok         85ms
  2026-03-29 14:55:10    erp-sync         on_submit    500      error      220ms
```

##### `moca api webhooks test`

```
Usage: moca api webhooks test WEBHOOK_NAME [flags]

Send a test payload to a configured webhook endpoint.

Flags:
  --site string       Target site
  --doctype string    DocType for generating test payload
  --event string      Event type to simulate (e.g., "on_submit")
```

---

#### 4.2.14 Monitoring & Diagnostics

##### `moca doctor`

**The most useful diagnostic command.** Checks everything and suggests fixes.

```
Usage: moca doctor [flags]

Flags:
  --fix               Attempt to auto-fix discovered issues
  --json              Output as JSON
  --verbose           Show all checks, not just failures

Example output:
  Moca Doctor — System Health Report
  ══════════════════════════════════════════════════════

  Infrastructure
  ──────────────────────────────────────────────────────
  ✓ PostgreSQL 16.2          connected (12ms)
  ✓ Redis 7.2.4              connected (1ms)
  ✗ Kafka 3.7.0              connection refused on localhost:9092
    → Fix: start Kafka, or set kafka.enabled=false in moca.yaml
  ✓ Meilisearch 1.7.0        connected (3ms)
  ✓ MinIO                    connected (8ms)

  Project
  ──────────────────────────────────────────────────────
  ✓ moca.yaml                valid
  ✓ moca.lock                up to date
  ✗ App "crm" v1.2.3         update available (v1.2.5)
  ✓ App "accounting" v2.1.0  up to date

  Sites
  ──────────────────────────────────────────────────────
  ✓ acme.localhost            healthy
    ├── ✓ Database schema      synced
    ├── ✓ Search index         synced (125,432 docs)
    ├── ✓ Scheduler            enabled, running
    ├── ✗ Pending migrations   2 unapplied
    │   → Fix: run `moca site migrate --site acme.localhost`
    └── ✓ Last backup          2h ago (verified ✓)

  ✓ globex.localhost          healthy
    ├── ✓ Database schema      synced
    ├── ✓ Search index         synced (45,120 docs)
    ├── ✓ Scheduler            enabled, running
    ├── ✓ Pending migrations   none
    └── ✗ Last backup          3d ago
        → Warning: consider running `moca backup create --site globex.localhost`

  Workers
  ──────────────────────────────────────────────────────
  ✓ Default queue             0 pending, 2 workers
  ✓ Long queue                3 pending, 1 worker
  ✓ Critical queue            0 pending, 1 worker
  ✗ Dead letter queue         12 failed jobs
    → Review with: `moca queue dead-letter list`

  Summary: 4 issues found (1 critical, 1 warning, 2 info)
```

##### `moca status`

```
Usage: moca status [flags]

Quick project status overview (lighter than doctor).

Example:
  Project: my-erp (development mode)
  Active site: acme.localhost
  Server: running on :8000 (PID 12345)
  Workers: 2 running
  Scheduler: running
  Sites: 3 (3 active)
  Queue: 3 pending / 0 failed
```

##### `moca monitor live`

```
Usage: moca monitor live [flags]

Launch an interactive TUI dashboard showing real-time metrics.

Flags:
  --refresh string    Refresh interval (default: "1s")

Displays:
  ┌─ Requests ──────────────────┬─ Workers ─────────────────────┐
  │ Req/s: 125                  │ Default:  2/2 active, 0 queue │
  │ Avg latency: 12ms           │ Long:     1/1 active, 3 queue │
  │ P99 latency: 85ms           │ Critical: 1/1 active, 0 queue │
  │ Errors/s: 0.2               │ Dead letter: 12 items         │
  │ Active connections: 42      │                               │
  ├─ Database ──────────────────┼─ Cache ───────────────────────┤
  │ Active queries: 3           │ Hit rate: 94.2%               │
  │ Pool: 12/25 connections     │ Memory: 128 MB                │
  │ Avg query time: 2.1ms       │ Keys: 45,230                  │
  ├─ Sites ─────────────────────┼─ Events ──────────────────────┤
  │ acme:    active (23 req/s)  │ Published: 450/min            │
  │ globex:  active (8 req/s)   │ Consumer lag: 12              │
  │ test:    idle               │ Outbox pending: 0             │
  └─────────────────────────────┴───────────────────────────────┘
```

##### `moca monitor audit`

```
Usage: moca monitor audit [flags]

Query the audit log.

Flags:
  --site string       Target site
  --doctype string    Filter by DocType
  --user string       Filter by user
  --action string     Filter by action (Create, Update, Submit, etc.)
  --since string      Time filter (e.g., "1h", "2d", "2026-03-01")
  --limit int         Max results (default: 50)
  --json              Output as JSON
```

---

#### 4.2.15 Queue & Events

##### `moca queue status`

```
Usage: moca queue status [flags]

Flags:
  --json              Output as JSON
  --watch             Refresh continuously

Example:
  QUEUE       PENDING    ACTIVE    FAILED    WORKERS    THROUGHPUT
  default     0          2         0         2          125 jobs/min
  long        3          1         0         1          8 jobs/min
  critical    0          0         0         1          45 jobs/min
  scheduler   0          1         0         1          1 job/min

  Dead letter: 12 items (oldest: 2d ago)
```

##### `moca queue list`

```
Usage: moca queue list [flags]

List jobs in a queue with their status and details.

Flags:
  --queue string      Queue name: "default", "long", "critical", "all" (default)
  --status string     Filter: "pending", "active", "failed", "all" (default: pending)
  --site string       Filter by site
  --limit int         Max results (default: 50)
  --json              Output as JSON

Example:
  ID              QUEUE     TYPE              SITE              STATUS    AGE
  job_abc123      long      generate_report   acme.localhost    active    2m
  job_def456      long      export_csv        acme.localhost    pending   5m
  job_ghi789      long      bulk_email        globex.localhost  pending   8m
```

##### `moca queue inspect`

```
Usage: moca queue inspect JOB_ID [flags]

Show detailed information about a specific job.

Flags:
  --json              Output as JSON

Example:
  Job ID:       job_abc123
  Queue:        long
  Type:         generate_report
  Site:         acme.localhost
  Status:       active
  Created:      2026-03-29 15:00:00 UTC
  Started:      2026-03-29 15:00:02 UTC
  Retries:      0/3
  Timeout:      5m
  Payload:
    report_name: "Sales Summary"
    filters: {"from_date": "2026-03-01", "to_date": "2026-03-29"}
    format: "xlsx"
```

##### `moca queue retry`

```
Usage: moca queue retry JOB_ID [flags]

Retry a failed job by re-enqueuing it.

Flags:
  --all-failed        Retry all failed jobs
  --queue string      Retry all failed in specific queue
  --force             Skip confirmation
```

##### `moca queue purge`

```
Usage: moca queue purge [flags]

Purge pending jobs from a queue.

Flags:
  --queue string      Queue to purge (required, or --all)
  --all               Purge all queues
  --site string       Only purge jobs for a specific site
  --type string       Only purge jobs of a specific type
  --force             Skip confirmation
```

##### `moca queue dead-letter list`

```
Usage: moca queue dead-letter list [flags]

List jobs in the dead letter queue (jobs that exhausted all retries).

Flags:
  --site string       Filter by site
  --limit int         Max results (default: 50)
  --json              Output as JSON
```

##### `moca queue dead-letter retry`

```
Usage: moca queue dead-letter retry JOB_ID [flags]

Move a dead letter job back to its original queue for retry.

Flags:
  --all               Retry all dead letter items
  --force             Skip confirmation
```

##### `moca queue dead-letter purge`

```
Usage: moca queue dead-letter purge [flags]

Delete all items from the dead letter queue.

Flags:
  --older-than string  Purge only items older than duration (e.g., "7d")
  --force              Skip confirmation
```

##### `moca events tail`

```
Usage: moca events tail [TOPIC] [flags]

Tail Kafka events in real-time (like `tail -f` for your event bus).

Flags:
  --site string       Filter by site
  --doctype string    Filter by doctype
  --event string      Filter by event type
  --format string     "short" (default), "full", "json"
  --since string      Start from offset (e.g., "1h", "beginning")

Example:
  $ moca events tail moca.doc.events --site acme --format short

  15:04:12 INSERT  SalesOrder   SO-0042   admin@acme.com
  15:04:15 UPDATE  Customer     CUST-001  admin@acme.com
  15:04:18 SUBMIT  SalesOrder   SO-0042   admin@acme.com
  15:04:20 INSERT  SalesInvoice INV-0018  admin@acme.com
  ^C
```

##### `moca events list-topics`

```
Usage: moca events list-topics [flags]

List all Kafka topics managed by Moca.

Flags:
  --json              Output as JSON
  --verbose           Show partition count, retention, and message counts

Example:
  TOPIC                          PARTITIONS   RETENTION   MESSAGES
  moca.doc.events                12           7d          1,245,000
  moca.audit.log                 6            90d         8,430,000
  moca.meta.changes              3            30d         342
  moca.integration.outbox        6            3d          12,500
  moca.workflow.transitions      6            30d         5,670
  moca.notifications             6            3d          89,000
  moca.search.indexing           3            1d          125,432
```

##### `moca events publish`

```
Usage: moca events publish TOPIC [flags]

Publish a test event to a Kafka topic. Useful for testing
consumers and webhook integrations.

Flags:
  --payload string    JSON event payload
  --file string       Read payload from file
  --site string       Site context for the event
  --doctype string    DocType for auto-generating a realistic payload
  --event string      Event type (e.g., "doc.created", "doc.submitted")
```

##### `moca events consumer-status`

```
Usage: moca events consumer-status [flags]

Show Kafka consumer group status including lag per partition.

Flags:
  --group string      Filter by consumer group
  --json              Output as JSON

Example:
  CONSUMER GROUP              TOPIC                  LAG     STATUS
  moca-webhook-dispatcher     moca.doc.events        0       up to date
  moca-search-indexer          moca.doc.events        12      catching up
  moca-search-indexer          moca.search.indexing   0       up to date
  moca-audit-writer           moca.audit.log         0       up to date
  custom-erp-sync             moca.doc.events        1,245   lagging ⚠
```

##### `moca events replay`

```
Usage: moca events replay TOPIC [flags]

Replay events from a specific point in time. Useful for rebuilding
search indexes, re-triggering webhooks, or disaster recovery.

Flags:
  --since string      Start time (e.g., "2026-03-29T10:00:00Z")
  --until string      End time (default: now)
  --consumer string   Target consumer group
  --dry-run           Show events without replaying
```

---

#### 4.2.16 User Management

##### `moca user add`

```
Usage: moca user add EMAIL [flags]

Flags:
  --site string           Target site
  --first-name string     First name
  --last-name string      Last name
  --password string       Password (prompted if not provided)
  --roles strings         Roles to assign
  --send-welcome          Send welcome email

Example:
  moca user add john@acme.com \
    --site acme.localhost \
    --first-name John \
    --last-name Doe \
    --roles "Sales Manager,Sales User"
```

##### `moca user remove`

```
Usage: moca user remove EMAIL [flags]

Remove a user from a site.

Flags:
  --site string       Target site
  --force             Skip confirmation
  --keep-data         Keep documents owned by the user
```

##### `moca user set-password`

```
Usage: moca user set-password EMAIL [flags]

Set or reset a user's password.

Flags:
  --site string       Target site
  --password string   New password (prompted securely if not provided)
  --force-reset       Force password change on next login
```

##### `moca user set-admin-password`

```
Usage: moca user set-admin-password [flags]

Set the Administrator password for a site.

Flags:
  --site string       Target site
  --password string   New password (prompted securely if not provided)
```

##### `moca user add-role`

```
Usage: moca user add-role EMAIL ROLE [flags]

Assign a role to a user.

Flags:
  --site string       Target site
```

##### `moca user remove-role`

```
Usage: moca user remove-role EMAIL ROLE [flags]

Remove a role from a user.

Flags:
  --site string       Target site
```

##### `moca user list`

```
Usage: moca user list [flags]

List all users on a site.

Flags:
  --site string       Target site
  --role string       Filter by role
  --status string     Filter: "active", "disabled", "all" (default: active)
  --json              Output as JSON
  --verbose           Show roles, last login, email
```

##### `moca user disable`

```
Usage: moca user disable EMAIL [flags]

Disable a user account. The user will not be able to log in.

Flags:
  --site string       Target site
```

##### `moca user enable`

```
Usage: moca user enable EMAIL [flags]

Re-enable a previously disabled user account.

Flags:
  --site string       Target site
```

##### `moca user impersonate`

```
Usage: moca user impersonate EMAIL [flags]

Generate a one-time login URL for any user (dev mode only).

Flags:
  --site string       Target site
  --open              Open URL in browser directly
  --ttl string        URL validity (default: "5m")
```

---

#### 4.2.17 Search Management

##### `moca search rebuild`

```
Usage: moca search rebuild [flags]

Rebuild Meilisearch indexes from database.

Flags:
  --site string       Target site (default: active)
  --all-sites         Rebuild for all sites
  --doctype string    Rebuild for specific DocType only
  --batch-size int    Batch size for indexing (default: 1000)
```

##### `moca search status`

```
Usage: moca search status [flags]

Show Meilisearch index status for all sites/doctypes.

Flags:
  --site string       Filter by site
  --json              Output as JSON

Example:
  SITE              DOCTYPE          DOCUMENTS    INDEX SIZE    LAST SYNC
  acme.localhost    SalesOrder       12,450       8.2 MB        5m ago
  acme.localhost    Customer         3,200        1.8 MB        5m ago
  acme.localhost    Item             8,900        4.1 MB        5m ago
  globex.localhost  SalesOrder       4,120        2.8 MB        5m ago
```

##### `moca search query`

```
Usage: moca search query QUERY [flags]

Search from the command line (uses Meilisearch).

Flags:
  --site string       Target site
  --doctype string    Search within a specific DocType
  --limit int         Max results (default: 10)
  --json              Output as JSON

Example:
  $ moca search query "acme corp" --site acme.localhost --doctype Customer

  SCORE    DOCTYPE     NAME          TITLE
  0.98     Customer    CUST-001      Acme Corporation
  0.72     Customer    CUST-045      Acme Supplies Ltd
  0.65     SalesOrder  SO-0042       Order for Acme Corp
```

---

#### 4.2.18 Cache Management

##### `moca cache clear`

```
Usage: moca cache clear [flags]

Flags:
  --site string       Target site (default: active)
  --all-sites         Clear for all sites
  --type string       Clear specific cache: "meta", "doc", "session", "all" (default)
```

##### `moca cache warm`

```
Usage: moca cache warm [flags]

Pre-load frequently accessed data into cache.

Flags:
  --site string       Target site
  --meta              Warm metadata cache (default: true)
  --hot-docs          Warm frequently accessed documents
```

---

#### 4.2.19 Log Management

##### `moca log search`

```
Usage: moca log search QUERY [flags]

Search through log files using structured queries.

Flags:
  --process string    "server", "worker", "scheduler", "all"
  --level string      Minimum level
  --since string      Time filter (e.g., "1h", "2d")
  --site string       Filter by site
  --request-id string Find logs for a specific request
  --limit int         Max results (default: 100)
  --json              Output as JSON
```

##### `moca log export`

```
Usage: moca log export [flags]

Export logs for a time range to a file.

Flags:
  --since string      Start time (required)
  --until string      End time (default: now)
  --process string    Filter by process
  --site string       Filter by site
  --format string     "json" (default), "text"
  --output string     Output file path (default: stdout)
  --compress          Compress output with gzip
```

##### `moca log tail`

```
Usage: moca log tail [flags]

Tail logs in real-time with rich filtering.

Flags:
  --process string    "server", "worker", "scheduler", "all" (default)
  --level string      Minimum level: "debug", "info", "warn", "error"
  --site string       Filter by site
  --request-id string Follow a specific request
  --json              Raw JSON log output
  --no-color          Disable color output

Example:
  $ moca log tail --level warn --site acme

  15:04:12 WARN  [server] acme  Slow query (245ms): SELECT ... FROM tab_sales_order WHERE ...
  15:04:15 ERROR [worker] acme  Webhook delivery failed: https://ext.example.com/hook (timeout)
  15:04:18 WARN  [server] acme  Rate limit approaching for API key "mobile-app" (58/60 rpm)
```

---

#### 4.2.20 Translation Management

##### `moca translate export`

```
Usage: moca translate export [flags]

Export all translatable strings from installed apps.

Flags:
  --app string        Export from specific app
  --format string     "po" (default), "csv", "json"
  --output string     Output directory
```

##### `moca translate import`

```
Usage: moca translate import FILE [flags]

Import translations from a PO, CSV, or JSON file.

Flags:
  --app string        Target app
  --language string   Target language code (e.g., "ar", "fr", "de")
  --overwrite         Overwrite existing translations
```

##### `moca translate status`

```
Usage: moca translate status [flags]

Show translation coverage for installed apps.

Flags:
  --app string        Filter by app
  --language string   Filter by language
  --json              Output as JSON

Example:
  APP            LANGUAGE    TRANSLATED    TOTAL    COVERAGE
  core           ar          1,245         1,300    95.8%
  core           fr          1,180         1,300    90.8%
  crm            ar          820           950      86.3%
  crm            fr          450           950      47.4%
  accounting     ar          680           720      94.4%
```

##### `moca translate compile`

```
Usage: moca translate compile [flags]

Compile PO translation files to optimized binary MO format
for production use.

Flags:
  --app string        Compile for a specific app
  --language string   Compile a specific language only
```

---

#### 4.2.21 Notification Configuration

##### `moca notify test-email`

```
Usage: moca notify test-email [flags]

Send a test email to verify SMTP configuration.

Flags:
  --site string       Target site
  --to string         Recipient email address (required)
  --provider string   "smtp" (default), "ses", "sendgrid"
```

##### `moca notify config`

```
Usage: moca notify config [flags]

Show or update notification provider settings.

Flags:
  --site string       Target site
  --set KEY=VALUE     Set a notification config value
  --json              Output current config as JSON

Example:
  moca notify config --set smtp.host=smtp.gmail.com --set smtp.port=587
  moca notify config --json
```

> **Note on OAuth2/SAML/OIDC:** Enterprise SSO configuration (OAuth2 providers, SAML identity providers, OIDC settings) is managed via the Desk UI administration panel at `Settings > Authentication`. CLI-based auth configuration is planned for a future release. For headless/automated deployments, use `moca config set` to write auth provider settings directly (e.g., `moca config set auth.oauth2.client_id=xxx --site acme.localhost`).

---

#### 4.2.22 Shell Completion

##### `moca completion`

```
Usage: moca completion SHELL

Generate shell completion scripts.

Examples:
  # Bash
  moca completion bash > /etc/bash_completion.d/moca

  # Zsh
  moca completion zsh > "${fpath[1]}/_moca"

  # Fish
  moca completion fish > ~/.config/fish/completions/moca.fish
```

---

## 5. Global Flags (Available on All Commands)

```
Global Flags:
  --site string       Override active site for this command
  --project string    Override project directory detection
  --env string        Override environment (dev/staging/prod)
  --json              Output as JSON (machine-readable)
  --table             Output as formatted table
  --quiet             Suppress non-essential output
  --verbose           Increase output verbosity
  --no-color          Disable colored output
  --debug             Enable debug logging
  --trace             Enable trace-level logging (very verbose)
  --timeout duration  Command timeout (default: varies by command)
  --help              Show help for any command
```

---

## 6. Context Detection & Resolution

One of Moca CLI's most important features is **automatic context detection**. It eliminates the need for repetitive flags.

```
┌─────────────────────────────────────────────────────────┐
│  Context Resolution Pipeline                             │
│                                                          │
│  1. Command-line flags (--site, --env, --project)        │
│     ▼ (highest priority)                                 │
│  2. Environment variables                                │
│     MOCA_SITE, MOCA_ENV, MOCA_PROJECT                    │
│     ▼                                                    │
│  3. Local state files                                    │
│     .moca/current_site, .moca/environment                │
│     ▼                                                    │
│  4. Project config                                       │
│     moca.yaml (development/production sections)          │
│     ▼                                                    │
│  5. Auto-detection                                       │
│     Walk up directory tree looking for moca.yaml          │
│     ▼ (lowest priority)                                  │
│  6. Defaults                                             │
│     Hardcoded sensible defaults                          │
└─────────────────────────────────────────────────────────┘
```

---

## 7. Error Handling Philosophy

Every error in Moca CLI follows this structure:

```
Error: {what happened}

Context:
  {relevant state that caused the error}

Cause:
  {why it happened, if determinable}

Fix:
  {exactly what command to run or action to take}

Reference:
  {link to docs if applicable}
```

**Example: database connection failure**

```
Error: Cannot connect to PostgreSQL

Context:
  Host: localhost:5432
  Database: moca_system
  User: moca

Cause:
  Connection refused — PostgreSQL may not be running.

Fix:
  1. Start PostgreSQL:  sudo systemctl start postgresql
  2. Or update host:    moca config set infrastructure.database.host <host>
  3. Verify manually:   psql -h localhost -p 5432 -U moca -d moca_system

Reference:
  https://docs.moca.dev/troubleshooting/database-connection
```

**Example: migration failure**

```
Error: Migration failed for site acme.localhost

Context:
  Migration: 003_add_delivery_tracking.sql (app: crm v1.2.5)
  SQL Error: column "delivery_date" already exists

Cause:
  The column was likely added manually or by a previous partial migration.

Fix:
  1. Check schema:    moca db diff --site acme.localhost --doctype SalesOrder
  2. Skip migration:  moca db migrate --site acme.localhost --skip 003
  3. Or rollback:     moca deploy rollback
  4. Full diagnostic: moca doctor --site acme.localhost

  Your site has been left unchanged. The backup taken before
  migration is available at: sites/acme.localhost/backups/pre_migrate_20260329.sql.gz
```

---

## 8. Extension System — Custom Commands

Apps can register custom CLI commands, extending the `moca` command tree.

### 8.1 Registering Custom Commands

```go
// In an app's hooks.go:
package crm

import (
    "github.com/osama1998H/moca/pkg/cli"
    "github.com/spf13/cobra"
)

func init() {
    cli.RegisterCommand(&cobra.Command{
        Use:   "crm:sync-contacts",
        Short: "Sync contacts from external CRM",
        RunE:  syncContactsCmd,
    })

    cli.RegisterCommand(&cobra.Command{
        Use:   "crm:generate-quotes",
        Short: "Bulk generate quotes from templates",
        RunE:  generateQuotesCmd,
    })
}
```

### 8.2 Using Custom Commands

```bash
# Custom commands are namespaced by app
moca crm:sync-contacts --source salesforce --site acme.localhost
moca crm:generate-quotes --template standard --site acme.localhost

# List all custom commands
moca help | grep ":"
```

### 8.3 Command Discovery

When `moca` starts, it scans all installed apps for registered commands and adds them to the command tree. This happens at CLI initialization time, not at runtime, so there's no performance penalty.

---

## 9. CLI Internal Package Layout

```
cmd/
  moca/                             # Main CLI entry point
    main.go                         # cobra root command
    init.go                         # moca init
    version.go                      # moca version
    doctor.go                       # moca doctor
    status.go                       # moca status
    site/                           # moca site *
      create.go
      drop.go
      list.go
      use.go
      migrate.go
      clone.go
      browse.go
      info.go
      ...
    app/                            # moca app *
      new.go
      get.go
      install.go
      uninstall.go
      update.go
      resolve.go
      publish.go
      ...
    db/                             # moca db *
      console.go
      migrate.go
      rollback.go
      diff.go
      seed.go
      ...
    backup/                         # moca backup *
      create.go
      restore.go
      list.go
      verify.go
      ...
    deploy/                         # moca deploy *
      setup.go
      update.go
      rollback.go
      history.go
      ...
    config/                         # moca config *
    generate/                       # moca generate *
    dev/                            # moca dev *
    test/                           # moca test *
    build/                          # moca build *
    worker/                         # moca worker *
    scheduler/                      # moca scheduler *
    api/                            # moca api *
    user/                           # moca user *
    search/                         # moca search *
    cache/                          # moca cache *
    queue/                          # moca queue *
    events/                         # moca events *
    monitor/                        # moca monitor *
    log/                            # moca log *
    translate/                      # moca translate *
    completion/                     # moca completion *

internal/                           # CLI internal services
  context/                          # Context resolver (project, site, env)
    resolver.go
    project.go
    site.go
    environment.go
  output/                           # Output formatting
    table.go
    json.go
    progress.go
    color.go
    error.go                        # Rich error formatting
  drivers/                          # Infrastructure drivers
    postgres.go
    redis.go
    kafka.go
    meilisearch.go
    s3.go
  lockfile/                         # moca.lock management
    resolve.go
    parse.go
    write.go
  manifest/                         # moca.yaml management
    parse.go
    validate.go
    merge.go
  scaffold/                         # Code generation templates
    app.go
    doctype.go
    module.go
    templates/                      # Go text/template files
  process/                          # Process management
    supervisor.go                   # In-process goroutine supervisor
    systemd.go                      # systemd integration
    pidfile.go
  health/                           # Health check logic
    checker.go
    postgres.go
    redis.go
    kafka.go
    site.go
```

---

## 10. Comparison: Bench vs Moca CLI

| Capability | Bench (Frappe) | Moca CLI |
|---|---|---|
| **Distribution** | pip install (Python + deps) | Single Go binary |
| **Project init** | `bench init` | `moca init` |
| **New site** | `bench new-site` | `moca site create` |
| **Drop site** | `bench drop-site` | `moca site drop` |
| **New app** | `bench new-app` / `make-app` | `moca app new` |
| **Get app** | `bench get-app URL` | `moca app get SOURCE --version RANGE` |
| **Install app** | `bench --site X install-app` | `moca app install APP` |
| **Version pinning** | None (always HEAD) | `moca.lock` with semver ranges |
| **Update** | `bench update` (all-or-nothing) | `moca deploy update` (atomic, rollback) |
| **Rollback** | Manual (restore backup) | `moca deploy rollback` (one command) |
| **Dev server** | `bench start` (honcho + Procfile) | `moca serve` (single process) |
| **Production setup** | `bench setup production` (many steps) | `moca deploy setup` (one command) |
| **NGINX config** | `bench setup nginx` | `moca generate caddy` (or nginx) |
| **Systemd** | `bench setup systemd` | `moca generate systemd` |
| **Docker** | Community-maintained | `moca generate docker` (built-in) |
| **Kubernetes** | Not supported | `moca generate k8s` |
| **Backup** | `bench backup` | `moca backup create` (encrypted, verified) |
| **Restore** | `bench restore` | `moca backup restore` (from ID, file, or S3) |
| **Backup verification** | Not available | `moca backup verify` |
| **Schema diff** | Not available | `moca db diff` |
| **Schema rollback** | Not available | `moca db rollback` |
| **Diagnostics** | `bench doctor` (basic) | `moca doctor` (comprehensive + fix suggestions) |
| **Live monitoring** | Not available | `moca monitor live` (TUI dashboard) |
| **Audit log query** | Not available from CLI | `moca monitor audit` |
| **API management** | Not available | `moca api list/docs/keys/webhooks` |
| **Queue inspection** | `bench show-pending-jobs` | `moca queue status/list/inspect/retry` |
| **Event streaming** | Not available | `moca events tail/replay/consumer-status` |
| **Console** | `bench console` (IPython) | `moca dev console` (Go REPL) |
| **Profiling** | Not available | `moca dev profile` (flamegraph) |
| **Test data generation** | Not available | `moca test factory` |
| **Translations** | `bench build-message-files` | `moca translate export/import/compile` |
| **Shell completion** | Not available | `moca completion bash/zsh/fish` |
| **Custom commands** | `bench --help` shows app commands | `moca APP:command` (namespaced) |
| **Error messages** | Raw Python tracebacks | Context-aware with suggested fixes |
| **Output formats** | Text only | Text, JSON, Table (--json, --table) |
| **Multi-environment** | Not built-in | `--env dev/staging/prod` |
| **Config diff** | Not available | `moca config diff` |

---

## 11. ADR Summary — Key CLI Design Decisions

### ADR-CLI-001: Single Binary over Python Package

**Decision:** Distribute the Moca CLI as a single compiled Go binary.
**Rationale:** Eliminates Python version conflicts, virtualenv management, pip dependency resolution, and the entire class of "bench won't install" issues that plague the Frappe ecosystem. A Go binary runs on any Linux/macOS/Windows machine without prerequisites.
**Trade-off:** Apps cannot dynamically extend the CLI with arbitrary Python scripts. Instead, they register Go commands at build time via `cli.RegisterCommand()` in `hooks.go`. These CLI extensions are compiled into the binary during `moca build server`.

### ADR-CLI-002: Cobra over Custom CLI Framework

**Decision:** Use the Cobra library for CLI command structure.
**Rationale:** Cobra is the de facto standard for Go CLIs (used by kubectl, docker, hugo, gh). It provides automatic help generation, shell completion, nested subcommands, and flag parsing. Developers building Moca apps will find the patterns familiar.
**Trade-off:** Slight overhead from Cobra's reflection-based flag binding. Negligible for a CLI tool.

### ADR-CLI-003: Structured Subcommands over Flat Commands

**Decision:** Use `moca site create` instead of `bench new-site`; `moca app get` instead of `bench get-app`.
**Rationale:** Bench's flat command structure (`bench new-site`, `bench get-app`, `bench backup`, `bench --site X install-app`) mixes site-scoped and global commands inconsistently. Moca CLI groups commands by resource (`site`, `app`, `db`, `backup`, etc.), which is more discoverable, more consistent, and enables better shell completion.
**Trade-off:** Slightly longer commands for simple operations. Mitigated by shell completion and aliases.

### ADR-CLI-004: moca.yaml + moca.lock over Implicit Configuration

**Decision:** Use an explicit project manifest (`moca.yaml`) and lockfile (`moca.lock`) instead of bench's implicit directory-structure-as-config approach.
**Rationale:** Bench infers state from directory contents (apps in `apps/`, sites in `sites/`, config in `sites/common_site_config.json`). This leads to inconsistencies when filesystem state diverges from expected state. Moca's explicit manifest is the single source of truth, the lockfile ensures reproducibility, and both are version-controlled.
**Trade-off:** Requires managing an additional file. The lockfile is auto-generated.

### ADR-CLI-005: Atomic Deploy with Auto-Rollback over All-or-Nothing Update

**Decision:** `moca deploy update` uses phased execution with automatic rollback on failure.
**Rationale:** `bench update` performs backup → pull → requirements → build → migrate → restart in sequence. If migration fails on site 3 of 5, you're left in a partially-migrated state with no automatic recovery. Moca's deploy engine takes snapshots before each phase and rolls back automatically on failure.
**Trade-off:** Slightly slower updates due to snapshot overhead. Worth it for reliability.

### ADR-CLI-006: In-Process Dev Server over Multi-Process Procfile

**Decision:** `moca serve` runs HTTP server, workers, scheduler, and file watcher in a single Go process.
**Rationale:** Bench uses honcho to manage 4+ processes (web, worker, scheduler, socketio, redis-queue, redis-cache). This causes slow startup, race conditions during boot, and complex process management. Go's goroutine model lets us run everything in a single process for development, with sub-second startup.
**Trade-off:** Less isolation between components in dev mode. Production mode still uses separate processes for stability.

---

## 12. Development Workflow Example

A complete walkthrough of what using Moca CLI feels like:

```bash
# 1. Install (single binary download)
curl -sSL https://get.moca.dev | sh
# or: brew install moca

# 2. Initialize a project
moca init my-erp --apps crm,accounting
cd my-erp

# 3. Create a development site
moca site create dev.localhost --admin-password admin --install-apps crm,accounting

# 4. Start developing
moca serve
# → Server running at http://dev.localhost:8000
# → React Desk at http://localhost:3000 (with HMR)
# → GraphiQL at http://localhost:8000/api/graphql

# 5. Create a custom app
moca app new custom-hr --module "Human Resources" --doctype Employee

# 6. Install it on the dev site
moca app install custom-hr

# 7. Run tests
moca test run --app custom-hr --verbose

# 8. Generate test data
moca test factory Employee 100

# 9. Check everything is healthy
moca doctor

# 10. Deploy to production
moca deploy setup --domain myerp.example.com --email admin@example.com

# 11. Later: update production
moca deploy update

# 12. Something went wrong? Roll back
moca deploy rollback
```

---

## 13. Production Workflow Example

```bash
# Initial production setup (run once)
moca init /opt/moca/my-erp --template enterprise
cd /opt/moca/my-erp

moca app get crm --version "~1.2.0"
moca app get accounting --version "^2.0.0"
moca app resolve

moca site create acme.myerp.com \
  --admin-password "${ADMIN_PASS}" \
  --install-apps crm,accounting \
  --timezone "Asia/Baghdad" \
  --currency IQD

moca deploy setup \
  --domain myerp.com \
  --email admin@myerp.com \
  --proxy caddy \
  --process systemd \
  --workers 4 \
  --background 3

# Daily operations
moca status                              # quick health check
moca doctor                              # thorough diagnostic
moca monitor live                        # real-time TUI dashboard
moca backup create --all-sites --upload  # manual backup
moca log tail --level error              # watch for errors

# Updates (run periodically)
moca app update --dry-run                # check what would update
moca deploy update                       # safe atomic update

# Troubleshooting
moca queue dead-letter list              # check failed jobs
moca events tail moca.doc.events         # watch event stream
moca db diff --site acme.myerp.com       # check schema drift
moca monitor audit --user admin --since 1h  # audit trail
```

---

*This document defines the CLI architecture for Moca v1.0. It should be read alongside [MOCA_SYSTEM_DESIGN.md](./MOCA_SYSTEM_DESIGN.md) for the full framework architecture.*
