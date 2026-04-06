package serve

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/events"
)

func TestParseDocChannel(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		site    string
		doctype string
		docname string
		ok      bool
	}{
		{
			name:    "valid channel",
			channel: "pubsub:doc:acme:SalesOrder:SO-001",
			site:    "acme",
			doctype: "SalesOrder",
			docname: "SO-001",
			ok:      true,
		},
		{
			name:    "name with colons",
			channel: "pubsub:doc:acme:User:admin@example.com",
			site:    "acme",
			doctype: "User",
			docname: "admin@example.com",
			ok:      true,
		},
		{
			name:    "name with embedded colon",
			channel: "pubsub:doc:acme:Config:key:value",
			site:    "acme",
			doctype: "Config",
			docname: "key:value",
			ok:      true,
		},
		{
			name:    "wrong prefix",
			channel: "other:doc:acme:SalesOrder:SO-001",
			ok:      false,
		},
		{
			name:    "missing name",
			channel: "pubsub:doc:acme:SalesOrder",
			ok:      false,
		},
		{
			name:    "empty site",
			channel: "pubsub:doc::SalesOrder:SO-001",
			ok:      false,
		},
		{
			name:    "empty string",
			channel: "",
			ok:      false,
		},
		{
			name:    "prefix only",
			channel: "pubsub:doc:",
			ok:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site, doctype, name, ok := parseDocChannel(tt.channel)
			if ok != tt.ok {
				t.Fatalf("parseDocChannel(%q): ok = %v, want %v", tt.channel, ok, tt.ok)
			}
			if !ok {
				return
			}
			if site != tt.site || doctype != tt.doctype || name != tt.docname {
				t.Fatalf("parseDocChannel(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.channel, site, doctype, name, tt.site, tt.doctype, tt.docname)
			}
		})
	}
}

func TestBuildBroadcastMessage(t *testing.T) {
	event := events.DocumentEvent{
		EventID:   "evt-1",
		EventType: "doc.updated",
		Timestamp: time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC),
		Site:      "acme",
		DocType:   "SalesOrder",
		DocName:   "SO-001",
		Action:    "Update",
		User:      "admin@example.com",
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	result, err := buildBroadcastMessage("acme", "SalesOrder", "SO-001", payload)
	if err != nil {
		t.Fatal(err)
	}

	var msg serverMessage
	if err := json.Unmarshal(result, &msg); err != nil {
		t.Fatal(err)
	}

	if msg.Type != "doc_update" {
		t.Fatalf("expected type doc_update, got %s", msg.Type)
	}
	if msg.Site != "acme" {
		t.Fatalf("expected site acme, got %s", msg.Site)
	}
	if msg.DocType != "SalesOrder" {
		t.Fatalf("expected doctype SalesOrder, got %s", msg.DocType)
	}
	if msg.Name != "SO-001" {
		t.Fatalf("expected name SO-001, got %s", msg.Name)
	}
	if msg.User != "admin@example.com" {
		t.Fatalf("expected user admin@example.com, got %s", msg.User)
	}
	if msg.EventType != "doc.updated" {
		t.Fatalf("expected event_type doc.updated, got %s", msg.EventType)
	}
	if msg.Timestamp != "2026-04-06T12:00:00Z" {
		t.Fatalf("expected timestamp 2026-04-06T12:00:00Z, got %s", msg.Timestamp)
	}
}

func TestBuildBroadcastMessage_InvalidPayload(t *testing.T) {
	_, err := buildBroadcastMessage("acme", "SalesOrder", "SO-001", []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON payload")
	}
}
