// Tests for MS-00-T5: Meilisearch Multi-Index Tenant Isolation Spike.
//
// Prerequisites: Meilisearch v1.12 running on localhost:7701
//
//	docker compose up -d
//
// Run:  go test -v -count=1 -race ./...
// Or:   make spike-meili  (from repo root)
//
// Environment overrides:
//
//	MEILI_URL=http://host:port   (default: http://localhost:7701)
//	MEILI_MASTER_KEY=yourkey     (default: moca_test_master_key)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	meilisearch "github.com/meilisearch/meilisearch-go"
)

const (
	defaultMeiliURL   = "http://localhost:7701"
	defaultMasterKey  = "moca_test_master_key"
	tenantAcme        = "acme"
	tenantGlobex      = "globex"
	docsPerTenant     = 1000
	batchSize         = 250
	acmeProductsIndex = "acme_products"
	globexProductsIdx = "globex_products"
	sharedProductsIdx = "products"
)

// meiliClient is the shared client for the test suite (uses master key).
var meiliClient meilisearch.ServiceManager

func TestMain(m *testing.M) {
	url := os.Getenv("MEILI_URL")
	if url == "" {
		url = defaultMeiliURL
	}
	key := os.Getenv("MEILI_MASTER_KEY")
	if key == "" {
		key = defaultMasterKey
	}

	meiliClient = meilisearch.New(url, meilisearch.WithAPIKey(key))

	if !meiliClient.IsHealthy() {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to Meilisearch at %s\n", url)
		fmt.Fprintf(os.Stderr, "  Start Meilisearch: docker compose up -d\n")
		os.Exit(1)
	}

	// Pre-test cleanup: delete any leftover indexes from previous runs.
	if err := cleanupTestIndexes(meiliClient, []string{"acme_", "globex_", sharedProductsIdx}); err != nil {
		fmt.Fprintf(os.Stderr, "pre-test cleanup: %v\n", err)
		os.Exit(1)
	}

	exitCode := m.Run()

	// Post-test cleanup.
	_ = cleanupTestIndexes(meiliClient, []string{"acme_", "globex_", sharedProductsIdx})

	os.Exit(exitCode)
}

// ────────────────────────────────────────────────────────────────────────────
// Test 1: Index-per-Tenant with Prefix Naming
// ────────────────────────────────────────────────────────────────────────────

