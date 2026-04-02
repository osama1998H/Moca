package auth

import (
	"errors"
	"fmt"
)

// ErrUserNotFound is returned when a user cannot be found by email or is disabled.
var ErrUserNotFound = errors.New("auth: user not found")

// ErrSessionNotFound is returned when a session ID does not exist or has expired.
var ErrSessionNotFound = errors.New("auth: session not found")

// PermDeniedError is returned when a user lacks a required permission on a doctype.
// It mirrors api.PermissionDeniedError but lives in pkg/auth to avoid a circular
// import between pkg/auth and pkg/api.
type PermDeniedError struct {
	User    string
	Doctype string
	Perm    string
}

func (e *PermDeniedError) Error() string {
	return fmt.Sprintf("user %q lacks %q permission on %s", e.User, e.Perm, e.Doctype)
}
