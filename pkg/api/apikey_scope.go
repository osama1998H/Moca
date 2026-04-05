package api

import (
	"context"
	"strings"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
)

// ScopeEnforcer wraps a PermissionChecker and additionally checks API key scopes.
// When the request was authenticated via API key (scopes present in context),
// the enforcer verifies the key's scopes permit the requested doctype+operation
// before delegating to the inner role-based permission checker.
//
// When no API key scopes are in context (JWT/session auth), ScopeEnforcer is a
// transparent pass-through to the inner checker.
type ScopeEnforcer struct {
	inner PermissionChecker
}

// NewScopeEnforcer creates a ScopeEnforcer wrapping the given permission checker.
func NewScopeEnforcer(inner PermissionChecker) *ScopeEnforcer {
	return &ScopeEnforcer{inner: inner}
}

// CheckDocPerm first checks API key scopes (if present), then delegates to the
// inner role-based permission checker.
func (s *ScopeEnforcer) CheckDocPerm(ctx context.Context, user *auth.User, doctype string, perm string) error {
	scopes := APIScopesFromContext(ctx)
	if len(scopes) > 0 {
		if !scopeAllows(scopes, doctype, perm) {
			return &PermissionDeniedError{
				User:    user.Email,
				Doctype: doctype,
				Perm:    "scope:" + perm,
			}
		}
	}
	return s.inner.CheckDocPerm(ctx, user, doctype, perm)
}

// scopeAllows checks whether any of the given scopes permit the requested
// doctype and operation. A scope matches if:
//   - DocTypes is empty (wildcard) or contains the doctype
//   - Operations is empty (wildcard) or contains the operation
//
// Any matching scope is sufficient for access.
func scopeAllows(scopes []meta.APIScopePerm, doctype, operation string) bool {
	for _, scope := range scopes {
		if matchesDocType(scope.DocTypes, doctype) && matchesOperation(scope.Operations, operation) {
			return true
		}
	}
	return false
}

// matchesDocType returns true if the doctype list is empty (wildcard) or
// contains the given doctype (case-insensitive).
func matchesDocType(allowed []string, doctype string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, dt := range allowed {
		if strings.EqualFold(dt, doctype) {
			return true
		}
	}
	return false
}

// matchesOperation returns true if the operations list is empty (wildcard) or
// contains the given operation (case-insensitive).
func matchesOperation(allowed []string, operation string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, op := range allowed {
		if strings.EqualFold(op, operation) {
			return true
		}
	}
	return false
}
