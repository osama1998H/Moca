# MS-26: Documentation, Packaging, and v1.0 Polish — Design Spec

**Date:** 2026-04-13
**Status:** Approved
**Dependencies:** MS-25 (Testing Framework — complete)

## Goal

Developer docs, API reference, deployment guides, release packaging (GoReleaser, Docker, install script), and final v1.0 polish.

## Decisions

| Topic | Decision | Rationale |
|-------|----------|-----------|
| GoReleaser | Add alongside existing `release.yml` | Evaluate adoption in the future; current CI already works |
| Dockerfile | Single file with `--target` per binary | One file to maintain, separate lean images per binary |
| License | Apache-2.0 | Patent protection, enterprise-friendly, Go ecosystem standard |
| Auto-generated docs | Into wiki with `<!-- AUTO-GENERATED -->` markers | Keeps wiki as single source of truth; preserves hand-written content |
| Docker image registry | ghcr.io via extended `release.yml` | Free for public repos, integrated with GitHub |
| Deployment guides | Separate wiki pages per strategy | Single-server, Docker, Kubernetes each get their own page |
| Docker Compose (user projects) | Already handled by `moca generate docker` (MS-21) | No new work needed |

## Out of Scope

- Video tutorials
- Hosted docs platform (wiki submodule is the docs platform)
- Changes to existing `release.yml` build logic (only additive: Docker push job)
- New CLI commands (MS-21's `moca generate` and `moca deploy` already cover deployment)

## Phase 1: Foundation

### 1.1 LICENSE

- File: `LICENSE` (repo root)
- Standard Apache-2.0 full text
- Copyright line: `Copyright 2026 Osama Mohammed`

### 1.2 CONTRIBUTING.md

- File: `CONTRIBUTING.md` (repo root)
- GitHub auto-detects this for new issues/PRs
- Content: brief welcome, links to wiki Contributing-* pages for details:
  - `Contributing-Development-Setup` — environment setup, Go version, Docker services
  - `Contributing-Code-Conventions` — naming, formatting, linting rules
  - `Contributing-Testing-Guide` — unit tests, integration tests, benchmarks
  - `Contributing-CI-CD-Pipeline` — GitHub Actions workflows
- Also includes: how to report bugs, how to propose features, PR process, code of conduct reference

### 1.3 CHANGELOG.md Update

- Add entries for milestones completed since 0.1.0-mvp:
  - `[0.2.0-alpha]` — MS-11 through MS-16 (CLI Operational, Multitenancy, App Scaffolding, Permissions, Background Jobs, CLI Queue/Events/Search)
  - `[0.3.0-beta]` — MS-17 through MS-23 (React Desk, API Keys/Webhooks, Desk Real-Time, GraphQL/Dashboard/Reports, Deployment, Security Hardening, Workflow Engine)
  - `[0.4.0-rc]` — MS-24 through MS-25 (Observability, Testing Framework)
  - `[1.0.0]` — MS-26 (Documentation, Packaging) — header only, filled at release
- Follow existing Keep a Changelog format
- Each entry follows the pattern: section header (Added/Changed/Fixed) with bullet points per feature

### 1.4 GoReleaser

- File: `.goreleaser.yml` (repo root)
- Configuration:
  - `builds`: 5 binaries (`moca`, `moca-server`, `moca-worker`, `moca-scheduler`, `moca-outbox`)
  - `goos`: linux, darwin, windows
  - `goarch`: amd64, arm64
  - `ldflags`: `-X main.Version={{.Version}} -X main.Commit={{.ShortCommit}} -X main.BuildDate={{.Date}}`
  - `env`: `CGO_ENABLED=0`
  - `archives`: `.tar.gz` for linux/darwin, `.zip` for windows
  - `checksum`: SHA-256
  - `snapshot`: enabled for local dev builds
  - `release.disable: true` — GoReleaser does NOT publish; existing `release.yml` handles GitHub Releases
- Makefile target:
  ```makefile
  release-local:  ## Build release archives locally (snapshot)
      goreleaser build --snapshot --clean
  ```

## Phase 2: Docker & CI

### 2.1 Dockerfile

- File: `Dockerfile` (repo root)
- Structure:

  ```
  # ---- Builder stage (shared) ----
  FROM golang:1.26-alpine AS builder
  WORKDIR /src
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  ARG VERSION=dev COMMIT=unknown BUILD_DATE=unknown
  RUN CGO_ENABLED=0 go build -ldflags "..." -o /out/moca ./cmd/moca
  RUN CGO_ENABLED=0 go build -ldflags "..." -o /out/moca-server ./cmd/moca-server
  RUN CGO_ENABLED=0 go build -ldflags "..." -o /out/moca-worker ./cmd/moca-worker
  RUN CGO_ENABLED=0 go build -ldflags "..." -o /out/moca-scheduler ./cmd/moca-scheduler
  RUN CGO_ENABLED=0 go build -ldflags "..." -o /out/moca-outbox ./cmd/moca-outbox

  # ---- Per-binary targets ----
  FROM gcr.io/distroless/static-debian12 AS moca
  COPY --from=builder /out/moca /usr/local/bin/moca
  ENTRYPOINT ["moca"]

  FROM gcr.io/distroless/static-debian12 AS moca-server
  COPY --from=builder /out/moca-server /usr/local/bin/moca-server
  EXPOSE 8000
  ENTRYPOINT ["moca-server"]

  FROM gcr.io/distroless/static-debian12 AS moca-worker
  COPY --from=builder /out/moca-worker /usr/local/bin/moca-worker
  ENTRYPOINT ["moca-worker"]

  FROM gcr.io/distroless/static-debian12 AS moca-scheduler
  COPY --from=builder /out/moca-scheduler /usr/local/bin/moca-scheduler
  ENTRYPOINT ["moca-scheduler"]

  FROM gcr.io/distroless/static-debian12 AS moca-outbox
  COPY --from=builder /out/moca-outbox /usr/local/bin/moca-outbox
  ENTRYPOINT ["moca-outbox"]
  ```

- Labels: `org.opencontainers.image.source`, `org.opencontainers.image.version`, `org.opencontainers.image.description`

### 2.2 .dockerignore

- File: `.dockerignore` (repo root)
- Excludes: `.git`, `bin/`, `desk/node_modules/`, `spikes/`, `wiki/`, `docs/`, `*.md` (except go.mod/go.sum), test fixtures, IDE files

### 2.3 Production Docker Compose Example

- File: `docker-compose.example.yml` (repo root)
- Services:
  - `moca-server` — builds from Dockerfile `--target moca-server`, port 8000, depends on postgres/redis
  - `moca-worker` — `--target moca-worker`, depends on postgres/redis
  - `moca-scheduler` — `--target moca-scheduler`, depends on redis
  - `moca-outbox` — `--target moca-outbox`, depends on postgres/redis
  - `postgres` — PostgreSQL 16, named volume, health check
  - `redis` — Redis 7, named volume, health check
  - `meilisearch` — Meilisearch v1.12, named volume, health check
- Shared `.env.example` for configuration
- Commented resource limits users can uncomment
- Networks: single `moca` bridge network

### 2.4 CI — Docker Image Publishing

- Extend `.github/workflows/release.yml` with new job: `docker-publish`
- Runs after existing `test` job (same dependency as `build`)
- Steps:
  1. Checkout with submodules
  2. Set up Docker Buildx
  3. Login to ghcr.io (`docker/login-action`)
  4. Build and push 5 images using `docker/build-push-action`:
     - `ghcr.io/osama1998h/moca:$TAG`
     - `ghcr.io/osama1998h/moca-server:$TAG`
     - `ghcr.io/osama1998h/moca-worker:$TAG`
     - `ghcr.io/osama1998h/moca-scheduler:$TAG`
     - `ghcr.io/osama1998h/moca-outbox:$TAG`
  5. Tags: version tag (`v1.0.0`) + `latest`
- Permissions: `packages: write` (already present for desk-publish)

## Phase 3: Auto-generation Tooling

### 3.1 Doc Generator Package

- Package: `internal/docgen/`
- Files:
  - `cli.go` — CLI reference generator
  - `api.go` — API/OpenAPI reference generator
  - `inject.go` — wiki marker injection utility
  - `cli_test.go`, `api_test.go`, `inject_test.go` — unit tests

### 3.2 CLI Reference Generator (`internal/docgen/cli.go`)

- Imports the root Cobra command from `cmd/moca/`
- Walks command tree recursively using `cmd.Commands()`
- For each command extracts: full path, aliases, short description, long description, flags (name, type, default, description), usage line, examples
- Groups commands by top-level group (site, app, db, deploy, generate, backup, queue, events, search, monitor, etc.)
- Outputs markdown with:
  - Table of contents with anchor links
  - Per-group sections with command tables
  - Per-command detail blocks (usage, flags table, examples)

### 3.3 API Reference Generator (`internal/docgen/api.go`)

- Reads registered routes from the API gateway configuration
- Generates OpenAPI 3.0 YAML spec → `docs/generated/openapi.yaml`
- Converts to markdown tables:
  - Standard CRUD endpoints: `POST /api/v1/resource/{doctype}`, `GET`, `PUT`, `DELETE`
  - Meta endpoint: `GET /api/v1/meta/{doctype}`
  - Method endpoint: `POST /api/v1/method/{dotted.path}`
  - Query parameters: filters, fields, order_by, limit, offset
  - Request/response envelope format
  - Authentication headers
  - Rate limiting headers
  - Error response format

### 3.4 Wiki Injection (`internal/docgen/inject.go`)

- Function: `InjectSection(filePath, startMarker, endMarker, content string) error`
- Reads file, finds `<!-- AUTO-GENERATED:START -->` and `<!-- AUTO-GENERATED:END -->` markers
- Replaces everything between markers with new content
- Preserves all content before start marker and after end marker
- Errors if markers not found (prevents silent failures on renamed files)
- Adds generation timestamp comment inside markers

### 3.5 Entry Point

- Binary: `cmd/moca-docgen/main.go`
- Subcommands: `cli`, `api`, `all`
- Not included in release builds (development tool only)

### 3.6 Makefile Targets

```makefile
docs-generate:       ## Generate CLI + API reference into wiki
    go run ./cmd/moca-docgen all

docs-generate-cli:   ## Generate CLI reference only
    go run ./cmd/moca-docgen cli

docs-generate-api:   ## Generate API reference only
    go run ./cmd/moca-docgen api
```

### 3.7 Wiki Marker Placement

Add markers to existing wiki files:

**`wiki/Reference-CLI-Commands.md`:**
```markdown
# CLI Commands

(existing hand-written overview stays here)

## Command Reference

<!-- AUTO-GENERATED:START -->
(generated content)
<!-- AUTO-GENERATED:END -->

(any hand-written tips/notes below stay here)
```

**`wiki/Reference-REST-API.md`:**
```markdown
# REST API

(existing hand-written overview stays here)

## Endpoints

<!-- AUTO-GENERATED:START -->
(generated content)
<!-- AUTO-GENERATED:END -->

(any hand-written examples below stay here)
```

## Phase 4: Wiki Expansion

### 4.1 Rewrite Stub Pages

**`Operations-Deployment.md`** — from placeholder to hub page:
- Overview of Moca's 5-process architecture
- Link to the three deployment guides
- Prerequisites summary (PostgreSQL 16+, Redis 7+, Meilisearch v1.12)
- Quick decision matrix: "Which deployment method is right for you?"

**`Operations-Docker-Setup.md`** — from placeholder to full content:
- Development Docker setup (existing `docker-compose.yml` for testing)
- Production Docker images (ghcr.io, Dockerfile targets)
- `moca generate docker` command reference
- Building custom images

### 4.2 New Deployment Guide Pages

**`Operations-Deployment-Single-Server.md`:**
- Target: Ubuntu 22.04/24.04 LTS
- Steps: install prerequisites, install Moca via `install.sh`, `moca init`, configure `moca.yaml`, `moca site create`, `moca deploy setup --proxy caddy --process systemd`
- Post-setup: backup scheduling, monitoring, log rotation
- Troubleshooting section

**`Operations-Deployment-Docker.md`:**
- Pull from ghcr.io or build locally
- `docker-compose.example.yml` walkthrough
- Environment configuration (`.env`)
- Volume management and backups
- Scaling workers (`docker compose up --scale moca-worker=3`)
- Upgrading: pull new tags, recreate containers

**`Operations-Deployment-Kubernetes.md`:**
- `moca generate k8s` walkthrough
- Manifest customization (replicas, resources, env)
- Ingress configuration with TLS
- HPA tuning for moca-server and moca-worker
- Secrets management (external-secrets or sealed-secrets)
- Multi-tenant considerations (one namespace per environment)

### 4.3 Expand Existing Pages

**`Guide-Getting-Started.md`** — verify and update:
- Install → init → create site → create app → serve flow
- Ensure commands match current CLI behavior
- Add expected output snippets

**`Guide-Creating-Your-First-App.md`** — ensure coverage of:
- App manifest (`app.yaml`)
- Defining MetaTypes (JSON schema files)
- Writing controllers
- Writing hooks
- Running tests
- Installing into a site

**`Concepts-Workflow-Engine.md`** — update for MS-23 completion:
- State machine configuration
- SLA timers
- Approval chains
- Transition rules and guards

**`Operations-Monitoring.md`** — update for MS-24 completion:
- Prometheus metrics endpoints
- OpenTelemetry tracing configuration
- `moca doctor` command reference
- Alert rule examples

**`Roadmap-Milestone-Status.md`** — update all statuses through MS-25

**`Roadmap-Changelog.md`** — sync with updated CHANGELOG.md

### 4.4 Sidebar Update (`_Sidebar.md`)

Add under **Operations**:
```markdown
- [Deployment](Operations-Deployment)
  - [Single Server](Operations-Deployment-Single-Server)
  - [Docker](Operations-Deployment-Docker)
  - [Kubernetes](Operations-Deployment-Kubernetes)
```

Verify all existing links resolve correctly.

## Acceptance Criteria

1. `LICENSE` file exists with Apache-2.0 text
2. `CONTRIBUTING.md` exists and GitHub renders it on new issues/PRs
3. `CHANGELOG.md` has entries for all milestones through MS-25
4. `.goreleaser.yml` builds all 5 binaries locally via `make release-local`
5. `Dockerfile` builds all 5 targets successfully
6. `docker-compose.example.yml` brings up the full stack with `docker compose -f docker-compose.example.yml up`
7. `release.yml` pushes 5 Docker images to ghcr.io on tag push
8. `make docs-generate` produces updated CLI and API reference in wiki
9. Auto-generated sections in wiki are between markers and don't overwrite hand-written content
10. All 3 deployment guides exist with complete step-by-step instructions
11. Stub pages (`Operations-Deployment.md`, `Operations-Docker-Setup.md`) are rewritten with real content
12. Wiki sidebar includes new deployment guide links
13. All existing wiki links resolve correctly (no broken links)
14. All tests pass on Linux and macOS

## File Inventory

**New files (main repo):**
- `LICENSE`
- `CONTRIBUTING.md`
- `.goreleaser.yml`
- `Dockerfile`
- `.dockerignore`
- `docker-compose.example.yml`
- `.env.example`
- `cmd/moca-docgen/main.go`
- `internal/docgen/cli.go`
- `internal/docgen/api.go`
- `internal/docgen/inject.go`
- `internal/docgen/cli_test.go`
- `internal/docgen/api_test.go`
- `internal/docgen/inject_test.go`
- `docs/generated/openapi.yaml` (generated)

**Modified files (main repo):**
- `CHANGELOG.md` — add milestone entries
- `Makefile` — add `release-local`, `docs-generate`, `docs-generate-cli`, `docs-generate-api` targets
- `.github/workflows/release.yml` — add `docker-publish` job

**New files (wiki submodule):**
- `Operations-Deployment-Single-Server.md`
- `Operations-Deployment-Docker.md`
- `Operations-Deployment-Kubernetes.md`

**Modified files (wiki submodule):**
- `_Sidebar.md` — add deployment guide links
- `Operations-Deployment.md` — rewrite from stub
- `Operations-Docker-Setup.md` — rewrite from stub
- `Reference-CLI-Commands.md` — add auto-generation markers
- `Reference-REST-API.md` — add auto-generation markers
- `Guide-Getting-Started.md` — verify and update
- `Guide-Creating-Your-First-App.md` — expand
- `Concepts-Workflow-Engine.md` — update for MS-23
- `Operations-Monitoring.md` — update for MS-24
- `Roadmap-Milestone-Status.md` — update statuses
- `Roadmap-Changelog.md` — sync with CHANGELOG.md
