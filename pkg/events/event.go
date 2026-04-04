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
