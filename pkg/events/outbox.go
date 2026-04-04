package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/osama1998H/moca/pkg/orm"
)

const (
	DefaultOutboxPollInterval = 100 * time.Millisecond
	DefaultOutboxBatchSize    = 100
	DefaultOutboxMaxRetries   = 3
)

// ActiveSiteLister provides the active tenant names to poll.
type ActiveSiteLister interface {
	ListActiveSites(ctx context.Context) ([]string, error)
}

// OutboxRow is the DB-backed representation of an outbox entry.
//
//nolint:govet // Field order mirrors the persisted outbox record shape.
type OutboxRow struct {
	ID           int64
	RetryCount   int
	Payload      []byte
	EventType    string
	Topic        string
	PartitionKey string
	Status       string
}

// OutboxStore encapsulates the tenant-aware persistence operations used by the
// poller so retry transitions can be tested without a database.
type OutboxStore interface {
	FetchPending(ctx context.Context, site string, limit int) ([]OutboxRow, error)
	MarkPublished(ctx context.Context, site string, ids []int64, publishedAt time.Time) error
	RecordFailure(ctx context.Context, site string, id int64, retryCount int, failed bool) error
}

// AfterPublishHook runs only after the event has been published successfully.
type AfterPublishHook func(ctx context.Context, event DocumentEvent) error

type normalizedOutboxRow struct {
	Topic string
	Event DocumentEvent
}

// OutboxPollerConfig configures the polling loop.
type OutboxPollerConfig struct {
	Store        OutboxStore
	Sites        ActiveSiteLister
	Producer     Producer
	Logger       *slog.Logger
	AfterPublish AfterPublishHook
	PollInterval time.Duration
	BatchSize    int
	MaxRetries   int
}

// OutboxPoller reads committed outbox rows, publishes them, and advances their
// lifecycle state to published or failed.
type OutboxPoller struct {
	store        OutboxStore
	sites        ActiveSiteLister
	producer     Producer
	logger       *slog.Logger
	afterPublish AfterPublishHook
	pollInterval time.Duration
	batchSize    int
	maxRetries   int
}

// NewOutboxPoller constructs an outbox poller with sensible defaults.
func NewOutboxPoller(cfg OutboxPollerConfig) (*OutboxPoller, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("events: outbox poller requires store")
	}
	if cfg.Sites == nil {
		return nil, fmt.Errorf("events: outbox poller requires site lister")
	}
	if cfg.Producer == nil {
		return nil, fmt.Errorf("events: outbox poller requires producer")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultOutboxPollInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultOutboxBatchSize
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultOutboxMaxRetries
	}

	return &OutboxPoller{
		store:        cfg.Store,
		sites:        cfg.Sites,
		producer:     cfg.Producer,
		logger:       cfg.Logger,
		pollInterval: cfg.PollInterval,
		batchSize:    cfg.BatchSize,
		maxRetries:   cfg.MaxRetries,
		afterPublish: cfg.AfterPublish,
	}, nil
}

// Run starts the polling loop until ctx is cancelled.
func (p *OutboxPoller) Run(ctx context.Context) error {
	if err := p.pollOnce(ctx); err != nil && ctx.Err() == nil {
		p.logger.Error("outbox initial poll failed", slog.String("error", err.Error()))
	}

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := p.pollOnce(ctx); err != nil && ctx.Err() == nil {
				p.logger.Error("outbox poll failed", slog.String("error", err.Error()))
			}
		}
	}
}

func (p *OutboxPoller) pollOnce(ctx context.Context) error {
	sites, err := p.sites.ListActiveSites(ctx)
	if err != nil {
		return fmt.Errorf("list active sites: %w", err)
	}
	for _, site := range sites {
		if err := p.processSite(ctx, site); err != nil {
			p.logger.Error("outbox site poll failed",
				slog.String("site", site),
				slog.String("error", err.Error()),
			)
		}
	}
	return nil
}

