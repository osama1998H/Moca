package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/queue"
)

// ── MatchFilters tests ───────────────────────────────────────────────────────

func TestMatchFilters(t *testing.T) {
	mt := &meta.MetaType{
		Name: "SalesOrder",
		Fields: []meta.FieldDef{
			{Name: "status", FieldType: meta.FieldTypeData},
			{Name: "amount", FieldType: meta.FieldTypeFloat},
			{Name: "customer", FieldType: meta.FieldTypeData},
		},
	}
	doc := document.NewDynamicDoc(mt, nil, true)
	_ = doc.Set("name", "SO-0001")
	_ = doc.Set("status", "Draft")
	_ = doc.Set("amount", 100.5)
	_ = doc.Set("customer", "Acme Corp")

	tests := []struct {
		filters map[string]any
		name    string
		want    bool
	}{
		{
			name:    "nil filters match",
			filters: nil,
			want:    true,
		},
		{
			name:    "empty filters match",
			filters: map[string]any{},
			want:    true,
		},
		{
			name:    "single field match",
			filters: map[string]any{"status": "Draft"},
			want:    true,
		},
		{
			name:    "single field mismatch",
			filters: map[string]any{"status": "Submitted"},
			want:    false,
		},
		{
			name:    "multi-field AND all match",
			filters: map[string]any{"status": "Draft", "customer": "Acme Corp"},
			want:    true,
		},
		{
			name:    "multi-field AND partial mismatch",
			filters: map[string]any{"status": "Draft", "customer": "Other"},
			want:    false,
		},
		{
			name:    "missing field returns false",
			filters: map[string]any{"nonexistent": "value"},
			want:    false,
		},
		{
			name:    "cross-type numeric comparison (float64 vs float64)",
			filters: map[string]any{"amount": 100.5},
			want:    true,
		},
		{
			name:    "cross-type numeric mismatch",
			filters: map[string]any{"amount": 200},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchFilters(doc, tt.filters)
			if got != tt.want {
				t.Errorf("MatchFilters() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── SignPayload tests ────────────────────────────────────────────────────────

func TestSignPayload(t *testing.T) {
	payload := []byte(`{"event":"after_insert","doctype":"SalesOrder"}`)
	secret := "test-secret-key"

	sig := SignPayload(payload, secret)

	// Must start with prefix.
	if sig[:7] != signaturePrefix {
		t.Fatalf("signature should start with %q, got %q", signaturePrefix, sig[:7])
	}

	// Verify the HMAC independently.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := signaturePrefix + hex.EncodeToString(mac.Sum(nil))
	if sig != expected {
		t.Errorf("SignPayload() = %q, want %q", sig, expected)
	}
}

func TestSignPayload_EmptySecret(t *testing.T) {
	sig := SignPayload([]byte("test"), "")
	if sig[:7] != signaturePrefix {
		t.Fatalf("empty secret should still produce valid signature format")
	}
}

// ── buildWebhookPayload tests ────────────────────────────────────────────────

func TestBuildWebhookPayload(t *testing.T) {
	ts := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	data := map[string]any{"name": "SO-0001", "status": "Draft"}

	body, err := buildWebhookPayload("after_insert", "SalesOrder", "SO-0001", "acme.localhost", data, ts)
	if err != nil {
		t.Fatalf("buildWebhookPayload() error = %v", err)
	}

	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.Event != "after_insert" {
		t.Errorf("Event = %q, want %q", payload.Event, "after_insert")
	}
	if payload.DocType != "SalesOrder" {
		t.Errorf("DocType = %q, want %q", payload.DocType, "SalesOrder")
	}
	if payload.DocumentName != "SO-0001" {
		t.Errorf("DocumentName = %q, want %q", payload.DocumentName, "SO-0001")
	}
	if payload.Site != "acme.localhost" {
		t.Errorf("Site = %q, want %q", payload.Site, "acme.localhost")
	}
	if payload.Timestamp != "2026-04-04T12:00:00Z" {
		t.Errorf("Timestamp = %q, want RFC3339", payload.Timestamp)
	}
	if payload.Data["name"] != "SO-0001" {
		t.Errorf("Data[name] = %v, want SO-0001", payload.Data["name"])
	}
}

// ── DeliveryHandler success test ─────────────────────────────────────────────

func TestDeliveryHandler_Success(t *testing.T) {
	var capturedBody []byte
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	producer := queue.NewProducer(rdb, slog.Default())
	wd := NewWebhookDispatcher(producer, nil, slog.Default())

	job := queue.Job{
		ID:   "test-job-1",
		Site: "acme.localhost",
		Type: JobTypeWebhookDelivery,
		Payload: map[string]any{
			"url":           srv.URL,
			"method":        "POST",
			"secret":        "my-secret",
			"headers":       map[string]any{"X-Custom": "value1"},
			"event":         "after_insert",
			"doctype":       "SalesOrder",
			"document_name": "SO-0001",
			"data":          map[string]any{"name": "SO-0001", "status": "Draft"},
			"site":          "acme.localhost",
			"db_schema":     "acme_localhost",
			"retry_count":   float64(3),
		},
		CreatedAt:  time.Now().UTC(),
		MaxRetries: 3,
		Timeout:    30 * time.Second,
	}

	err := wd.DeliveryHandler(context.Background(), job)
	if err != nil {
		t.Fatalf("DeliveryHandler() error = %v", err)
	}

	// Verify HMAC signature.
	sig := capturedHeaders.Get(signatureHeader)
	if sig == "" {
		t.Fatal("missing X-Moca-Signature-256 header")
	}
	expectedSig := SignPayload(capturedBody, "my-secret")
	if sig != expectedSig {
		t.Errorf("signature = %q, want %q", sig, expectedSig)
	}

	// Verify custom header.
	if capturedHeaders.Get("X-Custom") != "value1" {
		t.Errorf("X-Custom header = %q, want %q", capturedHeaders.Get("X-Custom"), "value1")
	}

	// Verify Content-Type.
	if capturedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedHeaders.Get("Content-Type"))
	}

	// Verify payload structure.
	var payload webhookPayload
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("unmarshal captured body: %v", err)
	}
	if payload.Event != "after_insert" {
		t.Errorf("payload.Event = %q, want after_insert", payload.Event)
	}
	if payload.DocType != "SalesOrder" {
		t.Errorf("payload.DocType = %q, want SalesOrder", payload.DocType)
	}
}

// ── DeliveryHandler retry test ───────────────────────────────────────────────

func TestDeliveryHandler_Retry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	producer := queue.NewProducer(rdb, slog.Default())
	wd := NewWebhookDispatcher(producer, nil, slog.Default())

	job := queue.Job{
		ID:   "test-retry-1",
		Site: "acme.localhost",
		Type: JobTypeWebhookDelivery,
		Payload: map[string]any{
			"url":           srv.URL,
			"method":        "POST",
			"secret":        "",
			"event":         "after_insert",
			"doctype":       "SalesOrder",
			"document_name": "SO-0001",
			"data":          map[string]any{},
			"site":          "acme.localhost",
			"db_schema":     "acme_localhost",
			"retry_count":   float64(3),
		},
		CreatedAt:  time.Now().UTC(),
		MaxRetries: 3,
		Retries:    0,
		Timeout:    30 * time.Second,
	}

	err := wd.DeliveryHandler(context.Background(), job)
	if err != nil {
		t.Fatalf("DeliveryHandler() should return nil (ACK), got %v", err)
	}

	// Verify a delayed retry job was enqueued (in the delayed sorted set).
	delayedKey := queue.DelayedKey("acme.localhost")
	members, err := rdb.ZRange(context.Background(), delayedKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRANGE error: %v", err)
	}
	if len(members) == 0 {
		t.Fatal("expected delayed retry job to be enqueued")
	}

	// Verify the retry job has incremented retries.
	var entry struct {
		Job queue.Job `json:"job"`
	}
	if err := json.Unmarshal([]byte(members[0]), &entry); err != nil {
		t.Fatalf("unmarshal delayed entry: %v", err)
	}
	if entry.Job.Retries != 1 {
		t.Errorf("retry job Retries = %d, want 1", entry.Job.Retries)
	}
}

// ── DeliveryHandler exhausted retries ────────────────────────────────────────

func TestDeliveryHandler_ExhaustedRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	producer := queue.NewProducer(rdb, slog.Default())
	wd := NewWebhookDispatcher(producer, nil, slog.Default())

	job := queue.Job{
		ID:   "test-exhausted-1",
		Site: "acme.localhost",
		Type: JobTypeWebhookDelivery,
		Payload: map[string]any{
			"url":           srv.URL,
			"method":        "POST",
			"secret":        "",
			"event":         "after_insert",
			"doctype":       "SalesOrder",
			"document_name": "SO-0001",
			"data":          map[string]any{},
			"site":          "acme.localhost",
			"db_schema":     "acme_localhost",
			"retry_count":   float64(3),
		},
		CreatedAt:  time.Now().UTC(),
		MaxRetries: 3,
		Retries:    3, // already at max
		Timeout:    30 * time.Second,
	}

	err := wd.DeliveryHandler(context.Background(), job)
	if err != nil {
		t.Fatalf("DeliveryHandler() should return nil, got %v", err)
	}

	// No delayed retry should be enqueued.
	delayedKey := queue.DelayedKey("acme.localhost")
	count, err := rdb.ZCard(context.Background(), delayedKey).Result()
	if err != nil {
		t.Fatalf("ZCARD error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 delayed jobs, got %d", count)
	}
}

