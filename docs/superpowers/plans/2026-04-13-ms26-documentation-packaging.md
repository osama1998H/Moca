# MS-26: Documentation, Packaging & v1.0 Polish — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship developer docs, API reference, deployment guides, release packaging (GoReleaser, Docker, install script), and final v1.0 polish.

**Architecture:** 4 phases executed sequentially — Foundation (LICENSE, CONTRIBUTING, CHANGELOG, GoReleaser), Docker & CI (Dockerfile, ghcr.io publishing, example compose), Auto-generation Tooling (CLI/API reference generators wired as hidden `moca docgen` subcommand), Wiki Expansion (deployment guides, stub rewrites, page updates). Documentation lives in the `wiki/` git submodule.

**Tech Stack:** Go 1.26+, Cobra, GoReleaser, Docker (multi-stage with build targets), GitHub Actions, GitHub Container Registry (ghcr.io)

**Important — Wiki Submodule Commits:** Every task that modifies files in `wiki/` requires TWO commits:
1. Inside the submodule: `cd wiki && git add <files> && git commit -m "..."`
2. In the main repo to update the submodule pointer: `cd /Users/osamamuhammed/Moca && git add wiki && git commit -m "..."`

If you only do one, the submodule reference will be stale.

**Design Spec:** `docs/superpowers/specs/2026-04-13-ms26-documentation-packaging-design.md`

---

## Phase 1: Foundation

### Task 1: Add Apache-2.0 LICENSE File

**Files:**
- Create: `LICENSE`

- [ ] **Step 1: Create the LICENSE file**

```
                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

   1. Definitions.

      "License" shall mean the terms and conditions for use, reproduction,
      and distribution as defined by Sections 1 through 9 of this document.

      "Licensor" shall mean the copyright owner or entity authorized by
      the copyright owner that is granting the License.

      "Legal Entity" shall mean the union of the acting entity and all
      other entities that control, are controlled by, or are under common
      control with that entity. For the purposes of this definition,
      "control" means (i) the power, direct or indirect, to cause the
      direction or management of such entity, whether by contract or
      otherwise, or (ii) ownership of fifty percent (50%) or more of the
      outstanding shares, or (iii) beneficial ownership of such entity.

      "You" (or "Your") shall mean an individual or Legal Entity
      exercising permissions granted by this License.

      "Source" form shall mean the preferred form for making modifications,
      including but not limited to software source code, documentation
      source, and configuration files.

      "Object" form shall mean any form resulting from mechanical
      transformation or translation of a Source form, including but
      not limited to compiled object code, generated documentation,
      and conversions to other media types.

      "Work" shall mean the work of authorship, whether in Source or
      Object form, made available under the License, as indicated by a
      copyright notice that is included in or attached to the work
      (an example is provided in the Appendix below).

      "Derivative Works" shall mean any work, whether in Source or Object
      form, that is based on (or derived from) the Work and for which the
      editorial revisions, annotations, elaborations, or other modifications
      represent, as a whole, an original work of authorship. For the purposes
      of this License, Derivative Works shall not include works that remain
      separable from, or merely link (or bind by name) to the interfaces of,
      the Work and Derivative Works thereof.

      "Contribution" shall mean any work of authorship, including
      the original version of the Work and any modifications or additions
      to that Work or Derivative Works thereof, that is intentionally
      submitted to the Licensor for inclusion in the Work by the copyright owner
      or by an individual or Legal Entity authorized to submit on behalf of
      the copyright owner. For the purposes of this definition, "submitted"
      means any form of electronic, verbal, or written communication sent
      to the Licensor or its representatives, including but not limited to
      communication on electronic mailing lists, source code control systems,
      and issue tracking systems that are managed by, or on behalf of, the
      Licensor for the purpose of discussing and improving the Work, but
      excluding communication that is conspicuously marked or otherwise
      designated in writing by the copyright owner as "Not a Contribution."

      "Contributor" shall mean Licensor and any individual or Legal Entity
      on behalf of whom a Contribution has been received by the Licensor and
      subsequently incorporated within the Work.

   2. Grant of Copyright License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      copyright license to reproduce, prepare Derivative Works of,
      publicly display, publicly perform, sublicense, and distribute the
      Work and such Derivative Works in Source or Object form.

   3. Grant of Patent License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      (except as stated in this section) patent license to make, have made,
      use, offer to sell, sell, import, and otherwise transfer the Work,
      where such license applies only to those patent claims licensable
      by such Contributor that are necessarily infringed by their
      Contribution(s) alone or by combination of their Contribution(s)
      with the Work to which such Contribution(s) was submitted. If You
      institute patent litigation against any entity (including a
      cross-claim or counterclaim in a lawsuit) alleging that the Work
      or a Contribution incorporated within the Work constitutes direct
      or contributory patent infringement, then any patent licenses
      granted to You under this License for that Work shall terminate
      as of the date such litigation is filed.

   4. Redistribution. You may reproduce and distribute copies of the
      Work or Derivative Works thereof in any medium, with or without
      modifications, and in Source or Object form, provided that You
      meet the following conditions:

      (a) You must give any other recipients of the Work or
          Derivative Works a copy of this License; and

      (b) You must cause any modified files to carry prominent notices
          stating that You changed the files; and

      (c) You must retain, in the Source form of any Derivative Works
          that You distribute, all copyright, patent, trademark, and
          attribution notices from the Source form of the Work,
          excluding those notices that do not pertain to any part of
          the Derivative Works; and

      (d) If the Work includes a "NOTICE" text file as part of its
          distribution, then any Derivative Works that You distribute must
          include a readable copy of the attribution notices contained
          within such NOTICE file, excluding any notices that do not
          pertain to any part of the Derivative Works, in at least one
          of the following places: within a NOTICE text file distributed
          as part of the Derivative Works; within the Source form or
          documentation, if provided along with the Derivative Works; or,
          within a display generated by the Derivative Works, if and
          wherever such third-party notices normally appear. The contents
          of the NOTICE file are for informational purposes only and
          do not modify the License. You may add Your own attribution
          notices within Derivative Works that You distribute, alongside
          or as an addendum to the NOTICE text from the Work, provided
          that such additional attribution notices cannot be construed
          as modifying the License.

      You may add Your own copyright statement to Your modifications and
      may provide additional or different license terms and conditions
      for use, reproduction, or distribution of Your modifications, or
      for any such Derivative Works as a whole, provided Your use,
      reproduction, and distribution of the Work otherwise complies with
      the conditions stated in this License.

   5. Submission of Contributions. Unless You explicitly state otherwise,
      any Contribution intentionally submitted for inclusion in the Work
      by You to the Licensor shall be under the terms and conditions of
      this License, without any additional terms or conditions.
      Notwithstanding the above, nothing herein shall supersede or modify
      the terms of any separate license agreement you may have executed
      with Licensor regarding such Contributions.

   6. Trademarks. This License does not grant permission to use the trade
      names, trademarks, service marks, or product names of the Licensor,
      except as required for reasonable and customary use in describing the
      origin of the Work and reproducing the content of the NOTICE file.

   7. Disclaimer of Warranty. Unless required by applicable law or
      agreed to in writing, Licensor provides the Work (and each
      Contributor provides its Contributions) on an "AS IS" BASIS,
      WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
      implied, including, without limitation, any warranties or conditions
      of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
      PARTICULAR PURPOSE. You are solely responsible for determining the
      appropriateness of using or redistributing the Work and assume any
      risks associated with Your exercise of permissions under this License.

   8. Limitation of Liability. In no event and under no legal theory,
      whether in tort (including negligence), contract, or otherwise,
      unless required by applicable law (such as deliberate and grossly
      negligent acts) or agreed to in writing, shall any Contributor be
      liable to You for damages, including any direct, indirect, special,
      incidental, or consequential damages of any character arising as a
      result of this License or out of the use or inability to use the
      Work (including but not limited to damages for loss of goodwill,
      work stoppage, computer failure or malfunction, or any and all
      other commercial damages or losses), even if such Contributor
      has been advised of the possibility of such damages.

   9. Accepting Warranty or Additional Liability. While redistributing
      the Work or Derivative Works thereof, You may choose to offer,
      and charge a fee for, acceptance of support, warranty, indemnity,
      or other liability obligations and/or rights consistent with this
      License. However, in accepting such obligations, You may act only
      on Your own behalf and on Your sole responsibility, not on behalf
      of any other Contributor, and only if You agree to indemnify,
      defend, and hold each Contributor harmless for any liability
      incurred by, or claims asserted against, such Contributor by reason
      of your accepting any such warranty or additional liability.

   END OF TERMS AND CONDITIONS

   APPENDIX: How to apply the Apache License to your work.

      To apply the Apache License to your work, attach the following
      boilerplate notice, with the fields enclosed by brackets "[]"
      replaced with your own identifying information. (Don't include
      the brackets!)  The text should be enclosed in the appropriate
      comment syntax for the file format. Please also get an
      application/legal review of the text.

   Copyright 2026 Osama Mohammed

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
```

Write this to `LICENSE` in the repo root.

- [ ] **Step 2: Verify the file exists**

Run: `head -3 LICENSE && tail -5 LICENSE`
Expected: First 3 lines show Apache header, last 5 show copyright notice.

- [ ] **Step 3: Commit**

```bash
git add LICENSE
git commit -m "chore: add Apache-2.0 LICENSE file"
```

---

### Task 2: Add CONTRIBUTING.md

**Files:**
- Create: `CONTRIBUTING.md`

- [ ] **Step 1: Create CONTRIBUTING.md**

Write this to `CONTRIBUTING.md` in the repo root:

```markdown
# Contributing to Moca

Thank you for your interest in contributing to the Moca framework!

## Getting Started

1. Fork the repository
2. Clone your fork and set up the development environment — see [Development Setup](https://github.com/osama1998H/Moca/wiki/Contributing-Development-Setup)
3. Create a feature branch from `main`
4. Make your changes
5. Submit a pull request

## Development

- **Environment setup:** [Development Setup](https://github.com/osama1998H/Moca/wiki/Contributing-Development-Setup)
- **Code style:** [Code Conventions](https://github.com/osama1998H/Moca/wiki/Contributing-Code-Conventions)
- **Running tests:** [Testing Guide](https://github.com/osama1998H/Moca/wiki/Contributing-Testing-Guide)
- **CI pipeline:** [CI/CD Pipeline](https://github.com/osama1998H/Moca/wiki/Contributing-CI-CD-Pipeline)

## Quick Reference

```bash
make build              # Build all binaries
make test               # Run tests with race detector
make test-integration   # Run integration tests (requires Docker)
make lint               # Run golangci-lint
```

## Reporting Bugs

Open a [GitHub Issue](https://github.com/osama1998H/Moca/issues/new) with:
- Steps to reproduce
- Expected vs actual behavior
- Go version, OS, and Moca version (`moca version`)

## Proposing Features

Open a [GitHub Issue](https://github.com/osama1998H/Moca/issues/new) describing:
- The problem you're trying to solve
- Your proposed solution
- Any alternatives you've considered

## Pull Request Process

1. Ensure tests pass locally (`make test && make lint`)
2. Write tests for new functionality
3. Keep PRs focused — one feature or fix per PR
4. Update documentation if your change affects user-facing behavior

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
```

