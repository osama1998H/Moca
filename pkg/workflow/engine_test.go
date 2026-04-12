package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// taskJSON is a minimal MetaType for Task documents used in engine tests.
// It includes workflow_state, docstatus, and grand_total as storable fields.
const taskJSON = `{
	"name": "Task",
	"module": "test",
	"naming_rule": {"rule": "uuid"},
	"fields": [
		{"name": "workflow_state", "field_type": "Data",  "label": "Workflow State"},
		{"name": "docstatus",      "field_type": "Int",   "label": "Doc Status"},
		{"name": "grand_total",    "field_type": "Float", "label": "Grand Total"}
	]
}`

// mustCompileTask compiles the task MetaType or fails the test.
func mustCompileTask(t *testing.T) *meta.MetaType {
	t.Helper()
	mt, err := meta.Compile([]byte(taskJSON))
	if err != nil {
		t.Fatalf("mustCompileTask: %v", err)
	}
	return mt
}

// newTaskDoc creates a DynamicDoc for Task with the given initial workflow_state.
func newTaskDoc(t *testing.T, state string, grandTotal float64) document.Document {
	t.Helper()
	mt := mustCompileTask(t)
	doc := document.NewDynamicDoc(mt, nil, false)
	if state != "" {
		if err := doc.Set("workflow_state", state); err != nil {
			t.Fatalf("newTaskDoc: Set workflow_state: %v", err)
		}
	}
	if grandTotal != 0 {
		if err := doc.Set("grand_total", grandTotal); err != nil {
			t.Fatalf("newTaskDoc: Set grand_total: %v", err)
		}
	}
	return doc
}

// newEngineDocCtx creates a DocContext with a SiteContext and user.
func newEngineDocCtx(site, email string, roles []string) *document.DocContext {
	user := &auth.User{
		Email: email,
		Roles: roles,
	}
	return document.NewDocContext(context.Background(), &tenancy.SiteContext{Name: site}, user)
}

// simpleWorkflow returns a workflow with 4 states and 3 transitions:
//   - Draft -> Pending Approval (Submit, role: User)
//   - Pending Approval -> Approved (Approve, role: Approver)
//   - Pending Approval -> Rejected (Reject, role: Approver, requireComment: true)
func simpleWorkflow() *meta.WorkflowMeta {
	return &meta.WorkflowMeta{
		Name:     "Task Workflow",
		DocType:  "Task",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Draft", Style: "grey"},
			{Name: "Pending Approval", Style: "orange"},
			{Name: "Approved", Style: "green", DocStatus: 1},
			{Name: "Rejected", Style: "red"},
		},
		Transitions: []meta.Transition{
			{From: "Draft", To: "Pending Approval", Action: "Submit", AllowedRoles: []string{"User"}},
			{From: "Pending Approval", To: "Approved", Action: "Approve", AllowedRoles: []string{"Approver"}},
			{From: "Pending Approval", To: "Rejected", Action: "Reject", AllowedRoles: []string{"Approver"}, RequireComment: true},
		},
	}
}

// setupEngine creates a WorkflowEngine with the given workflow registered for "test-site".
func setupEngine(wf *meta.WorkflowMeta) *WorkflowEngine {
	reg := NewWorkflowRegistry()
	reg.Set("test-site", wf.DocType, wf)
	return NewWorkflowEngine(WithRegistry(reg), WithEvaluator(NewConditionEvaluator()))
}

// TestEngine_Transition_Linear verifies a basic Draft -> Submit -> Pending Approval transition.
func TestEngine_Transition_Linear(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	state := doc.Get("workflow_state")
	if state != "Pending Approval" {
		t.Fatalf("expected workflow_state=%q, got %v", "Pending Approval", state)
	}
}

// TestEngine_Transition_BlockedWithoutRole verifies that a user without the required
// role cannot perform a transition.
func TestEngine_Transition_BlockedWithoutRole(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Pending Approval", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	err := engine.Transition(ctx, doc, "Approve", TransitionOpts{})
	if err == nil {
		t.Fatal("expected ErrNoPermission, got nil")
	}
	if !errors.Is(err, ErrNoPermission) {
		t.Fatalf("expected ErrNoPermission, got: %v", err)
	}
}

