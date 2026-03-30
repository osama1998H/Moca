# Spike 1: PostgreSQL Schema-Per-Tenant Isolation

**Status:** Not started
**Task:** MS-00-T2
**Design Reference:** `docs/blocker-resolution-strategies.md` (Blocker 2, lines 66-178)

## Objective

Validate the per-site pgxpool registry pattern under concurrent access.
Prove that the `AfterConnect` callback correctly and permanently sets `search_path`
for every connection in a tenant's pool, with zero cross-contamination.

## Key Questions to Answer

1. Does `AfterConnect` (not the deprecated `BeforeAcquire`) reliably isolate schemas per pool?
2. Under 100 concurrent goroutines across 10 tenant schemas, is there ever a data leak?
3. Do prepared statement caches stay isolated between separate pools?
4. How does idle pool eviction interact with `search_path` — is it re-set on new connections?

## Expected Deliverables

- `main.go` — DBManager, per-site pool registry, AfterConnect callback, assertSchema defense
- `main_test.go` — concurrent access test (100 goroutines × 10 schemas)
- `docker-compose.yml` — PostgreSQL 16 container
- `ADR-001-pg-tenant-isolation.md` — architecture decision record
