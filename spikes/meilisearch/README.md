# Spike 4: Meilisearch Multi-Index Tenant Isolation

**Status:** Not started
**Task:** MS-00-T5
**Design Reference:** `MOCA_SYSTEM_DESIGN.md` ADR-006 (lines 2093-2097)

## Objective

Validate Meilisearch as the full-text search engine with tenant isolation.
Compare prefix-based index-per-tenant vs the Meilisearch-recommended tenant-token pattern.
Measure bulk indexing performance and verify typo-tolerant search.

## Key Questions to Answer

1. Does prefix-based index naming (`tenant_acme_products`) provide correct tenant isolation?
2. At scale (100+ indexes), what is the memory overhead of the index-per-tenant pattern?
3. Is `AddDocumentsInBatches` reliable for bulk indexing 1,000 documents?
4. Does typo tolerance work as expected (e.g., "prodct" → "product" results)?
5. Should MOCA default to prefix-based or tenant-token isolation? What are the trade-offs?

## Expected Deliverables

- `main.go` — spike implementation (index creation, bulk indexing, search, multi-search)
- `main_test.go` — test suite for all 7 scenarios
- `docker-compose.yml` — Meilisearch latest v1.x container
- `ADR-006-meilisearch-tenant-isolation.md` — recommended isolation pattern with trade-offs
