package observe

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewMetricsCollector_NoPanic(t *testing.T) {
	mc := NewMetricsCollector()
	if mc == nil {
		t.Fatal("expected non-nil MetricsCollector")
	}
}

func TestNewMetricsCollector_AllMetricFamiliesPresent(t *testing.T) {
	mc := NewMetricsCollector()
	families, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	// Build a set of gathered metric names.
	got := make(map[string]bool, len(families))
	for _, f := range families {
		got[f.GetName()] = true
	}

	// All 13 metric names must be present (even if no observations yet,
	// Gather returns the descriptors for counters/gauges/histograms that
	// have been registered — though some may not appear until they have
	// at least one observation). We initialise one label set each to
	// guarantee presence.
	mc.HTTPRequestsTotal.WithLabelValues("s", "GET", "/", "200").Inc()
	mc.HTTPRequestDuration.WithLabelValues("s", "GET", "/").Observe(0.01)
	mc.DocOpsTotal.WithLabelValues("s", "Task", "create").Inc()
	mc.DocOpDuration.WithLabelValues("s", "Task", "create").Observe(0.01)
	mc.CacheHitsTotal.WithLabelValues("s", "metadata").Inc()
	mc.CacheMissesTotal.WithLabelValues("s", "metadata").Inc()
	mc.QueueJobsTotal.WithLabelValues("s", "default", "completed").Inc()
	mc.QueueJobDuration.WithLabelValues("s", "default", "email").Observe(0.01)
	mc.KafkaEventsPublished.WithLabelValues("events").Inc()
	mc.KafkaConsumerLag.WithLabelValues("events", "cg1").Set(5)
	mc.ActiveWSConnections.WithLabelValues("s").Set(1)
	mc.DBQueryDuration.WithLabelValues("s", "select").Observe(0.01)
	mc.DBPoolActive.WithLabelValues("s").Set(3)

	families, err = mc.Registry().Gather()
	if err != nil {
		t.Fatalf("gather failed after observations: %v", err)
	}
	got = make(map[string]bool, len(families))
	for _, f := range families {
		got[f.GetName()] = true
	}

	want := []string{
		"moca_http_requests_total",
		"moca_http_request_duration_seconds",
		"moca_doc_ops_total",
		"moca_doc_op_duration_seconds",
		"moca_cache_hits_total",
		"moca_cache_misses_total",
		"moca_queue_jobs_total",
		"moca_queue_job_duration_seconds",
		"moca_kafka_events_published_total",
		"moca_kafka_consumer_lag",
		"moca_active_ws_connections",
		"moca_db_query_duration_seconds",
		"moca_db_pool_active_connections",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("metric family %q not found in gathered output", name)
		}
	}
}

func TestMetricsCollector_Handler_ReturnsPrometheusText(t *testing.T) {
	mc := NewMetricsCollector()
	// Record one observation so the output is non-empty.
	mc.HTTPRequestsTotal.WithLabelValues("acme", "GET", "/api/v1/resource/Task", "200").Inc()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	mc.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	body, _ := io.ReadAll(rec.Body)
	bodyStr := string(body)

	// Prometheus text format contains HELP and TYPE lines.
	if !strings.Contains(bodyStr, "moca_http_requests_total") {
		t.Error("expected moca_http_requests_total in metrics output")
	}
	if !strings.Contains(bodyStr, "HELP") {
		t.Error("expected HELP line in Prometheus text output")
	}
	if !strings.Contains(bodyStr, "TYPE") {
		t.Error("expected TYPE line in Prometheus text output")
	}
}

func TestMetricsCollector_CounterIncrement(t *testing.T) {
	mc := NewMetricsCollector()

	// Increment a counter and verify the value changed.
	mc.HTTPRequestsTotal.WithLabelValues("acme", "POST", "/api/v1/resource/Task", "201").Inc()
	mc.HTTPRequestsTotal.WithLabelValues("acme", "POST", "/api/v1/resource/Task", "201").Inc()
	mc.HTTPRequestsTotal.WithLabelValues("acme", "POST", "/api/v1/resource/Task", "201").Inc()

	families, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	for _, f := range families {
		if f.GetName() != "moca_http_requests_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			// Find the metric with status=201.
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "status" && lp.GetValue() == "201" {
					if got := m.GetCounter().GetValue(); got != 3 {
						t.Errorf("counter value = %v, want 3", got)
					}
					return
				}
			}
		}
	}
	t.Error("moca_http_requests_total{status=201} not found in gathered metrics")
}

func TestMetricsCollector_CustomRegistryIsolation(t *testing.T) {
	// Two collectors should not interfere with each other.
	mc1 := NewMetricsCollector()
	mc2 := NewMetricsCollector()

	mc1.HTTPRequestsTotal.WithLabelValues("site1", "GET", "/", "200").Inc()

	// mc2 should have zero observations.
	families, err := mc2.Registry().Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	for _, f := range families {
		if f.GetName() == "moca_http_requests_total" {
			t.Error("mc2 should have no moca_http_requests_total observations")
		}
	}

	// mc1 should have the observation.
	families, err = mc1.Registry().Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	found := false
	for _, f := range families {
		if f.GetName() == "moca_http_requests_total" {
			found = true
			break
		}
	}
	if !found {
		t.Error("mc1 should have moca_http_requests_total observations")
	}
}