- [ ] **Step 2: Verify GitHub will detect it**

Run: `ls -la CONTRIBUTING.md`
Expected: File exists in repo root.

- [ ] **Step 3: Commit**

```bash
git add CONTRIBUTING.md
git commit -m "chore: add CONTRIBUTING.md with links to wiki guides"
```

---

### Task 3: Update CHANGELOG.md

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Read current CHANGELOG.md**

Run: `cat CHANGELOG.md`

Verify it currently has `[0.1.0-mvp]` as the only release section.

- [ ] **Step 2: Add new version entries**

Insert the following sections ABOVE the existing `[0.1.0-mvp]` section in `CHANGELOG.md`. Keep the existing header and format. The new content to insert after the `and this project adheres to...` line and before `## [0.1.0-mvp]`:

```markdown
## [1.0.0] - Unreleased

### Added

#### Documentation & Packaging (MS-26)
- Apache-2.0 LICENSE and CONTRIBUTING.md
- GoReleaser configuration for local cross-compilation builds
- Multi-target Dockerfile for all 5 binaries (distroless base)
- Production Docker Compose example with full stack
- Docker image publishing to ghcr.io via GitHub Actions
- Auto-generated CLI reference and API reference in wiki
- Deployment guides: single-server, Docker, Kubernetes
- Updated wiki documentation across all sections

## [0.4.0-rc] - 2026-04-13

### Added

#### Observability & Profiling (MS-24)
- Prometheus metrics registry with 13 framework metrics (HTTP, document ops, cache, queue, Kafka, WebSocket, database)
- OpenTelemetry tracing over OTLP/gRPC with configurable sample rate
- `moca doctor` with project, infrastructure, and per-site health checks
- `moca monitor metrics` for raw Prometheus text output
- `moca dev bench` for live PostgreSQL/Redis microbenchmarks with percentile output
- `moca dev profile` for pprof capture and SVG flamegraph generation
- Jaeger added to Docker Compose for local trace inspection

#### Testing Framework (MS-25)
- `moca test run` command with Go test runner integration
- Test fixture generation from MetaType definitions
- Coverage reporting with threshold enforcement
- Test data factories for document creation in tests
- Integration test helpers for API endpoint testing

## [0.3.0-beta] - 2026-04-07

### Added

#### React Desk Foundation (MS-17)
- Desk app shell with sidebar navigation and module switching
- MetaProvider for client-side MetaType resolution
- FormView with field rendering from MetaType definitions
- ListView with column configuration, sorting, and pagination
- 29 field components matching backend FieldTypes
- Desk distribution as @osama1998h/desk npm package on GitHub Packages

#### API Keys, Webhooks & Custom Endpoints (MS-18)
- API key authentication (`token api_key:api_secret`)
- Webhook dispatcher with retry and signature verification
- Custom endpoint router with per-doctype configuration
- APIConfig per DocType for fine-grained API control

#### Desk Real-Time, Custom Fields & Version Tracking (MS-19)
- WebSocket hub for real-time document updates
- Custom field type registry for app-defined field components
- Document version tracking with diff view

#### GraphQL, Dashboards, Reports, Translation & File Storage (MS-20)
- GraphQL endpoint auto-generated from MetaTypes
- Dashboard framework with chart widgets
- Report engine with query reports and script reports
- Translation system with per-site locale management
- File upload/download with S3-compatible storage (MinIO)

#### Deployment & Infrastructure Generation (MS-21)
- `moca generate caddy|nginx|systemd|docker|k8s|supervisor|env` (7 generators)
- `moca deploy setup` with 14-step production initialization pipeline
- `moca deploy update` with 4-phase atomic update and auto-rollback
- `moca deploy rollback|promote|status|history` commands
- Backup automation: `moca backup upload|download|prune|schedule`

#### Security Hardening (MS-22)
- OAuth2 authorization code flow
- SAML 2.0 and OIDC SSO integration
- Session management with secure cookie handling
- Encryption at rest for sensitive fields
- Notification system (email, in-app)

#### Workflow Engine (MS-23)
- State-machine workflows attached to MetaTypes
- Linear and parallel (fork/join) workflow support
- Role-based and expression-based transition guards
- Required comments on transitions
- Workflow REST API and Desk workflow bar/timeline components

## [0.2.0-alpha] - 2026-04-01

### Added

#### CLI Operational Commands (MS-11)
- `moca db migrate|rollback|diff|console|seed` commands
- `moca backup create|restore|list|verify` commands
- `moca config get|set|list|validate` commands
- MigrationRunner with version tracking via `tab_migration_log`

#### Multitenancy (MS-12)
- SiteManager for create/drop/list/use/info operations
- Schema-per-tenant isolation with per-site connection pools
- Redis namespace isolation per tenant
- Meilisearch index-per-tenant (`{site}_{doctype}`)

#### App Scaffolding & User Management (MS-13)
- `moca app new` with Go module scaffolding and go.work integration
- `moca user add|set-password|add-role|list` commands
- `moca build app|server|desk` commands
- Developer tools: `moca desk install|dev|update`

#### Permission Engine (MS-14)
- Role-Based Access Control (RBAC) with DocPerm rules
- Field-Level Security (FLS) for read/write field restrictions
- Row-Level Security (RLS) with owner-based and custom match rules
- Permission evaluation integrated into document lifecycle and API layer

#### Background Jobs, Scheduler, Events & Search Sync (MS-15)
- Redis Streams producer/consumer with at-least-once delivery
- Worker pool with configurable concurrency and DLQ
- Cron scheduler with distributed leader election
- Kafka event backend with transactional outbox
- Redis pub/sub event fallback
- Meilisearch sync daemon with real-time document indexing

#### CLI Queue, Events, Search & Monitor Commands (MS-16)
- `moca queue status|list|retry|purge` commands
- `moca events tail|replay` commands
- `moca search rebuild|status|query` commands
- `moca monitor metrics|audit|live` commands
- `moca log tail` for structured log streaming
```

- [ ] **Step 3: Add version link references at the bottom**

Add these lines at the bottom of `CHANGELOG.md`, before or after the existing `[0.1.0-mvp]` link:

```markdown
[1.0.0]: https://github.com/osama1998H/moca/releases/tag/v1.0.0
[0.4.0-rc]: https://github.com/osama1998H/moca/releases/tag/v0.4.0-rc
[0.3.0-beta]: https://github.com/osama1998H/moca/releases/tag/v0.3.0-beta
[0.2.0-alpha]: https://github.com/osama1998H/moca/releases/tag/v0.2.0-alpha
```

- [ ] **Step 4: Verify the changelog renders correctly**

Run: `head -20 CHANGELOG.md`
Expected: Header, then `## [1.0.0] - Unreleased` as the first version section.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: update CHANGELOG with entries for MS-11 through MS-26"
```

---

### Task 4: Add GoReleaser Configuration

**Files:**
- Create: `.goreleaser.yml`
- Modify: `Makefile`

- [ ] **Step 1: Create .goreleaser.yml**

Write this to `.goreleaser.yml` in the repo root:

```yaml
# GoReleaser configuration for local development builds.
# Production releases use .github/workflows/release.yml.
# Usage: make release-local
# Docs: https://goreleaser.com

version: 2

builds:
  - id: moca
    main: ./cmd/moca
    binary: moca
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -X main.Version={{.Version}}
      - -X main.Commit={{.ShortCommit}}
      - -X main.BuildDate={{.Date}}

  - id: moca-server
    main: ./cmd/moca-server
    binary: moca-server
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -X main.Version={{.Version}}
      - -X main.Commit={{.ShortCommit}}
      - -X main.BuildDate={{.Date}}

  - id: moca-worker
    main: ./cmd/moca-worker
    binary: moca-worker
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -X main.Version={{.Version}}
      - -X main.Commit={{.ShortCommit}}
      - -X main.BuildDate={{.Date}}

  - id: moca-scheduler
    main: ./cmd/moca-scheduler
    binary: moca-scheduler
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -X main.Version={{.Version}}
      - -X main.Commit={{.ShortCommit}}
      - -X main.BuildDate={{.Date}}

  - id: moca-outbox
    main: ./cmd/moca-outbox
    binary: moca-outbox
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -X main.Version={{.Version}}
      - -X main.Commit={{.ShortCommit}}
      - -X main.BuildDate={{.Date}}

archives:
  - id: default
    format: tar.gz
    format_overrides:
      - goos: windows
        format: zip
    name_template: "moca_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"
  algorithm: sha256

snapshot:
  version_template: "{{ .Tag }}-next"

# GoReleaser does NOT publish releases — release.yml handles that.
release:
  disable: true
```

- [ ] **Step 2: Add Makefile target**

Add the following target to `Makefile`, after the existing `clean` target and before the spike targets. Also add `release-local` to the `.PHONY` list and the `help` output:

In the `.PHONY` list, add `release-local` at the end.

In the `help` target, add this line after the `clean` echo:
```
	@echo "  release-local    Build release archives locally (GoReleaser snapshot)"
```

Add this target after `clean:`:
```makefile
## release-local: Build release archives locally using GoReleaser (snapshot mode)
release-local:
	goreleaser build --snapshot --clean
```

- [ ] **Step 3: Verify GoReleaser config syntax**

Run: `cat .goreleaser.yml | head -5`
Expected: Shows `version: 2` and `builds:` section.

- [ ] **Step 4: Commit**

```bash
git add .goreleaser.yml Makefile
git commit -m "chore: add GoReleaser config for local cross-compilation builds"
```

---

## Phase 2: Docker & CI

### Task 5: Create Dockerfile with Build Targets

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`

- [ ] **Step 1: Create .dockerignore**

Write this to `.dockerignore` in the repo root:

