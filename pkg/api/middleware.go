package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/moca-framework/moca/pkg/observe"
	"github.com/moca-framework/moca/pkg/tenancy"
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

// tenantMiddleware resolves the tenant from the X-Moca-Site header (primary)
// or the first subdomain of the Host header (fallback). The resolved
// SiteContext is stored in the request context.
func tenantMiddleware(resolver SiteResolver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			siteID := r.Header.Get("X-Moca-Site")
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
func subdomainFromHost(host string) string {
	// Strip port if present.
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	parts := strings.Split(host, ".")
	if len(parts) < 3 {
		return ""
	}
	return parts[0]
}

// authMiddleware authenticates the request and stores the user in context.
// It also re-enriches the logger with the resolved user identity.
func authMiddleware(authn Authenticator) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
