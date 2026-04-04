package serve

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterDeskDevProxy(t *testing.T) {
	// Start a fake Vite dev server.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Proxied", "true")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("vite-dev-response"))
	}))
	defer backend.Close()

	// Extract port from the test server.
	// backend.URL is like "http://127.0.0.1:PORT"
	var port int
	for i := len(backend.URL) - 1; i >= 0; i-- {
		if backend.URL[i] == ':' {
			for j := i + 1; j < len(backend.URL); j++ {
				port = port*10 + int(backend.URL[j]-'0')
			}
			break
		}
	}

	mux := http.NewServeMux()
	logger := slog.Default()
	registerDeskDevProxy(mux, port, logger)

	// Test proxying a regular HTTP request.
	r := httptest.NewRequest(http.MethodGet, "/desk/index.html", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "vite-dev-response" {
		t.Errorf("body = %q, want %q", w.Body.String(), "vite-dev-response")
	}
	if w.Header().Get("X-Proxied") != "true" {
		t.Error("X-Proxied header missing — request was not proxied")
	}
}

func TestRegisterDeskDevProxy_WebSocketUpgrade(t *testing.T) {
	var gotUpgrade string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUpgrade = r.Header.Get("Upgrade")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	var port int
	for i := len(backend.URL) - 1; i >= 0; i-- {
		if backend.URL[i] == ':' {
			for j := i + 1; j < len(backend.URL); j++ {
				port = port*10 + int(backend.URL[j]-'0')
			}
			break
		}
	}

	mux := http.NewServeMux()
	registerDeskDevProxy(mux, port, slog.Default())

	r := httptest.NewRequest(http.MethodGet, "/desk/", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if gotUpgrade != "websocket" {
		t.Errorf("backend received Upgrade = %q, want %q", gotUpgrade, "websocket")
	}
}
