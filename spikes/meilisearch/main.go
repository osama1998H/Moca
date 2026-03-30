// Package main implements a spike validating Meilisearch prefix-based
// tenant isolation, bulk indexing, and typo-tolerant search for the
// MOCA framework.
//
// Spike: MS-00-T5
// Design ref: MOCA_SYSTEM_DESIGN.md §8.2 (line 1431), ADR-006 (lines 2093-2097)
//
// Key architectural bet being validated:
//
//	Meilisearch with prefix-based index naming ({site_name}_{doctype})
//	provides correct tenant isolation for full-text search. Bulk indexing
//	via AddDocumentsInBatches is reliable at 1,000 documents per tenant,
//	and out-of-box typo tolerance is sufficient for MOCA's search workloads.
//
// This is throwaway spike code. Do not promote to pkg/.
package main

import (
	"fmt"
	"strings"
	"time"

	meilisearch "github.com/meilisearch/meilisearch-go"
)

// IndexName returns the Meilisearch index UID for a given tenant and doctype.
// Convention from MOCA_SYSTEM_DESIGN.md §8.2 (line 1431):
//
//	Search isolation: "Meilisearch index prefix: {site_name}_"
//
// The full UID format is "{site_name}_{doctype}", e.g. "acme_product".
func IndexName(tenant, doctype string) string {
	return fmt.Sprintf("%s_%s", tenant, doctype)
}

// Product is the test document type indexed in Meilisearch.
// It represents a MOCA document with the fields most relevant for
// tenant isolation testing: tenant_id (for cross-index verification),
// status and doctype (for filterable attribute testing), name (for
// typo tolerance), and category (for faceted search).
type Product struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Status   string  `json:"status"`
	DocType  string  `json:"doctype"`
	TenantID string  `json:"tenant_id"`
	Price    float64 `json:"price"`
}

// generateProducts creates count Product documents for the given tenant.
// Acme products are named "Acme Product NNN" (category "hardware").
// Globex products are named "Globex Widget NNN" (category "electronics").
// The distinct naming ensures isolation tests can verify that tenant A's
// terms ("Acme", "Product") are absent from tenant B's index.
func generateProducts(tenant string, count int) []Product {
	products := make([]Product, count)
	for i := range count {
		switch tenant {
		case "acme":
			products[i] = Product{
				ID:       fmt.Sprintf("acme-%04d", i+1),
				Name:     fmt.Sprintf("Acme Product %04d", i+1),
				Category: "hardware",
				Status:   "published",
				DocType:  "product",
				TenantID: "acme",
				Price:    float64(10+i) * 1.5,
			}
		case "globex":
			products[i] = Product{
				ID:       fmt.Sprintf("globex-%04d", i+1),
				Name:     fmt.Sprintf("Globex Widget %04d", i+1),
				Category: "electronics",
				Status:   "published",
				DocType:  "product",
				TenantID: "globex",
				Price:    float64(20+i) * 2.5,
			}
		default:
			products[i] = Product{
				ID:       fmt.Sprintf("%s-%04d", tenant, i+1),
				Name:     fmt.Sprintf("%s Item %04d", strings.Title(tenant), i+1), //nolint:staticcheck
				Category: "general",
				Status:   "published",
				DocType:  "product",
				TenantID: tenant,
				Price:    float64(i+1) * 1.0,
			}
		}
	}
	return products
}

// waitForTask polls until a Meilisearch task reaches a terminal state
// (succeeded, failed, or canceled). Returns nil on success, error otherwise.
// Used after every write operation (index creation, document indexing,
// settings update) because Meilisearch operations are asynchronous.
func waitForTask(client meilisearch.ServiceManager, taskUID int64) error {
	task, err := client.TaskManager().WaitForTask(taskUID, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("WaitForTask %d: %w", taskUID, err)
	}
	if task.Status == meilisearch.TaskStatusFailed {
		return fmt.Errorf("task %d failed: %s", taskUID, task.Error.Message)
	}
	return nil
}

// cleanupTestIndexes deletes all indexes whose UID starts with any of the
// given prefixes. Waits for all deletion tasks to complete.
// Called in TestMain before and after the test suite to prevent stale
// indexes from previous runs affecting test results.
func cleanupTestIndexes(client meilisearch.ServiceManager, prefixes []string) error {
	indexes, err := client.ListIndexes(&meilisearch.IndexesQuery{Limit: 1000})
	if err != nil {
		return fmt.Errorf("ListIndexes: %w", err)
	}

	for _, idx := range indexes.Results {
		matched := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(idx.UID, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		taskInfo, err := client.DeleteIndex(idx.UID)
		if err != nil {
			return fmt.Errorf("DeleteIndex %q: %w", idx.UID, err)
		}
		if err := waitForTask(client, taskInfo.TaskUID); err != nil {
			return fmt.Errorf("waitForTask (delete %q): %w", idx.UID, err)
		}
	}
	return nil
}

func main() {
	// The spike is test-driven. Run: go test -v -count=1 -race ./...
	// See main_test.go for all validation scenarios.
	fmt.Println("MS-00-T5: Meilisearch Multi-Index Tenant Isolation Spike")
	fmt.Println("Run: go test -v -count=1 -race ./...")
}
