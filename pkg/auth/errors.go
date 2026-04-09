package auth

import (
	"errors"
	"fmt"
)

// ErrUserNotFound is returned when a user cannot be found by email or is disabled.
var ErrUserNotFound = errors.New("auth: user not found")

// ErrSessionNotFound is returned when a session ID does not exist or has expired.
var ErrSessionNotFound = errors.New("auth: session not found")

// ErrUserDisabled is returned when a user exists but is disabled.
var ErrUserDisabled = errors.New("auth: user is disabled")

// ErrSSOStateInvalid is returned when an SSO CSRF state token is missing,
// expired, or already consumed.
var ErrSSOStateInvalid = errors.New("auth: invalid or expired SSO state")

// ErrSSOProviderNotFound is returned when a named SSO provider does not exist
// or is disabled.
var ErrSSOProviderNotFound = errors.New("auth: SSO provider not found or disabled")

// ErrSSOEmailMissing is returned when the identity provider's response does not
// contain the expected email claim/attribute.
var ErrSSOEmailMissing = errors.New("auth: SSO response missing email claim")

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
