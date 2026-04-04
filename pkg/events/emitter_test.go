package events

import (
	"context"
	"strings"
	"testing"
)

//nolint:govet // Test helper; keeping the captured fields explicit is clearer than packing for GC scan size.
type captureProducer struct {
	topic string
	event *DocumentEvent
	calls int
}

func (p *captureProducer) Publish(_ context.Context, topic string, event DocumentEvent) error {
	p.topic = topic
	eventCopy := event
	p.event = &eventCopy
	p.calls++
	return nil
}

func (p *captureProducer) Close() error { return nil }

func TestEmitterEmitDocumentEvent(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	emitter.Emit(TopicAuditLog, DocumentEvent{
		EventType: EventTypeDocCreated,
		Site:      "acme",
		DocType:   "SalesOrder",
		DocName:   "SO-0001",
		Action:    "insert",
	})

	if producer.calls != 1 {
		t.Fatalf("calls = %d, want 1", producer.calls)
	}
	if producer.topic != TopicAuditLog {
		t.Fatalf("topic = %q, want %q", producer.topic, TopicAuditLog)
	}
	if producer.event == nil {
		t.Fatal("expected captured event")
	}
	if producer.event.EventID == "" {
		t.Fatal("expected EventID to be generated")
	}
	if producer.event.Timestamp.IsZero() {
		t.Fatal("expected Timestamp to be populated")
	}
	if producer.event.Source != EventSourceMocaCore {
		t.Fatalf("Source = %q, want %q", producer.event.Source, EventSourceMocaCore)
	}
}

func TestEmitterEmitLegacyMapPayload(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	emitter.Emit("SalesOrder:insert", map[string]any{
		"name":       "SO-0001",
		"site":       "acme",
		"user":       "admin@example.com",
		"request_id": "req-1",
		"status":     "Draft",
	})

	if producer.calls != 1 {
		t.Fatalf("calls = %d, want 1", producer.calls)
	}
	if producer.topic != TopicDocumentEvents {
		t.Fatalf("topic = %q, want %q", producer.topic, TopicDocumentEvents)
	}
	if producer.event == nil {
		t.Fatal("expected captured event")
	}
	if producer.event.EventType != EventTypeDocCreated {
		t.Fatalf("EventType = %q, want %q", producer.event.EventType, EventTypeDocCreated)
	}
	if producer.event.DocType != "SalesOrder" {
		t.Fatalf("DocType = %q, want %q", producer.event.DocType, "SalesOrder")
	}
	if producer.event.DocName != "SO-0001" {
		t.Fatalf("DocName = %q, want %q", producer.event.DocName, "SO-0001")
	}
	if producer.event.Action != "insert" {
		t.Fatalf("Action = %q, want %q", producer.event.Action, "insert")
	}
	if producer.event.Site != "acme" {
		t.Fatalf("Site = %q, want %q", producer.event.Site, "acme")
	}
	if producer.event.User != "admin@example.com" {
		t.Fatalf("User = %q, want %q", producer.event.User, "admin@example.com")
	}
	if producer.event.RequestID != "req-1" {
		t.Fatalf("RequestID = %q, want %q", producer.event.RequestID, "req-1")
	}
}

func TestEmitterNilProducerIsNonFatal(t *testing.T) {
	emitter := &Emitter{}
	emitter.Emit("SalesOrder:insert", map[string]any{"name": "SO-0001"})
}

func TestEmitterUnsupportedPayloadDoesNotPublish(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	emitter.Emit("SalesOrder:insert", 42)

	if producer.calls != 0 {
		t.Fatalf("calls = %d, want 0", producer.calls)
	}
}

func TestEmitterUnsupportedLegacyActionDoesNotPublish(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	emitter.Emit("SalesOrder:submit", map[string]any{"name": "SO-0001"})

	if producer.calls != 0 {
		t.Fatalf("calls = %d, want 0", producer.calls)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
