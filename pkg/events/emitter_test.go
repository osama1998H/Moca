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

// ── Additional edge-case tests ─────────────────────────────────────────────

func TestEmitter_NilReceiver_DoesNotPanic(t *testing.T) {
	var e *Emitter
	e.Emit("test.topic", DocumentEvent{DocType: "SalesOrder"})
}

func TestEmitter_DocumentEventPointer(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	event := &DocumentEvent{DocType: "SalesOrder", DocName: "SO-001"}
	emitter.Emit("test.topic", event)

	if producer.calls != 1 {
		t.Fatalf("calls = %d, want 1", producer.calls)
	}
	if producer.event == nil {
		t.Fatal("expected captured event")
	}
	if producer.event.DocType != "SalesOrder" {
		t.Errorf("DocType = %q", producer.event.DocType)
	}
}

func TestEmitter_NilDocumentEventPointer(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	var event *DocumentEvent
	emitter.Emit("test.topic", event)

	if producer.calls != 0 {
		t.Error("should not publish nil *DocumentEvent")
	}
}

func TestEmitter_PreservesExistingEventID(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	emitter.Emit("topic", DocumentEvent{EventID: "custom-id", DocType: "X"})

	if producer.calls != 1 {
		t.Fatalf("calls = %d, want 1", producer.calls)
	}
	if producer.event == nil {
		t.Fatal("expected captured event")
	}
	if producer.event.EventID != "custom-id" {
		t.Errorf("EventID = %q, want custom-id", producer.event.EventID)
	}
}

func TestEmitter_LegacyMapPayload_Update(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	emitter.Emit("SalesOrder:update", map[string]any{"name": "SO-001"})

	if producer.calls != 1 {
		t.Fatalf("calls = %d, want 1", producer.calls)
	}
	if producer.event.EventType != EventTypeDocUpdated {
		t.Errorf("EventType = %q, want %q", producer.event.EventType, EventTypeDocUpdated)
	}
}

func TestEmitter_LegacyMapPayload_Delete(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	emitter.Emit("SalesOrder:delete", map[string]any{"name": "SO-001"})

	if producer.calls != 1 {
		t.Fatalf("calls = %d, want 1", producer.calls)
	}
	if producer.event.EventType != EventTypeDocDeleted {
		t.Errorf("EventType = %q, want %q", producer.event.EventType, EventTypeDocDeleted)
	}
}

func TestEmitter_LegacyMapPayload_MissingColon(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	emitter.Emit("SalesOrder", map[string]any{"name": "SO-001"})
	if producer.calls != 0 {
		t.Error("should not publish for topic without colon separator")
	}
}

func TestEmitter_LegacyMapPayload_EmptyDoctype(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	emitter.Emit(":insert", map[string]any{"name": "SO-001"})
	if producer.calls != 0 {
		t.Error("should not publish for empty doctype")
	}
}

func TestEmitter_LegacyMapPayload_EmptyAction(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	emitter.Emit("SalesOrder:", map[string]any{"name": "SO-001"})
	if producer.calls != 0 {
		t.Error("should not publish for empty action")
	}
}

func TestEmitter_LegacyMapPayload_PrevData(t *testing.T) {
	producer := &captureProducer{}
	emitter := NewEmitter(producer)

	prevData := map[string]any{"status": "Draft"}
	emitter.Emit("SalesOrder:insert", map[string]any{
		"name":      "SO-001",
		"prev_data": prevData,
	})

	if producer.calls != 1 {
		t.Fatalf("calls = %d, want 1", producer.calls)
	}
	if producer.event == nil {
		t.Fatal("expected captured event")
	}
	if producer.event.PrevData == nil {
		t.Error("PrevData should be set")
	}
}

func TestLegacyActionEventType_AllCases(t *testing.T) {
	tests := []struct {
		action   string
		wantType string
		wantOK   bool
	}{
		{"insert", EventTypeDocCreated, true},
		{"update", EventTypeDocUpdated, true},
		{"delete", EventTypeDocDeleted, true},
		{"submit", "", false},
		{"cancel", "", false},
		{"", "", false},
		{"INSERT", "", false},
	}
	for _, tt := range tests {
		gotType, gotOK := legacyActionEventType(tt.action)
		if gotType != tt.wantType || gotOK != tt.wantOK {
			t.Errorf("legacyActionEventType(%q) = (%q, %v), want (%q, %v)",
				tt.action, gotType, gotOK, tt.wantType, tt.wantOK)
		}
	}
}

func TestPayloadType_Variants(t *testing.T) {
	if got := payloadType(nil); got != "<nil>" {
		t.Errorf("nil = %q", got)
	}
	if got := payloadType("str"); got != "string" {
		t.Errorf("string = %q", got)
	}
	if got := payloadType(42); got != "int" {
		t.Errorf("int = %q", got)
	}
	if got := payloadType(DocumentEvent{}); got != "events.DocumentEvent" {
		t.Errorf("DocumentEvent = %q", got)
	}
}

func TestStringValue_Variants(t *testing.T) {
	s, ok := stringValue("hello")
	if !ok || s != "hello" {
		t.Errorf("string = (%q, %v)", s, ok)
	}
	_, ok = stringValue(42)
	if ok {
		t.Error("int should return false")
	}
	_, ok = stringValue(nil)
	if ok {
		t.Error("nil should return false")
	}
}
