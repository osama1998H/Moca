package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMethodHandler_POST_Success(t *testing.T) {
	reg := NewMethodRegistry()
	_ = reg.Register("send_email", func(_ context.Context, args map[string]any) (any, error) {
		return map[string]any{"status": "sent", "to": args["to"]}, nil
	})

	h := NewMethodHandler(reg, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	body := `{"to":"user@example.com","subject":"Hello"}`
	r := httptest.NewRequest("POST", "/api/v1/method/send_email", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data in response, got %v", resp)
	}
	if data["status"] != "sent" {
		t.Fatalf("expected status=sent, got %v", data["status"])
	}
}

func TestMethodHandler_GET_Success(t *testing.T) {
	reg := NewMethodRegistry()
	_ = reg.Register("get_status", func(_ context.Context, args map[string]any) (any, error) {
		return map[string]any{"order": args["order_id"], "status": "shipped"}, nil
	})

	h := NewMethodHandler(reg, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("GET", "/api/v1/method/get_status?order_id=ORD-001", nil)
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
	if data["order"] != "ORD-001" {
		t.Fatalf("expected order=ORD-001, got %v", data["order"])
	}
}

func TestMethodHandler_NotFound(t *testing.T) {
	reg := NewMethodRegistry()
	h := NewMethodHandler(reg, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("POST", "/api/v1/method/nonexistent", strings.NewReader("{}"))
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestMethodHandler_AuthRequired(t *testing.T) {
	reg := NewMethodRegistry()
	_ = reg.Register("test_method", func(_ context.Context, _ map[string]any) (any, error) {
		return "ok", nil
	})

	h := NewMethodHandler(reg, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	// No user in context — only site.
	r := httptest.NewRequest("POST", "/api/v1/method/test_method", strings.NewReader("{}"))
	ctx := WithSite(r.Context(), testSite)
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMethodHandler_TenantRequired(t *testing.T) {
	reg := NewMethodRegistry()
	_ = reg.Register("test_method", func(_ context.Context, _ map[string]any) (any, error) {
		return "ok", nil
	})

	h := NewMethodHandler(reg, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	// No site in context.
	r := httptest.NewRequest("POST", "/api/v1/method/test_method", strings.NewReader("{}"))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMethodHandler_MethodError(t *testing.T) {
	reg := NewMethodRegistry()
	_ = reg.Register("fail_method", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, errors.New("something went wrong")
	})

	h := NewMethodHandler(reg, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("POST", "/api/v1/method/fail_method", strings.NewReader("{}"))
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestMethodHandler_InvalidJSON(t *testing.T) {
	reg := NewMethodRegistry()
	_ = reg.Register("test_method", func(_ context.Context, _ map[string]any) (any, error) {
		return "ok", nil
	})

	h := NewMethodHandler(reg, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("POST", "/api/v1/method/test_method", strings.NewReader("not json"))
	r.Header.Set("Content-Type", "application/json")
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMethodRegistry_Duplicate(t *testing.T) {
	reg := NewMethodRegistry()
	_ = reg.Register("test", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, nil
	})
	err := reg.Register("test", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error on duplicate registration")
	}
}

func TestMethodRegistry_Names(t *testing.T) {
	reg := NewMethodRegistry()
	noop := func(_ context.Context, _ map[string]any) (any, error) { return nil, nil }
	_ = reg.Register("send_email", noop)
	_ = reg.Register("approve_order", noop)
	_ = reg.Register("get_status", noop)

	names := reg.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "approve_order" || names[1] != "get_status" || names[2] != "send_email" {
		t.Fatalf("expected sorted names, got %v", names)
	}
}

func TestMethodHandler_EmptyBody(t *testing.T) {
	reg := NewMethodRegistry()
	_ = reg.Register("no_args", func(_ context.Context, args map[string]any) (any, error) {
		return map[string]any{"count": len(args)}, nil
	})

	h := NewMethodHandler(reg, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("POST", "/api/v1/method/no_args", nil)
	r = contextWithSiteAndUser(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
