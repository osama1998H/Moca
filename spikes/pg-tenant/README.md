# Spike 1: PostgreSQL Schema-Per-Tenant Isolation

[![CI](https://github.com/osama1998H/Moca/actions/workflows/ci.yml/badge.svg?branch=main&event=push)](https://github.com/osama1998H/Moca/actions/workflows/ci.yml)

**Status:** Completed
**Task:** MS-00-T2
**Design Reference:** `docs/blocker-resolution-strategies.md` (Blocker 2, lines 66-178)
**ADR:** `ADR-001-pg-tenant-isolation.md`

## Result

All 7 tests pass with the `-race` flag. Zero cross-contamination across 100 concurrent
goroutines × 10 tenant schemas. The per-site `pgxpool.Pool` registry with `AfterConnect`
is validated as the correct isolation strategy.

## Key Findings

1. **`AfterConnect` (not deprecated `BeforeAcquire`) is the correct hook.** It sets
   `search_path` permanently for every physical connection in the pool. The setting
   persists across acquire/release cycles with zero per-acquire overhead.
2. **100 goroutines × 10 schemas: zero cross-contamination.** Every row read back by a
   goroutine contained the expected tenant schema name.
3. **Prepared statement caches are naturally isolated.** Separate pools have separate
   caches. No cross-pool statement reuse is possible.
4. **Idle eviction works.** Pools can be lazily created, evicted when idle, and
   transparently re-created on next access — all with correct `search_path` after
   re-creation.
5. **System pool isolation confirmed.** The `moca_system` pool cannot see tenant
   tables via unqualified names, and tenant operations do not affect the system pool.

## Questions Answered

| Question | Answer |
|----------|--------|
| `AfterConnect` vs deprecated `BeforeAcquire`? | `AfterConnect` is correct — permanent, zero overhead |
| Cross-contamination under concurrency? | None — 100 goroutines × 10 schemas, all clean |
| Prepared statement cache leakage? | Impossible — separate pools have separate caches |
| Idle eviction + `search_path`? | Re-created pool runs `AfterConnect` again — fully self-healing |

## Deliverables

- `main.go` — DBManager, per-site pool registry, AfterConnect callback, assertSchema defense
- `main_test.go` — 7 tests including 100 goroutines × 10 schemas concurrent access test
- `docker-compose.yml` — PostgreSQL 16 container on port 5433
- `ADR-001-pg-tenant-isolation.md` — architecture decision record with production guidance

## Running the Spike

```bash
cd spikes/pg-tenant
docker compose up -d
go test -v -count=1 -race ./...
docker compose down
```

Or from the repo root: `make spike-pg`