func (p *OutboxPoller) processSite(ctx context.Context, site string) error {
	rows, err := p.store.FetchPending(ctx, site, p.batchSize)
	if err != nil {
		return fmt.Errorf("fetch pending rows: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	var published []int64
	now := time.Now().UTC()

	for _, row := range rows {
		normalized, err := normalizeOutboxRow(site, row)
		if err != nil {
			p.recordFailure(ctx, site, row, err)
			continue
		}
		if err := p.producer.Publish(ctx, normalized.Topic, normalized.Event); err != nil {
			p.recordFailure(ctx, site, row, err)
			continue
		}
		if p.afterPublish != nil {
			if err := p.afterPublish(ctx, normalized.Event); err != nil {
				p.recordFailure(ctx, site, row, err)
				continue
			}
		}
		published = append(published, row.ID)
	}

	if len(published) == 0 {
		return nil
	}
	if err := p.store.MarkPublished(ctx, site, published, now); err != nil {
		return fmt.Errorf("mark published rows: %w", err)
	}
	return nil
}

func (p *OutboxPoller) recordFailure(ctx context.Context, site string, row OutboxRow, err error) {
	retryCount := row.RetryCount + 1
	failed := retryCount >= p.maxRetries
	if recErr := p.store.RecordFailure(ctx, site, row.ID, retryCount, failed); recErr != nil {
		p.logger.Error("outbox failure update failed",
			slog.String("site", site),
			slog.Int64("id", row.ID),
			slog.String("error", recErr.Error()),
		)
		return
	}
	p.logger.Warn("outbox publish failed",
		slog.String("site", site),
		slog.Int64("id", row.ID),
		slog.Int("retry_count", retryCount),
		slog.Bool("failed", failed),
		slog.String("error", err.Error()),
	)
}

func normalizeOutboxRow(site string, row OutboxRow) (normalizedOutboxRow, error) {
	if row.Topic == TopicDocumentEvents {
		return normalizeCanonicalRow(site, row)
	}
	return normalizeLegacyRow(site, row)
}

func normalizeCanonicalRow(site string, row OutboxRow) (normalizedOutboxRow, error) {
	var event DocumentEvent
	if err := json.Unmarshal(row.Payload, &event); err != nil {
		return normalizedOutboxRow{}, fmt.Errorf("unmarshal canonical payload: %w", err)
	}
	if event.EventType == "" {
		event.EventType = row.EventType
	}
	if event.Site == "" {
		event.Site = site
	}
	if event.DocType == "" && row.PartitionKey != "" {
		if _, doctype, ok := strings.Cut(row.PartitionKey, ":"); ok {
			event.DocType = doctype
		}
	}
	if err := EnsureDocumentEventDefaults(&event); err != nil {
		return normalizedOutboxRow{}, fmt.Errorf("normalize canonical event: %w", err)
	}
	return normalizedOutboxRow{Topic: TopicDocumentEvents, Event: event}, nil
}

func normalizeLegacyRow(site string, row OutboxRow) (normalizedOutboxRow, error) {
	var data any
	if len(row.Payload) > 0 {
		if err := json.Unmarshal(row.Payload, &data); err != nil {
			return normalizedOutboxRow{}, fmt.Errorf("unmarshal legacy payload: %w", err)
		}
	}
	docType := row.Topic
	docName := row.PartitionKey
	if m, ok := data.(map[string]any); ok {
		if v, ok := m["doctype"].(string); ok && v != "" {
			docType = v
		}
		if v, ok := m["name"].(string); ok && v != "" {
			docName = v
		}
	}

	event, err := NewDocumentEvent(
		mapLegacyEventType(row.EventType),
		site,
		docType,
		docName,
		"",
		"",
		data,
		nil,
	)
	if err != nil {
		return normalizedOutboxRow{}, err
	}
	if m, ok := data.(map[string]any); ok {
		if v, ok := m["user"].(string); ok && v != "" {
			event.User = v
		}
		if v, ok := m["request_id"].(string); ok && v != "" {
			event.RequestID = v
		}
		if prev, ok := m["prev_data"]; ok {
			event.PrevData = prev
		}
	}
	return normalizedOutboxRow{Topic: TopicDocumentEvents, Event: event}, nil
}

func mapLegacyEventType(eventType string) string {
	switch eventType {
	case "insert":
		return EventTypeDocCreated
	case "update":
		return EventTypeDocUpdated
	case "delete":
		return EventTypeDocDeleted
	default:
		return eventType
	}
}

// DBOutboxStore is the production store backed by tenant-scoped PostgreSQL.
type DBOutboxStore struct {
	db *orm.DBManager
}

func NewDBOutboxStore(db *orm.DBManager) *DBOutboxStore {
	return &DBOutboxStore{db: db}
}

func (s *DBOutboxStore) FetchPending(ctx context.Context, site string, limit int) ([]OutboxRow, error) {
	pool, err := s.db.ForSite(ctx, site)
	if err != nil {
		return nil, fmt.Errorf("resolve tenant pool: %w", err)
	}

	rows, err := pool.Query(ctx, `
SELECT
	"id",
	"event_type",
	"topic",
	COALESCE("partition_key", ''),
	"payload"::text,
	COALESCE("status", CASE WHEN "processed" THEN 'published' ELSE 'pending' END),
	COALESCE("retry_count", 0)
FROM tab_outbox
WHERE COALESCE("status", CASE WHEN "processed" THEN 'published' ELSE 'pending' END) = 'pending'
ORDER BY "created_at", "id"
LIMIT $1
`, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending rows: %w", err)
	}
	defer rows.Close()

	var result []OutboxRow
	for rows.Next() {
		var row OutboxRow
		var payload string
		if err := rows.Scan(
			&row.ID,
			&row.EventType,
			&row.Topic,
			&row.PartitionKey,
			&payload,
			&row.Status,
			&row.RetryCount,
		); err != nil {
			return nil, fmt.Errorf("scan pending row: %w", err)
		}
		row.Payload = []byte(payload)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending rows: %w", err)
	}
	return result, nil
}

func (s *DBOutboxStore) MarkPublished(ctx context.Context, site string, ids []int64, publishedAt time.Time) error {
	pool, err := s.db.ForSite(ctx, site)
	if err != nil {
		return fmt.Errorf("resolve tenant pool: %w", err)
	}
	_, err = pool.Exec(ctx, `
UPDATE tab_outbox
SET
	"status" = 'published',
	"published_at" = $1,
	"processed" = true
WHERE "id" = ANY($2)
`, publishedAt, ids)
	if err != nil {
		return fmt.Errorf("update published rows: %w", err)
	}
	return nil
}

func (s *DBOutboxStore) RecordFailure(ctx context.Context, site string, id int64, retryCount int, failed bool) error {
	pool, err := s.db.ForSite(ctx, site)
	if err != nil {
		return fmt.Errorf("resolve tenant pool: %w", err)
	}

	status := "pending"
	if failed {
		status = "failed"
	}

	_, err = pool.Exec(ctx, `
UPDATE tab_outbox
SET
	"retry_count" = $1,
	"status" = $2,
	"processed" = false
WHERE "id" = $3
`, retryCount, status, id)
	if err != nil {
		return fmt.Errorf("update failed row: %w", err)
	}
	return nil
}
