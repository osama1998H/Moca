# Spike 4: Meilisearch Multi-Index Tenant Isolation

[![CI](https://github.com/osama1998H/Moca/actions/workflows/ci.yml/badge.svg?branch=main&event=push)](https://github.com/osama1998H/Moca/actions/workflows/ci.yml)

**Status:** Completed
**Task:** MS-00-T5
**Design Reference:** `MOCA_SYSTEM_DESIGN.md` §8.2 (line 1431), ADR-006 (lines 2093-2097)
**ADR:** `ADR-006-meilisearch-tenant-isolation.md`

## Result

All 7 tests pass with the `-race` flag. 1,000 documents indexed per tenant via
`AddDocumentsInBatches` in ~256ms. Typo tolerance returns results for "Prodct"
and "Widgt" without any configuration. Prefix-based index isolation provides
complete tenant separation. Tenant-token alternative validated as viable for
high-tenant-count deployments.

## Key Findings

| Question | Answer |
|----------|--------|
| Prefix-based index isolation correct? | Yes — `acme_products` and `globex_products` are physically separate; cross-tenant search returns 0 hits |
| Bulk indexing reliable at 1,000 docs? | Yes — `AddDocumentsInBatches` (batch=250) indexed 1,000 docs in 4 tasks, ~256ms total |
| Typo tolerance works out of the box? | Yes — "Prodct"→"Product" and "Widgt"→"Widget" with zero configuration |
| Multi-search across tenant indexes? | Yes — single `MultiSearch` call returns scoped results from multiple indexes |
| Tenant-token pattern viable? | Yes — JWT-enforced single-index filtering works; acme and globex isolation confirmed |
| Recommended isolation strategy? | Index-per-tenant (default) for simplicity and alignment with schema-per-tenant model; tenant-token for 10,000+ tenants |

## ROADMAP Acceptance Criteria (line 122)

- [x] Creates index with tenant prefix (`acme_products`, `globex_products`)
- [x] Indexes 1,000 documents per tenant
- [x] Searches with typo tolerance ("Prodct" → "Product")
- [x] Verifies tenant isolation (acme's docs invisible to globex's index)

## Deliverables

- `main.go` — `Product` type, `IndexName`, `generateProducts`, `waitForTask`, `cleanupTestIndexes`
- `main_test.go` — 7 tests: `TestIndexPerTenant`, `TestBulkIndexing`, `TestTypoTolerance`,
  `TestTenantIsolation`, `TestFilterableAttributes`, `TestMultiSearch`, `TestTenantToken`
- `docker-compose.yml` — Meilisearch v1.12 container on port 7701 with master key
- `ADR-006-meilisearch-tenant-isolation.md` — recommended isolation strategy with trade-offs

## Running the Spike

```bash
cd spikes/meilisearch
docker compose up -d
GOWORK=off go test -v -count=1 -race ./...
docker compose down
```

Or from the repo root:

```bash
make spike-meili
```
