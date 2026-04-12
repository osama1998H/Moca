package workflow

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

// testWorkflowMeta returns a *meta.WorkflowMeta with 3 states and 2 transitions
// for use in registry and lookup helper tests.
func testWorkflowMeta() *meta.WorkflowMeta {
	return &meta.WorkflowMeta{
		Name:    "Test Workflow",
		DocType: "SalesOrder",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Draft", Style: "grey", AllowEdit: "All"},
			{Name: "Pending Approval", Style: "orange", AllowEdit: "Approver"},
			{Name: "Approved", Style: "green", AllowEdit: ""},
		},
		Transitions: []meta.Transition{
			{From: "Draft", To: "Pending Approval", Action: "Submit", AllowedRoles: []string{"Sales User"}},
			{From: "Pending Approval", To: "Approved", Action: "Approve", AllowedRoles: []string{"Approver"}},
		},
	}
}

func TestWorkflowRegistry_GetAndInvalidate(t *testing.T) {
	r := NewWorkflowRegistry()
	ctx := context.Background()

	// Get unknown doctype → error.
	_, err := r.Get(ctx, "site1", "SalesOrder")
	if err == nil {
		t.Fatal("expected ErrNoActiveWorkflow, got nil")
	}
	if err != ErrNoActiveWorkflow {
		t.Fatalf("expected ErrNoActiveWorkflow, got %v", err)
	}

	// Set a workflow, Get it, verify name.
	wf := testWorkflowMeta()
	r.Set("site1", "SalesOrder", wf)

	got, err := r.Get(ctx, "site1", "SalesOrder")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Test Workflow" {
		t.Fatalf("expected name %q, got %q", "Test Workflow", got.Name)
	}

	// Invalidate → Get returns error.
	r.Invalidate("site1", "SalesOrder")
	_, err = r.Get(ctx, "site1", "SalesOrder")
	if err != ErrNoActiveWorkflow {
		t.Fatalf("expected ErrNoActiveWorkflow after invalidate, got %v", err)
	}
}

func TestWorkflowRegistry_FindTransition(t *testing.T) {
	wf := testWorkflowMeta()

	// Find Draft→Submit → found.
	tr := FindTransition(wf, "Draft", "Submit", "")
	if tr == nil {
		t.Fatal("expected transition Draft→Submit to be found, got nil")
	}
	if tr.To != "Pending Approval" {
		t.Fatalf("expected To=%q, got %q", "Pending Approval", tr.To)
	}

	// Find Draft→Approve → nil.
	tr2 := FindTransition(wf, "Draft", "Approve", "")
	if tr2 != nil {
		t.Fatalf("expected nil for Draft→Approve, got %+v", tr2)
	}
}

func TestWorkflowRegistry_FindState(t *testing.T) {
	wf := testWorkflowMeta()

	// Find "Pending Approval" → found with correct style.
	s := FindState(wf, "Pending Approval")
	if s == nil {
		t.Fatal("expected state 'Pending Approval' to be found, got nil")
	}
	if s.Style != "orange" {
		t.Fatalf("expected style %q, got %q", "orange", s.Style)
	}

	// Find "Nonexistent" → nil.
	s2 := FindState(wf, "Nonexistent")
	if s2 != nil {
		t.Fatalf("expected nil for nonexistent state, got %+v", s2)
	}
}
