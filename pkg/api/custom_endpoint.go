package api

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// HandlerFunc is the function signature for custom endpoint handlers.
// It receives the resolved MetaType, site context, and authenticated user
// in addition to the standard HTTP writer and request.
type HandlerFunc func(w http.ResponseWriter, r *http.Request, mt *meta.MetaType, site *tenancy.SiteContext, user *auth.User)

// HandlerRegistry maps string handler names to HandlerFunc implementations.
// Apps register handlers by name during initialization; CustomEndpoint configs
// reference these names to bind routes to handlers.
type HandlerRegistry struct {
	handlers map[string]HandlerFunc
	mu       sync.RWMutex
}

// NewHandlerRegistry creates an empty handler registry.
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[string]HandlerFunc),
	}
}

// Register adds a named handler. Returns an error if the name is already registered.
func (r *HandlerRegistry) Register(name string, h HandlerFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[name]; exists {
		return fmt.Errorf("handler %q already registered", name)
	}
	r.handlers[name] = h
	return nil
}

// Get returns the handler registered under name, or false if not found.
func (r *HandlerRegistry) Get(name string) (HandlerFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}

// Names returns all registered handler names in sorted order.
func (r *HandlerRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CustomEndpointRouter dispatches requests to custom endpoints defined in
// MetaType APIConfig.CustomEndpoints. It uses catch-all patterns under
// /api/{version}/custom/{doctype}/{path...} to avoid conflicts with the
// standard CRUD routes under /resource/.
type CustomEndpointRouter struct {
	meta        MetaResolver
	handlers    *HandlerRegistry
	mwRegistry  *MiddlewareRegistry
	perm        PermissionChecker
	rateLimiter *RateLimiter
	logger      *slog.Logger
}

// NewCustomEndpointRouter creates a router wired to the given dependencies.
func NewCustomEndpointRouter(
	meta MetaResolver,
	handlers *HandlerRegistry,
	mwRegistry *MiddlewareRegistry,
	perm PermissionChecker,
	rl *RateLimiter,
	logger *slog.Logger,
) *CustomEndpointRouter {
	return &CustomEndpointRouter{
		meta:        meta,
		handlers:    handlers,
		mwRegistry:  mwRegistry,
		perm:        perm,
		rateLimiter: rl,
		logger:      logger,
	}
}

// RegisterRoutes registers catch-all patterns for custom endpoints.
func (c *CustomEndpointRouter) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version + "/custom/{doctype}/{path...}"
	mux.HandleFunc("GET "+p, c.handleCustom)
	mux.HandleFunc("POST "+p, c.handleCustom)
	mux.HandleFunc("PUT "+p, c.handleCustom)
	mux.HandleFunc("DELETE "+p, c.handleCustom)
	mux.HandleFunc("PATCH "+p, c.handleCustom)
}

func (c *CustomEndpointRouter) handleCustom(w http.ResponseWriter, r *http.Request) {
	// 1. Require site and user.
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "X-Moca-Site header or subdomain required")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	// 2. Resolve MetaType.
	doctype := r.PathValue("doctype")
	mt, err := c.meta.Get(r.Context(), site.Name, doctype)
	if err != nil {
		if !mapErrorResponse(w, err) {
			c.logError(r, err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		}
		return
	}

	// 3. Check API enabled.
	if mt.APIConfig == nil || !mt.APIConfig.Enabled {
		writeError(w, http.StatusNotFound, "DOCTYPE_NOT_FOUND", "doctype not found")
		return
	}

	// 4. Match custom endpoint by method + path.
	path := normalizePath(r.PathValue("path"))
	ep := matchCustomEndpoint(mt.APIConfig.CustomEndpoints, r.Method, path)
	if ep == nil {
		writeError(w, http.StatusNotFound, "ENDPOINT_NOT_FOUND",
			fmt.Sprintf("no custom endpoint %s /%s for %s", r.Method, path, doctype))
		return
	}

	// 5. Lookup handler.
	handler, ok := c.handlers.Get(ep.Handler)
	if !ok {
		writeError(w, http.StatusInternalServerError, "HANDLER_NOT_REGISTERED",
			fmt.Sprintf("handler %q not registered for endpoint %s /%s", ep.Handler, ep.Method, ep.Path))
		return
	}

	// 6. Check permissions (custom endpoints default to "read").
	if err := c.perm.CheckDocPerm(r.Context(), user, doctype, "read"); err != nil {
		if !mapErrorResponse(w, err) {
			writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "permission denied")
		}
		return
	}

	// 7. Per-endpoint rate limiting.
	if c.rateLimiter != nil && ep.RateLimit != nil {
		userID := "anonymous"
		if user.Email != "" {
			userID = user.Email
		}
		key := fmt.Sprintf("rl:%s:%s:%s:custom:%s", site.Name, userID, doctype, path)
		allowed, retryAfter, _ := c.rateLimiter.Allow(r.Context(), key, ep.RateLimit)
		if !allowed {
			retrySeconds := int(math.Ceil(retryAfter.Seconds()))
			if retrySeconds < 1 {
				retrySeconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests")
			return
		}
	}

	// 8. Build and apply middleware chain (DocType-level + endpoint-level).
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, mt, site, user)
	})

	final := c.applyMiddleware(mt, ep, inner)
	final.ServeHTTP(w, r)
}

// applyMiddleware composes DocType-level and endpoint-level middleware.
// DocType middleware is outermost; endpoint middleware is innermost.
func (c *CustomEndpointRouter) applyMiddleware(mt *meta.MetaType, ep *meta.CustomEndpoint, handler http.Handler) http.Handler {
	if c.mwRegistry == nil {
		return handler
	}

	h := handler

	// Endpoint-level middleware (innermost, runs last before handler).
	if len(ep.Middleware) > 0 {
		if chain, err := c.mwRegistry.Chain(ep.Middleware); err == nil {
			h = chain(h)
		} else if c.logger != nil {
			c.logger.Error("endpoint middleware resolution failed",
				slog.String("doctype", mt.Name),
				slog.String("path", ep.Path),
				slog.String("error", err.Error()),
			)
		}
	}

	// DocType-level middleware (outermost, runs first).
	if mt.APIConfig != nil && len(mt.APIConfig.Middleware) > 0 {
		if chain, err := c.mwRegistry.Chain(mt.APIConfig.Middleware); err == nil {
			h = chain(h)
		} else if c.logger != nil {
			c.logger.Error("per-doctype middleware resolution failed",
				slog.String("doctype", mt.Name),
				slog.String("error", err.Error()),
			)
		}
	}

	return h
}

func (c *CustomEndpointRouter) logError(r *http.Request, err error) {
	if c.logger != nil {
		c.logger.Error("custom endpoint request failed",
			slog.String("error", err.Error()),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
	}
}

// matchCustomEndpoint finds the first CustomEndpoint matching the given
// HTTP method and normalized path. Returns nil if no match.
func matchCustomEndpoint(endpoints []meta.CustomEndpoint, method, path string) *meta.CustomEndpoint {
	for i := range endpoints {
		ep := &endpoints[i]
		if !strings.EqualFold(ep.Method, method) {
			continue
		}
		if normalizePath(ep.Path) == path {
			return ep
		}
	}
	return nil
}

// normalizePath strips leading and trailing slashes for consistent matching.
func normalizePath(p string) string {
	return strings.Trim(p, "/")
}
