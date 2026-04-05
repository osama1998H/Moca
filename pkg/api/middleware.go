package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// Middleware is a function that wraps an http.Handler.
type Middleware func(next http.Handler) http.Handler

// SiteResolver looks up a tenant's SiteContext by its identifier.
// The gateway's tenantMiddleware calls this to map an incoming request
// to a tenant. Implementations may cache results in Redis or in-memory.
type SiteResolver interface {
	ResolveSite(ctx context.Context, siteID string) (*tenancy.SiteContext, error)
}

// CORSConfig controls Cross-Origin Resource Sharing headers.
type CORSConfig struct {
	AllowedOrigins []string // e.g. ["https://app.example.com"]; empty = no CORS headers
	AllowedMethods []string // defaults to ["GET","POST","PUT","DELETE","OPTIONS"]
	AllowedHeaders []string // defaults to ["Content-Type","Authorization","X-Moca-Site","X-Request-ID"]
	MaxAge         int      // preflight cache duration in seconds; 0 = omit header
}

// requestIDMiddleware generates a unique request ID (or reuses an existing
// X-Request-ID header), stores it in the context, sets the response header,
// and enriches the logger.
func requestIDMiddleware(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = generateRequestID()
			}

			ctx := WithRequestID(r.Context(), id)

			// Enrich logger with request ID (user not yet known).
			reqLogger := observe.WithRequest(logger, id, "")
			ctx = observe.ContextWithLogger(ctx, reqLogger)

			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// generateRequestID returns a 16-byte hex-encoded random string.
func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand never errors on supported platforms
	return hex.EncodeToString(b)
}

// corsMiddleware handles CORS preflight requests and sets response headers
// according to the provided CORSConfig.
func corsMiddleware(cfg CORSConfig) Middleware {
	if len(cfg.AllowedMethods) == 0 {
		cfg.AllowedMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}
	if len(cfg.AllowedHeaders) == 0 {
		cfg.AllowedHeaders = []string{"Content-Type", "Authorization", "X-Moca-Site", "X-Request-ID"}
	}

	methods := strings.Join(cfg.AllowedMethods, ", ")
	headers := strings.Join(cfg.AllowedHeaders, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && originAllowed(origin, cfg.AllowedOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", headers)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				if cfg.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", cfg.MaxAge))
				}
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// originAllowed checks if the given origin is in the allowed list.
// A wildcard "*" allows all origins.
func originAllowed(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	return false
}

