//go:build integration

package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/pkg/events"
)

const (
	evtRedisHost = "localhost"
	evtRedisPort = 6380
)

func integrationRedisClients(t *testing.T) *drivers.RedisClients {
	t.Helper()

	cfg := config.RedisConfig{
		Host:      evtRedisHost,
		Port:      evtRedisPort,
		DbCache:   0,
		DbQueue:   1,
		DbSession: 2,
		DbPubSub:  3,
	}

	clients := drivers.NewRedisClients(cfg, slog.Default())
	ctx := context.Background()
	if err := clients.Ping(ctx); err != nil {
		t.Skipf("Redis not available at %s:%d — start with: docker compose up -d", evtRedisHost, evtRedisPort)
	}
	t.Cleanup(func() { _ = clients.Close() })
	return clients
}

func TestIntegration_RedisProducerPublishAndSubscribe(t *testing.T) {
	clients := integrationRedisClients(t)
	ctx := context.Background()

	kafkaCfg := config.KafkaConfig{} // Kafka disabled → Redis fallback
	producer, err := events.NewProducer(kafkaCfg, clients)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	defer func() { _ = producer.Close() }()

	topic := "moca.test.events"

	// Subscribe before publishing.
	sub := clients.PubSub.Subscribe(ctx, topic)
	defer func() { _ = sub.Close() }()

	// Wait for subscription to be active.
	_, err = sub.Receive(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	event, err := events.NewDocumentEvent(
		events.EventTypeDocCreated,
		"test_site",
		"Article",
		"ART-001",
		"admin@test.dev",
		"req-123",
		map[string]any{"title": "Hello Integration"},
		nil,
	)
	if err != nil {
		t.Fatalf("NewDocumentEvent: %v", err)
	}

	if err := producer.Publish(ctx, topic, event); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Receive the published message.
	ch := sub.Channel()
	select {
	case msg := <-ch:
		if msg.Channel != topic {
			t.Errorf("channel = %q, want %q", msg.Channel, topic)
		}
		var received events.DocumentEvent
		if err := json.Unmarshal([]byte(msg.Payload), &received); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if received.EventID != event.EventID {
			t.Errorf("EventID = %q, want %q", received.EventID, event.EventID)
		}
		if received.DocType != "Article" {
			t.Errorf("DocType = %q, want %q", received.DocType, "Article")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for published message")
	}
}

func TestIntegration_OutboxPollerPublishesEvents(t *testing.T) {
	clients := integrationRedisClients(t)
	ctx := context.Background()

	// Use a fake outbox store that returns one pending row then nothing.
	event, err := events.NewDocumentEvent(
		events.EventTypeDocCreated,
		"outbox_site",
		"Task",
		"TASK-001",
		"admin@test.dev",
		"req-456",
		map[string]any{"status": "Open"},
		nil,
	)
	if err != nil {
		t.Fatalf("NewDocumentEvent: %v", err)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	store := &fakeOutboxStore{
		rows: []events.OutboxRow{
			{
				ID:           1,
				EventType:    events.EventTypeDocCreated,
				Topic:        events.TopicDocumentEvents,
				PartitionKey: events.PartitionKey("outbox_site", "Task"),
				Payload:      payload,
				Status:       "pending",
			},
		},
	}

	kafkaCfg := config.KafkaConfig{}
	producer, err := events.NewProducer(kafkaCfg, clients)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	defer func() { _ = producer.Close() }()

	// Subscribe to catch the event.
	sub := clients.PubSub.Subscribe(ctx, events.TopicDocumentEvents)
	defer func() { _ = sub.Close() }()
	_, err = sub.Receive(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	poller, err := events.NewOutboxPoller(events.OutboxPollerConfig{
		Store:        store,
		Sites:        &fakeSiteLister{sites: []string{"outbox_site"}},
		Producer:     producer,
		Logger:       slog.Default(),
		PollInterval: 50 * time.Millisecond,
		BatchSize:    10,
		MaxRetries:   3,
	})
	if err != nil {
		t.Fatalf("NewOutboxPoller: %v", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	go func() { _ = poller.Run(runCtx) }()

	// Wait for the event to be published.
	ch := sub.Channel()
	select {
	case msg := <-ch:
		var received events.DocumentEvent
		if err := json.Unmarshal([]byte(msg.Payload), &received); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if received.DocName != "TASK-001" {
			t.Errorf("DocName = %q, want %q", received.DocName, "TASK-001")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for outbox event")
	}

	cancel()

	// Verify the row was marked as published.
	if len(store.Published()) == 0 {
		t.Error("expected row to be marked published")
	}
}

// fakeOutboxStore returns the configured rows once, then empty.
type fakeOutboxStore struct {
	mu        sync.Mutex
	rows      []events.OutboxRow
	fetched   bool
	published []int64
}

func (s *fakeOutboxStore) FetchPending(_ context.Context, _ string, _ int) ([]events.OutboxRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fetched {
		return nil, nil
	}
	s.fetched = true
	return s.rows, nil
}

func (s *fakeOutboxStore) MarkPublished(_ context.Context, _ string, ids []int64, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published = append(s.published, ids...)
	return nil
}

func (s *fakeOutboxStore) Published() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]int64, len(s.published))
	copy(cp, s.published)
	return cp
}

func (s *fakeOutboxStore) RecordFailure(_ context.Context, _ string, id int64, _ int, _ bool) error {
	return fmt.Errorf("unexpected failure for row %d", id)
}

type fakeSiteLister struct {
	sites []string
}

func (l *fakeSiteLister) ListActiveSites(_ context.Context) ([]string, error) {
	return l.sites, nil
}

// Ensure fakeOutboxStore satisfies events.OutboxStore.
var _ events.OutboxStore = (*fakeOutboxStore)(nil)

// Ensure Redis subscribe helper type.
var _ redis.Cmdable = (*redis.Client)(nil)
