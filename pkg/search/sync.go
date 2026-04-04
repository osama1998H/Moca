package search

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/queue"
)

const JobTypeSearchSync = "search.sync"

type MetaResolver interface {
	Get(ctx context.Context, site, doctype string) (*meta.MetaType, error)
}

type Syncer struct {
	indexer *Indexer
	meta    MetaResolver
	logger  *slog.Logger
	kafka   config.KafkaConfig
}

func NewSyncer(client *Client, metaResolver MetaResolver, kafkaCfg config.KafkaConfig, logger *slog.Logger) *Syncer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Syncer{
		indexer: NewIndexer(client),
		meta:    metaResolver,
		kafka:   kafkaCfg,
		logger:  logger,
	}
}

func (s *Syncer) HandleEvent(ctx context.Context, event events.DocumentEvent) error {
	if s == nil || s.indexer == nil || s.meta == nil {
		return ErrUnavailable
	}
	if err := events.EnsureDocumentEventDefaults(&event); err != nil {
		return fmt.Errorf("normalize event defaults: %w", err)
	}

	mt, err := s.meta.Get(ctx, event.Site, event.DocType)
	if err != nil {
		if err == meta.ErrMetaTypeNotFound {
			return nil
		}
		return fmt.Errorf("load doctype %q: %w", event.DocType, err)
	}
	if !hasSearchableFields(mt) {
		return nil
	}

	switch event.EventType {
	case events.EventTypeDocDeleted:
		return s.indexer.RemoveDocument(ctx, event.Site, event.DocType, event.DocName)
	default:
		doc, err := normalizeIndexedDocument(event)
		if err != nil {
			return err
		}
		return s.indexer.IndexDocuments(ctx, event.Site, mt, []map[string]any{doc})
	}
}

func (s *Syncer) JobHandler(ctx context.Context, job queue.Job) error {
	event, err := decodeEventFromPayload(job.Payload)
	if err != nil {
		return fmt.Errorf("decode search.sync job: %w", err)
	}
	return s.HandleEvent(ctx, event)
}

func (s *Syncer) RunKafka(ctx context.Context) error {
	if s == nil || s.indexer == nil || s.indexer.client == nil || !s.indexer.client.available() {
		return ErrUnavailable
	}
	if s.kafka.Enabled == nil || !*s.kafka.Enabled {
		return nil
	}
	if len(s.kafka.Brokers) == 0 {
		return fmt.Errorf("search sync: kafka enabled but no brokers configured")
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(s.kafka.Brokers...),
		kgo.ConsumerGroup("moca-search-sync"),
		kgo.ConsumeTopics(events.TopicDocumentEvents),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("create kafka consumer: %w", err)
	}
	defer client.Close()

	for {
		fetches := client.PollFetches(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, fetchErr := range errs {
				s.logger.Error("search sync fetch failed",
					slog.String("topic", fetchErr.Topic),
					slog.Int("partition", int(fetchErr.Partition)),
					slog.String("error", fetchErr.Err.Error()),
				)
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}

		fetches.EachRecord(func(record *kgo.Record) {
			var event events.DocumentEvent
			if err := json.Unmarshal(record.Value, &event); err != nil {
				s.logger.Error("search sync decode failed",
					slog.String("topic", record.Topic),
					slog.String("error", err.Error()),
				)
				if commitErr := client.CommitRecords(ctx, record); commitErr != nil && ctx.Err() == nil {
					s.logger.Error("search sync commit failed after decode error", slog.String("error", commitErr.Error()))
				}
				return
			}

			if err := s.HandleEvent(ctx, event); err != nil {
				s.logger.Error("search sync handle failed",
					slog.String("doctype", event.DocType),
					slog.String("docname", event.DocName),
					slog.String("error", err.Error()),
				)
				return
			}

			if err := client.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
				s.logger.Error("search sync commit failed",
					slog.String("doctype", event.DocType),
					slog.String("docname", event.DocName),
					slog.String("error", err.Error()),
				)
			}
		})
	}
}

func normalizeIndexedDocument(event events.DocumentEvent) (map[string]any, error) {
	document, err := anyToMap(event.Data)
	if err != nil {
		return nil, fmt.Errorf("normalize document payload: %w", err)
	}
	if document == nil {
		document = make(map[string]any)
	}
	document["name"] = event.DocName
	document["doctype"] = event.DocType
	document["tenant_id"] = event.Site
	return document, nil
}

func decodeEventFromPayload(payload map[string]any) (events.DocumentEvent, error) {
	if raw, ok := payload["event"]; ok {
		return anyToEvent(raw)
	}
	return anyToEvent(payload)
}

func anyToEvent(value any) (events.DocumentEvent, error) {
	var event events.DocumentEvent
	raw, err := json.Marshal(value)
	if err != nil {
		return event, fmt.Errorf("marshal event payload: %w", err)
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return event, fmt.Errorf("unmarshal event payload: %w", err)
	}
	if err := events.EnsureDocumentEventDefaults(&event); err != nil {
		return event, err
	}
	return event, nil
}

func anyToMap(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, val := range typed {
			out[key] = val
		}
		return out, nil
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("unmarshal payload: %w", err)
		}
		return out, nil
	}
}