```
.git
.github
bin/
desk/node_modules/
desk/.vite/
spikes/
wiki/
docs/
*.md
!go.mod
!go.sum
.goreleaser.yml
.golangci.yml
bench-*.txt
cpu.prof
mem.prof
.env
.env.*
.DS_Store
```

- [ ] **Step 2: Create Dockerfile**

Write this to `Dockerfile` in the repo root:

```dockerfile
# =============================================================================
# Moca Framework — Multi-target Dockerfile
# =============================================================================
# Usage:
#   docker build --target moca-server -t moca-server .
#   docker build --target moca-worker -t moca-worker .
#   docker build --target moca-scheduler -t moca-scheduler .
#   docker build --target moca-outbox -t moca-outbox .
#   docker build --target moca -t moca .
# =============================================================================

# ---------------------------------------------------------------------------
# Builder stage — compiles all 5 binaries
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build arguments for version injection
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

# Compile all binaries as static executables
RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
    -o /out/moca ./cmd/moca && \
    CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
    -o /out/moca-server ./cmd/moca-server && \
    CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
    -o /out/moca-worker ./cmd/moca-worker && \
    CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
    -o /out/moca-scheduler ./cmd/moca-scheduler && \
    CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
    -o /out/moca-outbox ./cmd/moca-outbox

# ---------------------------------------------------------------------------
# moca — CLI tool
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12 AS moca

LABEL org.opencontainers.image.source="https://github.com/osama1998H/Moca"
LABEL org.opencontainers.image.description="Moca CLI tool"

COPY --from=builder /out/moca /usr/local/bin/moca
ENTRYPOINT ["moca"]

# ---------------------------------------------------------------------------
# moca-server — HTTP + WebSocket API server
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12 AS moca-server

LABEL org.opencontainers.image.source="https://github.com/osama1998H/Moca"
LABEL org.opencontainers.image.description="Moca API server"

COPY --from=builder /out/moca-server /usr/local/bin/moca-server
EXPOSE 8000
ENTRYPOINT ["moca-server"]

# ---------------------------------------------------------------------------
# moca-worker — Background job consumer
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12 AS moca-worker

LABEL org.opencontainers.image.source="https://github.com/osama1998H/Moca"
LABEL org.opencontainers.image.description="Moca background job worker"

COPY --from=builder /out/moca-worker /usr/local/bin/moca-worker
ENTRYPOINT ["moca-worker"]

# ---------------------------------------------------------------------------
# moca-scheduler — Cron scheduler
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12 AS moca-scheduler

LABEL org.opencontainers.image.source="https://github.com/osama1998H/Moca"
LABEL org.opencontainers.image.description="Moca cron scheduler"

COPY --from=builder /out/moca-scheduler /usr/local/bin/moca-scheduler
ENTRYPOINT ["moca-scheduler"]

# ---------------------------------------------------------------------------
# moca-outbox — Transactional outbox poller
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12 AS moca-outbox

LABEL org.opencontainers.image.source="https://github.com/osama1998H/Moca"
LABEL org.opencontainers.image.description="Moca transactional outbox poller"

COPY --from=builder /out/moca-outbox /usr/local/bin/moca-outbox
ENTRYPOINT ["moca-outbox"]
```

- [ ] **Step 3: Verify Dockerfile syntax**

Run: `head -5 Dockerfile && echo "---" && grep "^FROM" Dockerfile`
Expected: Shows the comment header and 6 FROM lines (1 builder + 5 targets).

- [ ] **Step 4: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "chore: add multi-target Dockerfile for all 5 binaries"
```

---

### Task 6: Create Production Docker Compose Example

**Files:**
- Create: `docker-compose.example.yml`
- Create: `.env.example`

- [ ] **Step 1: Create .env.example**

Write this to `.env.example` in the repo root:

```bash
# Moca Production Environment Configuration
# Copy to .env and adjust values before use.

# Database
POSTGRES_USER=moca
POSTGRES_PASSWORD=changeme
POSTGRES_DB=moca

# Redis
REDIS_PASSWORD=changeme

# Meilisearch
MEILI_MASTER_KEY=changeme

# Moca
MOCA_ENV=production
MOCA_DATABASE_HOST=postgres
MOCA_DATABASE_PORT=5432
MOCA_DATABASE_USER=moca
MOCA_DATABASE_PASSWORD=changeme
MOCA_DATABASE_NAME=moca
MOCA_REDIS_HOST=redis
MOCA_REDIS_PORT=6379
MOCA_REDIS_PASSWORD=changeme
MOCA_SEARCH_HOST=http://meilisearch:7700
MOCA_SEARCH_API_KEY=changeme
```

- [ ] **Step 2: Create docker-compose.example.yml**

Write this to `docker-compose.example.yml` in the repo root:

```yaml
# Moca Production Docker Compose
# Usage:
#   cp .env.example .env   # Edit with real values
#   docker compose -f docker-compose.example.yml up -d
#
# To build from source instead of pulling images:
#   docker compose -f docker-compose.example.yml up -d --build

services:
  # --- Moca Services ---

  moca-server:
    build:
      context: .
      target: moca-server
      args:
        VERSION: ${MOCA_VERSION:-dev}
    # Alternatively, pull from ghcr.io:
    # image: ghcr.io/osama1998h/moca-server:latest
    ports:
      - "8000:8000"
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    restart: unless-stopped
    # Uncomment to set resource limits:
    # deploy:
    #   resources:
    #     limits:
    #       memory: 512M
    #       cpus: "1.0"

  moca-worker:
    build:
      context: .
      target: moca-worker
    # image: ghcr.io/osama1998h/moca-worker:latest
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    restart: unless-stopped
    # Scale workers: docker compose -f docker-compose.example.yml up -d --scale moca-worker=3
    # deploy:
    #   resources:
    #     limits:
    #       memory: 256M
    #       cpus: "0.5"

  moca-scheduler:
    build:
      context: .
      target: moca-scheduler
    # image: ghcr.io/osama1998h/moca-scheduler:latest
    env_file: .env
    depends_on:
      redis:
        condition: service_healthy
    restart: unless-stopped

  moca-outbox:
    build:
      context: .
      target: moca-outbox
    # image: ghcr.io/osama1998h/moca-outbox:latest
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    restart: unless-stopped

  # --- Infrastructure ---

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-moca}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-changeme}
      POSTGRES_DB: ${POSTGRES_DB:-moca}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-moca}"]
      interval: 5s
      timeout: 3s
      retries: 5
    restart: unless-stopped
    # deploy:
    #   resources:
    #     limits:
    #       memory: 1G

  redis:
    image: redis:7-alpine
    command: redis-server --requirepass ${REDIS_PASSWORD:-changeme}
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "-a", "${REDIS_PASSWORD:-changeme}", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5
    restart: unless-stopped

  meilisearch:
    image: getmeili/meilisearch:v1.12
    environment:
      MEILI_MASTER_KEY: ${MEILI_MASTER_KEY:-changeme}
      MEILI_ENV: production
    volumes:
      - meili_data:/meili_data
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:7700/health"]
      interval: 5s
      timeout: 3s
      retries: 5
    restart: unless-stopped

volumes:
  postgres_data:
  redis_data:
  meili_data:

networks:
  default:
    name: moca
```

- [ ] **Step 3: Verify files**

Run: `head -5 docker-compose.example.yml && echo "---" && head -5 .env.example`
Expected: YAML comment header and env file header visible.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.example.yml .env.example
git commit -m "chore: add production Docker Compose example and .env template"
```

---

### Task 7: Extend release.yml for Docker Image Publishing

**Files:**
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Read current release.yml**

Run: `cat .github/workflows/release.yml`

Note the existing jobs: `prepare-release`, `test`, `desk-validate`, `desk-publish`, `build`, `publish`. The new `docker-publish` job should run after `test` (parallel with `build`).

- [ ] **Step 2: Add docker-publish job**

Add the following job to `.github/workflows/release.yml`, after the `desk-publish` job and before the `build` job. Also add `packages: write` to the top-level permissions if not already present.

First, update the top-level permissions to include packages:

```yaml
permissions:
  contents: write
  packages: write
```

Then add this job:

```yaml
  docker-publish:
    name: Publish Docker Images
    needs: test
    runs-on: ubuntu-latest
    permissions:
      packages: write
    strategy:
      matrix:
        target:
          - moca
          - moca-server
          - moca-worker
          - moca-scheduler
          - moca-outbox
    steps:
      - name: Checkout code
        uses: actions/checkout@v5

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Login to GitHub Container Registry
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract version
        id: vars
        run: echo "version=${GITHUB_REF_NAME#v}" >> "$GITHUB_OUTPUT"

      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: .
          target: ${{ matrix.target }}
          push: true
          tags: |
            ghcr.io/osama1998h/${{ matrix.target }}:${{ steps.vars.outputs.version }}
            ghcr.io/osama1998h/${{ matrix.target }}:latest
          build-args: |
            VERSION=${{ steps.vars.outputs.version }}
            COMMIT=${{ github.sha }}
            BUILD_DATE=${{ github.event.head_commit.timestamp }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

- [ ] **Step 3: Verify the YAML is valid**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))" && echo "Valid YAML"`
Expected: `Valid YAML`

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add Docker image publishing to ghcr.io on release"
```

---

## Phase 3: Auto-generation Tooling

### Task 8: Create Wiki Injection Utility

**Files:**
- Create: `internal/docgen/inject.go`
- Create: `internal/docgen/inject_test.go`

- [ ] **Step 1: Write the test for InjectSection**

Write this to `internal/docgen/inject_test.go`:

```go
package docgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectSection(t *testing.T) {
	t.Run("replaces content between markers", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.md")

		original := `# Title

Hand-written intro.

## Reference

<!-- AUTO-GENERATED:START -->
old generated content
<!-- AUTO-GENERATED:END -->

Hand-written footer.
`
		if err := os.WriteFile(path, []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		err := InjectSection(path, "<!-- AUTO-GENERATED:START -->", "<!-- AUTO-GENERATED:END -->", "new generated content\n")
		if err != nil {
			t.Fatal(err)
		}

		got, _ := os.ReadFile(path)
		content := string(got)

		if !strings.Contains(content, "Hand-written intro.") {
			t.Error("hand-written intro was lost")
		}
		if !strings.Contains(content, "Hand-written footer.") {
			t.Error("hand-written footer was lost")
		}
		if !strings.Contains(content, "new generated content") {
			t.Error("new content not injected")
		}
		if strings.Contains(content, "old generated content") {
			t.Error("old content not replaced")
		}
	})

	t.Run("errors when start marker not found", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.md")
		os.WriteFile(path, []byte("no markers here"), 0644)

		err := InjectSection(path, "<!-- AUTO-GENERATED:START -->", "<!-- AUTO-GENERATED:END -->", "content")
		if err == nil {
			t.Error("expected error when markers not found")
		}
	})

	t.Run("errors when end marker not found", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.md")
		os.WriteFile(path, []byte("<!-- AUTO-GENERATED:START -->\ncontent\n"), 0644)

		err := InjectSection(path, "<!-- AUTO-GENERATED:START -->", "<!-- AUTO-GENERATED:END -->", "content")
		if err == nil {
			t.Error("expected error when end marker not found")
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestInjectSection ./internal/docgen/...`
Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Write the implementation**

Write this to `internal/docgen/inject.go`:

```go
// Package docgen generates reference documentation from Moca's Cobra command
// tree and API route definitions, and injects generated content into wiki
// markdown files between AUTO-GENERATED markers.
package docgen

import (
	"fmt"
	"os"
	"strings"
)

// InjectSection reads the file at path, finds the lines containing
// startMarker and endMarker, and replaces everything between them with
// content. Content outside the markers is preserved.
//
// Returns an error if either marker is not found in the file.
func InjectSection(path, startMarker, endMarker, content string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	text := string(data)

	startIdx := strings.Index(text, startMarker)
	if startIdx == -1 {
		return fmt.Errorf("start marker %q not found in %s", startMarker, path)
	}

	// Find end marker after the start marker
	searchFrom := startIdx + len(startMarker)
	endIdx := strings.Index(text[searchFrom:], endMarker)
	if endIdx == -1 {
		return fmt.Errorf("end marker %q not found in %s", endMarker, path)
	}
	endIdx += searchFrom

	// Build new content: before start marker + start marker + newline + content + end marker + after end marker
	var b strings.Builder
	b.WriteString(text[:startIdx+len(startMarker)])
	b.WriteString("\n")
	b.WriteString(content)
	b.WriteString(text[endIdx:])

	return os.WriteFile(path, []byte(b.String()), 0644)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -run TestInjectSection ./internal/docgen/...`
Expected: PASS — all 3 subtests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/docgen/inject.go internal/docgen/inject_test.go
git commit -m "feat(docgen): add wiki marker injection utility"
```

---

### Task 9: Create CLI Reference Generator

**Files:**
- Create: `internal/docgen/cli.go`
- Create: `internal/docgen/cli_test.go`

- [ ] **Step 1: Write a basic test**

Write this to `internal/docgen/cli_test.go`:

```go
package docgen

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestGenerateCLIReference(t *testing.T) {
	// Build a minimal command tree for testing
	root := &cobra.Command{
		Use:   "moca",
		Short: "Test CLI",
	}
	site := &cobra.Command{
		Use:   "site",
		Short: "Site management commands",
	}
	site.AddCommand(&cobra.Command{
		Use:     "create <name>",
		Short:   "Create a new site",
		Aliases: []string{"new"},
	})
	site.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all sites",
	})
	root.AddCommand(site)

	md := GenerateCLIReference(root)

	if !strings.Contains(md, "## site") {
		t.Error("expected 'site' command group heading")
	}
	if !strings.Contains(md, "moca site create") {
		t.Error("expected 'moca site create' in output")
	}
	if !strings.Contains(md, "moca site list") {
		t.Error("expected 'moca site list' in output")
	}
	if !strings.Contains(md, "Create a new site") {
		t.Error("expected description in output")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestGenerateCLIReference ./internal/docgen/...`
Expected: FAIL — `GenerateCLIReference` not defined.

- [ ] **Step 3: Write the implementation**

Write this to `internal/docgen/cli.go`:

```go
package docgen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// GenerateCLIReference produces a markdown reference for all commands in the
// Cobra command tree rooted at root. Commands are grouped by their top-level
// parent. Hidden commands are skipped.
func GenerateCLIReference(root *cobra.Command) string {
	var b strings.Builder

	// Collect top-level command groups
	groups := root.Commands()
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name() < groups[j].Name()
	})

	for _, group := range groups {
		if group.Hidden {
			continue
		}

		subs := group.Commands()
		if len(subs) == 0 {
			// Top-level command with no subcommands
			b.WriteString(fmt.Sprintf("## %s\n\n", group.Name()))
			b.WriteString(fmt.Sprintf("| Command | Description |\n"))
			b.WriteString(fmt.Sprintf("|---------|-------------|\n"))
			writeCommandRow(&b, root.Name(), group)
			b.WriteString("\n")
			continue
		}

		// Command group with subcommands
		b.WriteString(fmt.Sprintf("## %s\n\n", group.Name()))
		if group.Short != "" {
			b.WriteString(fmt.Sprintf("> %s\n\n", group.Short))
		}

		b.WriteString("| Command | Description |\n")
		b.WriteString("|---------|-------------|\n")

		sort.Slice(subs, func(i, j int) bool {
			return subs[i].Name() < subs[j].Name()
		})

		for _, sub := range subs {
			if sub.Hidden {
				continue
			}
			writeCommandRow(&b, root.Name()+" "+group.Name(), sub)
		}
		b.WriteString("\n")

		// Write detailed flag tables for subcommands that have flags
		for _, sub := range subs {
			if sub.Hidden {
				continue
			}
			flags := collectFlags(sub)
			if len(flags) == 0 {
				continue
			}
			fullName := fmt.Sprintf("%s %s %s", root.Name(), group.Name(), sub.Name())
			b.WriteString(fmt.Sprintf("### `%s` flags\n\n", fullName))
			b.WriteString("| Flag | Type | Default | Description |\n")
			b.WriteString("|------|------|---------|-------------|\n")
			for _, f := range flags {
				b.WriteString(fmt.Sprintf("| `--%s` | %s | %s | %s |\n",
					f.name, f.typ, f.def, f.desc))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func writeCommandRow(b *strings.Builder, prefix string, cmd *cobra.Command) {
	fullCmd := fmt.Sprintf("`%s %s`", prefix, cmd.Use)
	aliases := ""
	if len(cmd.Aliases) > 0 {
		aliases = " (aliases: " + strings.Join(cmd.Aliases, ", ") + ")"
	}
	b.WriteString(fmt.Sprintf("| %s | %s%s |\n", fullCmd, cmd.Short, aliases))
}

type flagInfo struct {
	name string
	typ  string
	def  string
	desc string
}

func collectFlags(cmd *cobra.Command) []flagInfo {
	var flags []flagInfo
	cmd.Flags().VisitAll(func(f interface{ Name, DefValue, Usage string; Type() string }) {
		// This is a simplified visitor — the actual pflag.Flag is accessed below
	})
	// Use the concrete pflag API
	cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		flags = append(flags, flagInfo{
			name: f.Name,
			typ:  f.Value.Type(),
			def:  f.DefValue,
			desc: f.Usage,
		})
	})
	sort.Slice(flags, func(i, j int) bool {
		return flags[i].name < flags[j].name
	})
	return flags
}
```

Wait — that has a compilation issue with the `pflag` import. Let me fix:

Replace the `collectFlags` function and add the import. The full corrected `internal/docgen/cli.go`:

```go
package docgen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GenerateCLIReference produces a markdown reference for all commands in the
// Cobra command tree rooted at root. Commands are grouped by their top-level
// parent. Hidden commands are skipped.
func GenerateCLIReference(root *cobra.Command) string {
	var b strings.Builder

	groups := root.Commands()
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name() < groups[j].Name()
	})

	for _, group := range groups {
		if group.Hidden {
			continue
		}

		subs := group.Commands()
		if len(subs) == 0 {
			b.WriteString(fmt.Sprintf("## %s\n\n", group.Name()))
			b.WriteString("| Command | Description |\n")
			b.WriteString("|---------|-------------|\n")
			writeCommandRow(&b, root.Name(), group)
			b.WriteString("\n")
			continue
		}

		b.WriteString(fmt.Sprintf("## %s\n\n", group.Name()))
		if group.Short != "" {
			b.WriteString(fmt.Sprintf("> %s\n\n", group.Short))
		}

		b.WriteString("| Command | Description |\n")
		b.WriteString("|---------|-------------|\n")

		sort.Slice(subs, func(i, j int) bool {
			return subs[i].Name() < subs[j].Name()
		})

		for _, sub := range subs {
			if sub.Hidden {
				continue
			}
			writeCommandRow(&b, root.Name()+" "+group.Name(), sub)
		}
		b.WriteString("\n")

		for _, sub := range subs {
			if sub.Hidden {
				continue
			}
			flags := collectFlags(sub)
			if len(flags) == 0 {
				continue
			}
			fullName := fmt.Sprintf("%s %s %s", root.Name(), group.Name(), sub.Name())
			b.WriteString(fmt.Sprintf("### `%s` flags\n\n", fullName))
			b.WriteString("| Flag | Type | Default | Description |\n")
			b.WriteString("|------|------|---------|-------------|\n")
			for _, f := range flags {
				b.WriteString(fmt.Sprintf("| `--%s` | %s | %s | %s |\n",
					f.name, f.typ, f.def, f.desc))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func writeCommandRow(b *strings.Builder, prefix string, cmd *cobra.Command) {
	fullCmd := fmt.Sprintf("`%s %s`", prefix, cmd.Use)
	aliases := ""
	if len(cmd.Aliases) > 0 {
		aliases = " (aliases: " + strings.Join(cmd.Aliases, ", ") + ")"
	}
	b.WriteString(fmt.Sprintf("| %s | %s%s |\n", fullCmd, cmd.Short, aliases))
}

type flagInfo struct {
	name string
	typ  string
	def  string
	desc string
}

func collectFlags(cmd *cobra.Command) []flagInfo {
	var flags []flagInfo
	cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		flags = append(flags, flagInfo{
			name: f.Name,
			typ:  f.Value.Type(),
			def:  f.DefValue,
			desc: f.Usage,
		})
	})
	sort.Slice(flags, func(i, j int) bool {
		return flags[i].name < flags[j].name
	})
	return flags
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -run TestGenerateCLIReference ./internal/docgen/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/docgen/cli.go internal/docgen/cli_test.go
git commit -m "feat(docgen): add CLI reference markdown generator"
```

---

### Task 10: Create API Reference Generator

**Files:**
- Create: `internal/docgen/api.go`
- Create: `internal/docgen/api_test.go`

The Moca server already has a working OpenAPI generator at `pkg/api/openapi.go` that serves `GET /api/v1/openapi.json`. The API doc generator leverages this by documenting the generic CRUD patterns and framework endpoints (auth, workflow, notifications, etc.) that are always present regardless of user-defined doctypes.

- [ ] **Step 1: Write the test**

Write this to `internal/docgen/api_test.go`:

```go
package docgen

import (
	"strings"
	"testing"
)

func TestGenerateAPIReference(t *testing.T) {
	md := GenerateAPIReference()

	if !strings.Contains(md, "Document CRUD") {
		t.Error("expected Document CRUD section")
	}
	if !strings.Contains(md, "/api/v1/resource/{doctype}") {
		t.Error("expected resource endpoint")
	}
	if !strings.Contains(md, "Authentication") {
		t.Error("expected Authentication section")
	}
	if !strings.Contains(md, "Rate Limiting") {
		t.Error("expected Rate Limiting section")
	}
	if !strings.Contains(md, "Workflow") {
		t.Error("expected Workflow section")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestGenerateAPIReference ./internal/docgen/...`
Expected: FAIL — `GenerateAPIReference` not defined.

- [ ] **Step 3: Write the implementation**

Write this to `internal/docgen/api.go`:

```go
package docgen

import "strings"

// GenerateAPIReference produces a markdown reference documenting Moca's
// standard REST API endpoints. This covers the framework-level routes that
// exist for every deployment — CRUD, auth, workflow, search, notifications,
// etc. DocType-specific endpoints are auto-generated at runtime and served
// via GET /api/v1/openapi.json.
func GenerateAPIReference() string {
	var b strings.Builder

	b.WriteString("### Document CRUD\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `GET` | `/api/v1/resource/{doctype}` | List documents with filters, pagination, and field selection |\n")
	b.WriteString("| `POST` | `/api/v1/resource/{doctype}` | Create a new document |\n")
	b.WriteString("| `GET` | `/api/v1/resource/{doctype}/{name}` | Get a single document by name |\n")
	b.WriteString("| `PUT` | `/api/v1/resource/{doctype}/{name}` | Update a document (partial update) |\n")
	b.WriteString("| `DELETE` | `/api/v1/resource/{doctype}/{name}` | Delete a document |\n")
	b.WriteString("| `GET` | `/api/v1/resource/{doctype}/{name}/versions` | List document versions |\n")
	b.WriteString("| `GET` | `/api/v1/meta/{doctype}` | Get MetaType definition for a DocType |\n")
	b.WriteString("\n")

	b.WriteString("### Authentication\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `POST` | `/api/v1/auth/login` | Email/password login; returns access token and sets auth cookies |\n")
	b.WriteString("| `POST` | `/api/v1/auth/refresh` | Rotate access token using refresh cookie or token |\n")
	b.WriteString("| `POST` | `/api/v1/auth/logout` | Destroy session and clear auth cookies |\n")
	b.WriteString("\n")

	b.WriteString("### SSO / OAuth2 / SAML\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `GET` | `/api/v1/auth/sso/authorize?provider={name}` | Start OAuth2/OIDC/SAML login flow |\n")
	b.WriteString("| `GET` | `/api/v1/auth/sso/callback` | OAuth2/OIDC callback (creates session) |\n")
	b.WriteString("| `GET` | `/api/v1/auth/saml/metadata?provider={name}` | SAML SP metadata XML |\n")
	b.WriteString("| `POST` | `/api/v1/auth/saml/acs` | SAML assertion consumer service |\n")
	b.WriteString("\n")

	b.WriteString("### Workflow\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `POST` | `/api/v1/workflow/{doctype}/{name}/transition` | Execute a workflow action |\n")
	b.WriteString("| `GET` | `/api/v1/workflow/{doctype}/{name}/state` | Get current state and available actions |\n")
	b.WriteString("| `GET` | `/api/v1/workflow/{doctype}/{name}/history` | Get workflow action history |\n")
	b.WriteString("| `GET` | `/api/v1/workflow/pending` | List pending workflow items for current user |\n")
	b.WriteString("\n")

	b.WriteString("### Notifications\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `GET` | `/api/v1/notifications` | List unread notifications |\n")
	b.WriteString("| `GET` | `/api/v1/notifications/count` | Get unread count |\n")
	b.WriteString("| `PUT` | `/api/v1/notifications/mark-read` | Mark notifications as read |\n")
	b.WriteString("\n")

	b.WriteString("### Search\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `GET` | `/api/v1/search?q={query}` | Full-text search across DocTypes |\n")
	b.WriteString("\n")

	b.WriteString("### File Upload\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `POST` | `/api/v1/file/upload` | Upload a file (multipart/form-data) |\n")
	b.WriteString("| `GET` | `/api/v1/file/{name}` | Download a file |\n")
	b.WriteString("\n")

	b.WriteString("### Reports & Dashboards\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `GET` | `/api/v1/reports` | List available reports |\n")
	b.WriteString("| `GET` | `/api/v1/dashboards` | List available dashboards |\n")
	b.WriteString("\n")

	b.WriteString("### GraphQL\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `POST` | `/graphql` | GraphQL endpoint (auto-generated from MetaTypes) |\n")
	b.WriteString("\n")

	b.WriteString("### Server Method Calls\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `POST` | `/api/v1/method/{name}` | Call a whitelisted server method |\n")
	b.WriteString("\n")

	b.WriteString("### Custom Endpoints\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `*` | `/api/v1/custom/{doctype}/{path...}` | Per-DocType custom endpoints (defined in APIConfig) |\n")
	b.WriteString("\n")

	b.WriteString("### OpenAPI & Docs\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `GET` | `/api/v1/openapi.json` | Generated OpenAPI 3.0.3 spec |\n")
	b.WriteString("| `GET` | `/api/docs` | Swagger UI |\n")
	b.WriteString("\n")

	b.WriteString("### Health & Metrics\n\n")
	b.WriteString("| Method | Endpoint | Description |\n")
	b.WriteString("|--------|----------|-------------|\n")
	b.WriteString("| `GET` | `/health` | Health check |\n")
	b.WriteString("| `GET` | `/health/ready` | Readiness probe |\n")
	b.WriteString("| `GET` | `/health/live` | Liveness probe |\n")
	b.WriteString("| `GET` | `/metrics` | Prometheus metrics |\n")
	b.WriteString("\n")

	b.WriteString("### Rate Limiting\n\n")
	b.WriteString("All endpoints are subject to sliding-window rate limiting backed by Redis.\n")
	b.WriteString("Rate limit status is returned in response headers:\n\n")
	b.WriteString("```\n")
	b.WriteString("X-RateLimit-Limit: 100\n")
	b.WriteString("X-RateLimit-Remaining: 95\n")
	b.WriteString("X-RateLimit-Reset: 1620000000\n")
	b.WriteString("```\n")

	return b.String()
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -run TestGenerateAPIReference ./internal/docgen/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/docgen/api.go internal/docgen/api_test.go
git commit -m "feat(docgen): add API reference markdown generator"
```

---

### Task 11: Wire Docgen as Hidden CLI Subcommand

**Files:**
- Create: `cmd/moca/docgen.go`
- Modify: `cmd/moca/commands.go`
- Modify: `Makefile`

The docgen command is a hidden subcommand of the `moca` CLI. This avoids the problem of a separate `cmd/moca-docgen` binary being unable to import the `package main` command constructors. The `moca docgen` command has full access to the command tree via `root.Root()`.

- [ ] **Step 1: Create cmd/moca/docgen.go**

Write this to `cmd/moca/docgen.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/osama1998H/moca/internal/docgen"
	"github.com/spf13/cobra"
)

// NewDocgenCommand returns the hidden `moca docgen` command used to
// generate reference documentation into wiki/ markdown files.
func NewDocgenCommand() *cobra.Command {
	var wikiDir string

	cmd := &cobra.Command{
		Use:    "docgen",
		Short:  "Generate reference documentation",
		Hidden: true,
	}

	cmd.PersistentFlags().StringVar(&wikiDir, "wiki-dir", "wiki", "Path to wiki directory")

	cmd.AddCommand(
		newDocgenCLICommand(&wikiDir),
		newDocgenAPICommand(&wikiDir),
		newDocgenAllCommand(&wikiDir),
	)

	return cmd
}

func newDocgenCLICommand(wikiDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "cli",
		Short: "Generate CLI reference into wiki",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			md := docgen.GenerateCLIReference(root)
			path := filepath.Join(*wikiDir, "Reference-CLI-Commands.md")
			if err := docgen.InjectSection(path,
				"<!-- AUTO-GENERATED:START -->",
				"<!-- AUTO-GENERATED:END -->",
				md); err != nil {
				return fmt.Errorf("inject CLI reference: %w", err)
			}
			fmt.Fprintf(os.Stderr, "CLI reference injected into %s\n", path)
			return nil
		},
	}
}