// ── Hook handler enqueue test ────────────────────────────────────────────────

func TestHookHandler_EnqueuesOnMatch(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	producer := queue.NewProducer(rdb, slog.Default())
	wd := NewWebhookDispatcher(producer, nil, slog.Default())

	mt := &meta.MetaType{
		Name: "SalesOrder",
		Fields: []meta.FieldDef{
			{Name: "status", FieldType: meta.FieldTypeData},
		},
		APIConfig: &meta.APIConfig{
			Webhooks: []meta.WebhookConfig{
				{
					Event:      "after_insert",
					URL:        "https://example.com/hook",
					Method:     "POST",
					Secret:     "s3cret",
					RetryCount: 3,
				},
			},
		},
	}
	doc := document.NewDynamicDoc(mt, nil, true)
	_ = doc.Set("name", "SO-0001")
	_ = doc.Set("status", "Draft")

	ctx := document.NewDocContext(context.Background(), nil, nil)

	handler := wd.makeHookHandler("after_insert")
	err := handler(ctx, doc)
	if err != nil {
		t.Fatalf("hook handler returned error: %v", err)
	}

	// Verify job was enqueued to critical stream.
	streamKey := queue.StreamKey("", queue.QueueCritical)
	msgs, err := rdb.XRange(context.Background(), streamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRANGE error: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected webhook job to be enqueued in critical stream")
	}

	// Verify the job type.
	jobType, ok := msgs[0].Values["type"].(string)
	if !ok || jobType != JobTypeWebhookDelivery {
		t.Errorf("job type = %q, want %q", jobType, JobTypeWebhookDelivery)
	}
}

