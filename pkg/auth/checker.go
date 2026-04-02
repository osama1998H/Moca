package auth

import (
	"context"
	"fmt"
	"log/slog"
)

// RoleBasedPermChecker implements the api.PermissionChecker interface using
// CachedPermissionResolver. It resolves effective permissions for the user's
// roles on the requested doctype and checks the required permission bitmask.
//
// Users with the "Administrator" role bypass all permission checks.
type RoleBasedPermChecker struct {
	resolver *CachedPermissionResolver
	siteFunc SiteExtractor
	logger   *slog.Logger
}

// NewRoleBasedPermChecker creates a permission checker backed by the given resolver.
// siteFunc extracts the SiteContext from the request context (pass api.SiteFromContext).
func NewRoleBasedPermChecker(
	resolver *CachedPermissionResolver,
	siteFunc SiteExtractor,
	logger *slog.Logger,
) *RoleBasedPermChecker {
	if logger == nil {
		logger = slog.Default()
	}
	return &RoleBasedPermChecker{
		resolver: resolver,
		siteFunc: siteFunc,
		logger:   logger,
	}
}

// CheckDocPerm verifies that user holds the named permission on doctype.
// Returns nil if allowed, or *PermDeniedError if denied.
func (c *RoleBasedPermChecker) CheckDocPerm(ctx context.Context, user *User, doctype string, perm string) error {
	if IsAdministrator(user) {
		return nil
	}

	site := c.siteFunc(ctx)
	if site == nil {
		return fmt.Errorf("permission check: no site in context")
	}

	ep, err := c.resolver.Resolve(ctx, site.Name, user, doctype)
	if err != nil {
		return fmt.Errorf("permission check: %w", err)
	}

	if !ep.HasPerm(perm) {
		return &PermDeniedError{User: user.Email, Doctype: doctype, Perm: perm}
	}
	return nil
}

// IsAdministrator returns true if the user holds the "Administrator" role.
func IsAdministrator(user *User) bool {
	for _, r := range user.Roles {
		if r == "Administrator" {
			return true
		}
	}
	return false
}
