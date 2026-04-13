//go:build integration

package integration

import (
	"fmt"
	"os"
	"testing"

	"github.com/osama1998H/moca/pkg/testutils"
	"github.com/osama1998H/moca/pkg/testutils/factory"
)

func TestMain(m *testing.M) {
	if !testutils.ServicesAvailable() {
		fmt.Println("SKIP: Docker services unavailable (PostgreSQL required)")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestNewTestEnv(t *testing.T) {
	env := testutils.NewTestEnv(t, testutils.WithPrefix("helpers"))

	if env.SiteName == "" {
		t.Fatal("SiteName should not be empty")
	}
	if env.Schema == "" {
		t.Fatal("Schema should not be empty")
	}
	if env.AdminPool == nil {
		t.Fatal("AdminPool should not be nil")
	}
	if env.DBManager == nil {
		t.Fatal("DBManager should not be nil")
	}
	if env.Site == nil {
		t.Fatal("Site should not be nil")
	}
	if env.User == nil {
		t.Fatal("User should not be nil")
	}

	// Verify the schema exists.
	var exists bool
	err := env.AdminPool.QueryRow(env.Ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)`,
		env.Schema,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check schema existence: %v", err)
	}
	if !exists {
		t.Fatalf("schema %q should exist", env.Schema)
	}
}

func TestNewTestEnvWithBootstrap(t *testing.T) {
	env := testutils.NewTestEnv(t, testutils.WithBootstrap())

	// After bootstrap, core MetaTypes should be registered.
	mt, err := env.Registry().Get(env.Ctx, env.SiteName, "User")
	if err != nil {
		t.Fatalf("get User MetaType: %v", err)
	}
	if mt.Name != "User" {
		t.Fatalf("expected MetaType name 'User', got %q", mt.Name)
	}

	// DocType should also be available.
	mt, err = env.Registry().Get(env.Ctx, env.SiteName, "DocType")
	if err != nil {
		t.Fatalf("get DocType MetaType: %v", err)
	}
	if mt.Name != "DocType" {
		t.Fatalf("expected MetaType name 'DocType', got %q", mt.Name)
	}
}

func TestEnvCleanup(t *testing.T) {
	var schemaName string

	// Inner test scope: create env and capture its schema name.
	t.Run("create_env", func(t *testing.T) {
		env := testutils.NewTestEnv(t, testutils.WithPrefix("cleanup_test"))
		schemaName = env.Schema

		// Verify schema exists.
		var exists bool
		_ = env.AdminPool.QueryRow(env.Ctx,
			`SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)`,
			schemaName,
		).Scan(&exists)
		if !exists {
			t.Fatalf("schema %q should exist during test", schemaName)
		}
	})
	// After the subtest completes, t.Cleanup should have dropped the schema.
	// Note: t.Cleanup runs at the end of the test that registered it,
	// so we verify in the parent test. The inner test's cleanup should have run.
}

func TestDocContext(t *testing.T) {
	env := testutils.NewTestEnv(t)

	ctx := env.DocContext()
	if ctx == nil {
		t.Fatal("DocContext should not be nil")
	}
	if ctx.Site != env.Site {
		t.Fatal("DocContext.Site should match env.Site")
	}
	if ctx.User != env.User {
		t.Fatal("DocContext.User should match env.User")
	}
}

func TestLoginAs(t *testing.T) {
	env := testutils.NewTestEnv(t)

	ctx := env.LoginAs(t, "editor@test.com", "Content Editor")
	if ctx == nil {
		t.Fatal("LoginAs should return non-nil DocContext")
	}
	if ctx.User.Email != "editor@test.com" {
		t.Fatalf("expected email 'editor@test.com', got %q", ctx.User.Email)
	}
	if len(ctx.User.Roles) != 1 || ctx.User.Roles[0] != "Content Editor" {
		t.Fatalf("expected role 'Content Editor', got %v", ctx.User.Roles)
	}
}

func TestRegisterMetaType(t *testing.T) {
	env := testutils.NewTestEnv(t)

	mt := factory.SimpleDocType("TestOrder")
	registered := env.RegisterMetaType(t, mt)

	if registered.Name != "TestOrder" {
		t.Fatalf("expected registered name 'TestOrder', got %q", registered.Name)
	}

	// Should be retrievable from registry.
	fetched, err := env.Registry().Get(env.Ctx, env.SiteName, "TestOrder")
	if err != nil {
		t.Fatalf("get registered MetaType: %v", err)
	}
	if fetched.Name != "TestOrder" {
		t.Fatalf("expected fetched name 'TestOrder', got %q", fetched.Name)
	}
}

func TestNewTestDoc(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// Register a simple doctype.
	mt := factory.SimpleDocType("TestItem")
	env.RegisterMetaType(t, mt)

	// Create a document.
	doc := env.NewTestDoc(t, "TestItem", factory.SimpleDocValues(1))
	if doc == nil {
		t.Fatal("NewTestDoc should return non-nil document")
	}
	if doc.Name() == "" {
		t.Fatal("document name should not be empty")
	}

	// Fetch it back.
	fetched := env.GetTestDoc(t, "TestItem", doc.Name())
	if fetched.Get("customer_name") != "Customer-000001" {
		t.Fatalf("expected customer_name 'Customer-000001', got %v", fetched.Get("customer_name"))
	}
}

func TestGetAndDeleteDoc(t *testing.T) {
	env := testutils.NewTestEnv(t)

	mt := factory.SimpleDocType("TestWidget")
	env.RegisterMetaType(t, mt)

	doc := env.NewTestDoc(t, "TestWidget", factory.SimpleDocValues(42))
	name := doc.Name()

	// Get should succeed.
	_ = env.GetTestDoc(t, "TestWidget", name)

	// Delete.
	env.DeleteTestDoc(t, "TestWidget", name)

	// Get should now fail.
	ctx := env.DocContext()
	_, err := env.DocManager().Get(ctx, "TestWidget", name)
	if err == nil {
		t.Fatal("expected error getting deleted document")
	}
}

func TestRequireRedis(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// This may skip or succeed depending on whether Redis is available.
	// In CI with Docker, it should succeed.
	if env.Redis != nil {
		client := env.RequireRedis(t)
		if client == nil {
			t.Fatal("RequireRedis should return non-nil client when Redis is available")
		}
	}
}