func newDocgenAPICommand(wikiDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "api",
		Short: "Generate API reference into wiki",
		RunE: func(cmd *cobra.Command, args []string) error {
			md := docgen.GenerateAPIReference()
			path := filepath.Join(*wikiDir, "Reference-REST-API.md")
			if err := docgen.InjectSection(path,
				"<!-- AUTO-GENERATED:START -->",
				"<!-- AUTO-GENERATED:END -->",
				md); err != nil {
				return fmt.Errorf("inject API reference: %w", err)
			}
			fmt.Fprintf(os.Stderr, "API reference injected into %s\n", path)
			return nil
		},
	}
}

func newDocgenAllCommand(wikiDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "all",
		Short: "Generate all reference docs into wiki",
		RunE: func(cmd *cobra.Command, args []string) error {
			// CLI
			root := cmd.Root()
			cliMD := docgen.GenerateCLIReference(root)
			cliPath := filepath.Join(*wikiDir, "Reference-CLI-Commands.md")
			if err := docgen.InjectSection(cliPath,
				"<!-- AUTO-GENERATED:START -->",
				"<!-- AUTO-GENERATED:END -->",
				cliMD); err != nil {
				return fmt.Errorf("inject CLI reference: %w", err)
			}
			fmt.Fprintf(os.Stderr, "CLI reference injected into %s\n", cliPath)

			// API
			apiMD := docgen.GenerateAPIReference()
			apiPath := filepath.Join(*wikiDir, "Reference-REST-API.md")
			if err := docgen.InjectSection(apiPath,
				"<!-- AUTO-GENERATED:START -->",
				"<!-- AUTO-GENERATED:END -->",
				apiMD); err != nil {
				return fmt.Errorf("inject API reference: %w", err)
			}
			fmt.Fprintf(os.Stderr, "API reference injected into %s\n", apiPath)

			return nil
		},
	}
}
```

- [ ] **Step 2: Register in commands.go**

In `cmd/moca/commands.go`, add `NewDocgenCommand()` to the `allCommands()` slice. Add it at the end of the list, before the closing `}`:

```go
		NewNotifyCommand(),
		NewDocgenCommand(),