// TestIndexPerTenant validates prefix-based index creation and the IndexName
// naming convention from MOCA_SYSTEM_DESIGN.md §8.2.
func TestIndexPerTenant(t *testing.T) {
	// Verify IndexName produces the correct {tenant}_{doctype} format.
	cases := []struct {
		tenant  string
		doctype string
		want    string
	}{
		{"acme", "products", "acme_products"},
		{"globex", "products", "globex_products"},
		{"acme", "invoice", "acme_invoice"},
		{"site_123", "contact", "site_123_contact"},
	}
	for _, c := range cases {
		got := IndexName(c.tenant, c.doctype)
		if got != c.want {
			t.Errorf("IndexName(%q, %q) = %q, want %q", c.tenant, c.doctype, got, c.want)
		}
	}

	// Create acme_products and globex_products indexes.
	for _, uid := range []string{acmeProductsIndex, globexProductsIdx} {
		taskInfo, err := meiliClient.CreateIndex(&meilisearch.IndexConfig{
			Uid:        uid,
			PrimaryKey: "id",
		})
		if err != nil {
			t.Fatalf("CreateIndex %q: %v", uid, err)
		}
		if err := waitForTask(meiliClient, taskInfo.TaskUID); err != nil {
			t.Fatalf("waitForTask (create %q): %v", uid, err)
		}
	}

	// Verify both indexes exist and have the expected UIDs.
	for _, uid := range []string{acmeProductsIndex, globexProductsIdx} {
		idx, err := meiliClient.GetIndex(uid)
		if err != nil {
			t.Errorf("GetIndex %q: %v", uid, err)
			continue
		}
		if idx.UID != uid {
			t.Errorf("index UID = %q, want %q", idx.UID, uid)
		}
		if idx.PrimaryKey != "id" {
			t.Errorf("index %q PrimaryKey = %q, want %q", uid, idx.PrimaryKey, "id")
		}
	}

	// Verify index UIDs follow the {site_name}_{doctype} convention.
	if !strings.HasPrefix(acmeProductsIndex, tenantAcme+"_") {
		t.Errorf("acme index UID %q does not start with %q", acmeProductsIndex, tenantAcme+"_")
	}
	if !strings.HasPrefix(globexProductsIdx, tenantGlobex+"_") {
		t.Errorf("globex index UID %q does not start with %q", globexProductsIdx, tenantGlobex+"_")
	}

	t.Logf("VALIDATED: IndexName produces {tenant}_{doctype} format; "+
		"acme_products and globex_products created with correct primary keys")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 2: Bulk Indexing (1,000 documents per tenant)
// ────────────────────────────────────────────────────────────────────────────

// TestBulkIndexing validates AddDocumentsInBatches at 1,000 documents per
// tenant. Verifies final document count via GetStats.
func TestBulkIndexing(t *testing.T) {
	acmeDocs := generateProducts(tenantAcme, docsPerTenant)
	globexDocs := generateProducts(tenantGlobex, docsPerTenant)

	start := time.Now()

	// Index acme documents in batches.
	acmeTasks, err := meiliClient.Index(acmeProductsIndex).AddDocumentsInBatches(acmeDocs, batchSize, nil)
	if err != nil {
		t.Fatalf("AddDocumentsInBatches acme: %v", err)
	}
	for i, taskInfo := range acmeTasks {
		if err := waitForTask(meiliClient, taskInfo.TaskUID); err != nil {
			t.Fatalf("waitForTask acme batch %d: %v", i, err)
		}
	}

	// Index globex documents in batches.
	globexTasks, err := meiliClient.Index(globexProductsIdx).AddDocumentsInBatches(globexDocs, batchSize, nil)
	if err != nil {
		t.Fatalf("AddDocumentsInBatches globex: %v", err)
	}
	for i, taskInfo := range globexTasks {
		if err := waitForTask(meiliClient, taskInfo.TaskUID); err != nil {
			t.Fatalf("waitForTask globex batch %d: %v", i, err)
		}
	}

	elapsed := time.Since(start)

	// Verify each tenant's index has exactly docsPerTenant documents.
	acmeStats, err := meiliClient.Index(acmeProductsIndex).GetStats()
	if err != nil {
		t.Fatalf("GetStats acme: %v", err)
	}
	if acmeStats.NumberOfDocuments != docsPerTenant {
		t.Errorf("acme_products: doc count = %d, want %d", acmeStats.NumberOfDocuments, docsPerTenant)
	}

	globexStats, err := meiliClient.Index(globexProductsIdx).GetStats()
	if err != nil {
		t.Fatalf("GetStats globex: %v", err)
	}
	if globexStats.NumberOfDocuments != docsPerTenant {
		t.Errorf("globex_products: doc count = %d, want %d", globexStats.NumberOfDocuments, docsPerTenant)
	}

	// Verify expected number of batch tasks (1000 docs / 250 per batch = 4 tasks each).
	expectedBatches := docsPerTenant / batchSize
	if len(acmeTasks) != expectedBatches {
		t.Errorf("acme batch count = %d, want %d", len(acmeTasks), expectedBatches)
	}
	if len(globexTasks) != expectedBatches {
		t.Errorf("globex batch count = %d, want %d", len(globexTasks), expectedBatches)
	}

	t.Logf("VALIDATED: %d docs indexed per tenant via AddDocumentsInBatches (batch=%d, %d tasks) in %v",
		docsPerTenant, batchSize, expectedBatches, elapsed.Round(time.Millisecond))
}

// ────────────────────────────────────────────────────────────────────────────
// Test 3: Typo Tolerance
// ────────────────────────────────────────────────────────────────────────────

// TestTypoTolerance validates that Meilisearch's out-of-box typo tolerance
// returns results for misspelled queries. Key finding from ADR-006:
// "excellent typo tolerance out of the box."
func TestTypoTolerance(t *testing.T) {
	acmeIdx := meiliClient.Index(acmeProductsIndex)
	globexIdx := meiliClient.Index(globexProductsIdx)

	// "Prodct" should match "Product" (1 typo, single-char deletion).
	resp, err := acmeIdx.Search("Prodct", &meilisearch.SearchRequest{Limit: 5})
	if err != nil {
		t.Fatalf("Search 'Prodct' in acme: %v", err)
	}
	if resp.EstimatedTotalHits == 0 {
		t.Error("typo 'Prodct': expected hits for 'Product', got 0")
	}
	t.Logf("typo 'Prodct' -> %d hits (estimated) in acme_products", resp.EstimatedTotalHits)

	// "Widgt" should match "Widget" (1 typo, single-char deletion).
	resp, err = globexIdx.Search("Widgt", &meilisearch.SearchRequest{Limit: 5})
	if err != nil {
		t.Fatalf("Search 'Widgt' in globex: %v", err)
	}
	if resp.EstimatedTotalHits == 0 {
		t.Error("typo 'Widgt': expected hits for 'Widget', got 0")
	}
	t.Logf("typo 'Widgt' -> %d hits (estimated) in globex_products", resp.EstimatedTotalHits)

	// Exact search should produce results and show relevance.
	resp, err = acmeIdx.Search("Acme Product", &meilisearch.SearchRequest{Limit: 10})
	if err != nil {
		t.Fatalf("Search 'Acme Product' in acme: %v", err)
	}
	if resp.EstimatedTotalHits == 0 {
		t.Error("exact 'Acme Product': expected hits, got 0")
	}
	t.Logf("exact 'Acme Product' -> %d hits (estimated)", resp.EstimatedTotalHits)

	// Verify a hit contains expected product data.
	if len(resp.Hits) > 0 {
		// Parse the first hit to verify it's an acme product.
		hitBytes, err := json.Marshal(resp.Hits[0])
		if err == nil {
			var p Product
			if err := json.Unmarshal(hitBytes, &p); err == nil {
				if p.TenantID != tenantAcme {
					t.Errorf("hit TenantID = %q, want %q", p.TenantID, tenantAcme)
				}
			}
		}
	}

	t.Log("VALIDATED: Meilisearch typo tolerance returns results for single-char typos " +
		"('Prodct'→'Product', 'Widgt'→'Widget') without any configuration")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 4: Tenant Isolation (Cross-Index)
// ────────────────────────────────────────────────────────────────────────────

// TestTenantIsolation verifies that prefix-based index isolation prevents
// cross-tenant data leakage. This is the critical security guarantee:
// site A's documents must be invisible to site B's index.
func TestTenantIsolation(t *testing.T) {
	acmeIdx := meiliClient.Index(acmeProductsIndex)
	globexIdx := meiliClient.Index(globexProductsIdx)

	// Searching acme index for globex-specific terms must return 0 hits.
	resp, err := acmeIdx.Search("Globex", &meilisearch.SearchRequest{Limit: 10})
	if err != nil {
		t.Fatalf("Search 'Globex' in acme_products: %v", err)
	}
	if resp.EstimatedTotalHits != 0 {
		t.Errorf("acme_products search for 'Globex' returned %d hits, want 0 (cross-tenant leak!)",
			resp.EstimatedTotalHits)
	}

	resp, err = acmeIdx.Search("Widget", &meilisearch.SearchRequest{Limit: 10})
	if err != nil {
		t.Fatalf("Search 'Widget' in acme_products: %v", err)
	}
	if resp.EstimatedTotalHits != 0 {
		t.Errorf("acme_products search for 'Widget' returned %d hits, want 0 (cross-tenant leak!)",
			resp.EstimatedTotalHits)
	}

	// Searching globex index for acme-specific terms must return 0 hits.
	// "Acme" appears only in acme product names ("Acme Product NNNN").
	resp, err = globexIdx.Search("Acme", &meilisearch.SearchRequest{Limit: 10})
	if err != nil {
		t.Fatalf("Search 'Acme' in globex_products: %v", err)
	}
	if resp.EstimatedTotalHits != 0 {
		t.Errorf("globex_products search for 'Acme' returned %d hits, want 0 (cross-tenant leak!)",
			resp.EstimatedTotalHits)
	}

	// "hardware" is acme's category; globex's category is "electronics".
	// Note: "product" / "Product" is NOT a valid isolation test term — it
	// appears in the doctype field of BOTH tenants' documents by design.
	resp, err = globexIdx.Search("hardware", &meilisearch.SearchRequest{Limit: 10})
	if err != nil {
		t.Fatalf("Search 'hardware' in globex_products: %v", err)
	}
	if resp.EstimatedTotalHits != 0 {
		t.Errorf("globex_products search for 'hardware' (acme category) returned %d hits, want 0 (cross-tenant leak!)",
			resp.EstimatedTotalHits)
	}

	// Positive controls: each index must find its own content.
	resp, err = acmeIdx.Search("Acme", &meilisearch.SearchRequest{Limit: 5})
	if err != nil {
		t.Fatalf("Search 'Acme' in acme_products (positive control): %v", err)
	}
	if resp.EstimatedTotalHits == 0 {
		t.Error("acme_products: positive control 'Acme' returned 0 hits")
	}

	resp, err = globexIdx.Search("Globex", &meilisearch.SearchRequest{Limit: 5})
	if err != nil {
		t.Fatalf("Search 'Globex' in globex_products (positive control): %v", err)
	}
	if resp.EstimatedTotalHits == 0 {
		t.Error("globex_products: positive control 'Globex' returned 0 hits")
	}

	t.Log("VALIDATED: prefix-based index isolation is complete — acme_products " +
		"returns zero globex results and vice versa; positive controls confirm each " +
		"index serves only its own tenant's data")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 5: Filterable Attributes and Faceted Search
// ────────────────────────────────────────────────────────────────────────────

// TestFilterableAttributes configures tenant_id, status, doctype, and category
// as filterable on acme_products, then validates filter queries and facet
// distribution. This matches the MOCA pattern where MetaType fields are
// declared filterable/searchable.
func TestFilterableAttributes(t *testing.T) {
	acmeIdx := meiliClient.Index(acmeProductsIndex)

	// Configure filterable attributes.
	taskInfo, err := acmeIdx.UpdateSettings(&meilisearch.Settings{
		FilterableAttributes: []string{"tenant_id", "status", "doctype", "category"},
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if err := waitForTask(meiliClient, taskInfo.TaskUID); err != nil {
		t.Fatalf("waitForTask (UpdateSettings): %v", err)
	}

	// Filter by status = published.
	resp, err := acmeIdx.Search("", &meilisearch.SearchRequest{
		Filter: "status = published",
		Limit:  5,
	})
	if err != nil {
		t.Fatalf("Search with filter status=published: %v", err)
	}
	if resp.EstimatedTotalHits == 0 {
		t.Error("filter status=published: expected hits, got 0")
	}
	// Verify all returned hits have status=published.
	for i, hit := range resp.Hits {
		hitBytes, err := json.Marshal(hit)
		if err != nil {
			continue
		}
		var p Product
		if err := json.Unmarshal(hitBytes, &p); err != nil {
			continue
		}
		if p.Status != "published" {
			t.Errorf("hit[%d] status = %q, want %q (filter not enforced)", i, p.Status, "published")
		}
	}

	// Compound filter: status = published AND doctype = product.
	resp, err = acmeIdx.Search("", &meilisearch.SearchRequest{
		Filter: "status = published AND doctype = product",
		Limit:  5,
	})
	if err != nil {
		t.Fatalf("Search with compound filter: %v", err)
	}
	if resp.EstimatedTotalHits == 0 {
		t.Error("compound filter: expected hits, got 0")
	}
	t.Logf("compound filter status=published AND doctype=product: %d hits (estimated)",
		resp.EstimatedTotalHits)

	// Filter for non-existent status: should return 0 results.
	resp, err = acmeIdx.Search("", &meilisearch.SearchRequest{
		Filter: "status = archived",
		Limit:  5,
	})
	if err != nil {
		t.Fatalf("Search with filter status=archived: %v", err)
	}
	if resp.EstimatedTotalHits != 0 {
		t.Errorf("filter status=archived: expected 0 hits, got %d", resp.EstimatedTotalHits)
	}

	// Request facet distribution for status, doctype, category.
	resp, err = acmeIdx.Search("", &meilisearch.SearchRequest{
		Facets: []string{"status", "doctype", "category"},
		Limit:  0,
	})
	if err != nil {
		t.Fatalf("Search with facets: %v", err)
	}
	if resp.FacetDistribution == nil {
		t.Error("FacetDistribution is nil: facets not returned")
	} else {
		t.Logf("FacetDistribution: %s", string(resp.FacetDistribution))
		// Verify the distribution contains the expected facet keys.
		var facets map[string]map[string]int64
		if err := json.Unmarshal(resp.FacetDistribution, &facets); err == nil {
			for _, key := range []string{"status", "doctype", "category"} {
				if _, ok := facets[key]; !ok {
					t.Errorf("FacetDistribution missing key %q", key)
				}
			}
			// Verify "published" appears in status facet.
			if statusFacet, ok := facets["status"]; ok {
				if statusFacet["published"] == 0 {
					t.Error("FacetDistribution status.published count = 0, expected > 0")
				}
				t.Logf("status facet: published=%d", statusFacet["published"])
			}
		}
	}

	t.Log("VALIDATED: filterable attributes (tenant_id, status, doctype, category) " +
		"configured; filter queries and facet distribution work as expected")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 6: Multi-Search Across Tenant Indexes
// ────────────────────────────────────────────────────────────────────────────

// TestMultiSearch validates the MultiSearch API for cross-tenant admin
// scenarios (e.g., a super-admin searching across multiple sites in one
// request). Each result set is scoped to its own index.
func TestMultiSearch(t *testing.T) {
	resp, err := meiliClient.MultiSearch(&meilisearch.MultiSearchRequest{
		Queries: []*meilisearch.SearchRequest{
			{
				IndexUID: acmeProductsIndex,
				Query:    "Acme",
				Limit:    5,
			},
			{
				IndexUID: globexProductsIdx,
				Query:    "Globex",
				Limit:    5,
			},
		},
	})
	if err != nil {
		t.Fatalf("MultiSearch: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Fatalf("MultiSearch returned %d result sets, want 2", len(resp.Results))
	}

	acmeResult := resp.Results[0]
	globexResult := resp.Results[1]

	// Verify each result set is attributed to the correct index.
	if acmeResult.IndexUID != acmeProductsIndex {
		t.Errorf("result[0].IndexUID = %q, want %q", acmeResult.IndexUID, acmeProductsIndex)
	}
	if globexResult.IndexUID != globexProductsIdx {
		t.Errorf("result[1].IndexUID = %q, want %q", globexResult.IndexUID, globexProductsIdx)
	}

	// Verify each result set has hits.
	if acmeResult.EstimatedTotalHits == 0 {
		t.Error("MultiSearch acme result: 0 hits, expected > 0")
	}
	if globexResult.EstimatedTotalHits == 0 {
		t.Error("MultiSearch globex result: 0 hits, expected > 0")
	}

	// Verify result isolation: acme results should not contain globex terms.
	for i, hit := range acmeResult.Hits {
		hitBytes, err := json.Marshal(hit)
		if err != nil {
			continue
		}
		var p Product
		if err := json.Unmarshal(hitBytes, &p); err != nil {
			continue
		}
		if p.TenantID != tenantAcme {
			t.Errorf("MultiSearch acme hit[%d]: TenantID = %q, want %q", i, p.TenantID, tenantAcme)
		}
	}
	for i, hit := range globexResult.Hits {
		hitBytes, err := json.Marshal(hit)
		if err != nil {
			continue
		}
		var p Product
		if err := json.Unmarshal(hitBytes, &p); err != nil {
			continue
		}
		if p.TenantID != tenantGlobex {
			t.Errorf("MultiSearch globex hit[%d]: TenantID = %q, want %q", i, p.TenantID, tenantGlobex)
		}
	}

	t.Logf("VALIDATED: MultiSearch returned %d acme hits and %d globex hits in a single "+
		"API call; result sets are correctly scoped to their respective indexes",
		acmeResult.EstimatedTotalHits, globexResult.EstimatedTotalHits)
}

// ────────────────────────────────────────────────────────────────────────────
// Test 7: Tenant Token Alternative (Single-Index + JWT Isolation)
// ────────────────────────────────────────────────────────────────────────────

// TestTenantToken validates the Meilisearch-recommended tenant-token approach
// as an alternative to the prefix-based index-per-tenant pattern.
// In this pattern, all tenants share a single index ("products") with a
// tenant_id field. JWT tokens embed filter rules that the server enforces,
// ensuring tenant A cannot read tenant B's documents.
//
// This test compares both approaches and documents trade-offs for the ADR.
func TestTenantToken(t *testing.T) {
	// Step 1: Create a shared "products" index with tenant_id as filterable.
	taskInfo, err := meiliClient.CreateIndex(&meilisearch.IndexConfig{
		Uid:        sharedProductsIdx,
		PrimaryKey: "id",
	})
	if err != nil {
		t.Fatalf("CreateIndex shared products: %v", err)
	}
	if err := waitForTask(meiliClient, taskInfo.TaskUID); err != nil {
		t.Fatalf("waitForTask (create shared products): %v", err)
	}

	sharedIdx := meiliClient.Index(sharedProductsIdx)

	// Configure tenant_id as filterable (required for tenant token filtering).
	settingsTask, err := sharedIdx.UpdateSettings(&meilisearch.Settings{
		FilterableAttributes: []string{"tenant_id", "status", "doctype"},
	})
	if err != nil {
		t.Fatalf("UpdateSettings shared products: %v", err)
	}
	if err := waitForTask(meiliClient, settingsTask.TaskUID); err != nil {
		t.Fatalf("waitForTask (settings shared products): %v", err)
	}

	// Step 2: Index both tenants' documents into the shared index.
	// Use different ID prefixes to avoid collision (already distinct in generateProducts).
	allDocs := append(generateProducts(tenantAcme, 50), generateProducts(tenantGlobex, 50)...)
	indexTasks, err := sharedIdx.AddDocumentsInBatches(allDocs, 50, nil)
	if err != nil {
		t.Fatalf("AddDocumentsInBatches shared: %v", err)
	}
	for i, taskInfo := range indexTasks {
		if err := waitForTask(meiliClient, taskInfo.TaskUID); err != nil {
			t.Fatalf("waitForTask (index shared batch %d): %v", i, err)
		}
	}

	// Verify the shared index has all 100 documents.
	stats, err := sharedIdx.GetStats()
	if err != nil {
		t.Fatalf("GetStats shared products: %v", err)
	}
	if stats.NumberOfDocuments != 100 {
		t.Errorf("shared products: doc count = %d, want 100", stats.NumberOfDocuments)
	}

	// Step 3: Get the default search API key for token generation.
	// Meilisearch auto-creates default keys at startup with a master key set.
	keysResult, err := meiliClient.KeyReader().GetKeys(&meilisearch.KeysQuery{Limit: 20})
	if err != nil {
		t.Fatalf("GetKeys: %v", err)
	}

	var searchAPIKeyUID, searchAPIKeyValue string
	for _, k := range keysResult.Results {
		for _, action := range k.Actions {
			if action == "search" || action == "*" {
				searchAPIKeyUID = k.UID
				searchAPIKeyValue = k.Key
				break
			}
		}
		if searchAPIKeyUID != "" {
			break
		}
	}
	if searchAPIKeyUID == "" {
		t.Skip("no search API key found — skipping tenant token test")
	}

	// Step 4: Generate a tenant token for acme.
	// The search rules restrict all queries to documents where tenant_id = "acme".
	acmeSearchRules := map[string]interface{}{
		sharedProductsIdx: map[string]interface{}{
			"filter": "tenant_id = acme",
		},
	}
	acmeToken, err := meiliClient.GenerateTenantToken(
		searchAPIKeyUID,
		acmeSearchRules,
		&meilisearch.TenantTokenOptions{
			APIKey:    searchAPIKeyValue,
			ExpiresAt: time.Now().Add(1 * time.Hour),
		},
	)
	if err != nil {
		t.Fatalf("GenerateTenantToken for acme: %v", err)
	}

	// Step 5: Create a client scoped to the acme tenant token.
	acmeScopedClient := meilisearch.New(
		os.Getenv("MEILI_URL"),
		meilisearch.WithAPIKey(acmeToken),
	)
	if url := os.Getenv("MEILI_URL"); url == "" {
		acmeScopedClient = meilisearch.New(defaultMeiliURL, meilisearch.WithAPIKey(acmeToken))
	}

	// Step 6: Search with the acme-scoped client — must only return acme documents.
	acmeResp, err := acmeScopedClient.Index(sharedProductsIdx).Search("", &meilisearch.SearchRequest{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search with acme token: %v", err)
	}
	if acmeResp.EstimatedTotalHits == 0 {
		t.Error("acme token search: 0 hits, expected acme documents")
	}
	// Verify all returned hits belong to acme.
	for i, hit := range acmeResp.Hits {
		hitBytes, err := json.Marshal(hit)
		if err != nil {
			continue
		}
		var p Product
		if err := json.Unmarshal(hitBytes, &p); err != nil {
			continue
		}
		if p.TenantID != tenantAcme {
			t.Errorf("acme token hit[%d]: TenantID = %q, want %q (token isolation failure!)",
				i, p.TenantID, tenantAcme)
		}
	}

	// Verify globex terms are not accessible with the acme token.
	acmeGlobexResp, err := acmeScopedClient.Index(sharedProductsIdx).Search("Widget", &meilisearch.SearchRequest{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search 'Widget' with acme token: %v", err)
	}
	if acmeGlobexResp.EstimatedTotalHits != 0 {
		t.Errorf("acme token search for 'Widget' (globex-only term) returned %d hits, want 0 (token isolation failure!)",
			acmeGlobexResp.EstimatedTotalHits)
	}

	// Step 7: Generate a globex token and verify the inverse.
	globexSearchRules := map[string]interface{}{
		sharedProductsIdx: map[string]interface{}{
			"filter": "tenant_id = globex",
		},
	}
	globexToken, err := meiliClient.GenerateTenantToken(
		searchAPIKeyUID,
		globexSearchRules,
		&meilisearch.TenantTokenOptions{
			APIKey:    searchAPIKeyValue,
			ExpiresAt: time.Now().Add(1 * time.Hour),
		},
	)
	if err != nil {
		t.Fatalf("GenerateTenantToken for globex: %v", err)
	}

	globexScopedClient := meilisearch.New(defaultMeiliURL, meilisearch.WithAPIKey(globexToken))

	globexResp, err := globexScopedClient.Index(sharedProductsIdx).Search("Widget", &meilisearch.SearchRequest{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search 'Widget' with globex token: %v", err)
	}
	if globexResp.EstimatedTotalHits == 0 {
		t.Error("globex token search for 'Widget': 0 hits, expected globex documents")
	}

	// Verify acme terms invisible to globex token.
	globexAcmeResp, err := globexScopedClient.Index(sharedProductsIdx).Search("Acme", &meilisearch.SearchRequest{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search 'Acme' with globex token: %v", err)
	}
	if globexAcmeResp.EstimatedTotalHits != 0 {
		t.Errorf("globex token search for 'Acme' (acme-only term) returned %d hits, want 0 (token isolation failure!)",
			globexAcmeResp.EstimatedTotalHits)
	}

	t.Logf("VALIDATED: tenant token isolation — acme token sees %d docs (0 globex); "+
		"globex token sees %d docs (0 acme). Single-index + JWT filtering works.",
		acmeResp.EstimatedTotalHits, globexResp.EstimatedTotalHits)
	t.Log("TRADE-OFFS documented in ADR-006:")
	t.Log("  Index-per-tenant: simpler mental model, aligns with schema-per-tenant, " +
		"easier cross-tenant admin queries via MultiSearch")
	t.Log("  Tenant-token (single index): fewer indexes at scale (1 vs N per doctype), " +
		"requires API key management and server-enforced JWT filtering")
	t.Log("  RECOMMENDATION: index-per-tenant as default; tenant-token for 10,000+ tenants " +
		"where index count becomes a concern")
}
