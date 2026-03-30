// Package observe implements observability for MOCA.
//
// All MOCA binaries expose Prometheus metrics, emit OpenTelemetry traces,
// and use structured JSON logging. Health check endpoints are provided for
// each binary to integrate with load balancers and orchestrators.
//
// Key components:
//   - Metrics: Prometheus metric registration and exposition (/metrics)
//   - Tracing: OpenTelemetry trace provider setup and span helpers
//   - Logging: structured JSON logger with request-scoped fields
//   - Health: /healthz and /readyz endpoint handlers
package observe
