package api

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/osama1998H/moca/pkg/observe"
)

// statusRecorder wraps http.ResponseWriter to capture the HTTP status code
// written by downstream handlers. The zero value records status 200 (the
// implicit default when WriteHeader is never called).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader captures the status code and delegates to the wrapped writer.
func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker, delegating to the wrapped ResponseWriter.
// This is required for WebSocket upgrades when the writer is wrapped by
// metrics or tracing middleware.
func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := sr.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
	}
	return hijacker.Hijack()
}

// metricsMiddleware records HTTP request count and duration using the
// provided MetricsCollector. It captures the route pattern (Go 1.22+
// r.Pattern) for the path label to avoid cardinality explosion from
// dynamic path parameters.
//
// The middleware should be placed after auth (so user/site context is
// available) but before rate limiting in the chain.
func metricsMiddleware(mc *observe.MetricsCollector) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			duration := time.Since(start)

			// Use r.Pattern (Go 1.22+) for the path label. This contains the
			// registered route pattern (e.g. "GET /api/v1/resource/{doctype}/{name}")
			// rather than the actual URL path, preventing high-cardinality labels.
			pattern := r.Pattern
			if pattern == "" {
				pattern = r.URL.Path
			}

			// Extract the site name from context; empty string if not resolved yet.
			site := ""
			if sc := SiteFromContext(r.Context()); sc != nil {
				site = sc.Name
			}

			statusStr := strconv.Itoa(rec.status)

			mc.HTTPRequestsTotal.WithLabelValues(site, r.Method, pattern, statusStr).Inc()
			mc.HTTPRequestDuration.WithLabelValues(site, r.Method, pattern).Observe(duration.Seconds())
		})
	}
}
