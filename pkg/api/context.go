package api

import (
	"context"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// contextKey is an unexported type for context keys in this package,
// preventing collisions with keys defined in other packages.
type contextKey int

const (
	userKey          contextKey = iota // *auth.User
	siteKey                            // *tenancy.SiteContext
	requestIDKey                       // string
	apiVersionKey                      // string ("v1", "v2", ...)
	operationTypeKey                   // OperationType
	apiKeyIDKey                        // string (API key ID)
	apiScopesKey                       // []meta.APIScopePerm
	apiRateLimitKey                    // *meta.RateLimitConfig
	languageKey                        // string (e.g. "ar", "fr", "de")
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

// WithAPIVersion stores the API version string (e.g. "v1") in ctx.
func WithAPIVersion(ctx context.Context, version string) context.Context {
	return context.WithValue(ctx, apiVersionKey, version)
}

// APIVersionFromContext retrieves the API version string stored by WithAPIVersion.
// Returns an empty string if no version is present.
func APIVersionFromContext(ctx context.Context) string {
	v, _ := ctx.Value(apiVersionKey).(string)
	return v
}

// WithOperationType stores the current API operation type in ctx.
func WithOperationType(ctx context.Context, op OperationType) context.Context {
	return context.WithValue(ctx, operationTypeKey, op)
}

// OperationTypeFromContext retrieves the OperationType stored by WithOperationType.
// Returns OpGet as the zero-value default.
func OperationTypeFromContext(ctx context.Context) OperationType {
	op, _ := ctx.Value(operationTypeKey).(OperationType)
	return op
}

// WithAPIKeyID stores the authenticated API key ID in ctx.
func WithAPIKeyID(ctx context.Context, keyID string) context.Context {
	return context.WithValue(ctx, apiKeyIDKey, keyID)
}

// APIKeyIDFromContext retrieves the API key ID stored by WithAPIKeyID.
// Returns an empty string if no API key is present.
func APIKeyIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(apiKeyIDKey).(string)
	return id
}

// WithAPIScopes stores the API key's scope restrictions in ctx.
func WithAPIScopes(ctx context.Context, scopes []meta.APIScopePerm) context.Context {
	return context.WithValue(ctx, apiScopesKey, scopes)
}

// APIScopesFromContext retrieves the API key scopes stored by WithAPIScopes.
// Returns nil if no scopes are present (i.e. not an API key request).
func APIScopesFromContext(ctx context.Context) []meta.APIScopePerm {
	s, _ := ctx.Value(apiScopesKey).([]meta.APIScopePerm)
	return s
}

// WithAPIRateLimit stores the API key's rate limit config in ctx.
func WithAPIRateLimit(ctx context.Context, cfg *meta.RateLimitConfig) context.Context {
	return context.WithValue(ctx, apiRateLimitKey, cfg)
}

// APIRateLimitFromContext retrieves the API key rate limit config stored by WithAPIRateLimit.
// Returns nil if no API key rate limit is present.
func APIRateLimitFromContext(ctx context.Context) *meta.RateLimitConfig {
	c, _ := ctx.Value(apiRateLimitKey).(*meta.RateLimitConfig)
	return c
}

// WithLanguage stores the negotiated language code in ctx.
func WithLanguage(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, languageKey, lang)
}

// LanguageFromContext retrieves the language code stored by WithLanguage.
// Returns an empty string if no language is present.
func LanguageFromContext(ctx context.Context) string {
	l, _ := ctx.Value(languageKey).(string)
	return l
}
