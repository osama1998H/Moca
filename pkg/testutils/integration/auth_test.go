//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/testutils"
)

func TestCreateTestUser(t *testing.T) {
	env := testutils.NewTestEnv(t, testutils.WithBootstrap())

	user := env.CreateTestUser(t, "editor@test.com", "Test Editor", "Content Editor")
	if user.Email != "editor@test.com" {
		t.Fatalf("expected email 'editor@test.com', got %q", user.Email)
	}
	if user.FullName != "Test Editor" {
		t.Fatalf("expected full name 'Test Editor', got %q", user.FullName)
	}
	if len(user.Roles) != 1 || user.Roles[0] != "Content Editor" {
		t.Fatalf("expected role 'Content Editor', got %v", user.Roles)
	}

	// The user document should exist in the database.
	doc := env.GetTestDoc(t, "User", "editor@test.com")
	if doc.Get("full_name") != "Test Editor" {
		t.Fatalf("expected stored full_name 'Test Editor', got %v", doc.Get("full_name"))
	}
}

func TestCreateMultipleTestUsers(t *testing.T) {
	env := testutils.NewTestEnv(t, testutils.WithBootstrap())

	user1 := env.CreateTestUser(t, "user1@test.com", "User One", "System Manager")
	user2 := env.CreateTestUser(t, "user2@test.com", "User Two", "Content Editor")

	if user1.Email == user2.Email {
		t.Fatal("users should have different emails")
	}
	if user1.Roles[0] == user2.Roles[0] {
		t.Fatal("users should have different roles")
	}
}

func TestLoginAsWithRoles(t *testing.T) {
	env := testutils.NewTestEnv(t)

	ctx := env.LoginAs(t, "multi@test.com", "System Manager", "Content Editor")
	if len(ctx.User.Roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(ctx.User.Roles))
	}
}
