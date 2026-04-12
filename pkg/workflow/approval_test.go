package workflow

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestApprovalManager_SingleApproval(t *testing.T) {
	am := NewApprovalManager()
	tr := &meta.Transition{Action: "Approve", QuorumCount: 0}

	rec := am.RecordAction("PurchaseOrder", "PO-001", "Pending", "Approve", "", "alice", "LGTM")
	if rec.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if rec.Action != "Approve" {
		t.Errorf("Action = %q, want %q", rec.Action, "Approve")
	}
	if rec.User != "alice" {
		t.Errorf("User = %q, want %q", rec.User, "alice")
	}

	result := am.CheckQuorum("PurchaseOrder", "PO-001", "Pending", "Approve", "", tr)
	if result.Required != 1 {
		t.Errorf("Required = %d, want 1", result.Required)
	}
	if result.Received != 1 {
		t.Errorf("Received = %d, want 1", result.Received)
	}
	if !result.IsMet {
		t.Error("IsMet = false, want true")
	}
}

func TestApprovalManager_QuorumNotMet(t *testing.T) {
	am := NewApprovalManager()
	tr := &meta.Transition{Action: "Approve", QuorumCount: 3}

	am.RecordAction("PurchaseOrder", "PO-002", "Pending", "Approve", "", "alice", "")
	am.RecordAction("PurchaseOrder", "PO-002", "Pending", "Approve", "", "bob", "")

	result := am.CheckQuorum("PurchaseOrder", "PO-002", "Pending", "Approve", "", tr)
	if result.Required != 3 {
		t.Errorf("Required = %d, want 3", result.Required)
	}
	if result.Received != 2 {
		t.Errorf("Received = %d, want 2", result.Received)
	}
	if result.IsMet {
		t.Error("IsMet = true, want false")
	}
}

func TestApprovalManager_QuorumMet(t *testing.T) {
	am := NewApprovalManager()
	tr := &meta.Transition{Action: "Approve", QuorumCount: 2}

	am.RecordAction("PurchaseOrder", "PO-003", "Pending", "Approve", "", "alice", "")
	am.RecordAction("PurchaseOrder", "PO-003", "Pending", "Approve", "", "bob", "")

	result := am.CheckQuorum("PurchaseOrder", "PO-003", "Pending", "Approve", "", tr)
	if result.Required != 2 {
		t.Errorf("Required = %d, want 2", result.Required)
	}
	if result.Received != 2 {
		t.Errorf("Received = %d, want 2", result.Received)
	}
	if !result.IsMet {
		t.Error("IsMet = false, want true")
	}
}

func TestApprovalManager_DuplicateApproval(t *testing.T) {
	am := NewApprovalManager()

	am.RecordAction("PurchaseOrder", "PO-004", "Pending", "Approve", "branch-a", "alice", "")

	if !am.HasAlreadyActed("PurchaseOrder", "PO-004", "Pending", "Approve", "branch-a", "alice") {
		t.Error("HasAlreadyActed(alice) = false, want true")
	}
	if am.HasAlreadyActed("PurchaseOrder", "PO-004", "Pending", "Approve", "branch-a", "bob") {
		t.Error("HasAlreadyActed(bob) = true, want false")
	}
}

func TestApprovalManager_Delegate(t *testing.T) {
	am := NewApprovalManager()

	am.Delegate("PurchaseOrder", "PO-005", "alice", "bob")

	actions := am.GetActions("PurchaseOrder", "PO-005")
	if len(actions) != 1 {
		t.Fatalf("len(actions) = %d, want 1", len(actions))
	}
	if actions[0].Action != "Delegate" {
		t.Errorf("Action = %q, want %q", actions[0].Action, "Delegate")
	}
	if actions[0].User != "alice" {
		t.Errorf("User = %q, want %q", actions[0].User, "alice")
	}
	if actions[0].Comment != "Delegated to bob" {
		t.Errorf("Comment = %q, want %q", actions[0].Comment, "Delegated to bob")
	}
}
