//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/testutils"
	"github.com/osama1998H/moca/pkg/testutils/factory"
)

func TestCrossTenantIsolation(t *testing.T) {
	// Create two independent test environments (separate schemas).
	envA := testutils.NewTestEnv(t, testutils.WithPrefix("tenant_a"))
	envB := testutils.NewTestEnv(t, testutils.WithPrefix("tenant_b"))

	// Register same doctype in both tenants.
	mtA := factory.SimpleDocType("TenantDoc")
	envA.RegisterMetaType(t, mtA)
	mtB := factory.SimpleDocType("TenantDoc")
	envB.RegisterMetaType(t, mtB)

	// Create a document in tenant A.
	docA := envA.NewTestDoc(t, "TenantDoc", map[string]any{
		"customer_name": "Tenant A Customer",
		"status":        "Open",
	})

	// Create a document in tenant B.
	docB := envB.NewTestDoc(t, "TenantDoc", map[string]any{
		"customer_name": "Tenant B Customer",
		"status":        "Closed",
	})

	// Tenant A should only see its own docs.
	ctxA := envA.DocContext()
	docsA, totalA, err := envA.DocManager().GetList(ctxA, "TenantDoc", factory.EmptyListOptions())
	if err != nil {
		t.Fatalf("list tenant A: %v", err)
	}
	if totalA != 1 {
		t.Fatalf("tenant A should have 1 doc, got %d", totalA)
	}
	if docsA[0].Get("customer_name") != "Tenant A Customer" {
		t.Fatalf("tenant A doc should be 'Tenant A Customer', got %v", docsA[0].Get("customer_name"))
	}

	// Tenant B should only see its own docs.
	ctxB := envB.DocContext()
	docsB, totalB, err := envB.DocManager().GetList(ctxB, "TenantDoc", factory.EmptyListOptions())
	if err != nil {
		t.Fatalf("list tenant B: %v", err)
	}
	if totalB != 1 {
		t.Fatalf("tenant B should have 1 doc, got %d", totalB)
	}
	if docsB[0].Get("customer_name") != "Tenant B Customer" {
		t.Fatalf("tenant B doc should be 'Tenant B Customer', got %v", docsB[0].Get("customer_name"))
	}

	// Cross-tenant access should fail.
	_, err = envA.DocManager().Get(ctxA, "TenantDoc", docB.Name())
	if err == nil {
		t.Fatal("tenant A should not be able to access tenant B's document")
	}

	_, err = envB.DocManager().Get(ctxB, "TenantDoc", docA.Name())
	if err == nil {
		t.Fatal("tenant B should not be able to access tenant A's document")
	}
}

func TestMultipleTenantsIndependentSchemas(t *testing.T) {
	env1 := testutils.NewTestEnv(t, testutils.WithPrefix("schema_1"))
	env2 := testutils.NewTestEnv(t, testutils.WithPrefix("schema_2"))

	// Verify they have different schemas.
	if env1.Schema == env2.Schema {
		t.Fatalf("schemas should be different: %q vs %q", env1.Schema, env2.Schema)
	}
	if env1.SiteName == env2.SiteName {
		t.Fatalf("site names should be different: %q vs %q", env1.SiteName, env2.SiteName)
	}
}
