package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
)

// MethodFunc is the function signature for whitelisted API methods.
// For GET requests, args contains query parameters; for POST, args contains
// the decoded JSON body. The returned value is serialized in the response.
type MethodFunc func(ctx context.Context, args map[string]any) (any, error)

// MethodRegistry maps method names to handler functions, serving the
// /api/v1/method/{name} endpoint. Only explicitly registered methods
// are accessible — no reflection-based discovery.
type MethodRegistry struct {
	methods map[string]MethodFunc
	mu      sync.RWMutex
}

// NewMethodRegistry creates an empty method registry.
func NewMethodRegistry() *MethodRegistry {
	return &MethodRegistry{
		methods: make(map[string]MethodFunc),
	}
}

// Register adds a named method. Returns an error if the name is already registered.
func (r *MethodRegistry) Register(name string, fn MethodFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.methods[name]; exists {
		return fmt.Errorf("method %q already registered", name)
	}
	r.methods[name] = fn
	return nil
}

// Get returns the method registered under name, or false if not found.
func (r *MethodRegistry) Get(name string) (MethodFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.methods[name]
	return fn, ok
}

// Names returns all registered method names in sorted order.
func (r *MethodRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.methods))
	for name := range r.methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// MethodHandler serves whitelisted API methods at /api/v1/method/{name}.
// Methods require authentication but handle their own authorization.
type MethodHandler struct {
	methods *MethodRegistry
	logger  *slog.Logger
}

// NewMethodHandler creates a MethodHandler wired to the given registries.
func NewMethodHandler(methods *MethodRegistry, logger *slog.Logger) *MethodHandler {
	return &MethodHandler{
		methods: methods,
		logger:  logger,
	}
}

// RegisterRoutes registers GET and POST routes for whitelisted methods.
func (h *MethodHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version + "/method/{name}"
	mux.HandleFunc("GET "+p, h.handleMethod)
	mux.HandleFunc("POST "+p, h.handleMethod)
}

func (h *MethodHandler) handleMethod(w http.ResponseWriter, r *http.Request) {
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

	name := r.PathValue("name")
	fn, ok := h.methods.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "METHOD_NOT_FOUND", "method not registered: "+name)
		return
	}

	args, err := h.parseArgs(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
		return
	}

	result, err := fn(r.Context(), args)
	if err != nil {
		if !mapErrorResponse(w, err) {
			if h.logger != nil {
				h.logger.Error("method invocation failed",
					slog.String("method", name),
					slog.String("error", err.Error()),
				)
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		}
		return
	}

	writeSuccess(w, http.StatusOK, result)
}

// parseArgs extracts arguments from the request. For GET requests, query
// parameters are used. For POST requests, the JSON body is decoded.
func (h *MethodHandler) parseArgs(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	if r.Method == http.MethodGet {
		args := make(map[string]any, len(r.URL.Query()))
		for k, v := range r.URL.Query() {
			if len(v) == 1 {
				args[k] = v[0]
			} else {
				args[k] = v
			}
		}
		return args, nil
	}

	// POST: decode JSON body.
	if r.Body == nil || r.ContentLength == 0 {
		return map[string]any{}, nil
	}
	defer func() { _ = r.Body.Close() }()

	var args map[string]any
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err := dec.Decode(&args); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}
