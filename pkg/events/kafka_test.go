package events

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
)

type mockKafkaClient struct {
	record *kgo.Record
	err    error
	closed bool
}

func (m *mockKafkaClient) ProduceSync(_ context.Context, record *kgo.Record) error {
	m.record = record
	return m.err
}

func (m *mockKafkaClient) Close() {
	m.closed = true
}

func TestKafkaProducerPublish(t *testing.T) {
	mockClient := &mockKafkaClient{}
	producer := newKafkaProducerWithClient(mockClient)
	event := DocumentEvent{
		EventID:   "evt-1",
		EventType: EventTypeDocUpdated,
		Site:      "acme",
		DocType:   "SalesOrder",
		DocName:   "SO-0001",
		Action:    "update",
	}

	if err := producer.Publish(context.Background(), TopicDocumentEvents, event); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if mockClient.record == nil {
		t.Fatal("expected record to be produced")
	}
	if mockClient.record.Topic != TopicDocumentEvents {
		t.Fatalf("record topic = %q, want %q", mockClient.record.Topic, TopicDocumentEvents)
	}
	if got := string(mockClient.record.Key); got != "acme:SalesOrder" {
		t.Fatalf("record key = %q, want %q", got, "acme:SalesOrder")
	}

	var got DocumentEvent
	if err := json.Unmarshal(mockClient.record.Value, &got); err != nil {
		t.Fatalf("Unmarshal(record.Value): %v", err)
	}
	if got.EventID != event.EventID || got.DocName != event.DocName {
		t.Fatalf("got event = %+v, want %+v", got, event)
	}
}

func TestKafkaProducerPublishWrapsError(t *testing.T) {
	mockClient := &mockKafkaClient{err: errors.New("broker down")}
	producer := newKafkaProducerWithClient(mockClient)

	err := producer.Publish(context.Background(), TopicDocumentEvents, DocumentEvent{
		Site:    "acme",
		DocType: "SalesOrder",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "broker down" || !containsAll(got, "kafka publish", TopicDocumentEvents, "broker down") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestKafkaProducerCloseDelegates(t *testing.T) {
	mockClient := &mockKafkaClient{}
	producer := newKafkaProducerWithClient(mockClient)

	if err := producer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !mockClient.closed {
		t.Fatal("expected Close to delegate to underlying client")
	}
}
