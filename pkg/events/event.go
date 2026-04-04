package events

import (
	"crypto/rand"
	"fmt"
	"time"
)

const (
	// EventSourceMocaCore identifies framework-originated events.
	EventSourceMocaCore = "moca-core"

	// Document lifecycle event types from MOCA_SYSTEM_DESIGN.md §6.2.
	EventTypeDocCreated   = "doc.created"
	EventTypeDocUpdated   = "doc.updated"
	EventTypeDocSubmitted = "doc.submitted"
	EventTypeDocCancelled = "doc.cancelled"
	EventTypeDocDeleted   = "doc.deleted"

	// Kafka / Redis topics from MOCA_SYSTEM_DESIGN.md §6.1.
	TopicDocumentEvents      = "moca.doc.events"
	TopicAuditLog            = "moca.audit.log"
	TopicMetaChanges         = "moca.meta.changes"
	TopicIntegrationOutbox   = "moca.integration.outbox"
	TopicWorkflowTransitions = "moca.workflow.transitions"
	TopicNotifications       = "moca.notifications"
	TopicSearchIndexing      = "moca.search.indexing"
)

// DocumentEvent is the canonical event envelope for document lifecycle events.
type DocumentEvent struct {
	EventID   string    `json:"event_id"`
	EventType string    `json:"event_type"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Site      string    `json:"site"`
	DocType   string    `json:"doctype"`
	DocName   string    `json:"docname"`
	Action    string    `json:"action"`
	User      string    `json:"user"`
	Data      any       `json:"data,omitempty"`
	PrevData  any       `json:"prev_data,omitempty"`
	RequestID string    `json:"request_id"`
}

// PartitionKey returns the tenant + doctype partition key used for Kafka
// ordering guarantees.
func PartitionKey(site, doctype string) string {
	return site + ":" + doctype
}

// CDCTopic returns the per-tenant CDC topic name.
func CDCTopic(site, doctype string) string {
	return fmt.Sprintf("moca.cdc.%s.%s", site, doctype)
}

// NewDocumentEvent builds the canonical document lifecycle envelope used by
// the transactional outbox and downstream consumers.
func NewDocumentEvent(
	eventType, site, doctype, docname, user, requestID string,
	data, prevData any,
) (DocumentEvent, error) {
	eventID, err := newEventID()
	if err != nil {
		return DocumentEvent{}, fmt.Errorf("generate event id: %w", err)
	}

	return DocumentEvent{
		EventID:   eventID,
		EventType: eventType,
		Timestamp: time.Now().UTC(),
		Source:    EventSourceMocaCore,
		Site:      site,
		DocType:   doctype,
		DocName:   docname,
		Action:    actionForEventType(eventType),
		User:      user,
		Data:      data,
		PrevData:  prevData,
		RequestID: requestID,
	}, nil
}

// EnsureDocumentEventDefaults fills framework-managed fields when callers
// construct a DocumentEvent manually or when a legacy payload is normalized.
func EnsureDocumentEventDefaults(event *DocumentEvent) error {
	if event == nil {
		return nil
	}
	if event.EventID == "" {
		eventID, err := newEventID()
		if err != nil {
			return fmt.Errorf("generate event id: %w", err)
		}
		event.EventID = eventID
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Source == "" {
		event.Source = EventSourceMocaCore
	}
	if event.Action == "" {
		event.Action = actionForEventType(event.EventType)
	}
	return nil
}

func actionForEventType(eventType string) string {
	switch eventType {
	case EventTypeDocCreated:
		return "insert"
	case EventTypeDocUpdated:
		return "update"
	case EventTypeDocDeleted:
		return "delete"
	default:
		return eventType
	}
}

func newEventID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	// RFC 4122, version 4.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
	), nil
}
