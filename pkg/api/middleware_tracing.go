package api

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/osama1998H/moca/pkg/observe"
)

// tracingMiddleware creates OpenTelemetry spans for each HTTP request.
// It records HTTP method, route pattern, site context, and response status.
// Place it after requestIDMiddleware in the chain so request IDs are available.
func tracingMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tracer := observe.Tracer("moca.http")

			// Use the registered route pattern (Go 1.22+) when available for
			// the span name; fall back to the raw URL path.
			spanName := r.Method + " " + r.URL.Path

			ctx, span := tracer.Start(r.Context(), spanName,
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.String()),
				))
			defer span.End()

			// Add site context if already resolved by upstream middleware.
			if site := SiteFromContext(ctx); site != nil {
				span.SetAttributes(attribute.String("moca.site", site.Name))
			}

			// Wrap the writer to capture the response status code.
			sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r.WithContext(ctx))

			// After handler execution, enrich the span with the route pattern
			// (populated by the mux after routing) and the status code.
			if pattern := r.Pattern; pattern != "" {
				span.SetAttributes(attribute.String("http.route", pattern))
			}
			span.SetAttributes(attribute.Int("http.status_code", sw.status))
			if sw.status >= 400 {
				span.SetStatus(codes.Error, http.StatusText(sw.status))
			}
		})
	}
}
