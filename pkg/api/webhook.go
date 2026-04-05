package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/queue"
)

// JobTypeWebhookDelivery is the job type registered on the worker pool for
// processing outbound webhook HTTP calls.
const JobTypeWebhookDelivery = "webhook.delivery"

const (
	webhookDeliveryTimeout = 30 * time.Second
	signatureHeader        = "X-Moca-Signature-256"
	signaturePrefix        = "sha256="
	maxResponseBodyLog     = 1024 // bytes captured in delivery log
	webhookHookPriority    = 900  // runs after default (500) business logic
	webhookAppName         = "moca_webhooks"
)

// webhookEvents maps WebhookConfig.Event strings to document lifecycle events.
// Only these events trigger webhook dispatch.
var webhookEvents = map[string]document.DocEvent{
	"after_insert": document.EventAfterInsert,
	"on_update":    document.EventOnUpdate,
	"on_submit":    document.EventOnSubmit,
	"on_cancel":    document.EventOnCancel,
	"on_trash":     document.EventOnTrash,
	"after_delete": document.EventAfterDelete,
}

// webhookPayload is the JSON envelope sent to webhook receivers.
type webhookPayload struct {
	Event        string         `json:"event"`
	DocType      string         `json:"doctype"`
	DocumentName string         `json:"document_name"`
	Data         map[string]any `json:"data"`
	Timestamp    string         `json:"timestamp"`
	Site         string         `json:"site"`
}

// WebhookDispatcher enqueues webhook delivery jobs when document lifecycle
// events fire and processes those jobs in the background worker pool.
type WebhookDispatcher struct {
	producer *queue.Producer
	db       *orm.DBManager
	logger   *slog.Logger
}

// NewWebhookDispatcher creates a dispatcher that enqueues webhook delivery jobs
// via the given queue producer and writes delivery logs via the DB manager.
func NewWebhookDispatcher(producer *queue.Producer, db *orm.DBManager, logger *slog.Logger) *WebhookDispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookDispatcher{
		producer: producer,
		db:       db,
		logger:   logger,
	}
}

// RegisterHooks registers global document event hooks on the HookRegistry for
// every event that webhooks can trigger on. The hooks inspect each document's
// APIConfig.Webhooks and enqueue matching delivery jobs.
func (wd *WebhookDispatcher) RegisterHooks(registry *hooks.HookRegistry) {
	for eventName, docEvent := range webhookEvents {
		registry.RegisterGlobal(docEvent, hooks.PrioritizedHandler{
			Handler:  wd.makeHookHandler(eventName),
			AppName:  webhookAppName,
			Priority: webhookHookPriority,
		})
	}
	wd.logger.Info("webhook hooks registered",
		slog.Int("events", len(webhookEvents)),
	)
}

// makeHookHandler returns a DocEventHandler closure for the given event name.
// The closure inspects the document's MetaType webhooks and enqueues delivery
// jobs for each matching webhook. Enqueue failures are logged but never
// propagated to the document lifecycle (best-effort).
func (wd *WebhookDispatcher) makeHookHandler(eventName string) hooks.DocEventHandler {
	return func(ctx *document.DocContext, doc document.Document) error {
		mt := doc.Meta()
		if mt == nil || mt.APIConfig == nil || len(mt.APIConfig.Webhooks) == 0 {
			return nil
		}

		for i := range mt.APIConfig.Webhooks {
			wh := &mt.APIConfig.Webhooks[i]
			if wh.Event != eventName {
				continue
			}
			if !MatchFilters(doc, wh.Filters) {
				continue
			}

			jobID, err := generateWebhookJobID()
			if err != nil {
				wd.logger.Error("webhook: generate job ID failed",
					slog.String("error", err.Error()),
				)
				continue
			}

			// Gather document data for the payload.
			var docData map[string]any
			if dm, ok := doc.(*document.DynamicDoc); ok {
				docData = dm.AsMap()
			}

			siteName := ""
			dbSchema := ""
			if ctx.Site != nil {
				siteName = ctx.Site.Name
				dbSchema = ctx.Site.DBSchema
			}

			retryCount := wh.RetryCount
			if retryCount <= 0 {
				retryCount = 3
			}

			// Serialise custom headers for the job payload.
			var headersPayload map[string]any
			if len(wh.Headers) > 0 {
				headersPayload = make(map[string]any, len(wh.Headers))
				for k, v := range wh.Headers {
					headersPayload[k] = v
				}
			}

			payload := map[string]any{
				"url":           wh.URL,
				"method":        wh.Method,
				"secret":        wh.Secret,
				"headers":       headersPayload,
				"event":         eventName,
				"doctype":       mt.Name,
				"document_name": doc.Name(),
				"data":          docData,
				"site":          siteName,
				"db_schema":     dbSchema,
				"retry_count":   retryCount,
			}

			job := queue.Job{
				ID:         jobID,
				Site:       siteName,
				Type:       JobTypeWebhookDelivery,
				Payload:    payload,
				CreatedAt:  time.Now().UTC(),
				MaxRetries: retryCount,
				Timeout:    webhookDeliveryTimeout,
			}

			if _, err := wd.producer.Enqueue(ctx, siteName, queue.QueueCritical, job); err != nil {
				wd.logger.Error("webhook: enqueue failed (best-effort)",
					slog.String("url", wh.URL),
					slog.String("event", eventName),
					slog.String("doctype", mt.Name),
					slog.String("error", err.Error()),
				)
			}
		}

		return nil // always nil — webhooks must not block document lifecycle
	}
}

