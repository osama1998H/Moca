package observe

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// readyTimeout is the deadline given to each dependency ping in /health/ready.
const readyTimeout = 5 * time.Second

// Pinger is implemented by any dependency that can verify its own connectivity.
// *pgxpool.Pool and *drivers.RedisClients both satisfy this interface directly.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthChecker registers and serves the three health check endpoints.
// It is safe for concurrent use from multiple HTTP handler goroutines.
// Field order is chosen to minimise the GC pointer-scan region.
type HealthChecker struct {
	db      Pinger
	redis   Pinger
	logger  *slog.Logger
	version string
}

// NewHealthChecker creates a HealthChecker with the given dependency pingers,
// binary version string, and structured logger.
func NewHealthChecker(db, redis Pinger, version string, logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		db:      db,
		redis:   redis,
		version: version,
		logger:  logger,
	}
}

// RegisterRoutes registers the three health endpoints on mux.
// Uses Go 1.26+ method+path routing patterns.
//
//	GET /health       — always 200; confirms the process is running
//	GET /health/live  — always 200; Kubernetes liveness probe
//	GET /health/ready — 200 when PG+Redis are reachable; 503 otherwise
func (hc *HealthChecker) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", hc.handleHealth)
	mux.HandleFunc("GET /health/live", hc.handleLive)
	mux.HandleFunc("GET /health/ready", hc.handleReady)
}

// handleHealth returns 200 with a static status and the binary version.
// No dependency checks are performed — this endpoint proves the process is up.
func (hc *HealthChecker) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": hc.version,
	})
}

// handleLive returns 200 unconditionally.
// Used as a Kubernetes liveness probe; a failing liveness probe restarts the pod.
// Dependency checks are intentionally absent: a broken DB should not restart the pod.
func (hc *HealthChecker) handleLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady pings both PostgreSQL and Redis.
// Returns 200 with checks map when all dependencies are reachable.
// Returns 503 with the failed check details when any dependency is down.
// Used as a Kubernetes readiness probe; a failing readiness probe removes the
// pod from the load balancer without restarting it.
func (hc *HealthChecker) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()

	checks := make(map[string]string, 2)
	healthy := true

	if err := hc.db.Ping(ctx); err != nil {
		checks["postgres"] = err.Error()
		healthy = false
		hc.logger.Warn("readiness check failed",
			slog.String("component", "postgres"),
			slog.String("error", err.Error()),
		)
	} else {
		checks["postgres"] = "ok"
	}

	if err := hc.redis.Ping(ctx); err != nil {
		checks["redis"] = err.Error()
		healthy = false
		hc.logger.Warn("readiness check failed",
			slog.String("component", "redis"),
			slog.String("error", err.Error()),
		)
	} else {
		checks["redis"] = "ok"
	}

	status := http.StatusOK
	statusStr := "ok"
	if !healthy {
		status = http.StatusServiceUnavailable
		statusStr = "degraded"
	}

	writeJSON(w, status, map[string]any{
		"status": statusStr,
		"checks": checks,
	})
}

// writeJSON sets Content-Type, writes the HTTP status code, and encodes v as JSON.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck // ResponseWriter write errors are non-actionable
}
