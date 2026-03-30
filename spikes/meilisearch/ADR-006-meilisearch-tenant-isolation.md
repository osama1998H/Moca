# ADR-006: Meilisearch Prefix-Based Index-Per-Tenant over Tenant Tokens

**Status:** Accepted
**Spike:** MS-00-T5
**Date:** 2026-03-30
**Validated by:** `GOWORK=off go test -v -count=1 -race ./...` — all 7 tests pass

---

## Context

MOCA needs full-text search across documents with tenant isolation. The design
chose Meilisearch over Elasticsearch (MOCA_SYSTEM_DESIGN.md ADR-006) because
Meilisearch is simpler to operate and provides excellent typo tolerance without
configuration.

The fundamental tension: two viable tenant isolation strategies exist.

1. **Index-per-tenant with prefix naming** (design doc §8.2 line 1431 specifies
   this): each site gets its own Meilisearch index per doctype, named
   `{site_name}_{doctype}` (e.g. `acme_product`, `acme_invoice`). Isolation is
   enforced by index boundaries — a request to `acme_product` physically cannot
   reach `globex_product`.

2. **Tenant tokens** (Meilisearch's recommended approach): all tenants share a
   single index (e.g. `products`) with a `tenant_id` filterable attribute.
   JWT tokens embed filter rules (`"filter": "tenant_id = acme"`) that the
   server enforces on every search. A search with a tenant token cannot see
   documents belonging to other tenants.

At scale (10,000 tenants × 10 doctypes = 100,000 Meilisearch indexes),
the index-per-tenant approach may create resource pressure. This spike validates
both strategies and recommends a default.

---

## Decision

Use **index-per-tenant with prefix naming** (`{site_name}_{doctype}`) as the
default isolation strategy for MOCA. The tenant-token approach is retained as
an alternative for deployments with very high tenant counts (10,000+).

### Index Naming Convention

The convention from MOCA_SYSTEM_DESIGN.md §8.2:

| Index UID format | Example |
|-----------------|---------|
| `{site_name}_{doctype}` | `acme_product` |
| | `acme_invoice` |
| | `globex_product` |
| | `tenant_123_contact` |

Implementation: `IndexName(tenant, doctype string) string` in `pkg/search/indexer.go`.

### Filterable Attributes

Every MOCA index should configure at minimum:

```go
FilterableAttributes: []string{"tenant_id", "status", "doctype", "category"},
```

The `tenant_id` field is required even in index-per-tenant mode because:
- It enables multi-search queries across tenant indexes to be filtered safely.
- If MOCA migrates to the tenant-token strategy later, documents already have
  the required field — no re-indexing needed.

### Bulk Indexing

Use `AddDocumentsInBatches(docs, batchSize, nil)` for initial indexing and
large updates. A batch size of 250 documents is a reliable default. The method
returns one `TaskInfo` per batch; wait for all tasks before asserting counts.

```
1,000 documents / batch size 250 = 4 tasks
2 × 1,000 documents indexed in ~256ms total (both tenants, sequential)
```

### Search Sync Pipeline

In production, search indexing follows the path from
MOCA_SYSTEM_DESIGN.md §6.5 (lines 1247, 1252):

- **With Kafka**: `document save → outbox → moca.doc.events topic →
  moca-search-sync → Meilisearch`
- **Without Kafka**: synchronous indexing on document save (or via Redis
  Streams background job in MS-15)

---

## Alternatives Considered

### Option A: Tenant Tokens (Single Shared Index) — Retained as Alternative

- All tenants share one index per doctype (e.g. a single `product` index for
  all sites). Documents include a `tenant_id` filterable field.
- JWT tokens generated via `GenerateTenantToken()` embed filter rules:
  `{"product": {"filter": "tenant_id = acme"}}`.
- The Meilisearch server enforces the filter on every search — a client with
  an acme token cannot see globex documents.

**Advantages at scale:**
- 1 index per doctype regardless of tenant count (10,000 tenants × 10 doctypes
  = 10 indexes instead of 100,000).
- Simpler index lifecycle: create once per doctype, not per site.

**Disadvantages:**
- API key management: tenant tokens require an underlying API key UID and value
  (not the master key) and must be generated per search session.
- Cross-tenant admin queries require different token scoping logic.
- Documents from all tenants are co-located in one index, which can affect
  ranking quality (word frequencies are corpus-wide, not per-tenant).
- Meilisearch's documentation recommends this approach for multi-tenancy, but
  it is less familiar to developers who think of each site as an isolated unit.

**When to prefer tenant tokens:** when tenant count exceeds ~1,000 and index
memory overhead becomes a measured concern. The spike validated that the
tenant-token approach works correctly (both acme and globex isolation verified).

### Option B: Elasticsearch (Rejected)

Rejected in MOCA_SYSTEM_DESIGN.md ADR-006. Meilisearch provides sufficient
search quality for business application document search, with dramatically
simpler operations (no cluster management, no shard tuning, embedded RocksDB).
The trade-off (less powerful aggregations and analytics) is acceptable because
MOCA's primary use case is document retrieval, not log analytics.

### Option C: Shared Pool with Namespace Field (Rejected)

Using a single index with a namespace/tenant field but without server-enforced
filtering (i.e., application-side filtering in `pkg/search/query.go`) was
rejected because:
- Application-side filtering is a security risk: a bug in the filter-injection
  code could expose cross-tenant data.
- The tenant-token approach provides the same shared-index economics with
  server-enforced guarantees.

---

## Validation Results

| Test | Result | Key Observation |
|------|--------|-----------------|
| `TestIndexPerTenant` | PASS | `acme_products` and `globex_products` created; UIDs match `{tenant}_{doctype}` convention |
| `TestBulkIndexing` | PASS | 1,000 docs per tenant via `AddDocumentsInBatches` (batch=250, 4 tasks); ~256ms for both tenants |
| `TestTypoTolerance` | PASS | "Prodct" → 1,000 hits; "Widgt" → 1,000 hits; typo tolerance works with zero configuration |
| `TestTenantIsolation` | PASS | "Globex" in acme_products → 0 hits; "Acme" in globex_products → 0 hits; no cross-tenant leakage |
| `TestFilterableAttributes` | PASS | `tenant_id`, `status`, `doctype`, `category` configured; compound filters and facet distribution work |
| `TestMultiSearch` | PASS | Single API call returned 1,000 acme hits + 1,000 globex hits, each scoped to its index |
| `TestTenantToken` | PASS | acme token sees 50 docs (0 globex); globex token sees 50 docs (0 acme); JWT isolation confirmed |

All tests passed with `-race`. No data races detected.

### Key Observations

**Typo tolerance is genuinely zero-configuration.** "Prodct" (missing one char)
returned 1,000 hits. "Widgt" (missing one char) returned 1,000 hits. Meilisearch
allows up to 2 typos for words ≥ 8 characters, and 1 typo for words 5–7
characters. For business application search ("invioce" → "invoice",
"customr" → "customer"), this is excellent out-of-the-box behavior.

**Bulk indexing is fast and reliable.** 1,000 documents in 4 batches of 250
completed in ~256ms (both tenants sequentially). `AddDocumentsInBatches`
returns one `TaskInfo` per batch. Each task must be awaited before asserting
document counts — a `waitForTask` helper is essential for correctness.

**Prefix-based isolation is absolute.** The `acme_products` index physically
cannot contain globex documents. There is no code path by which a mis-configured
query could read across index boundaries. This aligns with the schema-per-tenant
mental model from ADR-001 (PostgreSQL) and makes the system easier to audit.

**Tenant-token isolation is server-enforced and reliable.** The JWT filter rules
are enforced by Meilisearch, not by application code. A client with an acme
token that queries the shared `products` index cannot receive globex documents
regardless of what query it submits. This is the correct trust model.

**Test design note: "product" is not a unique acme term.** The `doctype` field
is set to `"product"` for all documents from all tenants. Searching for "Product"
in the globex index returns globex documents because their doctype is "product".
This is correct behavior, not a leak. Isolation tests must use terms that are
unique to one tenant's documents (names, categories — not shared field values).

**MultiSearch is ideal for cross-tenant admin queries.** In a single HTTP
request, an admin can query multiple tenant indexes with individual query strings
and limits. Each result set is labelled with its `IndexUID`. This is more
efficient than N sequential searches.

---

## Consequences for Production

### Index Lifecycle

Create indexes during `create-site` (MOCA_SYSTEM_DESIGN.md §8.3, step 7).
Delete indexes during `remove-site`. Per-site, per-doctype index creation is
performed by `pkg/search/indexer.go` (MS-15 deliverable).

```go
// Pseudocode for pkg/search/indexer.go
func CreateSiteIndex(client meili.ServiceManager, site, doctype string) error {
    uid := IndexName(site, doctype)
    taskInfo, err := client.CreateIndex(&meili.IndexConfig{Uid: uid, PrimaryKey: "name"})
    if err != nil { return err }
    return waitForTask(client, taskInfo.TaskUID)
}
```

### Scaling Thresholds

| Tenant count | Indexes (×10 doctypes) | Recommendation |
|-------------|----------------------|---------------|
| ≤ 1,000 | ≤ 10,000 | Index-per-tenant (default) |
| 1,000–10,000 | 10,000–100,000 | Monitor memory; consider tenant-token |
| > 10,000 | > 100,000 | Switch to tenant-token pattern |

The decision point is empirical: measure Meilisearch's RSS under production
load before switching. A Meilisearch instance on a 4 GiB server comfortably
handles thousands of small indexes.

### Search Sync Pipeline

- **MS-15 deliverable**: `pkg/search/sync.go` — document events → Meilisearch.
- **With Kafka** (line 1247): `moca.doc.events` → `moca-search-sync` process.
- **Without Kafka** (line 1252): synchronous in `moca-server` or Redis Streams
  background job. The `moca-search-sync` binary exits immediately if Kafka is
  disabled (see `docs/blocker-resolution-strategies.md`).

### SDK Version Lock

Pin `github.com/meilisearch/meilisearch-go v0.36.1` in `pkg/search/go.mod`
(or the root `go.mod`). The SDK is pre-1.0; minor versions may introduce
breaking API changes. Review release notes before upgrading.

---

## References

- `MOCA_SYSTEM_DESIGN.md` §8.2 line 1431 — Meilisearch index prefix isolation
- `MOCA_SYSTEM_DESIGN.md` ADR-006 lines 2093-2097 — Meilisearch over Elasticsearch
- `ROADMAP.md` line 122 — Acceptance criteria for Spike 4
- `ROADMAP.md` MS-15 lines 751-796 — Meilisearch indexer, sync, and search API
- `MOCA_SYSTEM_DESIGN.md` §6.5 lines 1247, 1252 — Kafka-optional search sync
- meilisearch-go v0.36.1: `CreateIndex`, `AddDocumentsInBatches`, `Search`,
  `MultiSearch`, `GenerateTenantToken`, `WaitForTask`
- Meilisearch tenant tokens: https://www.meilisearch.com/docs/learn/security/tenant_tokens
