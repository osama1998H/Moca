package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
	pkgsearch "github.com/osama1998H/moca/pkg/search"
)

// Gateway is the root HTTP handler for the Moca API layer.
// It owns the ServeMux, dependency references, and middleware chain.
// Field order is chosen to minimise the GC pointer-scan region.
type Gateway struct {
	mux           *http.ServeMux
	docManager    *document.DocManager
	registry      *meta.Registry
	search        SearchService
	redis         *drivers.RedisClients
	rateLimiter   *RateLimiter
	versionRouter *VersionRouter
	logger        *slog.Logger
	defaultRate   *meta.RateLimitConfig
	auth          Authenticator
	perm          PermissionChecker
	fieldPerm     Transformer
	siteResolver       SiteResolver
	apiKeyStore        APIKeyValidator
	middlewareRegistry *MiddlewareRegistry
	handlerRegistry    *HandlerRegistry
	methodRegistry     *MethodRegistry
	reportRegistry     *ReportRegistry
	dashboardRegistry  *DashboardRegistry
	metrics            *observe.MetricsCollector
	i18nMiddleware     Middleware
	cors               CORSConfig
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
	if g.i18nMiddleware != nil {
		h = g.i18nMiddleware(h)
	}
	if g.metrics != nil {
		h = metricsMiddleware(g.metrics)(h)
	}
	h = authMiddleware(g.auth, g.apiKeyStore)(h)
	h = tenantMiddleware(g.siteResolver)(h)
	h = corsMiddleware(g.cors)(h)
	h = tracingMiddleware()(h)
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

// FieldLevelTransformer returns the gateway's field-level permission transformer.
// May return nil if no field-level filtering is configured.
func (g *Gateway) FieldLevelTransformer() Transformer {
	return g.fieldPerm
}

// Logger returns the gateway's logger.
func (g *Gateway) Logger() *slog.Logger {
	return g.logger
}

// SearchService abstracts the full-text query layer used by the search API.
type SearchService interface {
	Search(ctx context.Context, site string, mt *meta.MetaType, query string, filters []orm.Filter, page, limit int) ([]pkgsearch.SearchResult, int, error)
}

// SearchService returns the configured search query service.
func (g *Gateway) SearchService() SearchService {
	return g.search
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

// WithSearchService sets the search query service.
func WithSearchService(s SearchService) GatewayOption {
	return func(g *Gateway) { g.search = s }
}

// WithAuthenticator sets the authenticator.
func WithAuthenticator(a Authenticator) GatewayOption {
	return func(g *Gateway) { g.auth = a }
}

// WithPermissionChecker sets the permission checker.
func WithPermissionChecker(pc PermissionChecker) GatewayOption {
	return func(g *Gateway) { g.perm = pc }
}

// WithFieldLevelTransformer sets the field-level permission transformer.
func WithFieldLevelTransformer(t Transformer) GatewayOption {
	return func(g *Gateway) { g.fieldPerm = t }
}

// WithSiteResolver sets the site resolver for tenant middleware.
func WithSiteResolver(sr SiteResolver) GatewayOption {
	return func(g *Gateway) { g.siteResolver = sr }
}

// WithAPIKeyStore sets the API key validator for token-based authentication.
func WithAPIKeyStore(v APIKeyValidator) GatewayOption {
	return func(g *Gateway) { g.apiKeyStore = v }
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

// WithMiddlewareRegistry sets the named middleware registry.
func WithMiddlewareRegistry(r *MiddlewareRegistry) GatewayOption {
	return func(g *Gateway) { g.middlewareRegistry = r }
}

// WithHandlerRegistry sets the custom endpoint handler registry.
func WithHandlerRegistry(r *HandlerRegistry) GatewayOption {
	return func(g *Gateway) { g.handlerRegistry = r }
}

// WithMethodRegistry sets the whitelisted method registry.
func WithMethodRegistry(r *MethodRegistry) GatewayOption {
	return func(g *Gateway) { g.methodRegistry = r }
}

// WithMetricsCollector sets the Prometheus metrics collector.
func WithMetricsCollector(mc *observe.MetricsCollector) GatewayOption {
	return func(g *Gateway) { g.metrics = mc }
}

// MetricsCollector returns the gateway's metrics collector.
func (g *Gateway) MetricsCollector() *observe.MetricsCollector { return g.metrics }

// WithI18nMiddleware sets the i18n language negotiation middleware.
func WithI18nMiddleware(mw Middleware) GatewayOption {
	return func(g *Gateway) { g.i18nMiddleware = mw }
}

// RateLimiter returns the gateway's rate limiter.
func (g *Gateway) RateLimiter() *RateLimiter { return g.rateLimiter }

// MiddlewareRegistry returns the gateway's named middleware registry.
func (g *Gateway) MiddlewareRegistry() *MiddlewareRegistry { return g.middlewareRegistry }

// HandlerRegistry returns the gateway's custom endpoint handler registry.
func (g *Gateway) HandlerRegistry() *HandlerRegistry { return g.handlerRegistry }

// MethodRegistry returns the gateway's whitelisted method registry.
func (g *Gateway) MethodRegistry() *MethodRegistry { return g.methodRegistry }

// WithReportRegistry sets the report definition registry.
func WithReportRegistry(r *ReportRegistry) GatewayOption {
	return func(g *Gateway) { g.reportRegistry = r }
}

// WithDashboardRegistry sets the dashboard definition registry.
func WithDashboardRegistry(r *DashboardRegistry) GatewayOption {
	return func(g *Gateway) { g.dashboardRegistry = r }
}

// ReportRegistry returns the gateway's report definition registry.
func (g *Gateway) ReportRegistry() *ReportRegistry { return g.reportRegistry }

// DashboardRegistry returns the gateway's dashboard definition registry.
func (g *Gateway) DashboardRegistry() *DashboardRegistry { return g.dashboardRegistry }

// SetVersionRouter sets the version router after gateway construction.
// This resolves the circular construction dependency: ResourceHandler needs
// Gateway, VersionRouter needs ResourceHandler, and Gateway.Handler() needs
// VersionRouter for the middleware chain.
func (g *Gateway) SetVersionRouter(vr *VersionRouter) {
	g.versionRouter = vr
}
