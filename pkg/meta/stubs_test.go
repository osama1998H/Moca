package meta_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/meta"
)

// TestWorkflowState_ParallelFields verifies that WorkflowState can unmarshal
// the parallel-workflow fields introduced for MS-23 (AND-split/join support).
func TestWorkflowState_ParallelFields(t *testing.T) {
	raw := `{
		"name":        "review",
		"style":       "warning",
		"allow_edit":  "Editor",
		"update_field":"status",
		"update_value":"In Review",
		"doc_status":  1,
		"is_fork":     true,
		"join_target": "approved",
		"branch_name": "legal-review"
	}`

	var ws meta.WorkflowState
	if err := json.Unmarshal([]byte(raw), &ws); err != nil {
		t.Fatalf("json.Unmarshal WorkflowState: %v", err)
	}

	// Existing fields must still work.
	if ws.Name != "review" {
		t.Errorf("Name = %q, want %q", ws.Name, "review")
	}
	if ws.DocStatus != 1 {
		t.Errorf("DocStatus = %d, want 1", ws.DocStatus)
	}

	// New parallel fields.
	if !ws.IsFork {
		t.Errorf("IsFork = false, want true")
	}
	if ws.JoinTarget != "approved" {
		t.Errorf("JoinTarget = %q, want %q", ws.JoinTarget, "approved")
	}
	if ws.BranchName != "legal-review" {
		t.Errorf("BranchName = %q, want %q", ws.BranchName, "legal-review")
	}

	t.Log("WorkflowState parallel fields unmarshal correctly")
}

// TestWorkflowState_ParallelFields_OmitEmpty ensures the new fields are omitted
// from JSON when they hold zero values (omitempty behaviour).
func TestWorkflowState_OmitEmpty(t *testing.T) {
	ws := meta.WorkflowState{
		Name:      "draft",
		DocStatus: 0,
	}

	data, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("json.Marshal WorkflowState: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	for _, key := range []string{"is_fork", "join_target", "branch_name"} {
		if _, ok := m[key]; ok {
			t.Errorf("key %q present in JSON output, want omitted (omitempty)", key)
		}
	}

	t.Log("WorkflowState zero parallel fields correctly omitted from JSON")
}

// TestTransition_QuorumFields verifies that Transition can unmarshal
// the quorum fields introduced for MS-23 approval-quorum support.
func TestTransition_QuorumFields(t *testing.T) {
	raw := `{
		"from":            "draft",
		"to":              "approved",
		"action":          "Approve",
		"condition":       "",
		"auto_action":     "",
		"allowed_roles":   ["Manager", "Director"],
		"require_comment": true,
		"quorum_count":    2,
		"quorum_roles":    ["Manager", "Director", "VP"]
	}`

	var tr meta.Transition
	if err := json.Unmarshal([]byte(raw), &tr); err != nil {
		t.Fatalf("json.Unmarshal Transition: %v", err)
	}

	// Existing fields.
	if tr.From != "draft" {
		t.Errorf("From = %q, want %q", tr.From, "draft")
	}
	if tr.Action != "Approve" {
		t.Errorf("Action = %q, want %q", tr.Action, "Approve")
	}
	if !tr.RequireComment {
		t.Errorf("RequireComment = false, want true")
	}
	if len(tr.AllowedRoles) != 2 {
		t.Errorf("AllowedRoles len = %d, want 2", len(tr.AllowedRoles))
	}

	// New quorum fields.
	if tr.QuorumCount != 2 {
		t.Errorf("QuorumCount = %d, want 2", tr.QuorumCount)
	}
	if len(tr.QuorumRoles) != 3 {
		t.Errorf("QuorumRoles len = %d, want 3", len(tr.QuorumRoles))
	}
	if tr.QuorumRoles[0] != "Manager" {
		t.Errorf("QuorumRoles[0] = %q, want %q", tr.QuorumRoles[0], "Manager")
	}
	if tr.QuorumRoles[2] != "VP" {
		t.Errorf("QuorumRoles[2] = %q, want %q", tr.QuorumRoles[2], "VP")
	}

	t.Log("Transition quorum fields unmarshal correctly")
}

// TestTransition_QuorumFields_OmitEmpty ensures quorum fields are omitted when zero.
func TestTransition_OmitEmpty(t *testing.T) {
	tr := meta.Transition{
		From:   "draft",
		To:     "submitted",
		Action: "Submit",
	}

	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("json.Marshal Transition: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	for _, key := range []string{"quorum_count", "quorum_roles"} {
		if _, ok := m[key]; ok {
			t.Errorf("key %q present in JSON output, want omitted (omitempty)", key)
		}
	}

	t.Log("Transition zero quorum fields correctly omitted from JSON")
}

// TestSLARule_JSONRoundTrip verifies that SLARule marshals and unmarshals
// correctly with a 24-hour duration.
func TestSLARule_JSONRoundTrip(t *testing.T) {
	original := meta.SLARule{
		State:            "pending",
		EscalationRole:   "Manager",
		EscalationAction: "notify",
		MaxDuration:      24 * time.Hour,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal SLARule: %v", err)
	}

	var roundTripped meta.SLARule
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("json.Unmarshal SLARule: %v", err)
	}

	if roundTripped.State != original.State {
		t.Errorf("State = %q, want %q", roundTripped.State, original.State)
	}
	if roundTripped.EscalationRole != original.EscalationRole {
		t.Errorf("EscalationRole = %q, want %q", roundTripped.EscalationRole, original.EscalationRole)
	}
	if roundTripped.EscalationAction != original.EscalationAction {
		t.Errorf("EscalationAction = %q, want %q", roundTripped.EscalationAction, original.EscalationAction)
	}
	if roundTripped.MaxDuration != original.MaxDuration {
		t.Errorf("MaxDuration = %v, want %v", roundTripped.MaxDuration, original.MaxDuration)
	}

	t.Logf("SLARule round-trip OK (MaxDuration=%v)", roundTripped.MaxDuration)
}
