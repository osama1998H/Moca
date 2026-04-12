package workflow

import (
	"fmt"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
)

// WorkflowBridge connects the WorkflowEngine to the document lifecycle via the
// HookRegistry. It registers global hooks for EventBeforeSave and EventAfterSave
// that read workflow action flags from the DocContext and delegate to the engine.
//
// Usage:
//
//	bridge := NewWorkflowBridge(engine)
//	bridge.Register(hookRegistry)
type WorkflowBridge struct {
	engine *WorkflowEngine
}

// NewWorkflowBridge constructs a WorkflowBridge backed by the given engine.
func NewWorkflowBridge(engine *WorkflowEngine) *WorkflowBridge {
	return &WorkflowBridge{engine: engine}
}

// Register adds two global hooks to r:
//
//  1. EventBeforeSave (priority 100): if ctx.Flags["workflow_action"] is a
//     non-empty string, calls engine.CanTransition. Returns ErrTransitionBlocked
//     if the transition is not permitted.
//
//  2. EventAfterSave (priority 100): if ctx.Flags["workflow_action"] is a
//     non-empty string, calls engine.Transition with opts populated from
//     ctx.Flags["workflow_comment"] and ctx.Flags["workflow_branch"].
//
// Both hooks are no-ops when "workflow_action" is absent or empty.
func (b *WorkflowBridge) Register(r *hooks.HookRegistry) {
	r.RegisterGlobal(document.EventBeforeSave, hooks.PrioritizedHandler{
		AppName:  "workflow",
		Priority: 100,
		Handler:  b.beforeSaveHandler(),
	})

	r.RegisterGlobal(document.EventAfterSave, hooks.PrioritizedHandler{
		AppName:  "workflow",
		Priority: 100,
		Handler:  b.afterSaveHandler(),
	})
}

// beforeSaveHandler returns the EventBeforeSave hook function. It checks whether
// the requested workflow transition is permitted before the document is saved.
func (b *WorkflowBridge) beforeSaveHandler() hooks.DocEventHandler {
	return func(ctx *document.DocContext, doc document.Document) error {
		action := workflowAction(ctx)
		if action == "" {
			return nil
		}

		can, err := b.engine.CanTransition(ctx, doc, action)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrTransitionBlocked, err)
		}
		if !can {
			return fmt.Errorf("%w: action %q is not permitted", ErrTransitionBlocked, action)
		}
		return nil
	}
}

// afterSaveHandler returns the EventAfterSave hook function. It executes the
// workflow transition after the document has been persisted.
func (b *WorkflowBridge) afterSaveHandler() hooks.DocEventHandler {
	return func(ctx *document.DocContext, doc document.Document) error {
		action := workflowAction(ctx)
		if action == "" {
			return nil
		}

		comment := flagString(ctx, "workflow_comment")
		branch := flagString(ctx, "workflow_branch")

		return b.engine.Transition(ctx, doc, action, TransitionOpts{
			Comment:    comment,
			BranchName: branch,
		})
	}
}

// workflowAction extracts the "workflow_action" flag from ctx as a string.
// Returns "" if absent, nil, or not a string.
func workflowAction(ctx *document.DocContext) string {
	if ctx == nil || ctx.Flags == nil {
		return ""
	}
	return flagString(ctx, "workflow_action")
}

// flagString returns the value of key from ctx.Flags as a string, or "" if
// absent, nil, or not a string type.
func flagString(ctx *document.DocContext, key string) string {
	if ctx == nil || ctx.Flags == nil {
		return ""
	}
	v, ok := ctx.Flags[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}
