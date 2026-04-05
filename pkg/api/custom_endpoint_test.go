package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

func testMetaTypeWithCustomEndpoints() *meta.MetaType {
	return &meta.MetaType{
		Name: "SalesOrder",
		APIConfig: &meta.APIConfig{
			Enabled:     true,
			AllowGet:    true,
			AllowCreate: true,
			AllowList:   true,
			CustomEndpoints: []meta.CustomEndpoint{
				{
					Method:  "POST",
					Path:    "approve",
					Handler: "approve_order",
				},
				{
					Method:  "GET",
					Path:    "dashboard/summary",
					Handler: "order_summary",
				},
				{
					Method:     "POST",
					Path:       "cancel",
					Handler:    "cancel_order",
					Middleware: []string{"audit"},
				},
			},
		},
	}
}

func newCustomEndpointRouter(resolver MetaResolver) *CustomEndpointRouter {
	handlers := NewHandlerRegistry()
	_ = handlers.Register("approve_order", func(w http.ResponseWriter, r *http.Request, mt *meta.MetaType, _ *tenancy.SiteContext, _ *auth.User) {
		writeSuccess(w, http.StatusOK, map[string]any{"approved": true, "doctype": mt.Name})
	})
	_ = handlers.Register("order_summary", func(w http.ResponseWriter, r *http.Request, mt *meta.MetaType, _ *tenancy.SiteContext, _ *auth.User) {
		writeSuccess(w, http.StatusOK, map[string]any{"total_orders": 42})
	})
	_ = handlers.Register("cancel_order", func(w http.ResponseWriter, r *http.Request, mt *meta.MetaType, _ *tenancy.SiteContext, _ *auth.User) {
		writeSuccess(w, http.StatusOK, map[string]any{"cancelled": true})
	})

	mwRegistry := NewMiddlewareRegistry()
	_ = mwRegistry.Register("audit", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Audit", "true")
			next.ServeHTTP(w, r)
		})
	})

	return NewCustomEndpointRouter(
		resolver,
		handlers,
		mwRegistry,
		AllowAllPermissionChecker{},
		nil, // no rate limiter in tests
		nil, // no logger in tests
	)
}

func TestCustomEndpoint_Success_POST(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMetaTypeWithCustomEndpoints(), nil
		},
	}
	router := newCustomEndpointRouter(resolver)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("POST", "/api/v1/custom/SalesOrder/approve", nil)
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map in response, got %v", resp)
	}
	if data["approved"] != true {
		t.Fatalf("expected approved=true, got %v", data)
	}
	if data["doctype"] != "SalesOrder" {
		t.Fatalf("expected doctype=SalesOrder, got %v", data["doctype"])
	}
}

func TestCustomEndpoint_Success_GET_MultiSegmentPath(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMetaTypeWithCustomEndpoints(), nil
		},
	}
	router := newCustomEndpointRouter(resolver)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("GET", "/api/v1/custom/SalesOrder/dashboard/summary", nil)
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map in response, got %v", resp)
	}
	if data["total_orders"] != float64(42) {
		t.Fatalf("expected total_orders=42, got %v", data["total_orders"])
	}
}