// DeliveryHandler is the queue.JobHandler that executes outbound webhook HTTP
// calls. Register it on the worker pool with Handle(JobTypeWebhookDelivery, ...).
func (wd *WebhookDispatcher) DeliveryHandler(ctx context.Context, job queue.Job) error {
	p := job.Payload

	url, _ := p["url"].(string)
	method, _ := p["method"].(string)
	secret, _ := p["secret"].(string)
	event, _ := p["event"].(string)
	doctype, _ := p["doctype"].(string)
	docName, _ := p["document_name"].(string)
	site, _ := p["site"].(string)
	dbSchema, _ := p["db_schema"].(string)
	data, _ := p["data"].(map[string]any)

	retryCount := 3
	if rc, ok := p["retry_count"].(float64); ok {
		retryCount = int(rc)
	}

	if method == "" {
		method = http.MethodPost
	}

	// Build the signed payload.
	body, err := buildWebhookPayload(event, doctype, docName, site, data, time.Now().UTC())
	if err != nil {
		wd.logger.Error("webhook: build payload failed",
			slog.String("url", url),
			slog.String("error", err.Error()),
		)
		return nil // do not retry on serialisation errors
	}

	signature := ""
	if secret != "" {
		signature = SignPayload(body, secret)
	}

	// Build HTTP request.
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		wd.logger.Error("webhook: create request failed",
			slog.String("url", url),
			slog.String("error", err.Error()),
		)
		return nil
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Moca-Webhook/1.0")
	if signature != "" {
		req.Header.Set(signatureHeader, signature)
	}

	// Apply custom headers from webhook config.
	if hdrs, ok := p["headers"].(map[string]any); ok {
		for k, v := range hdrs {
			if s, ok := v.(string); ok {
				req.Header.Set(k, s)
			}
		}
	}

	// Execute request.
	start := time.Now()
	client := &http.Client{Timeout: webhookDeliveryTimeout}
	resp, err := client.Do(req)
	durationMs := int(time.Since(start).Milliseconds())

	// Build log entry.
	entry := webhookLogEntry{
		Event:        event,
		URL:          url,
		DocType:      doctype,
		DocumentName: docName,
		Attempt:      job.Retries + 1,
		DurationMs:   durationMs,
		Timestamp:    time.Now().UTC(),
	}

	if err != nil {
		entry.ErrorMessage = err.Error()
		wd.logger.Warn("webhook: delivery failed",
			slog.String("url", url),
			slog.String("event", event),
			slog.Int("attempt", entry.Attempt),
			slog.String("error", err.Error()),
		)
	} else {
		defer func() { _ = resp.Body.Close() }()
		entry.StatusCode = resp.StatusCode
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyLog))
		entry.ResponseBody = string(respBody)
	}

	// Persist delivery log.
	wd.writeLog(ctx, site, dbSchema, entry)

	// Determine success (2xx status).
	success := err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300
	if success {
		wd.logger.Info("webhook: delivered",
			slog.String("url", url),
			slog.String("event", event),
			slog.Int("status", resp.StatusCode),
			slog.Int("duration_ms", durationMs),
		)
		return nil
	}

	// Retry if attempts remain.
	if job.Retries < retryCount {
		backoff := time.Duration(5*(1<<uint(job.Retries))) * time.Second
		runAfter := time.Now().UTC().Add(backoff)

		retryJob := queue.Job{
			ID:         job.ID + fmt.Sprintf("-r%d", job.Retries+1),
			Site:       job.Site,
			Type:       JobTypeWebhookDelivery,
			Payload:    job.Payload,
			CreatedAt:  time.Now().UTC(),
			MaxRetries: retryCount,
			Retries:    job.Retries + 1,
			Timeout:    webhookDeliveryTimeout,
		}

		if err := wd.producer.EnqueueDelayed(ctx, job.Site, queue.QueueCritical, retryJob, runAfter); err != nil {
			wd.logger.Error("webhook: retry enqueue failed",
				slog.String("url", url),
				slog.Int("attempt", entry.Attempt),
				slog.String("error", err.Error()),
			)
		} else {
			wd.logger.Info("webhook: retry scheduled",
				slog.String("url", url),
				slog.Int("next_attempt", job.Retries+2),
				slog.Duration("backoff", backoff),
			)
		}
	} else {
		wd.logger.Warn("webhook: retries exhausted",
			slog.String("url", url),
			slog.String("event", event),
			slog.Int("attempts", entry.Attempt),
		)
	}

	return nil // always ACK — retries are new delayed jobs
}

