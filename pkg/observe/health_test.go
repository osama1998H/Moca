package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockPinger is a test double for the Pinger interface.
type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(_ context.Context) error {
	return m.err
}

// healthy and unhealthy are convenience instances used across tests.
var (
	healthy   = &mockPinger{err: nil}
	unhealthy = &mockPinger{err: errors.New("connection refused")}
)

// newTestChecker builds a HealthChecker backed by a buffer logger and registers
// its routes on a fresh ServeMux. The returned mux is used to dispatch requests.
func newTestChecker(db, redis Pinger) *http.ServeMux {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelWarn)
	hc := NewHealthChecker(db, redis, "test-v1.0.0", logger)
	mux := http.NewServeMux()
	hc.RegisterRoutes(mux)
	return mux
}

// decodeJSON is a test helper that decodes the recorder body into map[string]any.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	return out
}

// ── /health ──────────────────────────────────────────────────────────────────

func TestHealthEndpoint_Always200(t *testing.T) {
	mux := newTestChecker(healthy, healthy)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	body := decodeJSON(t, rec)
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if body["version"] != "test-v1.0.0" {
		t.Errorf("version = %v, want test-v1.0.0", body["version"])
	}
}

// ── /health/live ─────────────────────────────────────────────────────────────

// TestHealthLiveEndpoint_Always200 verifies that /health/live returns 200 even
// when both dependency pingers report failures. Liveness must not check deps.
func TestHealthLiveEndpoint_Always200(t *testing.T) {
	mux := newTestChecker(unhealthy, unhealthy)
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
}

// ── /health/ready ─────────────────────────────────────────────────────────────

func TestHealthReadyEndpoint_200WhenHealthy(t *testing.T) {
	mux := newTestChecker(healthy, healthy)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks field missing or wrong type: %v", body["checks"])
	}
	if checks["postgres"] != "ok" {
		t.Errorf("checks.postgres = %v, want ok", checks["postgres"])
	}
	if checks["redis"] != "ok" {
		t.Errorf("checks.redis = %v, want ok", checks["redis"])
	}
}

func TestHealthReadyEndpoint_503WhenDBDown(t *testing.T) {
	mux := newTestChecker(unhealthy, healthy)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["status"] != "degraded" {
		t.Errorf("status = %v, want degraded", body["status"])
	}
	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks field missing or wrong type: %v", body["checks"])
	}
	if checks["postgres"] == "ok" {
		t.Error("checks.postgres should be an error string, got ok")
	}
	if checks["redis"] != "ok" {
		t.Errorf("checks.redis = %v, want ok", checks["redis"])
	}
}

func TestHealthReadyEndpoint_503WhenRedisDown(t *testing.T) {
	mux := newTestChecker(healthy, unhealthy)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["status"] != "degraded" {
		t.Errorf("status = %v, want degraded", body["status"])
	}
	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks field missing or wrong type: %v", body["checks"])
	}
	if checks["postgres"] != "ok" {
		t.Errorf("checks.postgres = %v, want ok", checks["postgres"])
	}
	if checks["redis"] == "ok" {
		t.Error("checks.redis should be an error string, got ok")
	}
}

func TestHealthReadyEndpoint_503WhenBothDown(t *testing.T) {
	mux := newTestChecker(unhealthy, unhealthy)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["status"] != "degraded" {
		t.Errorf("status = %v, want degraded", body["status"])
	}
	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks field missing or wrong type: %v", body["checks"])
	}
	if checks["postgres"] == "ok" {
		t.Error("checks.postgres should be an error string, got ok")
	}
	if checks["redis"] == "ok" {
		t.Error("checks.redis should be an error string, got ok")
	}
}

// ── Method routing ────────────────────────────────────────────────────────────

// TestRegisterRoutes_MethodNotAllowed verifies that Go 1.22+ ServeMux returns
// 405 for non-GET requests on method-specific routes.
func TestRegisterRoutes_MethodNotAllowed(t *testing.T) {
	mux := newTestChecker(healthy, healthy)
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /health: status = %d, want 405", rec.Code)
	}
}
