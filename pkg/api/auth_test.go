package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moca-framework/moca/pkg/auth"
)

func TestNoopAuthenticator_ReturnsGuest(t *testing.T) {
	a := NoopAuthenticator{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	user, err := a.Authenticate(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Email != "Guest" {
		t.Errorf("Email = %q, want %q", user.Email, "Guest")
	}
	if len(user.Roles) != 1 || user.Roles[0] != "Guest" {
		t.Errorf("Roles = %v, want [Guest]", user.Roles)
	}
}

func TestAllowAllPermissionChecker_Allows(t *testing.T) {
	pc := AllowAllPermissionChecker{}
	u := &auth.User{Email: "alice@example.com"}

	err := pc.CheckDocPerm(context.Background(), u, "SalesOrder", "create")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestPermissionDeniedError_Message(t *testing.T) {
	e := &PermissionDeniedError{User: "bob@example.com", Doctype: "Invoice", Perm: "delete"}
	want := `user "bob@example.com" lacks "delete" permission on Invoice`
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
}
