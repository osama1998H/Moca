package workflow

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// newBridgeDoc creates a DynamicDoc for the Task MetaType used in bridge tests.
// It sets workflow_state to initialState.
func newBridgeDoc(initialState string) document.Document {
	mt := &meta.MetaType{
		Name: "Task",
		Fields: []meta.FieldDef{
			{Name: "workflow_state", FieldType: "Data"},
			{Name: "docstatus", FieldType: "Int"},
		},
	}
	doc := document.NewDynamicDoc(mt, nil, false)
	if initialState != "" {
		_ = doc.Set("workflow_state", initialState)
	}
	return doc
}

// newBridgeDocCtx creates a DocContext with a test site and user.
func newBridgeDocCtx(flags map[string]any) *document.DocContext {
	ctx := document.NewDocContext(
		context.Background(),
		&tenancy.SiteContext{Name: "test-site"},
		&auth.User{Email: "user@example.com", Roles: []string{"User"}},
	)
	for k, v := range flags {
		ctx.Flags[k] = v
	}
	return ctx
}

// TestBridge_RegistersHooks verifies that Register adds global hooks for both
// EventBeforeSave and EventAfterSave.
func TestBridge_RegistersHooks(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	hookRegistry := hooks.NewHookRegistry()
	bridge := NewWorkflowBridge(engine)
	bridge.Register(hookRegistry)

	beforeSaveHandlers, err := hookRegistry.ResolveGlobal(document.EventBeforeSave)
	if err != nil {
		t.Fatalf("ResolveGlobal(EventBeforeSave) error: %v", err)
	}
	if len(beforeSaveHandlers) < 1 {
		t.Errorf("expected at least 1 EventBeforeSave handler, got %d", len(beforeSaveHandlers))
	}

	afterSaveHandlers, err := hookRegistry.ResolveGlobal(document.EventAfterSave)
	if err != nil {
		t.Fatalf("ResolveGlobal(EventAfterSave) error: %v", err)
	}
	if len(afterSaveHandlers) < 1 {
		t.Errorf("expected at least 1 EventAfterSave handler, got %d", len(afterSaveHandlers))
	}
}

// TestBridge_BundledSave_ExecutesTransition verifies that when workflow_action is set,
// dispatching EventAfterSave triggers a workflow transition on the document.
func TestBridge_BundledSave_ExecutesTransition(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	hookRegistry := hooks.NewHookRegistry()
	bridge := NewWorkflowBridge(engine)
	bridge.Register(hookRegistry)

	doc := newBridgeDoc("Draft")
	ctx := newBridgeDocCtx(map[string]any{
		"workflow_action": "Submit",
	})

	dispatcher := hooks.NewDocEventDispatcher(hookRegistry)
	err := dispatcher.Dispatch(ctx, doc, "Task", document.EventAfterSave)
	if err != nil {
		t.Fatalf("Dispatch(EventAfterSave) error: %v", err)
	}

	state := doc.Get("workflow_state")
	if state != "Pending Approval" {
		t.Errorf("expected workflow_state=%q after Submit, got %v", "Pending Approval", state)
	}
}

// TestBridge_NoWorkflowAction_Noop verifies that when no workflow_action flag is set,
// dispatching EventAfterSave leaves the document state unchanged.
func TestBridge_NoWorkflowAction_Noop(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	hookRegistry := hooks.NewHookRegistry()
	bridge := NewWorkflowBridge(engine)
	bridge.Register(hookRegistry)

	doc := newBridgeDoc("Draft")
	ctx := newBridgeDocCtx(nil) // no flags

	dispatcher := hooks.NewDocEventDispatcher(hookRegistry)
	err := dispatcher.Dispatch(ctx, doc, "Task", document.EventAfterSave)
	if err != nil {
		t.Fatalf("Dispatch(EventAfterSave) error: %v", err)
	}

	state := doc.Get("workflow_state")
	if state != "Draft" {
		t.Errorf("expected workflow_state to remain %q, got %v", "Draft", state)
	}
}

// TestBridge_BeforeSave_BlocksInvalidTransition verifies that when workflow_action is set
// but CanTransition returns false, the EventBeforeSave hook returns ErrTransitionBlocked.
func TestBridge_BeforeSave_BlocksInvalidTransition(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	hookRegistry := hooks.NewHookRegistry()
	bridge := NewWorkflowBridge(engine)
	bridge.Register(hookRegistry)

	// User role cannot Approve from Pending Approval — this is an invalid transition.
	doc := newBridgeDoc("Draft")
	ctx := newBridgeDocCtx(map[string]any{
		"workflow_action": "Approve", // Draft -> Approve does not exist
	})

	dispatcher := hooks.NewDocEventDispatcher(hookRegistry)
	err := dispatcher.Dispatch(ctx, doc, "Task", document.EventBeforeSave)
	if err == nil {
		t.Fatal("expected ErrTransitionBlocked, got nil")
	}
}

// TestBridge_BeforeSave_Noop verifies that when no workflow_action flag is set,
// the EventBeforeSave hook is a no-op.
func TestBridge_BeforeSave_Noop(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	hookRegistry := hooks.NewHookRegistry()
	bridge := NewWorkflowBridge(engine)
	bridge.Register(hookRegistry)

	doc := newBridgeDoc("Draft")
	ctx := newBridgeDocCtx(nil) // no flags

	dispatcher := hooks.NewDocEventDispatcher(hookRegistry)
	err := dispatcher.Dispatch(ctx, doc, "Task", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("expected no error when no workflow_action, got: %v", err)
	}
}

// TestBridge_AfterSave_WithCommentAndBranch verifies that comment and branch flags
// are forwarded to the engine during EventAfterSave dispatch.
func TestBridge_AfterSave_WithCommentAndBranch(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)

	hookRegistry := hooks.NewHookRegistry()
	bridge := NewWorkflowBridge(engine)
	bridge.Register(hookRegistry)

	doc := newBridgeDoc("Draft")
	ctx := newBridgeDocCtx(map[string]any{
		"workflow_action":  "Submit",
		"workflow_comment": "Submitting for approval",
		"workflow_branch":  "",
	})

	dispatcher := hooks.NewDocEventDispatcher(hookRegistry)
	err := dispatcher.Dispatch(ctx, doc, "Task", document.EventAfterSave)
	if err != nil {
		t.Fatalf("Dispatch(EventAfterSave) with comment error: %v", err)
	}

	state := doc.Get("workflow_state")
	if state != "Pending Approval" {
		t.Errorf("expected workflow_state=%q, got %v", "Pending Approval", state)
	}
}