// TestEngine_Transition_CommentRequired verifies that a transition requiring a comment
// fails without one and succeeds with one.
func TestEngine_Transition_CommentRequired(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	// Without comment -> error.
	doc := newTaskDoc(t, "Pending Approval", 0)
	ctx := newEngineDocCtx("test-site", "approver@example.com", []string{"Approver"})

	err := engine.Transition(ctx, doc, "Reject", TransitionOpts{})
	if err == nil {
		t.Fatal("expected ErrCommentRequired, got nil")
	}
	if !errors.Is(err, ErrCommentRequired) {
		t.Fatalf("expected ErrCommentRequired, got: %v", err)
	}

	// With comment -> success.
	doc2 := newTaskDoc(t, "Pending Approval", 0)
	err = engine.Transition(ctx, doc2, "Reject", TransitionOpts{Comment: "Not good enough"})
	if err != nil {
		t.Fatalf("expected no error with comment, got: %v", err)
	}
	if doc2.Get("workflow_state") != "Rejected" {
		t.Fatalf("expected workflow_state=%q, got %v", "Rejected", doc2.Get("workflow_state"))
	}
}

// TestEngine_Transition_ConditionBlocked verifies that a condition expression can
// block a transition when the condition evaluates to false.
func TestEngine_Transition_ConditionBlocked(t *testing.T) {
	wf := simpleWorkflow()
	// Add a condition to the Submit transition.
	wf.Transitions[0].Condition = "doc.grand_total > 0"

	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0) // grand_total = 0
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err == nil {
		t.Fatal("expected ErrConditionFailed, got nil")
	}
	if !errors.Is(err, ErrConditionFailed) {
		t.Fatalf("expected ErrConditionFailed, got: %v", err)
	}
}

// TestEngine_Transition_InvalidAction verifies that performing an action not valid
// for the current state returns ErrInvalidAction.
func TestEngine_Transition_InvalidAction(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "approver@example.com", []string{"Approver"})

	err := engine.Transition(ctx, doc, "Approve", TransitionOpts{})
	if err == nil {
		t.Fatal("expected ErrInvalidAction, got nil")
	}
	if !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("expected ErrInvalidAction, got: %v", err)
	}
}

// TestEngine_GetAvailableActions verifies that the correct actions are returned
// for a given state and user.
func TestEngine_GetAvailableActions(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Pending Approval", 0)
	ctx := newEngineDocCtx("test-site", "approver@example.com", []string{"Approver"})

	actions, err := engine.GetAvailableActions(ctx, doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(actions) != 2 {
		t.Fatalf("expected 2 available actions, got %d: %+v", len(actions), actions)
	}

	// Verify the action names are Approve and Reject.
	actionNames := make(map[string]bool)
	for _, a := range actions {
		actionNames[a.Action] = true
	}
	if !actionNames["Approve"] {
		t.Error("expected 'Approve' in available actions")
	}
	if !actionNames["Reject"] {
		t.Error("expected 'Reject' in available actions")
	}
}

// TestEngine_GetAvailableActions_FilteredByRole verifies that a user without the
// required role sees no actions from Pending Approval.
func TestEngine_GetAvailableActions_FilteredByRole(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Pending Approval", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	actions, err := engine.GetAvailableActions(ctx, doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(actions) != 0 {
		t.Fatalf("expected 0 available actions for User role from Pending Approval, got %d: %+v", len(actions), actions)
	}
}