// ── Hook handler filter mismatch ─────────────────────────────────────────────

func TestHookHandler_SkipsOnFilterMismatch(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	producer := queue.NewProducer(rdb, slog.Default())
	wd := NewWebhookDispatcher(producer, nil, slog.Default())

	mt := &meta.MetaType{
		Name: "SalesOrder",
		Fields: []meta.FieldDef{
			{Name: "status", FieldType: meta.FieldTypeData},
		},
		APIConfig: &meta.APIConfig{
			Webhooks: []meta.WebhookConfig{
				{
					Event:      "after_insert",
					URL:        "https://example.com/hook",
					Method:     "POST",
					Filters:    map[string]any{"status": "Submitted"},
					RetryCount: 3,
				},
			},
		},
	}
	doc := document.NewDynamicDoc(mt, nil, true)
	_ = doc.Set("name", "SO-0001")
	_ = doc.Set("status", "Draft") // Does NOT match filter "Submitted"

	ctx := document.NewDocContext(context.Background(), nil, nil)

	handler := wd.makeHookHandler("after_insert")
	err := handler(ctx, doc)
	if err != nil {
		t.Fatalf("hook handler returned error: %v", err)
	}

	// No job should be enqueued.
	streamKey := queue.StreamKey("", queue.QueueCritical)
	msgs, err := rdb.XRange(context.Background(), streamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRANGE error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(msgs))
	}
}

// ── Hook handler best-effort on Redis failure ────────────────────────────────

func TestHookHandler_BestEffortOnRedisFailure(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	// Close Redis to simulate failure.
	mr.Close()

	producer := queue.NewProducer(rdb, slog.Default())
	wd := NewWebhookDispatcher(producer, nil, slog.Default())

	mt := &meta.MetaType{
		Name: "SalesOrder",
		Fields: []meta.FieldDef{
			{Name: "status", FieldType: meta.FieldTypeData},
		},
		APIConfig: &meta.APIConfig{
			Webhooks: []meta.WebhookConfig{
				{
					Event:      "after_insert",
					URL:        "https://example.com/hook",
					Method:     "POST",
					RetryCount: 3,
				},
			},
		},
	}
	doc := document.NewDynamicDoc(mt, nil, true)
	_ = doc.Set("name", "SO-0001")
	_ = doc.Set("status", "Draft")

	ctx := document.NewDocContext(context.Background(), nil, nil)

	handler := wd.makeHookHandler("after_insert")
	err := handler(ctx, doc)

	// Handler must return nil even when Redis is down — best-effort.
	if err != nil {
		t.Fatalf("hook handler should return nil on Redis failure, got %v", err)
	}
}

// ── RegisterHooks test ───────────────────────────────────────────────────────

func TestRegisterHooks(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	producer := queue.NewProducer(rdb, slog.Default())
	wd := NewWebhookDispatcher(producer, nil, slog.Default())

	registry := hooks.NewHookRegistry()
	wd.RegisterHooks(registry)

	// Verify handlers are registered for all 6 webhook events.
	for eventName, docEvent := range webhookEvents {
		handlers, err := registry.Resolve("AnyDocType", docEvent)
		if err != nil {
			t.Errorf("Resolve(%q) error: %v", eventName, err)
			continue
		}
		if len(handlers) == 0 {
			t.Errorf("no handlers registered for event %q", eventName)
		}
	}
}

// ── EnsureWebhookLogTable idempotency (no real DB, just verify DDL) ──────────

func TestWebhookEvents_AllMapped(t *testing.T) {
	// Verify all 6 expected events are in the map.
	expected := []string{"after_insert", "on_update", "on_submit", "on_cancel", "on_trash", "after_delete"}
	for _, e := range expected {
		if _, ok := webhookEvents[e]; !ok {
			t.Errorf("missing webhook event mapping for %q", e)
		}
	}
	if len(webhookEvents) != 6 {
		t.Errorf("webhookEvents has %d entries, want 6", len(webhookEvents))
	}
}