// tenantMiddleware resolves the tenant using three strategies in priority order:
//  1. X-Moca-Site header (explicit, highest priority)
//  2. Path prefix /sites/{site}/... (rewrite path for downstream handlers)
//  3. Subdomain from Host header (lowest priority)
//
// The resolved SiteContext is stored in the request context.
func tenantMiddleware(resolver SiteResolver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip tenant resolution for non-API paths (static assets, health).
			if strings.HasPrefix(r.URL.Path, "/desk/") || r.URL.Path == "/health" || r.URL.Path == "/ws" {
				next.ServeHTTP(w, r)
				return
			}

			siteID := r.Header.Get("X-Moca-Site")

			// Try path-based resolution and rewrite the URL path.
			if siteID == "" {
				if id, stripped := siteFromPath(r.URL.Path); id != "" {
					siteID = id
					r.URL.Path = stripped
				}
			}

			if siteID == "" {
				siteID = subdomainFromHost(r.Host)
			}
			if siteID == "" {
				http.Error(w, `{"error":{"code":"TENANT_REQUIRED","message":"X-Moca-Site header or subdomain required"}}`, http.StatusBadRequest)
				return
			}

			site, err := resolver.ResolveSite(r.Context(), siteID)
			if err != nil {
				logger := observe.LoggerFromContext(r.Context())
				logger.Warn("tenant resolution failed",
					slog.String("site_id", siteID),
					slog.String("error", err.Error()),
				)
				if errors.Is(err, tenancy.ErrSiteDisabled) {
					http.Error(w, `{"error":{"code":"SITE_DISABLED","message":"site is under maintenance"}}`, http.StatusServiceUnavailable)
					return
				}
				http.Error(w, `{"error":{"code":"TENANT_NOT_FOUND","message":"site not found"}}`, http.StatusNotFound)
				return
			}

			ctx := WithSite(r.Context(), site)

			// Enrich logger with site context.
			logger := observe.LoggerFromContext(ctx)
			logger = observe.WithSite(logger, site.Name)
			ctx = observe.ContextWithLogger(ctx, logger)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// subdomainFromHost extracts the first subdomain from the Host header.
// Returns empty string if the host has no subdomain (e.g. "localhost", "example.com").
// Special case: "acme.localhost" is treated as subdomain "acme" for local development.
func subdomainFromHost(host string) string {
	// Strip port if present.
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	parts := strings.Split(host, ".")
	// Special case: *.localhost for local development.
	if len(parts) == 2 && parts[1] == "localhost" {
		return parts[0]
	}
	if len(parts) < 3 {
		return ""
	}
	return parts[0]
}

// siteFromPath extracts a site identifier from a /sites/{site}/... path prefix.
// Returns the site ID and the remaining path with the prefix stripped.
// If the path does not match, both return values are empty strings.
func siteFromPath(path string) (siteID, strippedPath string) {
	const prefix = "/sites/"
	if !strings.HasPrefix(path, prefix) {
		return "", ""
	}
	rest := path[len(prefix):]
	if rest == "" {
		return "", ""
	}
	// Split on the next slash to get the site name.
	idx := strings.Index(rest, "/")
	if idx == -1 {
		// Path is /sites/{site} with no trailing content.
		return rest, "/"
	}
	return rest[:idx], rest[idx:]
}

// authMiddleware authenticates the request and stores the user in context.
// It also re-enriches the logger with the resolved user identity.
//
// When apiKeys is non-nil, the middleware first checks for an "Authorization: token KEY:SECRET"
// header. If present, API key validation is attempted. On failure, a 401/403 is returned
// immediately — the request does NOT fall through to JWT/session/Guest auth.
// If no token header is present, the regular auth chain is used.
func authMiddleware(authn Authenticator, apiKeys APIKeyValidator) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for non-API paths (static assets, health).
			if strings.HasPrefix(r.URL.Path, "/desk/") || r.URL.Path == "/health" || r.URL.Path == "/ws" {
				next.ServeHTTP(w, r)
				return
			}

			// 1. Try API key auth if configured and token header is present.
			if apiKeys != nil {
				if keyID, _ := extractTokenAuth(r); keyID != "" {
					identity, err := apiKeys.ValidateRequest(r.Context(), r)
					if err != nil {
						// Do NOT fall through — caller explicitly chose token auth.
						writeAPIKeyError(w, err)
						return
					}
					ctx := WithUser(r.Context(), identity.User)
					ctx = WithAPIKeyID(ctx, identity.KeyID)
					ctx = WithAPIScopes(ctx, identity.Scopes)
					if identity.RateLimit != nil {
						ctx = WithAPIRateLimit(ctx, identity.RateLimit)
					}

					// Re-enrich logger with API key identity.
					reqID := RequestIDFromContext(ctx)
					logger := observe.LoggerFromContext(ctx)
					logger = observe.WithRequest(logger, reqID, identity.User.Email)
					logger = logger.With(slog.String("api_key", identity.KeyID))
					ctx = observe.ContextWithLogger(ctx, logger)

					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// 2. Regular auth (JWT / session cookie / Guest fallback).
			user, err := authn.Authenticate(r)
			if err != nil {
				http.Error(w, `{"error":{"code":"AUTH_FAILED","message":"authentication failed"}}`, http.StatusUnauthorized)
				return
			}

			ctx := WithUser(r.Context(), user)

			// Re-enrich logger with user identity.
			reqID := RequestIDFromContext(ctx)
			logger := observe.LoggerFromContext(ctx)
			logger = observe.WithRequest(logger, reqID, user.Email)
			ctx = observe.ContextWithLogger(ctx, logger)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeAPIKeyError maps API key validation errors to appropriate HTTP responses.
func writeAPIKeyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrIPNotAllowed):
		http.Error(w, `{"error":{"code":"IP_NOT_ALLOWED","message":"request IP not in API key allowlist"}}`, http.StatusForbidden)
	case errors.Is(err, ErrAPIKeyExpired):
		http.Error(w, `{"error":{"code":"API_KEY_EXPIRED","message":"api key has expired"}}`, http.StatusUnauthorized)
	case errors.Is(err, ErrAPIKeyRevoked):
		http.Error(w, `{"error":{"code":"API_KEY_REVOKED","message":"api key has been revoked"}}`, http.StatusUnauthorized)
	default:
		http.Error(w, `{"error":{"code":"AUTH_FAILED","message":"invalid api key credentials"}}`, http.StatusUnauthorized)
	}
}
