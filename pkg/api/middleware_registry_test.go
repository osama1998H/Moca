package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareRegistry_RegisterAndGet(t *testing.T) {
	r := NewMiddlewareRegistry()

	mw := func(next http.Handler) http.Handler { return next }
	if err := r.Register("audit", mw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := r.Get("audit")
	if !ok {
		t.Fatal("expected middleware to be found")
	}
	if got == nil {
		t.Fatal("expected non-nil middleware")
	}
}

func TestMiddlewareRegistry_RegisterDuplicate(t *testing.T) {
	r := NewMiddlewareRegistry()

	mw := func(next http.Handler) http.Handler { return next }
	_ = r.Register("audit", mw)

	err := r.Register("audit", mw)
	if err == nil {
		t.Fatal("expected error on duplicate registration")
	}
}

func TestMiddlewareRegistry_GetNotFound(t *testing.T) {
	r := NewMiddlewareRegistry()

	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected middleware not found")
	}
}

func TestMiddlewareRegistry_Names(t *testing.T) {
	r := NewMiddlewareRegistry()
	noop := func(next http.Handler) http.Handler { return next }

	_ = r.Register("cache", noop)
	_ = r.Register("audit", noop)
	_ = r.Register("metrics", noop)

	names := r.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "audit" || names[1] != "cache" || names[2] != "metrics" {
		t.Fatalf("expected sorted names [audit cache metrics], got %v", names)
	}
}

func TestMiddlewareRegistry_Chain_Order(t *testing.T) {
	r := NewMiddlewareRegistry()

	var order []string
	makeMW := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	_ = r.Register("first", makeMW("first"))
	_ = r.Register("second", makeMW("second"))
	_ = r.Register("third", makeMW("third"))

	chain, err := r.Chain([]string{"first", "second", "third"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	})

	handler := chain(inner)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	expected := []string{"first", "second", "third", "handler"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("position %d: expected %q, got %q (full: %v)", i, v, order[i], order)
		}
	}
}

func TestMiddlewareRegistry_Chain_UnknownName(t *testing.T) {
	r := NewMiddlewareRegistry()
	noop := func(next http.Handler) http.Handler { return next }
	_ = r.Register("audit", noop)

	_, err := r.Chain([]string{"audit", "nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown middleware name")
	}
}

func TestMiddlewareRegistry_Chain_Empty(t *testing.T) {
	r := NewMiddlewareRegistry()

	chain, err := r.Chain(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := chain(inner)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	if !called {
		t.Fatal("expected inner handler to be called")
	}
}
