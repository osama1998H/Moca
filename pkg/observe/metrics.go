package observe

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// defaultBuckets defines the histogram bucket boundaries (in seconds) used for
// all duration metrics. The range covers sub-millisecond cache hits through
// multi-second report queries.
var defaultBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// MetricsCollector holds all Prometheus metrics for the Moca framework.
// It uses a custom prometheus.Registry to avoid polluting the global default
// registry, making tests deterministic and allowing multiple collectors in
// the same process (e.g. during integration tests).
type MetricsCollector struct {
	// HTTPRequestsTotal counts completed HTTP requests by site, method, path pattern, and status code.
	HTTPRequestsTotal *prometheus.CounterVec
	// HTTPRequestDuration records HTTP request latencies in seconds.
	HTTPRequestDuration *prometheus.HistogramVec

	// DocOpsTotal counts document operations (create, read, update, delete, submit, cancel, amend).
	DocOpsTotal *prometheus.CounterVec
	// DocOpDuration records document operation latencies in seconds.
	DocOpDuration *prometheus.HistogramVec

	// CacheHitsTotal counts cache hits by site and cache type (e.g. "metadata", "document").
	CacheHitsTotal *prometheus.CounterVec
	// CacheMissesTotal counts cache misses by site and cache type.
	CacheMissesTotal *prometheus.CounterVec

	// QueueJobsTotal counts background jobs by site, queue name, and status (enqueued, completed, failed, dlq).
	QueueJobsTotal *prometheus.CounterVec
	// QueueJobDuration records background job execution latencies in seconds.
	QueueJobDuration *prometheus.HistogramVec

	// KafkaEventsPublished counts events published to Kafka topics.
	KafkaEventsPublished *prometheus.CounterVec
	// KafkaConsumerLag tracks the lag of Kafka consumer groups per topic.
	KafkaConsumerLag *prometheus.GaugeVec

	// ActiveWSConnections tracks the number of active WebSocket connections per site.
	ActiveWSConnections *prometheus.GaugeVec

	// DBQueryDuration records database query latencies in seconds.
	DBQueryDuration *prometheus.HistogramVec
	// DBPoolActive tracks the number of active (in-use) database connections per site.
	DBPoolActive *prometheus.GaugeVec

	registry *prometheus.Registry
}

// NewMetricsCollector creates a MetricsCollector with all 13 metrics registered
// on a dedicated prometheus.Registry. The caller should use Handler() to expose
// the /metrics endpoint, and pass the collector to middleware and subsystems
// that record observations.
func NewMetricsCollector() *MetricsCollector {
	reg := prometheus.NewRegistry()

	mc := &MetricsCollector{
		registry: reg,

		HTTPRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "moca_http_requests_total",
			Help: "Total number of HTTP requests by site, method, path, and status code.",
		}, []string{"site", "method", "path", "status"}),

		HTTPRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "moca_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: defaultBuckets,
		}, []string{"site", "method", "path"}),

		DocOpsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "moca_doc_ops_total",
			Help: "Total number of document operations by site, doctype, and operation.",
		}, []string{"site", "doctype", "operation"}),

		DocOpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "moca_doc_op_duration_seconds",
			Help:    "Document operation latency in seconds.",
			Buckets: defaultBuckets,
		}, []string{"site", "doctype", "operation"}),

		CacheHitsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "moca_cache_hits_total",
			Help: "Total number of cache hits by site and cache type.",
		}, []string{"site", "cache_type"}),

		CacheMissesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "moca_cache_misses_total",
			Help: "Total number of cache misses by site and cache type.",
		}, []string{"site", "cache_type"}),

		QueueJobsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "moca_queue_jobs_total",
			Help: "Total number of background jobs by site, queue, and status.",
		}, []string{"site", "queue", "status"}),

		QueueJobDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "moca_queue_job_duration_seconds",
			Help:    "Background job execution latency in seconds.",
			Buckets: defaultBuckets,
		}, []string{"site", "queue", "job_type"}),

		KafkaEventsPublished: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "moca_kafka_events_published_total",
			Help: "Total number of events published to Kafka by topic.",
		}, []string{"topic"}),

		KafkaConsumerLag: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "moca_kafka_consumer_lag",
			Help: "Kafka consumer group lag by topic and consumer group.",
		}, []string{"topic", "consumer_group"}),

		ActiveWSConnections: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "moca_active_ws_connections",
			Help: "Number of active WebSocket connections per site.",
		}, []string{"site"}),

		DBQueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "moca_db_query_duration_seconds",
			Help:    "Database query latency in seconds.",
			Buckets: defaultBuckets,
		}, []string{"site", "operation"}),

		DBPoolActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "moca_db_pool_active_connections",
			Help: "Number of active (in-use) database connections per site.",
		}, []string{"site"}),
	}

	// Register all collectors on the custom registry.
	reg.MustRegister(
		mc.HTTPRequestsTotal,
		mc.HTTPRequestDuration,
		mc.DocOpsTotal,
		mc.DocOpDuration,
		mc.CacheHitsTotal,
		mc.CacheMissesTotal,
		mc.QueueJobsTotal,
		mc.QueueJobDuration,
		mc.KafkaEventsPublished,
		mc.KafkaConsumerLag,
		mc.ActiveWSConnections,
		mc.DBQueryDuration,
		mc.DBPoolActive,
	)

	return mc
}

// Handler returns an http.Handler that serves the Prometheus metrics endpoint.
// It uses the collector's custom registry, so only Moca metrics are exposed
// (no default Go process metrics from the global registry).
func (mc *MetricsCollector) Handler() http.Handler {
	return promhttp.HandlerFor(mc.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// Registry returns the underlying prometheus.Registry for advanced use cases
// such as gathering metrics in tests.
func (mc *MetricsCollector) Registry() *prometheus.Registry {
	return mc.registry
}
