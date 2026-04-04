package events

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Emitter publishes domain events to subscribers.
// The zero value is safe and acts as a non-fatal no-op when no producer
// has been configured.
type Emitter struct {
	producer Producer
}

// NewEmitter creates an emitter backed by the given Producer.
func NewEmitter(producer Producer) *Emitter {
	return &Emitter{producer: producer}
}

// Emit publishes an event with the given topic and payload.
func (e *Emitter) Emit(topic string, payload any) {
	if e == nil {
		return
	}
	if e.producer == nil {
		slog.Default().Debug("event emitter has no producer configured", slog.String("topic", topic))
		return
	}

	publishTopic, event, ok := normalizeEmitPayload(topic, payload)
	if !ok {
		return
	}

	if event.EventID == "" {
		eventID, err := newEventID()
		if err != nil {
			slog.Default().Warn("event emitter failed to generate event ID",
				slog.String("topic", publishTopic),
				slog.String("error", err.Error()),
			)
			return
		}
		event.EventID = eventID
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Source == "" {
		event.Source = EventSourceMocaCore
	}

	if err := e.producer.Publish(context.Background(), publishTopic, event); err != nil {
		slog.Default().Warn("event emitter publish failed",
			slog.String("topic", publishTopic),
			slog.String("error", err.Error()),
		)
	}
}

func normalizeEmitPayload(topic string, payload any) (string, DocumentEvent, bool) {
	switch event := payload.(type) {
	case DocumentEvent:
		return topic, event, true
	case *DocumentEvent:
		if event == nil {
			slog.Default().Warn("event emitter received nil document event", slog.String("topic", topic))
			return "", DocumentEvent{}, false
		}
		return topic, *event, true
	case map[string]any:
		return normalizeLegacyEvent(topic, event)
	default:
		slog.Default().Warn("event emitter received unsupported payload type",
			slog.String("topic", topic),
			slog.String("type", payloadType(payload)),
		)
		return "", DocumentEvent{}, false
	}
}

func normalizeLegacyEvent(topic string, payload map[string]any) (string, DocumentEvent, bool) {
	doctype, action, ok := strings.Cut(topic, ":")
	if !ok || doctype == "" || action == "" {
		slog.Default().Warn("event emitter could not normalize legacy topic",
			slog.String("topic", topic),
		)
		return "", DocumentEvent{}, false
	}

	eventType, ok := legacyActionEventType(action)
	if !ok {
		slog.Default().Warn("event emitter received unsupported legacy action",
			slog.String("topic", topic),
			slog.String("action", action),
		)
		return "", DocumentEvent{}, false
	}

	eventID, err := newEventID()
	if err != nil {
		slog.Default().Warn("event emitter failed to generate legacy event ID",
			slog.String("topic", topic),
			slog.String("error", err.Error()),
		)
		return "", DocumentEvent{}, false
	}

	event := DocumentEvent{
		EventID:   eventID,
		EventType: eventType,
		Timestamp: time.Now().UTC(),
		Source:    EventSourceMocaCore,
		DocType:   doctype,
		Action:    action,
		Data:      payload,
	}
	if name, ok := stringValue(payload["name"]); ok {
		event.DocName = name
	}
	if site, ok := stringValue(payload["site"]); ok {
		event.Site = site
	}
	if user, ok := stringValue(payload["user"]); ok {
		event.User = user
	}
	if requestID, ok := stringValue(payload["request_id"]); ok {
		event.RequestID = requestID
	}
	if prevData, ok := payload["prev_data"]; ok {
		event.PrevData = prevData
	}

	return TopicDocumentEvents, event, true
}

func legacyActionEventType(action string) (string, bool) {
	switch action {
	case "insert":
		return EventTypeDocCreated, true
	case "update":
		return EventTypeDocUpdated, true
	case "delete":
		return EventTypeDocDeleted, true
	default:
		return "", false
	}
}

func stringValue(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func payloadType(v any) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T", v)
}
