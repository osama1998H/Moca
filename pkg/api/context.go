package api

import (
	"context"

	"github.com/moca-framework/moca/pkg/auth"
	"github.com/moca-framework/moca/pkg/tenancy"
)

// contextKey is an unexported type for context keys in this package,
// preventing collisions with keys defined in other packages.
type contextKey int

const (
	userKey      contextKey = iota // *auth.User
	siteKey                        // *tenancy.SiteContext
	requestIDKey                   // string
)

// WithUser stores the authenticated user in ctx.
func WithUser(ctx context.Context, user *auth.User) context.Context {
	return context.WithValue(ctx, userKey, user)
}

// UserFromContext retrieves the *auth.User stored by WithUser.
// Returns nil if no user is present.
func UserFromContext(ctx context.Context) *auth.User {
	u, _ := ctx.Value(userKey).(*auth.User)
	return u
}

// WithSite stores the resolved tenant site in ctx.
func WithSite(ctx context.Context, site *tenancy.SiteContext) context.Context {
	return context.WithValue(ctx, siteKey, site)
}

// SiteFromContext retrieves the *tenancy.SiteContext stored by WithSite.
// Returns nil if no site is present.
func SiteFromContext(ctx context.Context) *tenancy.SiteContext {
	s, _ := ctx.Value(siteKey).(*tenancy.SiteContext)
	return s
}

// WithRequestID stores the request ID string in ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext retrieves the request ID stored by WithRequestID.
// Returns an empty string if no request ID is present.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}
