package events

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDocumentEventJSONShape(t *testing.T) {
	event := DocumentEvent{
		EventID:   "evt-1",
		EventType: EventTypeDocCreated,
		Timestamp: time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC),
		Source:    EventSourceMocaCore,
		Site:      "acme",
		DocType:   "SalesOrder",
		DocName:   "SO-0001",
		Action:    "insert",
		User:      "admin@example.com",
		Data:      map[string]any{"name": "SO-0001"},
		RequestID: "req-1",
	}

	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	jsonStr := string(raw)
	for _, want := range []string{
		`"event_id":"evt-1"`,
		`"event_type":"doc.created"`,
		`"source":"moca-core"`,
		`"site":"acme"`,
		`"doctype":"SalesOrder"`,
		`"docname":"SO-0001"`,
		`"action":"insert"`,
		`"request_id":"req-1"`,
	} {
		if !strings.Contains(jsonStr, want) {
			t.Errorf("JSON missing %q: %s", want, jsonStr)
		}
	}
}

func TestPartitionKey(t *testing.T) {
	if got := PartitionKey("acme", "SalesOrder"); got != "acme:SalesOrder" {
		t.Fatalf("PartitionKey = %q, want %q", got, "acme:SalesOrder")
	}
}

func TestCDCTopic(t *testing.T) {
	if got := CDCTopic("acme", "SalesOrder"); got != "moca.cdc.acme.SalesOrder" {
		t.Fatalf("CDCTopic = %q, want %q", got, "moca.cdc.acme.SalesOrder")
	}
}

func TestNewEventIDFormat(t *testing.T) {
	id, err := newEventID()
	if err != nil {
		t.Fatalf("newEventID: %v", err)
	}
	if len(id) != 36 {
		t.Fatalf("len(id) = %d, want 36 (%q)", len(id), id)
	}
	if id[14] != '4' {
		t.Fatalf("id version nibble = %q, want %q in %q", id[14], '4', id)
	}
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Fatalf("id %q does not match UUID layout", id)
	}
}
