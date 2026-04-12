package workflow

import (
	"testing"
)

func TestNewWorkflowEvent(t *testing.T) {
	ev := NewWorkflowEvent(
		EventTypeTransition,
		"site1",
		"SalesOrder",
		"SO-0001",
		"Test Workflow",
		"Submit",
		"Draft",
		"Pending Approval",
		"",
		"user@example.com",
		"looks good",
		"req-abc",
	)

	if ev.EventID == "" {
		t.Fatal("expected non-empty EventID")
	}
	if ev.EventType != EventTypeTransition {
		t.Fatalf("expected EventType=%q, got %q", EventTypeTransition, ev.EventType)
	}
	if ev.DocType != "SalesOrder" {
		t.Fatalf("expected DocType=%q, got %q", "SalesOrder", ev.DocType)
	}
	if ev.FromState != "Draft" {
		t.Fatalf("expected FromState=%q, got %q", "Draft", ev.FromState)
	}
	if ev.Timestamp.IsZero() {
		t.Fatal("expected non-zero Timestamp")
	}
}

func TestWorkflowEventTypes(t *testing.T) {
	constants := []string{
		EventTypeTransition,
		EventTypeFork,
		EventTypeJoin,
		EventTypeQuorumVote,
		EventTypeQuorumMet,
		EventTypeSLAStarted,
		EventTypeSLABreached,
		EventTypeSLACancelled,
		EventTypeDelegated,
	}

	// Verify exactly 9 constants.
	if len(constants) != 9 {
		t.Fatalf("expected 9 event type constants, got %d", len(constants))
	}

	// Verify all are unique.
	seen := make(map[string]struct{}, len(constants))
	for _, c := range constants {
		if _, dup := seen[c]; dup {
			t.Fatalf("duplicate event type constant: %q", c)
		}
		seen[c] = struct{}{}
	}
}