```

- [ ] **Step 3: Add Makefile targets**

Add these targets to `Makefile` after the `release-local` target. Also add `docs-generate docs-generate-cli docs-generate-api` to the `.PHONY` list and add help entries:

In the `help` target, add:
```
	@echo "  docs-generate    Generate CLI + API reference into wiki/"
	@echo "  docs-generate-cli Generate CLI reference only"
	@echo "  docs-generate-api Generate API reference only"
```

Add these targets:

```makefile
## docs-generate: Generate CLI + API reference into wiki/
docs-generate:
	$(GO) run ./cmd/moca docgen all --wiki-dir wiki/

## docs-generate-cli: Generate CLI reference only
docs-generate-cli:
	$(GO) run ./cmd/moca docgen cli --wiki-dir wiki/

## docs-generate-api: Generate API reference only
docs-generate-api:
	$(GO) run ./cmd/moca docgen api --wiki-dir wiki/
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./cmd/moca/`
Expected: No errors.

- [ ] **Step 5: Commit**

```bash
git add cmd/moca/docgen.go cmd/moca/commands.go Makefile
git commit -m "feat: add hidden 'moca docgen' command for reference generation"
```

---

### Task 12: Add Auto-generation Markers to Wiki Pages

**Files:**
- Modify: `wiki/Reference-CLI-Commands.md`
- Modify: `wiki/Reference-REST-API.md`

- [ ] **Step 1: Add markers to Reference-CLI-Commands.md**

In `wiki/Reference-CLI-Commands.md`, add these markers after the existing `## Command Groups` line (line 22) and before the first `### Project & Site Management` section (line 24). Replace all the manually written command tables (lines 24 through the end of `## Related` section) with:

```markdown
<!-- AUTO-GENERATED:START -->
<!-- Run 'make docs-generate-cli' to regenerate this section -->
<!-- AUTO-GENERATED:END -->

## Related

- [Configuration Reference](Reference-Configuration)
- [Quick Start Guide](Guide-Getting-Started)
```

Keep everything above `## Command Groups` (lines 1-22) intact — the Global Flags table and Context Resolution section are hand-written and stay.

- [ ] **Step 2: Add markers to Reference-REST-API.md**

In `wiki/Reference-REST-API.md`, add markers after the existing `## Interactive API Docs` section (after line 29) and before `## Authentication` (line 31). Replace the auto-generatable endpoint sections (Authentication through Rate Limiting) with:

```markdown
<!-- AUTO-GENERATED:START -->
<!-- Run 'make docs-generate-api' to regenerate this section -->
<!-- AUTO-GENERATED:END -->

## Related

- [REST API Usage Guide](Guide-REST-API-Usage)
- [Query Engine](Architecture-Query-Engine)
```

Keep everything from line 1 through the end of the `## Interactive API Docs` section intact — the Base URL, Headers, and Interactive API Docs sections are hand-written.

- [ ] **Step 3: Run the doc generator to fill in content**

Run: `go run ./cmd/moca docgen all --wiki-dir wiki/`
Expected: Two success messages printed to stderr.

- [ ] **Step 4: Verify the generated content**

Run: `grep -c "AUTO-GENERATED" wiki/Reference-CLI-Commands.md && grep -c "AUTO-GENERATED" wiki/Reference-REST-API.md`
Expected: Both files show `2` (start and end markers present).

- [ ] **Step 5: Commit (wiki submodule)**

```bash
cd wiki
git add Reference-CLI-Commands.md Reference-REST-API.md
git commit -m "docs: add auto-generation markers and generated CLI/API reference"
cd ..
git add wiki
git commit -m "docs: update wiki submodule with auto-generated references"
```

---

## Phase 4: Wiki Expansion

### Task 13: Rewrite Deployment Hub Page and Docker Setup Page

**Files:**
- Modify: `wiki/Operations-Deployment.md`
- Modify: `wiki/Operations-Docker-Setup.md`

- [ ] **Step 1: Rewrite Operations-Deployment.md**

Replace the entire content of `wiki/Operations-Deployment.md` with:

```markdown
# Deployment

> Deploy Moca to production using your preferred method.

## Architecture

A production Moca deployment runs five stateless processes backed by three infrastructure services:

| Process | Role | Scaling |
|---------|------|---------|
| `moca-server` | HTTP + WebSocket API | Horizontal behind load balancer |
| `moca-worker` | Redis Streams job consumer | Horizontal by queue pressure |
| `moca-scheduler` | Cron job trigger | Single leader (Redis distributed lock) |
| `moca-outbox` | Transactional outbox poller | Single leader (Redis distributed lock) |

| Service | Required | Purpose |
|---------|----------|---------|
| PostgreSQL 16+ | Yes | Primary data store (schema-per-tenant) |
| Redis 7+ | Yes | Cache, queue, sessions, pub/sub |
| Meilisearch v1.12 | Recommended | Full-text search |

## Deployment Methods

Choose the method that fits your infrastructure:

| Method | Best For | Guide |
|--------|----------|-------|
| **Single Server** | Small teams, low traffic, getting started | [Single Server Guide](Operations-Deployment-Single-Server) |
| **Docker** | Containerized environments, easy scaling | [Docker Guide](Operations-Deployment-Docker) |
| **Kubernetes** | Large-scale, multi-tenant, high availability | [Kubernetes Guide](Operations-Deployment-Kubernetes) |

## Infrastructure Generation

Moca can generate deployment configs for your target platform:

```bash
moca generate caddy       # Caddy reverse proxy config
moca generate nginx       # NGINX config
moca generate systemd     # systemd unit files (6 units)
moca generate docker      # Dockerfile + docker-compose
moca generate k8s         # Kubernetes manifests (7 files)
moca generate supervisor  # supervisord.conf
moca generate env         # .env files (dotenv, docker, systemd formats)
```

## Deployment Orchestration

For automated deployment workflows:

```bash
moca deploy setup     # One-command production initialization (14-step pipeline)
moca deploy update    # Safe atomic update with auto-rollback
moca deploy rollback  # Restore previous deployment
moca deploy promote   # Promote staging to production
moca deploy status    # Current deployment status
moca deploy history   # Past deployment records
```

## Related

- [Docker Setup](Operations-Docker-Setup)
- [Backup & Restore](Operations-Backup-and-Restore)
- [Monitoring](Operations-Monitoring)
- [Security](Operations-Security)
```

- [ ] **Step 2: Rewrite Operations-Docker-Setup.md**

Replace the entire content of `wiki/Operations-Docker-Setup.md` with:

```markdown
# Docker Setup

> Run Moca in Docker containers for development and production.

## Development

The repository includes a `docker-compose.yml` for local development with test infrastructure:

```bash
docker compose up -d    # Start PostgreSQL, Redis, Meilisearch
make test-integration   # Run tests against Docker services
docker compose down     # Stop services
```

Development services use tmpfs for speed and are configured on non-standard ports to avoid conflicts:
- PostgreSQL: port 5433
- Redis: port 6380
- Meilisearch: port 7700

## Production Docker Images

Official images are published to GitHub Container Registry on each release:

```bash
docker pull ghcr.io/osama1998h/moca-server:latest
docker pull ghcr.io/osama1998h/moca-worker:latest
docker pull ghcr.io/osama1998h/moca-scheduler:latest
docker pull ghcr.io/osama1998h/moca-outbox:latest
docker pull ghcr.io/osama1998h/moca:latest          # CLI tool
```

Images use `gcr.io/distroless/static-debian12` as base (~2MB) and contain a single static binary.

## Building Custom Images

The repository Dockerfile uses build targets for each binary:

```bash
docker build --target moca-server -t my-moca-server .
docker build --target moca-worker -t my-moca-worker .
docker build --target moca-scheduler -t my-moca-scheduler .
docker build --target moca-outbox -t my-moca-outbox .
```

## Generate Docker Configs

For project-specific Docker configs with your settings applied:

```bash
moca generate docker    # Generates Dockerfile, docker-compose.yml, docker-compose.prod.yml, .dockerignore
```

## Docker Compose Production Example

The repository includes `docker-compose.example.yml` with the full Moca stack. See the [Docker Deployment Guide](Operations-Deployment-Docker) for a complete walkthrough.

## Related

- [Docker Deployment Guide](Operations-Deployment-Docker)
- [Deployment Overview](Operations-Deployment)
- [Quick Start Guide](Guide-Getting-Started)
```

- [ ] **Step 3: Commit (wiki submodule)**

```bash
cd wiki
git add Operations-Deployment.md Operations-Docker-Setup.md
git commit -m "docs: rewrite deployment and Docker setup pages from stubs"
cd ..
git add wiki
git commit -m "docs: update wiki submodule with deployment and Docker pages"
```

---

### Task 14: Create Single Server Deployment Guide

**Files:**
- Create: `wiki/Operations-Deployment-Single-Server.md`

- [ ] **Step 1: Write the guide**

Write this to `wiki/Operations-Deployment-Single-Server.md`:

```markdown
# Single Server Deployment

> Deploy Moca on a single server with systemd and Caddy.

## Prerequisites

- Ubuntu 22.04 or 24.04 LTS (or compatible Linux distribution)
- PostgreSQL 16+
- Redis 7+
- Meilisearch v1.12 (optional, for search)
- A domain name pointed at your server (for TLS)

## 1. Install Moca

```bash
curl -fsSL https://raw.githubusercontent.com/osama1998H/moca/main/install.sh | sh
```

This installs all 5 binaries to `/usr/local/bin/` (or `~/.local/bin/` if not writable).

Verify:

```bash
moca version
```

## 2. Initialize a Project

```bash
mkdir /opt/moca && cd /opt/moca
moca init my-project
cd my-project
```

## 3. Configure

Edit `moca.yaml` with your database and Redis credentials:

```yaml
database:
  host: localhost
  port: 5432
  user: moca
  password: your_db_password
  name: moca_prod

redis:
  host: localhost
  port: 6379
  password: your_redis_password

search:
  host: http://localhost:7700
  api_key: your_meili_key