func TestCustomEndpoint_NotFound_NoMatchingEndpoint(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMetaTypeWithCustomEndpoints(), nil
		},
	}
	router := newCustomEndpointRouter(resolver)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("POST", "/api/v1/custom/SalesOrder/nonexistent", nil)
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCustomEndpoint_MethodMismatch(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMetaTypeWithCustomEndpoints(), nil
		},
	}
	router := newCustomEndpointRouter(resolver)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	// "approve" is POST-only; GET should not match.
	r := httptest.NewRequest("GET", "/api/v1/custom/SalesOrder/approve", nil)
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCustomEndpoint_APIDisabled(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			mt := testMetaTypeWithCustomEndpoints()
			mt.APIConfig.Enabled = false
			return mt, nil
		},
	}
	router := newCustomEndpointRouter(resolver)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("POST", "/api/v1/custom/SalesOrder/approve", nil)
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCustomEndpoint_HandlerNotRegistered(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			mt := &meta.MetaType{
				Name: "TestDoc",
				APIConfig: &meta.APIConfig{
					Enabled: true,
					CustomEndpoints: []meta.CustomEndpoint{
						{Method: "POST", Path: "action", Handler: "unregistered_handler"},
					},
				},
			}
			return mt, nil
		},
	}

	// Empty handler registry — handler not registered.
	router := NewCustomEndpointRouter(
		resolver,
		NewHandlerRegistry(),
		NewMiddlewareRegistry(),
		AllowAllPermissionChecker{},
		nil,
		nil,
	)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("POST", "/api/v1/custom/TestDoc/action", nil)
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCustomEndpoint_AuthRequired(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMetaTypeWithCustomEndpoints(), nil
		},
	}
	router := newCustomEndpointRouter(resolver)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	// No user in context.
	r := httptest.NewRequest("POST", "/api/v1/custom/SalesOrder/approve", nil)
	ctx := WithSite(r.Context(), testSite)
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestCustomEndpoint_TenantRequired(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMetaTypeWithCustomEndpoints(), nil
		},
	}
	router := newCustomEndpointRouter(resolver)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	// No site in context.
	r := httptest.NewRequest("POST", "/api/v1/custom/SalesOrder/approve", nil)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCustomEndpoint_PermissionDenied(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMetaTypeWithCustomEndpoints(), nil
		},
	}

	router := NewCustomEndpointRouter(
		resolver,
		newCustomEndpointRouter(resolver).handlers, // reuse registered handlers
		NewMiddlewareRegistry(),
		denyAllPermChecker{}, // deny all permissions
		nil,
		nil,
	)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("POST", "/api/v1/custom/SalesOrder/approve", nil)
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCustomEndpoint_MiddlewareExecutionOrder(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return &meta.MetaType{
				Name: "TestDoc",
				APIConfig: &meta.APIConfig{
					Enabled:    true,
					Middleware: []string{"doctype_mw"},
					CustomEndpoints: []meta.CustomEndpoint{
						{
							Method:     "POST",
							Path:       "action",
							Handler:    "test_handler",
							Middleware: []string{"endpoint_mw"},
						},
					},
				},
			}, nil
		},
	}

	var order []string
	handlers := NewHandlerRegistry()
	_ = handlers.Register("test_handler", func(w http.ResponseWriter, r *http.Request, _ *meta.MetaType, _ *tenancy.SiteContext, _ *auth.User) {
		order = append(order, "handler")
		writeSuccess(w, http.StatusOK, "ok")
	})

	mwRegistry := NewMiddlewareRegistry()
	_ = mwRegistry.Register("doctype_mw", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "doctype_mw")
			next.ServeHTTP(w, r)
		})
	})
	_ = mwRegistry.Register("endpoint_mw", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "endpoint_mw")
			next.ServeHTTP(w, r)
		})
	})

	router := NewCustomEndpointRouter(resolver, handlers, mwRegistry, AllowAllPermissionChecker{}, nil, nil)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("POST", "/api/v1/custom/TestDoc/action", nil)
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	expected := []string{"doctype_mw", "endpoint_mw", "handler"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("position %d: expected %q, got %q (full: %v)", i, v, order[i], order)
		}
	}
}

func TestCustomEndpoint_EndpointMiddlewareHeader(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMetaTypeWithCustomEndpoints(), nil
		},
	}
	router := newCustomEndpointRouter(resolver)
	mux := http.NewServeMux()
	router.RegisterRoutes(mux, "v1")

	// "cancel" endpoint has "audit" middleware which sets X-Audit header.
	r := httptest.NewRequest("POST", "/api/v1/custom/SalesOrder/cancel", nil)
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Audit") != "true" {
		t.Fatal("expected X-Audit header from audit middleware")
	}
}

func TestHandlerRegistry_Duplicate(t *testing.T) {
	reg := NewHandlerRegistry()
	noop := func(http.ResponseWriter, *http.Request, *meta.MetaType, *tenancy.SiteContext, *auth.User) {}
	_ = reg.Register("test", noop)

	err := reg.Register("test", noop)
	if err == nil {
		t.Fatal("expected error on duplicate registration")
	}
}

func TestHandlerRegistry_Names(t *testing.T) {
	reg := NewHandlerRegistry()
	noop := func(http.ResponseWriter, *http.Request, *meta.MetaType, *tenancy.SiteContext, *auth.User) {}
	_ = reg.Register("z_handler", noop)
	_ = reg.Register("a_handler", noop)

	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "a_handler" || names[1] != "z_handler" {
		t.Fatalf("expected sorted names, got %v", names)
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"approve", "approve"},
		{"/approve", "approve"},
		{"approve/", "approve"},
		{"/approve/", "approve"},
		{"dashboard/summary", "dashboard/summary"},
		{"/dashboard/summary/", "dashboard/summary"},
		{"", ""},
	}
	for _, tc := range tests {
		got := normalizePath(tc.input)
		if got != tc.expected {
			t.Errorf("normalizePath(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
