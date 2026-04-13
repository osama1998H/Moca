//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/testutils"
	"github.com/osama1998H/moca/pkg/testutils/factory"
)

func TestMeilisearchAvailability(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// Search tests require Meilisearch. Skip if unavailable.
	if env.Redis == nil {
		t.Skip("Redis unavailable; search sync depends on Redis")
	}

	// Verify we can register a searchable doctype.
	mt := factory.SimpleDocType("SearchDoc")
	env.RegisterMetaType(t, mt)

	// Create searchable documents.
	for i := 1; i <= 5; i++ {
		env.NewTestDoc(t, "SearchDoc", factory.SimpleDocValues(i))
	}

	// Note: Full Meilisearch integration tests depend on the search sync daemon
	// being active (MS-15). These tests verify the document creation path works
	// with search-enabled MetaTypes.
}