```

## 4. Create a Site

```bash
moca site create mysite
```

## 5. Deploy

The `moca deploy setup` command automates the full production setup:

```bash
moca deploy setup \
  --domain mysite.example.com \
  --email admin@example.com \
  --proxy caddy \
  --process systemd \
  --tls \
  --firewall \
  --fail2ban
```

This runs a 14-step pipeline:
1. Validates system requirements
2. Switches to production mode
3. Builds frontend assets and binaries
4. Generates Caddy reverse proxy config
5. Generates systemd unit files
6. Configures Redis for production
7. Configures log rotation
8. Configures automated backups
9. Sets up firewall rules (UFW)
10. Sets up fail2ban
11. Obtains TLS certificate (Let's Encrypt)
12. Starts all services
13. Runs health checks
14. Records deployment

Use `--dry-run` to preview without making changes.

## 6. Post-Setup

### Verify services are running

```bash
moca deploy status
systemctl status moca-server@1.service
```

### Configure automated backups

```bash
moca backup schedule --daily --retain 7
```

### Monitor

```bash
moca doctor --site mysite
moca monitor metrics
```

## Updating

```bash
moca deploy update
```

This pulls updates, runs migrations, rebuilds assets, and performs a rolling restart with auto-rollback on failure.

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Port 8000 not reachable | Check firewall: `ufw status` |
| TLS certificate failed | Verify DNS points to server, port 80/443 open |
| Service won't start | Check logs: `journalctl -u moca-server@1.service` |
| Database connection refused | Verify PostgreSQL is running and credentials correct |

## Related

- [Deployment Overview](Operations-Deployment)
- [Monitoring](Operations-Monitoring)
- [Backup & Restore](Operations-Backup-and-Restore)
```

- [ ] **Step 2: Commit (wiki submodule)**

```bash
cd wiki
git add Operations-Deployment-Single-Server.md
git commit -m "docs: add single server deployment guide"
cd ..
git add wiki
git commit -m "docs: update wiki submodule with single server deployment guide"
```

---

### Task 15: Create Docker Deployment Guide

**Files:**
- Create: `wiki/Operations-Deployment-Docker.md`

- [ ] **Step 1: Write the guide**

Write this to `wiki/Operations-Deployment-Docker.md`:

```markdown
# Docker Deployment

> Run Moca in Docker with the production compose example.

## Prerequisites

- Docker Engine 24+ with Docker Compose v2
- At least 2GB RAM available for the full stack

## Quick Start

```bash
# Clone and configure
git clone https://github.com/osama1998H/Moca.git
cd Moca
cp .env.example .env
# Edit .env with real passwords and keys
```

## Using Pre-built Images

Pull official images from GitHub Container Registry:

```bash
docker pull ghcr.io/osama1998h/moca-server:latest
docker pull ghcr.io/osama1998h/moca-worker:latest
docker pull ghcr.io/osama1998h/moca-scheduler:latest
docker pull ghcr.io/osama1998h/moca-outbox:latest
```

Edit `docker-compose.example.yml` and uncomment the `image:` lines (comment out the `build:` blocks).

## Using Local Builds

Build from source using the multi-target Dockerfile:

```bash
docker compose -f docker-compose.example.yml build
```

## Start the Stack

```bash
docker compose -f docker-compose.example.yml up -d
```

This starts:
- `moca-server` on port 8000
- `moca-worker` (background job consumer)
- `moca-scheduler` (cron trigger)
- `moca-outbox` (outbox poller)
- PostgreSQL 16
- Redis 7
- Meilisearch v1.12

## Initialize Moca

After the stack is running, create your first site:

```bash
docker compose -f docker-compose.example.yml exec moca-server moca site create mysite
```

## Environment Configuration

All configuration is via `.env`. Key variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `POSTGRES_USER` | PostgreSQL user | `moca` |
| `POSTGRES_PASSWORD` | PostgreSQL password | `changeme` |
| `REDIS_PASSWORD` | Redis password | `changeme` |
| `MEILI_MASTER_KEY` | Meilisearch API key | `changeme` |
| `MOCA_ENV` | Environment mode | `production` |

## Scaling Workers

Scale the worker service for higher job throughput:

```bash
docker compose -f docker-compose.example.yml up -d --scale moca-worker=3
```

## Data Persistence

Data is stored in Docker named volumes:
- `postgres_data` — Database files
- `redis_data` — Redis persistence
- `meili_data` — Search indexes

Back up volumes before upgrades:

```bash
docker compose -f docker-compose.example.yml exec postgres pg_dumpall -U moca > backup.sql
```

## Upgrading

```bash
# Pull new images
docker compose -f docker-compose.example.yml pull

# Recreate containers
docker compose -f docker-compose.example.yml up -d
```

Or if building from source:

```bash
git pull
docker compose -f docker-compose.example.yml up -d --build
```

## Resource Limits

Uncomment the `deploy.resources` sections in `docker-compose.example.yml` to set memory and CPU limits per service.

## Related

- [Docker Setup](Operations-Docker-Setup)
- [Deployment Overview](Operations-Deployment)
- [Kubernetes Deployment](Operations-Deployment-Kubernetes)
```

- [ ] **Step 2: Commit (wiki submodule)**

```bash
cd wiki
git add Operations-Deployment-Docker.md
git commit -m "docs: add Docker deployment guide"
cd ..
git add wiki
git commit -m "docs: update wiki submodule with Docker deployment guide"
```

---

### Task 16: Create Kubernetes Deployment Guide

**Files:**
- Create: `wiki/Operations-Deployment-Kubernetes.md`

- [ ] **Step 1: Write the guide**

Write this to `wiki/Operations-Deployment-Kubernetes.md`:

```markdown
# Kubernetes Deployment

> Deploy Moca to Kubernetes using generated manifests.

## Prerequisites

- Kubernetes 1.28+
- `kubectl` configured for your cluster
- Moca CLI installed locally
- Container images available (ghcr.io or your private registry)

## 1. Generate Manifests

```bash
moca generate k8s
```

This produces 7 manifests in the current directory:

| File | Resource |
|------|----------|
| `deployment.yaml` | Deployment with configurable replicas and health checks |
| `service.yaml` | ClusterIP + LoadBalancer service |
| `ingress.yaml` | Ingress with TLS configuration |
| `configmap.yaml` | Non-sensitive configuration |
| `secret.yaml` | Database credentials and API keys |
| `hpa.yaml` | Horizontal Pod Autoscaler (CPU/memory) |
| `pdb.yaml` | Pod Disruption Budget for availability |

## 2. Configure Secrets

Edit `secret.yaml` with base64-encoded credentials:

```bash
echo -n 'your_db_password' | base64
```

Or use external secrets management:
- [external-secrets](https://external-secrets.io/) for cloud provider secret stores
- [sealed-secrets](https://sealed-secrets.netlify.app/) for git-committed encrypted secrets

## 3. Apply Manifests

```bash
kubectl create namespace moca
kubectl apply -n moca -f configmap.yaml
kubectl apply -n moca -f secret.yaml
kubectl apply -n moca -f deployment.yaml
kubectl apply -n moca -f service.yaml
kubectl apply -n moca -f ingress.yaml
kubectl apply -n moca -f hpa.yaml
kubectl apply -n moca -f pdb.yaml
```

## 4. Verify

```bash
kubectl get pods -n moca
kubectl logs -n moca deployment/moca-server
```

## Ingress Configuration

The generated `ingress.yaml` supports TLS. Ensure you have a cert-manager or similar TLS provider configured:

```yaml
spec:
  tls:
    - hosts:
        - moca.example.com
      secretName: moca-tls
```

## HPA Tuning

Default HPA scales `moca-server` and `moca-worker` based on CPU utilization. Adjust thresholds in `hpa.yaml`:

```yaml
metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
```

Recommended scaling:

| Service | Min | Max | Scale On |
|---------|-----|-----|----------|
| moca-server | 2 | 10 | CPU 70% |
| moca-worker | 1 | 5 | CPU 80% |
| moca-scheduler | 1 | 1 | N/A (single leader) |
| moca-outbox | 1 | 1 | N/A (single leader) |

## Multi-Tenant Considerations

- One Kubernetes namespace per environment (staging, production)
- All tenants share the same pods — tenant isolation is at the PostgreSQL schema level
- Use separate Meilisearch instances for very high tenant counts (>100)

## Database

PostgreSQL should run outside Kubernetes for production (managed service or dedicated VM). If running in-cluster, use a StatefulSet with persistent volumes.

## Related

- [Deployment Overview](Operations-Deployment)
- [Docker Deployment](Operations-Deployment-Docker)
- [Monitoring](Operations-Monitoring)
```

- [ ] **Step 2: Commit (wiki submodule)**

```bash
cd wiki
git add Operations-Deployment-Kubernetes.md
git commit -m "docs: add Kubernetes deployment guide"
cd ..
git add wiki
git commit -m "docs: update wiki submodule with Kubernetes deployment guide"
```

---

### Task 17: Update Wiki Sidebar

**Files:**
- Modify: `wiki/_Sidebar.md`

- [ ] **Step 1: Update _Sidebar.md**

In `wiki/_Sidebar.md`, replace the Operations section (lines 46-52) with nested deployment links:

```markdown
**Operations**
- [Deployment](Operations-Deployment)
  - [Single Server](Operations-Deployment-Single-Server)
  - [Docker](Operations-Deployment-Docker)
  - [Kubernetes](Operations-Deployment-Kubernetes)
- [Docker Setup](Operations-Docker-Setup)
- [Backup & Restore](Operations-Backup-and-Restore)
- [Monitoring](Operations-Monitoring)
- [Security](Operations-Security)
```

- [ ] **Step 2: Commit (wiki submodule)**

```bash
cd wiki
git add _Sidebar.md
git commit -m "docs: add deployment guide links to sidebar"
cd ..
git add wiki
git commit -m "docs: update wiki submodule sidebar with deployment links"
```

---

### Task 18: Update Milestone Status and Changelog Wiki Pages

**Files:**
- Modify: `wiki/Roadmap-Milestone-Status.md`
- Modify: `wiki/Roadmap-Changelog.md`

- [ ] **Step 1: Update Roadmap-Milestone-Status.md**

Replace the entire content of `wiki/Roadmap-Milestone-Status.md` with:

