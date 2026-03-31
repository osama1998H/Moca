package api

import (
	"log/slog"
	"net/http"

	"github.com/moca-framework/moca/internal/drivers"
	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/meta"
)

// Gateway is the root HTTP handler for the Moca API layer.
// It owns the ServeMux, dependency references, and middleware chain.
// Field order is chosen to minimise the GC pointer-scan region.
type Gateway struct {
	mux           *http.ServeMux
	docManager    *document.DocManager
	registry      *meta.Registry
	redis         *drivers.RedisClients
	rateLimiter   *RateLimiter
	versionRouter *VersionRouter
	logger        *slog.Logger
	defaultRate   *meta.RateLimitConfig
	auth          Authenticator
	perm          PermissionChecker
	siteResolver  SiteResolver
	cors          CORSConfig
}

// GatewayOption configures a Gateway during construction.
type GatewayOption func(*Gateway)

// NewGateway creates a Gateway with the given options. It applies sensible
// defaults for auth (NoopAuthenticator) and permissions (AllowAllPermissionChecker)
// so the gateway is functional out of the box.
func NewGateway(opts ...GatewayOption) *Gateway {
	g := &Gateway{
		mux:    http.NewServeMux(),
		auth:   NoopAuthenticator{},
		perm:   AllowAllPermissionChecker{},
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Handler returns an http.Handler with the full middleware chain applied.
// Chain order: RequestID → CORS → Tenant → Auth → RateLimit → Version → ServeMux.
//
// Rate limiting is placed after Auth so the key includes user identity.
// Tenant resolution is before Auth because auth may need the site context.
// Version middleware is innermost (closest to handlers) since it only sets
// context and headers — it doesn't need auth/tenant info.
func (g *Gateway) Handler() http.Handler {
	var h http.Handler = g.mux

	// Wrap innermost to outermost (last applied runs first).
	if g.versionRouter != nil {
		h = g.versionRouter.Middleware()(h)
	}
	h = rateLimitMiddleware(g.rateLimiter, g.defaultRate)(h)
	h = authMiddleware(g.auth)(h)
	h = tenantMiddleware(g.siteResolver)(h)
	h = corsMiddleware(g.cors)(h)
	h = requestIDMiddleware(g.logger)(h)

	return h
}

// Mux returns the underlying ServeMux so that route handlers (REST, health, etc.)
// can register their endpoints.
func (g *Gateway) Mux() *http.ServeMux {
	return g.mux
}

// DocManager returns the gateway's document manager.
func (g *Gateway) DocManager() *document.DocManager {
	return g.docManager
}

// Registry returns the gateway's metadata registry.
func (g *Gateway) Registry() *meta.Registry {
	return g.registry
}

// PermChecker returns the gateway's permission checker.
func (g *Gateway) PermChecker() PermissionChecker {
	return g.perm
}

// Logger returns the gateway's logger.
func (g *Gateway) Logger() *slog.Logger {
	return g.logger
}

// --- Functional Options ---

// WithDocManager sets the document manager.
func WithDocManager(dm *document.DocManager) GatewayOption {
	return func(g *Gateway) { g.docManager = dm }
}

// WithRegistry sets the metadata registry.
func WithRegistry(r *meta.Registry) GatewayOption {
	return func(g *Gateway) { g.registry = r }
}

// WithRedis sets the Redis clients.
func WithRedis(rc *drivers.RedisClients) GatewayOption {
	return func(g *Gateway) { g.redis = rc }
}

// WithAuthenticator sets the authenticator.
func WithAuthenticator(a Authenticator) GatewayOption {
	return func(g *Gateway) { g.auth = a }
}

// WithPermissionChecker sets the permission checker.
func WithPermissionChecker(pc PermissionChecker) GatewayOption {
	return func(g *Gateway) { g.perm = pc }
}

// WithSiteResolver sets the site resolver for tenant middleware.
func WithSiteResolver(sr SiteResolver) GatewayOption {
	return func(g *Gateway) { g.siteResolver = sr }
}

// WithRateLimiter sets the rate limiter and default rate limit config.
func WithRateLimiter(rl *RateLimiter, defaultCfg *meta.RateLimitConfig) GatewayOption {
	return func(g *Gateway) {
		g.rateLimiter = rl
		g.defaultRate = defaultCfg
	}
}

// WithCORS sets the CORS configuration.
func WithCORS(cfg CORSConfig) GatewayOption {
	return func(g *Gateway) { g.cors = cfg }
}

// WithVersionRouter sets the version router for multi-version API support.
func WithVersionRouter(vr *VersionRouter) GatewayOption {
	return func(g *Gateway) { g.versionRouter = vr }
}

// WithLogger sets the structured logger.
func WithLogger(l *slog.Logger) GatewayOption {
	return func(g *Gateway) { g.logger = l }
}

// SetVersionRouter sets the version router after gateway construction.
// This resolves the circular construction dependency: ResourceHandler needs
// Gateway, VersionRouter needs ResourceHandler, and Gateway.Handler() needs
// VersionRouter for the middleware chain.
func (g *Gateway) SetVersionRouter(vr *VersionRouter) {
	g.versionRouter = vr
}
