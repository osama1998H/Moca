package api

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
)

// MiddlewareRegistry maps string names to Middleware functions, enabling
// per-DocType and per-endpoint middleware configuration via APIConfig.
// Registration happens at startup; lookups happen per-request under RLock.
type MiddlewareRegistry struct {
	middlewares map[string]Middleware
	mu          sync.RWMutex
}

// NewMiddlewareRegistry creates an empty middleware registry.
func NewMiddlewareRegistry() *MiddlewareRegistry {
	return &MiddlewareRegistry{
		middlewares: make(map[string]Middleware),
	}
}

// Register adds a named middleware. Returns an error if the name is already registered.
func (r *MiddlewareRegistry) Register(name string, mw Middleware) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.middlewares[name]; exists {
		return fmt.Errorf("middleware %q already registered", name)
	}
	r.middlewares[name] = mw
	return nil
}

// Get returns the middleware registered under name, or false if not found.
func (r *MiddlewareRegistry) Get(name string) (Middleware, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	mw, ok := r.middlewares[name]
	return mw, ok
}

// Names returns all registered middleware names in sorted order.
func (r *MiddlewareRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.middlewares))
	for name := range r.middlewares {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Chain composes the named middlewares into a single Middleware.
// The first name in the slice becomes the outermost wrapper (runs first).
// Returns an error if any name is not registered.
func (r *MiddlewareRegistry) Chain(names []string) (Middleware, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	mws := make([]Middleware, len(names))
	for i, name := range names {
		mw, ok := r.middlewares[name]
		if !ok {
			return nil, fmt.Errorf("middleware %q not registered", name)
		}
		mws[i] = mw
	}

	return func(final http.Handler) http.Handler {
		h := final
		// Wrap from last to first so that mws[0] is outermost.
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}, nil
}
