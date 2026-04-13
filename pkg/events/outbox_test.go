package events

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"
)

type fakeOutboxStore struct {
	rows      map[string][]OutboxRow
	published map[string][]int64
	failures  []outboxFailure
	mu        sync.Mutex
}

type outboxFailure struct {
	Site       string
	ID         int64
	RetryCount int
	Failed     bool
}

func (s *fakeOutboxStore) FetchPending(_ context.Context, site string, limit int) ([]OutboxRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := append([]OutboxRow(nil), s.rows[site]...)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (s *fakeOutboxStore) MarkPublished(_ context.Context, site string, ids []int64, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published[site] = append(s.published[site], ids...)
	return nil
}

func (s *fakeOutboxStore) RecordFailure(_ context.Context, site string, id int64, retryCount int, failed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = append(s.failures, outboxFailure{
		Site:       site,
		ID:         id,
		RetryCount: retryCount,
		Failed:     failed,
	})
	return nil
}

type fakeSiteLister struct {
	sites []string
}

func (l fakeSiteLister) ListActiveSites(context.Context) ([]string, error) {
	return append([]string(nil), l.sites...), nil
}

type fakeProducer struct {
	failFor map[string]error
	events  []DocumentEvent
	topics  []string
	mu      sync.Mutex
}

func (p *fakeProducer) Publish(_ context.Context, topic string, event DocumentEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.failFor[event.DocName]; err != nil {
		return err
	}
	p.topics = append(p.topics, topic)
	p.events = append(p.events, event)
	return nil
}

func (p *fakeProducer) Close() error { return nil }

func TestOutboxPollerPollOncePublishesCanonicalAndLegacyRows(t *testing.T) {
	store := &fakeOutboxStore{
		rows: map[string][]OutboxRow{
			"acme": {
				{
					ID:        1,
					EventType: EventTypeDocCreated,
					Topic:     TopicDocumentEvents,
					Payload:   []byte(`{"event_type":"doc.created","site":"acme","doctype":"Order","docname":"ORD-1","data":{"name":"ORD-1","title":"Alpha"}}`),
				},
				{
					ID:           2,
					EventType:    "insert",
					Topic:        "Invoice",
					PartitionKey: "INV-1",
					Payload:      []byte(`{"name":"INV-1","status":"Draft"}`),
				},
			},
			"globex": {
				{
					ID:        3,
					EventType: EventTypeDocUpdated,
					Topic:     TopicDocumentEvents,
					Payload:   []byte(`{"event_type":"doc.updated","site":"globex","doctype":"Order","docname":"ORD-9","data":{"name":"ORD-9"}}`),
				},
			},
		},
		published: make(map[string][]int64),
	}
	producer := &fakeProducer{}
	var hooked []string

	poller, err := NewOutboxPoller(OutboxPollerConfig{
		Store:    store,
		Sites:    fakeSiteLister{sites: []string{"acme", "globex"}},
		Producer: producer,
		Logger:   slog.Default(),
		AfterPublish: func(_ context.Context, event DocumentEvent) error {
			hooked = append(hooked, event.DocName)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewOutboxPoller: %v", err)
	}

	if err := poller.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	if got, want := store.published["acme"], []int64{1, 2}; !slices.Equal(got, want) {
		t.Fatalf("published[acme] = %v, want %v", got, want)
	}
	if got, want := store.published["globex"], []int64{3}; !slices.Equal(got, want) {
		t.Fatalf("published[globex] = %v, want %v", got, want)
	}

	if len(producer.events) != 3 {
		t.Fatalf("published events = %d, want 3", len(producer.events))
	}
	if producer.topics[1] != TopicDocumentEvents {
		t.Fatalf("legacy row published to %q, want %q", producer.topics[1], TopicDocumentEvents)
	}
	if producer.events[1].DocType != "Invoice" || producer.events[1].DocName != "INV-1" {
		t.Fatalf("legacy normalization = %#v", producer.events[1])
	}
	if producer.events[1].EventType != EventTypeDocCreated {
		t.Fatalf("legacy event type = %q, want %q", producer.events[1].EventType, EventTypeDocCreated)
	}
	if producer.events[1].EventID == "" || producer.events[1].Timestamp.IsZero() {
		t.Fatalf("legacy event defaults not populated: %#v", producer.events[1])
	}
	if got, want := hooked, []string{"ORD-1", "INV-1", "ORD-9"}; !slices.Equal(got, want) {
		t.Fatalf("after publish hook = %v, want %v", got, want)
	}
}

func TestOutboxPollerRecordsRetryAndFailureState(t *testing.T) {
	store := &fakeOutboxStore{
		rows: map[string][]OutboxRow{
			"acme": {
				{
					ID:         10,
					RetryCount: 0,
					EventType:  EventTypeDocUpdated,
					Topic:      TopicDocumentEvents,
					Payload:    []byte(`{"event_type":"doc.updated","site":"acme","doctype":"Order","docname":"ORD-10","data":{"name":"ORD-10"}}`),
				},
				{
					ID:         11,
					RetryCount: 1,
					EventType:  EventTypeDocUpdated,
					Topic:      TopicDocumentEvents,
					Payload:    []byte(`{"event_type":"doc.updated","site":"acme","doctype":"Order","docname":"ORD-11","data":{"name":"ORD-11"}}`),
				},
			},
		},
		published: make(map[string][]int64),
	}
	producer := &fakeProducer{
		failFor: map[string]error{
			"ORD-10": errors.New("temporary"),
			"ORD-11": errors.New("permanent"),
		},
	}

	poller, err := NewOutboxPoller(OutboxPollerConfig{
		Store:      store,
		Sites:      fakeSiteLister{sites: []string{"acme"}},
		Producer:   producer,
		Logger:     slog.Default(),
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("NewOutboxPoller: %v", err)
	}

	if err := poller.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	if len(store.failures) != 2 {
		t.Fatalf("failures = %d, want 2", len(store.failures))
	}
	if store.failures[0].ID != 10 || store.failures[0].RetryCount != 1 || store.failures[0].Failed {
		t.Fatalf("first failure = %#v, want retry pending", store.failures[0])
	}
	if store.failures[1].ID != 11 || store.failures[1].RetryCount != 2 || !store.failures[1].Failed {
		t.Fatalf("second failure = %#v, want terminal failure", store.failures[1])
	}
}

// mockCDCMetaProvider implements CDCMetaProvider for testing.
type mockCDCMetaProvider struct {
	cdcEnabled map[string]bool
}

func (m *mockCDCMetaProvider) IsCDCEnabled(_ context.Context, _, doctype string) bool {
	return m.cdcEnabled[doctype]
}

func TestOutboxPollerCDCFanOut(t *testing.T) {
	store := &fakeOutboxStore{
		rows: map[string][]OutboxRow{
			"testsite": {
				{
					ID:        1,
					EventType: EventTypeDocCreated,
					Topic:     TopicDocumentEvents,
					Payload:   []byte(`{"event_type":"doc.created","site":"testsite","doctype":"SalesOrder","docname":"SO-1","data":{"name":"SO-1"}}`),
				},
			},
		},
		published: make(map[string][]int64),
	}
	producer := &fakeProducer{}
	cdcProvider := &mockCDCMetaProvider{
		cdcEnabled: map[string]bool{"SalesOrder": true},
	}

	poller, err := NewOutboxPoller(OutboxPollerConfig{
		Store:           store,
		Sites:           fakeSiteLister{sites: []string{"testsite"}},
		Producer:        producer,
		Logger:          slog.Default(),
		CDCMetaProvider: cdcProvider,
	})
	if err != nil {
		t.Fatalf("NewOutboxPoller: %v", err)
	}

	if err := poller.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	// Expect two publishes: primary TopicDocumentEvents and CDC topic.
	if got := len(producer.topics); got != 2 {
		t.Fatalf("published topic count = %d, want 2", got)
	}

	expectedCDCTopic := CDCTopic("testsite", "SalesOrder")
	if producer.topics[0] != TopicDocumentEvents {
		t.Fatalf("first publish topic = %q, want %q", producer.topics[0], TopicDocumentEvents)
	}
	if producer.topics[1] != expectedCDCTopic {
		t.Fatalf("second publish topic = %q, want %q", producer.topics[1], expectedCDCTopic)
	}

	// The row should be marked published.
	if got, want := store.published["testsite"], []int64{1}; !slices.Equal(got, want) {
		t.Fatalf("published[testsite] = %v, want %v", got, want)
	}
}

func TestOutboxPollerNoCDCFanOutWhenProviderNil(t *testing.T) {
	store := &fakeOutboxStore{
		rows: map[string][]OutboxRow{
			"testsite": {
				{
					ID:        1,
					EventType: EventTypeDocCreated,
					Topic:     TopicDocumentEvents,
					Payload:   []byte(`{"event_type":"doc.created","site":"testsite","doctype":"SalesOrder","docname":"SO-1","data":{"name":"SO-1"}}`),
				},
			},
		},
		published: make(map[string][]int64),
	}
	producer := &fakeProducer{}

	poller, err := NewOutboxPoller(OutboxPollerConfig{
		Store:    store,
		Sites:    fakeSiteLister{sites: []string{"testsite"}},
		Producer: producer,
		Logger:   slog.Default(),
		// CDCMetaProvider is nil — no CDC fan-out expected.
	})
	if err != nil {
		t.Fatalf("NewOutboxPoller: %v", err)
	}

	if err := poller.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	// Only one publish: the primary TopicDocumentEvents.
	if got := len(producer.topics); got != 1 {
		t.Fatalf("published topic count = %d, want 1 (no CDC fan-out)", got)
	}
	if producer.topics[0] != TopicDocumentEvents {
		t.Fatalf("publish topic = %q, want %q", producer.topics[0], TopicDocumentEvents)
	}
}

// topicFailProducer is a fakeProducer that returns an error for a specific topic.
type topicFailProducer struct {
	failErr   error
	failTopic string
	fakeProducer
}

func (p *topicFailProducer) Publish(ctx context.Context, topic string, event DocumentEvent) error {
	if topic == p.failTopic {
		return p.failErr
	}
	return p.fakeProducer.Publish(ctx, topic, event)
}

func TestOutboxPollerCDCFanOutFailureRecorded(t *testing.T) {
	store := &fakeOutboxStore{
		rows: map[string][]OutboxRow{
			"testsite": {
				{
					ID:        1,
					EventType: EventTypeDocCreated,
					Topic:     TopicDocumentEvents,
					Payload:   []byte(`{"event_type":"doc.created","site":"testsite","doctype":"SalesOrder","docname":"SO-1","data":{"name":"SO-1"}}`),
				},
			},
		},
		published: make(map[string][]int64),
	}

	producer := &topicFailProducer{
		failTopic: CDCTopic("testsite", "SalesOrder"),
		failErr:   errors.New("cdc publish failed"),
	}
	cdcProvider := &mockCDCMetaProvider{
		cdcEnabled: map[string]bool{"SalesOrder": true},
	}

	poller, err := NewOutboxPoller(OutboxPollerConfig{
		Store:           store,
		Sites:           fakeSiteLister{sites: []string{"testsite"}},
		Producer:        producer,
		Logger:          slog.Default(),
		CDCMetaProvider: cdcProvider,
	})
	if err != nil {
		t.Fatalf("NewOutboxPoller: %v", err)
	}

	if err := poller.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	// Primary publish succeeded, CDC publish failed — row must NOT be marked published.
	if got := store.published["testsite"]; len(got) != 0 {
		t.Fatalf("published[testsite] = %v, want empty (CDC failure should prevent marking published)", got)
	}
	// Failure should be recorded.
	if len(store.failures) != 1 || store.failures[0].ID != 1 {
		t.Fatalf("failures = %v, want exactly one failure for row 1", store.failures)
	}
}

func TestOutboxPollerRunStopsOnContextCancellation(t *testing.T) {
	poller, err := NewOutboxPoller(OutboxPollerConfig{
		Store:    &fakeOutboxStore{rows: map[string][]OutboxRow{}, published: make(map[string][]int64)},
		Sites:    fakeSiteLister{},
		Producer: &fakeProducer{},
		Logger:   slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewOutboxPoller: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- poller.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}