```markdown
# Milestone Status

> Status of all 30 milestones as of April 2026.

## Complete

| MS | Name | Description |
|----|------|-------------|
| 00 | Architecture Validation | 5 spikes: PostgreSQL tenant isolation, Redis Streams, Go workspace, Meilisearch, Cobra CLI |
| 01 | Project Structure & Config | Go module layout, YAML config, build system |
| 02 | PostgreSQL & Redis Foundation | pgxpool, schema-per-tenant, Redis 4-DB client factory |
| 03 | Metadata Registry | MetaType compiler, 3-tier cache, DDL generator, migrator |
| 04 | Document Runtime | Document interface, lifecycle engine, naming, validation, CRUD |
| 05 | Query Engine & Reports | Dynamic QueryBuilder, 15 operators, JSONB transparency, ReportDef |
| 06 | REST API Layer | Auto-generated CRUD, middleware chain, rate limiting, versioning |
| 07 | CLI Foundation | Cobra command registry, context detection, output formatters |
| 08 | Hook Registry & App System | Priority hooks, dependency DAG, AppManifest, app loader |
| 09 | CLI Site & App Commands | `moca init`, `moca site`, `moca app` commands |
| 10 | Dev Server & Hot Reload | `moca serve` with in-process workers, file watching |
| 11 | CLI Operational Commands | `moca db`, `moca backup`, `moca config` commands |
| 12 | Multitenancy | Site resolver, schema management, per-site pools, Redis/Meili isolation |
| 13 | App Scaffolding & User Mgmt | `moca app new`, `moca user` commands, dev tools |
| 14 | Permission Engine | RBAC, field-level security, row-level security, custom rules |
| 15 | Background Jobs & Events | Redis Streams worker, scheduler, Kafka/Redis events, outbox, search sync |
| 16 | CLI Queue/Events/Search/Monitor | `moca queue`, `moca events`, `moca search`, `moca monitor` commands |
| 17 | React Desk Foundation | App shell, MetaProvider, FormView, ListView, field components |
| 18 | API Keys & Webhooks | API key auth, webhook dispatcher, custom endpoints, APIConfig per DocType |
| 19 | Desk Real-Time & Custom Fields | WebSocket hub, custom field type registry, version tracking |
| 20 | GraphQL, Dashboards & Reports | GraphQL auto-gen, dashboard framework, report engine, i18n, file storage |
| 21 | Deployment & Infrastructure | `moca generate` (7 generators), `moca deploy` (6 commands), backup automation |
| 22 | Security Hardening | OAuth2, SAML/OIDC SSO, encryption at rest, notifications |
| 23 | Workflow Engine | State machine, transitions, approval chains, SLA timers |
| 24 | Observability & Profiling | Prometheus metrics, OpenTelemetry tracing, `moca doctor`, profiling |
| 25 | Testing Framework | Test runner, fixture generation, coverage, test data factories |

## In Progress

| MS | Name | Description |
|----|------|-------------|
| 26 | Documentation & v1.0 | Developer docs, packaging, Docker images, deployment guides |

## Post-v1.0

| MS | Name | Description |
|----|------|-------------|
| 27 | Portal SSR | Server-side rendered public pages |
| 28 | Advanced Features | VirtualDoc, CDC, dev console, Playwright integration |
| 29 | Plugin Marketplace | WASM sandboxing, plugin registry, marketplace |

## Related

- [Roadmap Overview](Roadmap-Overview)
- [Changelog](Roadmap-Changelog)
```

- [ ] **Step 2: Update Roadmap-Changelog.md**

Replace the entire content of `wiki/Roadmap-Changelog.md` with:

```markdown
# Changelog

> Release notes for published versions.

## v1.0.0 (Upcoming)

Documentation, packaging, and v1.0 polish (MS-26):
- Apache-2.0 LICENSE and CONTRIBUTING.md
- GoReleaser for local cross-compilation
- Multi-target Dockerfile for all 5 binaries
- Docker image publishing to ghcr.io
- Auto-generated CLI and API reference in wiki
- Deployment guides: single-server, Docker, Kubernetes

## v0.4.0-rc (2026-04-13)

Observability and testing (MS-24, MS-25):
- Prometheus metrics (13 framework metrics)
- OpenTelemetry tracing over OTLP/gRPC
- `moca doctor` with project/infra/site diagnostics
- `moca dev bench` and `moca dev profile`
- Test runner, fixture generation, coverage reporting

## v0.3.0-beta (2026-04-07)

Feature-complete beta (MS-17 through MS-23):
- React Desk app shell with FormView, ListView, 29 field components
- API key auth, webhooks, custom endpoints
- WebSocket real-time, custom field types, version tracking
- GraphQL, dashboards, reports, i18n, file storage
- Deployment tooling (7 generators, 6 deploy commands)
- OAuth2, SAML/OIDC SSO, encryption, notifications
- Workflow engine with state machine and approval chains

## v0.2.0-alpha (2026-04-01)

Core backend features (MS-11 through MS-16):
- CLI operational commands (db, backup, config)
- Full multitenancy with schema-per-tenant isolation
- App scaffolding, user management, developer tools
- Permission engine (RBAC, FLS, RLS)
- Background jobs, scheduler, Kafka/Redis events, search sync
- CLI queue/events/search/monitor commands

## v0.1.0-mvp (2026-04-02)

Initial MVP release (MS-00 through MS-10):
- Project initialization and configuration
- PostgreSQL schema-per-tenant multitenancy
- MetaType registry with 3-tier cache
- Document runtime with 16-event lifecycle
- Dynamic query builder with 15 operators
- REST API with auto-generated CRUD endpoints
- CLI foundation with 100+ commands
- Hook registry, app system, dev server

## Nightly

Nightly builds are available via GitHub Actions. See [CI/CD Pipeline](Contributing-CI-CD-Pipeline).

## Related

- [Milestone Status](Roadmap-Milestone-Status)
- [Roadmap Overview](Roadmap-Overview)
```

- [ ] **Step 3: Commit (wiki submodule)**

```bash
cd wiki
git add Roadmap-Milestone-Status.md Roadmap-Changelog.md
git commit -m "docs: update milestone status through MS-25 and sync changelog"
cd ..
git add wiki
git commit -m "docs: update wiki submodule with current milestone status"
```

---

### Task 19: Update Remaining Wiki Pages

**Files:**
- Modify: `wiki/Guide-Getting-Started.md`
- Modify: `wiki/Guide-Creating-Your-First-App.md`

These pages are already well-written and current. The changes here are minor additions.

- [ ] **Step 1: Update Guide-Getting-Started.md**

In `wiki/Guide-Getting-Started.md`, add an alternative install method after the existing `## 1. Install Moca CLI` section. After the `go install` line (line 16), add:

```markdown
Or use the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/osama1998H/moca/main/install.sh | sh
```
```

- [ ] **Step 2: Update Guide-Creating-Your-First-App.md**

In `wiki/Guide-Creating-Your-First-App.md`, add a testing section before the `## Add Desk Extensions` section (before line 111). Insert:

```markdown
## Test Your App

Run the app's tests:

```bash
moca test run --app todo --site mysite
```

This runs all tests in the app's test directory using Moca's test runner with fixture support and coverage reporting.
```

- [ ] **Step 3: Commit (wiki submodule)**

```bash
cd wiki
git add Guide-Getting-Started.md Guide-Creating-Your-First-App.md
git commit -m "docs: add install script option and testing section to guides"
cd ..
git add wiki
git commit -m "docs: update wiki submodule with guide improvements"
```

---

### Task 20: Final MS-26 Plan Doc Update

**Files:**
- Modify: `docs/milestones/MS-26-documentation-packaging-v1-polish-plan.md` (create if not exists)

- [ ] **Step 1: Create the milestone plan document**

Create `docs/milestones/MS-26-documentation-packaging-v1-polish-plan.md` following the pattern of existing milestone plans (e.g., `docs/milestones/MS-16-cli-queue-events-search-monitor-log-commands-plan.md`):

```markdown
# MS-26: Documentation, Packaging, and v1.0 Polish — Plan

## Status: In Progress

## Overview

Developer docs, API reference, deployment guides, release packaging (GoReleaser, Docker, install script), and final v1.0 polish. This is the final milestone before v1.0.

## Dependencies

- MS-25 (Testing Framework) — Complete

## Tasks

### Task 1 (MS-26-T1): Foundation — LICENSE, CONTRIBUTING, CHANGELOG, GoReleaser
- Status: Complete
- Deliverables: `LICENSE` (Apache-2.0), `CONTRIBUTING.md`, updated `CHANGELOG.md`, `.goreleaser.yml`

### Task 2 (MS-26-T2): Docker & CI — Dockerfile, Compose, ghcr.io Publishing
- Status: Complete
- Deliverables: `Dockerfile` (multi-target), `.dockerignore`, `docker-compose.example.yml`, `.env.example`, `release.yml` docker-publish job

### Task 3 (MS-26-T3): Auto-generation Tooling — CLI/API Reference Generators
- Status: Complete
- Deliverables: `internal/docgen/` package, `moca docgen` hidden command, Makefile targets, wiki markers

### Task 4 (MS-26-T4): Wiki Expansion — Deployment Guides, Page Updates
- Status: Complete
- Deliverables: 3 deployment guides, 2 rewritten stub pages, sidebar update, milestone status update, guide improvements

## Acceptance Criteria

- [ ] `LICENSE` file exists with Apache-2.0 text
- [ ] `CONTRIBUTING.md` exists in repo root
- [ ] `CHANGELOG.md` has entries for all milestones through MS-25
- [ ] `.goreleaser.yml` present with 5 binary builds
- [ ] `Dockerfile` builds all 5 targets
- [ ] `docker-compose.example.yml` defines full production stack
- [ ] `release.yml` includes `docker-publish` job for ghcr.io
- [ ] `make docs-generate` injects CLI/API reference into wiki
- [ ] 3 deployment guides exist (single-server, Docker, K8s)
- [ ] Wiki sidebar updated with deployment guide links
- [ ] Milestone status page shows MS-00 through MS-25 as complete
```

- [ ] **Step 2: Commit**

```bash
git add docs/milestones/MS-26-documentation-packaging-v1-polish-plan.md
git commit -m "docs: add MS-26 milestone plan document"
```

---

## Summary

| Phase | Tasks | Description |
|-------|-------|-------------|
| 1: Foundation | 1-4 | LICENSE, CONTRIBUTING.md, CHANGELOG update, GoReleaser |
| 2: Docker & CI | 5-7 | Dockerfile, docker-compose.example.yml, ghcr.io CI job |
| 3: Auto-generation | 8-12 | inject utility, CLI generator, API generator, docgen command, wiki markers |
| 4: Wiki Expansion | 13-20 | Deployment guides, stub rewrites, sidebar, milestone status, plan doc |
