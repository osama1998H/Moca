package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/moca-framework/moca/pkg/auth"
)

// Authenticator resolves an *auth.User from an incoming HTTP request.
// Concrete implementations (JWT, session, API key) are provided by MS-14.
type Authenticator interface {
	Authenticate(r *http.Request) (*auth.User, error)
}

// PermissionChecker verifies that a user holds a specific permission on a doctype.
// perm is one of "read", "write", "create", "delete", "submit", "cancel", "amend".
// Return nil to allow, or a *PermissionDeniedError to deny.
type PermissionChecker interface {
	CheckDocPerm(ctx context.Context, user *auth.User, doctype string, perm string) error
}

// PermissionDeniedError is returned when a user lacks a required permission.
type PermissionDeniedError struct {
	User    string
	Doctype string
	Perm    string
}

func (e *PermissionDeniedError) Error() string {
	return fmt.Sprintf("user %q lacks %q permission on %s", e.User, e.Perm, e.Doctype)
}

// NoopAuthenticator always returns a guest user. Used as a placeholder
// until real authentication is implemented in MS-14.
type NoopAuthenticator struct{}

// Authenticate returns a guest user with the "Guest" role.
func (NoopAuthenticator) Authenticate(_ *http.Request) (*auth.User, error) {
	return &auth.User{
		Email:    "Guest",
		FullName: "Guest",
		Roles:    []string{"Guest"},
	}, nil
}

// AllowAllPermissionChecker permits every operation. Used as a placeholder
// until real permissions are implemented in MS-14.
type AllowAllPermissionChecker struct{}

// CheckDocPerm always returns nil (allowed).
func (AllowAllPermissionChecker) CheckDocPerm(_ context.Context, _ *auth.User, _ string, _ string) error {
	return nil
}