// ────────────────────────────────────────────────────────────────────────────
// Pure functions
// ────────────────────────────────────────────────────────────────────────────

// MatchFilters checks whether a document matches all webhook filter conditions.
// An empty or nil filter map always matches. Each filter entry requires the
// document field value to equal the filter value (AND logic).
func MatchFilters(doc document.Document, filters map[string]any) bool {
	if len(filters) == 0 {
		return true
	}
	for field, expected := range filters {
		actual := doc.Get(field)
		if fmt.Sprintf("%v", actual) != fmt.Sprintf("%v", expected) {
			return false
		}
	}
	return true
}

// SignPayload computes an HMAC-SHA256 signature over the payload using the
// given secret and returns it in the format "sha256=<hex>".
func SignPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// buildWebhookPayload constructs the canonical JSON payload for a webhook delivery.
func buildWebhookPayload(event, doctype, docName, site string, data map[string]any, ts time.Time) ([]byte, error) {
	p := webhookPayload{
		Event:        event,
		DocType:      doctype,
		DocumentName: docName,
		Data:         data,
		Timestamp:    ts.Format(time.RFC3339),
		Site:         site,
	}
	return json.Marshal(p)
}

// ────────────────────────────────────────────────────────────────────────────
// Delivery logging
// ────────────────────────────────────────────────────────────────────────────

type webhookLogEntry struct {
	Timestamp    time.Time
	Event        string
	URL          string
	DocType      string
	DocumentName string
	ResponseBody string
	ErrorMessage string
	StatusCode   int
	DurationMs   int
	Attempt      int
}

// writeLog persists a delivery log entry to tab_webhook_log. Errors are logged
// but never propagated.
func (wd *WebhookDispatcher) writeLog(ctx context.Context, site, dbSchema string, entry webhookLogEntry) {
	if wd.db == nil || site == "" {
		return
	}

	pool, err := wd.db.ForSite(ctx, site)
	if err != nil {
		wd.logger.Error("webhook: get site pool for log",
			slog.String("site", site),
			slog.String("error", err.Error()),
		)
		return
	}

	if err := logDelivery(ctx, pool, dbSchema, entry); err != nil {
		wd.logger.Error("webhook: write delivery log",
			slog.String("site", site),
			slog.String("error", err.Error()),
		)
	}
}

func logDelivery(ctx context.Context, pool *pgxpool.Pool, schema string, entry webhookLogEntry) error {
	logID, err := generateWebhookJobID()
	if err != nil {
		return fmt.Errorf("generate log id: %w", err)
	}

	table := pgx.Identifier{schema, "tab_webhook_log"}.Sanitize()
	query := fmt.Sprintf(`INSERT INTO %s
		("name", "webhook_event", "webhook_url", "doctype", "document_name",
		 "status_code", "response_body", "duration_ms", "attempt", "error_message", "created_at")
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, table)

	_, err = pool.Exec(ctx, query,
		logID,
		entry.Event,
		entry.URL,
		entry.DocType,
		entry.DocumentName,
		entry.StatusCode,
		truncateString(entry.ResponseBody, maxResponseBodyLog),
		entry.DurationMs,
		entry.Attempt,
		entry.ErrorMessage,
		entry.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("insert webhook log: %w", err)
	}
	return nil
}

// EnsureWebhookLogTable creates the tab_webhook_log table if it does not exist.
// This is the fallback for sites that already exist; new sites get the table
// via GenerateSystemTablesDDL.
func EnsureWebhookLogTable(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	table := pgx.Identifier{schema, "tab_webhook_log"}.Sanitize()

	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	"name"            TEXT PRIMARY KEY,
	"webhook_event"   TEXT NOT NULL,
	"webhook_url"     TEXT NOT NULL,
	"doctype"         TEXT NOT NULL,
	"document_name"   TEXT NOT NULL,
	"status_code"     INTEGER,
	"response_body"   TEXT,
	"duration_ms"     INTEGER,
	"attempt"         INTEGER NOT NULL DEFAULT 1,
	"error_message"   TEXT,
	"created_at"      TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, table)
	if _, err := pool.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("create tab_webhook_log: %w", err)
	}

	idxDoctype := fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s ON %s ("doctype", "document_name")`,
		pgx.Identifier{schema, "idx_webhook_log_doctype"}.Sanitize(), table,
	)
	if _, err := pool.Exec(ctx, idxDoctype); err != nil {
		return fmt.Errorf("create idx_webhook_log_doctype: %w", err)
	}

	idxEvent := fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s ON %s ("webhook_event")`,
		pgx.Identifier{schema, "idx_webhook_log_event"}.Sanitize(), table,
	)
	if _, err := pool.Exec(ctx, idxEvent); err != nil {
		return fmt.Errorf("create idx_webhook_log_event: %w", err)
	}

	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

func generateWebhookJobID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// Ensure unused import suppression for meta package used indirectly via MetaType.
var _ = (*meta.WebhookConfig)(nil)