// TestEngine_GetState_Linear verifies that GetState returns the correct status for
// a linear (non-parallel) workflow.
func TestEngine_GetState_Linear(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	status, err := engine.GetState(ctx, doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.WorkflowName != "Task Workflow" {
		t.Fatalf("expected WorkflowName=%q, got %q", "Task Workflow", status.WorkflowName)
	}
	if status.IsParallel {
		t.Error("expected IsParallel=false for linear workflow")
	}
	if len(status.Branches) != 1 {
		t.Fatalf("expected 1 branch, got %d", len(status.Branches))
	}
	if status.Branches[0].CurrentState != "Draft" {
		t.Fatalf("expected branch state=%q, got %q", "Draft", status.Branches[0].CurrentState)
	}
	if status.Branches[0].Style != "grey" {
		t.Fatalf("expected branch style=%q, got %q", "grey", status.Branches[0].Style)
	}
}

// TestEngine_Transition_SetsDocStatus verifies that transitioning to a state with
// DocStatus > 0 also sets the document's docstatus field.
func TestEngine_Transition_SetsDocStatus(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	// Move to Pending Approval first.
	doc := newTaskDoc(t, "Pending Approval", 0)
	ctx := newEngineDocCtx("test-site", "approver@example.com", []string{"Approver"})

	err := engine.Transition(ctx, doc, "Approve", TransitionOpts{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	docStatus := doc.Get("docstatus")
	if docStatus != 1 {
		t.Fatalf("expected docstatus=1, got %v", docStatus)
	}
}

// TestEngine_Transition_SetsUpdateField verifies that transitioning to a state with
// UpdateField and UpdateValue sets the corresponding field on the document.
func TestEngine_Transition_SetsUpdateField(t *testing.T) {
	wf := simpleWorkflow()
	// Add UpdateField/UpdateValue to the Rejected state.
	for i, s := range wf.States {
		if s.Name == "Rejected" {
			wf.States[i].UpdateField = "workflow_state"
			wf.States[i].UpdateValue = "Rejected"
		}
	}

	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Pending Approval", 0)
	ctx := newEngineDocCtx("test-site", "approver@example.com", []string{"Approver"})

	err := engine.Transition(ctx, doc, "Reject", TransitionOpts{Comment: "rejected"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if doc.Get("workflow_state") != "Rejected" {
		t.Fatalf("expected workflow_state=%q, got %v", "Rejected", doc.Get("workflow_state"))
	}
}

// TestEngine_Transition_DefaultState verifies that when workflow_state is empty,
// the engine defaults to the first state in the workflow.
func TestEngine_Transition_DefaultState(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	// Create a doc without setting workflow_state (empty/nil).
	doc := newTaskDoc(t, "", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if doc.Get("workflow_state") != "Pending Approval" {
		t.Fatalf("expected workflow_state=%q, got %v", "Pending Approval", doc.Get("workflow_state"))
	}
}

// TestEngine_Transition_NoActiveWorkflow verifies that transitioning on a document
// without a registered workflow returns ErrNoActiveWorkflow.
func TestEngine_Transition_NoActiveWorkflow(t *testing.T) {
	engine := NewWorkflowEngine(WithRegistry(NewWorkflowRegistry()), WithEvaluator(NewConditionEvaluator()))

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err == nil {
		t.Fatal("expected ErrNoActiveWorkflow, got nil")
	}
	if !errors.Is(err, ErrNoActiveWorkflow) {
		t.Fatalf("expected ErrNoActiveWorkflow, got: %v", err)
	}
}

// TestEngine_Transition_EmptyAllowedRoles verifies that a transition with no
// AllowedRoles is permitted for any user.
func TestEngine_Transition_EmptyAllowedRoles(t *testing.T) {
	wf := simpleWorkflow()
	// Remove allowed roles from Submit transition.
	wf.Transitions[0].AllowedRoles = nil

	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "anyone@example.com", nil)

	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("expected no error with empty AllowedRoles, got: %v", err)
	}
}

// TestEngine_RegisterAutoAction verifies that auto-actions are executed after transition.
func TestEngine_RegisterAutoAction(t *testing.T) {
	wf := simpleWorkflow()
	// Add an auto-action to the Submit transition.
	wf.Transitions[0].AutoAction = "notify_approver"

	engine := setupEngine(wf)

	var autoActionCalled bool
	engine.RegisterAutoAction("notify_approver", func(ctx *document.DocContext, doc document.Document) error {
		autoActionCalled = true
		return nil
	})

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !autoActionCalled {
		t.Error("expected auto-action to be called")
	}
}

// TestEngine_CanTransition_True verifies CanTransition returns true for a valid transition.
func TestEngine_CanTransition_True(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	can, err := engine.CanTransition(ctx, doc, "Submit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !can {
		t.Error("expected CanTransition to return true")
	}
}

// TestEngine_CanTransition_False verifies CanTransition returns false when role is missing.
func TestEngine_CanTransition_False(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Pending Approval", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	can, err := engine.CanTransition(ctx, doc, "Approve")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if can {
		t.Error("expected CanTransition to return false for user without Approver role")
	}
}

// TestEngine_GetState_BranchEnteredAt verifies that the branch EnteredAt field is populated.
func TestEngine_GetState_BranchEnteredAt(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	status, err := engine.GetState(ctx, doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.Branches[0].EnteredAt.IsZero() {
		t.Error("expected non-zero EnteredAt for branch")
	}

	// EnteredAt should be recent (within the last minute).
	if time.Since(status.Branches[0].EnteredAt) > time.Minute {
		t.Error("EnteredAt is too far in the past")
	}
}

// TestEngine_FunctionalOptions verifies that functional options configure the engine.
func TestEngine_FunctionalOptions(t *testing.T) {
	reg := NewWorkflowRegistry()
	eval := NewConditionEvaluator()

	engine := NewWorkflowEngine(
		WithRegistry(reg),
		WithEvaluator(eval),
	)

	if engine.registry != reg {
		t.Error("expected registry to be set via WithRegistry")
	}
	if engine.evaluator != eval {
		t.Error("expected evaluator to be set via WithEvaluator")
	}
}

// --- Parallel (AND-split/join) workflow tests ---

// parallelWorkflow returns a workflow with a fork state, two parallel branches,
// and a join state:
//
//	Draft -> Review (IsFork) -> [Finance branch, Legal branch] -> Approved (join)
func parallelWorkflow() *meta.WorkflowMeta {
	return &meta.WorkflowMeta{
		Name: "Dual Approval", DocType: "Task", IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Draft", Style: "Info", DocStatus: 0},
			{Name: "Review", Style: "Warning", DocStatus: 0, IsFork: true},
			{Name: "Pending Finance", Style: "Warning", DocStatus: 0, BranchName: "Finance", JoinTarget: "Approved"},
			{Name: "Finance Approved", Style: "Success", DocStatus: 0, BranchName: "Finance", JoinTarget: "Approved"},
			{Name: "Pending Legal", Style: "Warning", DocStatus: 0, BranchName: "Legal", JoinTarget: "Approved"},
			{Name: "Legal Approved", Style: "Success", DocStatus: 0, BranchName: "Legal", JoinTarget: "Approved"},
			{Name: "Approved", Style: "Success", DocStatus: 1},
		},
		Transitions: []meta.Transition{
			{From: "Draft", To: "Review", Action: "Submit", AllowedRoles: []string{"User"}},
			{From: "Pending Finance", To: "Finance Approved", Action: "Approve", AllowedRoles: []string{"Finance Approver"}},
			{From: "Pending Legal", To: "Legal Approved", Action: "Approve", AllowedRoles: []string{"Legal Approver"}},
		},
	}
}

// TestEngine_Transition_Fork verifies that transitioning to a fork state creates
// parallel branches. After Submit from Draft, GetState should return IsParallel=true
// with two branches: Finance at "Pending Finance" and Legal at "Pending Legal".
func TestEngine_Transition_Fork(t *testing.T) {
	wf := parallelWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// The doc's workflow_state should be set to the fork state.
	if doc.Get("workflow_state") != "Review" {
		t.Fatalf("expected workflow_state=%q, got %v", "Review", doc.Get("workflow_state"))
	}

	// GetState should report parallel branches.
	status, err := engine.GetState(ctx, doc)
	if err != nil {
		t.Fatalf("GetState error: %v", err)
	}
	if !status.IsParallel {
		t.Error("expected IsParallel=true after fork")
	}
	if len(status.Branches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(status.Branches))
	}

	// Verify branch states.
	branchMap := make(map[string]BranchStatus)
	for _, b := range status.Branches {
		branchMap[b.BranchName] = b
	}

	fb, ok := branchMap["Finance"]
	if !ok {
		t.Fatal("expected Finance branch to exist")
	}
	if fb.CurrentState != "Pending Finance" {
		t.Fatalf("expected Finance branch state=%q, got %q", "Pending Finance", fb.CurrentState)
	}
	if !fb.IsActive {
		t.Error("expected Finance branch to be active")
	}

	lb, ok := branchMap["Legal"]
	if !ok {
		t.Fatal("expected Legal branch to exist")
	}
	if lb.CurrentState != "Pending Legal" {
		t.Fatalf("expected Legal branch state=%q, got %q", "Pending Legal", lb.CurrentState)
	}
	if !lb.IsActive {
		t.Error("expected Legal branch to be active")
	}
}

// TestEngine_Transition_ParallelBranch verifies that a single branch can transition
// independently. After fork, approving the Finance branch moves it to
// "Finance Approved" while the Legal branch remains at "Pending Legal".
func TestEngine_Transition_ParallelBranch(t *testing.T) {
	wf := parallelWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	// Fork.
	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	// Approve Finance branch.
	finCtx := newEngineDocCtx("test-site", "finance@example.com", []string{"Finance Approver"})
	err = engine.Transition(finCtx, doc, "Approve", TransitionOpts{BranchName: "Finance"})
	if err != nil {
		t.Fatalf("Finance Approve error: %v", err)
	}

	// GetState: Finance should be "Finance Approved", Legal still "Pending Legal".
	status, err := engine.GetState(ctx, doc)
	if err != nil {
		t.Fatalf("GetState error: %v", err)
	}

	branchMap := make(map[string]BranchStatus)
	for _, b := range status.Branches {
		branchMap[b.BranchName] = b
	}

	fb := branchMap["Finance"]
	if fb.CurrentState != "Finance Approved" {
		t.Fatalf("expected Finance branch state=%q, got %q", "Finance Approved", fb.CurrentState)
	}

	lb := branchMap["Legal"]
	if lb.CurrentState != "Pending Legal" {
		t.Fatalf("expected Legal branch state=%q, got %q", "Pending Legal", lb.CurrentState)
	}
	if !lb.IsActive {
		t.Error("expected Legal branch to still be active")
	}
}

// TestEngine_Transition_Join verifies that when all parallel branches complete,
// the engine auto-activates the join state. After approving both Finance and Legal,
// the doc's workflow_state should be "Approved" with docstatus=1.
func TestEngine_Transition_Join(t *testing.T) {
	wf := parallelWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	// Fork.
	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	// Approve Finance.
	finCtx := newEngineDocCtx("test-site", "finance@example.com", []string{"Finance Approver"})
	err = engine.Transition(finCtx, doc, "Approve", TransitionOpts{BranchName: "Finance"})
	if err != nil {
		t.Fatalf("Finance Approve error: %v", err)
	}

	// Approve Legal.
	legalCtx := newEngineDocCtx("test-site", "legal@example.com", []string{"Legal Approver"})
	err = engine.Transition(legalCtx, doc, "Approve", TransitionOpts{BranchName: "Legal"})
	if err != nil {
		t.Fatalf("Legal Approve error: %v", err)
	}

	// After both branches complete, workflow_state should be the join target.
	if doc.Get("workflow_state") != "Approved" {
		t.Fatalf("expected workflow_state=%q, got %v", "Approved", doc.Get("workflow_state"))
	}

	// docstatus should be set from the join state.
	if doc.Get("docstatus") != 1 {
		t.Fatalf("expected docstatus=1, got %v", doc.Get("docstatus"))
	}

	// GetState should no longer be parallel (branches cleared).
	status, err := engine.GetState(ctx, doc)
	if err != nil {
		t.Fatalf("GetState error: %v", err)
	}
	if status.IsParallel {
		t.Error("expected IsParallel=false after join (no active branches)")
	}
}

// TestEngine_GetAvailableActions_Parallel verifies that GetAvailableActions returns
// per-branch actions when the document is in a forked state.
func TestEngine_GetAvailableActions_Parallel(t *testing.T) {
	wf := parallelWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	// Fork.
	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	// Finance Approver should see the Finance branch action.
	finCtx := newEngineDocCtx("test-site", "finance@example.com", []string{"Finance Approver"})
	actions, err := engine.GetAvailableActions(finCtx, doc)
	if err != nil {
		t.Fatalf("GetAvailableActions error: %v", err)
	}

	if len(actions) == 0 {
		t.Fatal("expected at least 1 available action for Finance Approver")
	}

	// Verify the Finance branch action is present.
	found := false
	for _, a := range actions {
		if a.BranchName == "Finance" && a.Action == "Approve" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Finance branch Approve action, got: %+v", actions)
	}
}

// TestEngine_Transition_BranchNotFound verifies that transitioning with a non-existent
// branch name returns ErrBranchNotFound.
func TestEngine_Transition_BranchNotFound(t *testing.T) {
	wf := parallelWorkflow()
	engine := setupEngine(wf)

	doc := newTaskDoc(t, "Draft", 0)
	ctx := newEngineDocCtx("test-site", "user@example.com", []string{"User"})

	// Fork.
	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	// Try transition on a non-existent branch.
	err = engine.Transition(ctx, doc, "Approve", TransitionOpts{BranchName: "NonExistent"})
	if err == nil {
		t.Fatal("expected ErrBranchNotFound, got nil")
	}
	if !errors.Is(err, ErrBranchNotFound) {
		t.Fatalf("expected ErrBranchNotFound, got: %v", err)
	}
}
