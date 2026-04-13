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

- [x] `LICENSE` file exists with Apache-2.0 text
- [x] `CONTRIBUTING.md` exists in repo root
- [x] `CHANGELOG.md` has entries for all milestones through MS-25
- [x] `.goreleaser.yml` present with 5 binary builds
- [x] `Dockerfile` builds all 5 targets
- [x] `docker-compose.example.yml` defines full production stack
- [x] `release.yml` includes `docker-publish` job for ghcr.io
- [x] `make docs-generate` injects CLI/API reference into wiki
- [x] 3 deployment guides exist (single-server, Docker, K8s)
- [x] Wiki sidebar updated with deployment guide links
- [x] Milestone status page shows MS-00 through MS-25 as complete
