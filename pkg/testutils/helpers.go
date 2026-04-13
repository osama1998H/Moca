package testutils

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
)

// CreateTestUser inserts a User document into the test site and returns an
// auth.User ready for use in DocContext. Requires WithBootstrap to have been
// used when creating the TestEnv (the User MetaType must be registered).
func (e *TestEnv) CreateTestUser(t testing.TB, email, fullName string, roles ...string) *auth.User {
	t.Helper()

	if len(roles) == 0 {
		roles = []string{"System Manager"}
	}

	// Build HasRole child rows for the User document.
	roleRows := make([]any, 0, len(roles))
	for _, role := range roles {
		roleRows = append(roleRows, map[string]any{
			"role": role,
		})
	}

	values := map[string]any{
		"email":     email,
		"full_name": fullName,
		"enabled":   true,
		"user_type": "System",
		"password":  "TestPass123!",
		"roles":     roleRows,
	}

	ctx := document.NewDocContext(context.Background(), e.Site, e.User)
	_, err := e.DocManager().Insert(ctx, "User", values)
	if err != nil {
		t.Fatalf("create test user %q: %v", email, err)
	}

	return &auth.User{
		Email:    email,
		FullName: fullName,
		Roles:    roles,
	}
}

// LoginAs returns a DocContext authenticated as the user with the given email.
// The user does not need to exist in the database — this creates an in-memory
// auth.User with the specified roles for use in permission-sensitive tests.
func (e *TestEnv) LoginAs(t testing.TB, email string, roles ...string) *document.DocContext {
	t.Helper()

	if len(roles) == 0 {
		roles = []string{"System Manager"}
	}

	user := &auth.User{
		Email:    email,
		FullName: email,
		Roles:    roles,
	}

	return document.NewDocContext(context.Background(), e.Site, user)
}

// NewTestDoc creates and inserts a document of the given doctype using the
// DocManager. Returns the created DynamicDoc. Fails the test on error.
func (e *TestEnv) NewTestDoc(t testing.TB, doctype string, values map[string]any) *document.DynamicDoc {
	t.Helper()

	ctx := e.DocContext()
	doc, err := e.DocManager().Insert(ctx, doctype, values)
	if err != nil {
		t.Fatalf("create test doc %q: %v", doctype, err)
	}
	return doc
}

// GetTestDoc retrieves a document by doctype and name. Fails the test on error.
func (e *TestEnv) GetTestDoc(t testing.TB, doctype, name string) *document.DynamicDoc {
	t.Helper()

	ctx := e.DocContext()
	doc, err := e.DocManager().Get(ctx, doctype, name)
	if err != nil {
		t.Fatalf("get test doc %s %q: %v", doctype, name, err)
	}
	return doc
}

// DeleteTestDoc deletes a document by doctype and name. Fails the test on error.
func (e *TestEnv) DeleteTestDoc(t testing.TB, doctype, name string) {
	t.Helper()

	ctx := e.DocContext()
	if err := e.DocManager().Delete(ctx, doctype, name); err != nil {
		t.Fatalf("delete test doc %s %q: %v", doctype, name, err)
	}
}
