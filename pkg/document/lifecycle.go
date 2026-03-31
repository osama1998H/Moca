package document

import "fmt"

// DocEvent is the type for lifecycle event constants. String values are used
// for readability in logs, hooks, and audit records.
type DocEvent string

// The 14 lifecycle event constants fired during document CRUD operations.
// BeforeRename and AfterRename are handled by dedicated DocLifecycle methods
// rather than DocEvent constants (they carry extra parameters).
const (
	EventBeforeInsert   DocEvent = "before_insert"
	EventAfterInsert    DocEvent = "after_insert"
	EventBeforeValidate DocEvent = "before_validate"
	EventValidate       DocEvent = "validate"
	EventBeforeSave     DocEvent = "before_save"
	EventAfterSave      DocEvent = "after_save"
	EventOnUpdate       DocEvent = "on_update"
	EventBeforeSubmit   DocEvent = "before_submit"
	EventOnSubmit       DocEvent = "on_submit"
	EventBeforeCancel   DocEvent = "before_cancel"
	EventOnCancel       DocEvent = "on_cancel"
	EventOnTrash        DocEvent = "on_trash"
	EventAfterDelete    DocEvent = "after_delete"
	EventOnChange       DocEvent = "on_change"
)

// DocLifecycle is the interface implemented by document controllers. Every
// method corresponds to a point in the document lifecycle where custom logic
// can be injected. All methods are optional -- embed BaseController to get
// no-op defaults so you only implement the hooks you need.
//
// The 14 standard methods accept a DocContext and a Document. The 2 rename
// methods additionally receive the old and new document names.
//
// Controllers receive the document as a Document interface so that
// implementations outside the document package can interact with it using the
// full public API (Get, Set, GetChild, AddChild, etc.).
type DocLifecycle interface {
	// Insert hooks
	BeforeInsert(ctx *DocContext, doc Document) error
	AfterInsert(ctx *DocContext, doc Document) error

	// Validate hooks (fired on both insert and update)
	BeforeValidate(ctx *DocContext, doc Document) error
	Validate(ctx *DocContext, doc Document) error

	// Save hooks (fired on both insert and update)
	BeforeSave(ctx *DocContext, doc Document) error
	AfterSave(ctx *DocContext, doc Document) error

	// Update hook
	OnUpdate(ctx *DocContext, doc Document) error

	// Submit/cancel hooks (for submittable doctypes; MS-23 adds Submit/Cancel CRUD methods)
	BeforeSubmit(ctx *DocContext, doc Document) error
	OnSubmit(ctx *DocContext, doc Document) error
	BeforeCancel(ctx *DocContext, doc Document) error
	OnCancel(ctx *DocContext, doc Document) error

	// Delete hooks
	OnTrash(ctx *DocContext, doc Document) error
	AfterDelete(ctx *DocContext, doc Document) error

	// Change hook (idempotent, may fire multiple times; errors are logged not propagated)
	OnChange(ctx *DocContext, doc Document) error

	// Rename hooks (not in DocEvent constants; carry extra parameters)
	BeforeRename(ctx *DocContext, doc Document, oldName, newName string) error
	AfterRename(ctx *DocContext, doc Document, oldName, newName string) error
}

// DocLifecycleExtension wraps an existing DocLifecycle with additional
// behaviour. Implement this interface to intercept lifecycle calls without
// fully replacing the base controller.
//
// Example: a cross-cutting audit extension that logs all events before
// delegating to the inner controller:
//
//	type auditExt struct{}
//	func (auditExt) Wrap(inner DocLifecycle) DocLifecycle {
//	    return &auditedController{inner: inner}
//	}
type DocLifecycleExtension interface {
	Wrap(inner DocLifecycle) DocLifecycle
}

// dispatchEvent calls the appropriate DocLifecycle method for event.
// Returns an error for unknown event names. The doc is passed as Document so
// the controller receives the public interface.
func dispatchEvent(ctrl DocLifecycle, event DocEvent, ctx *DocContext, doc Document) error {
	switch event {
	case EventBeforeInsert:
		return ctrl.BeforeInsert(ctx, doc)
	case EventAfterInsert:
		return ctrl.AfterInsert(ctx, doc)
	case EventBeforeValidate:
		return ctrl.BeforeValidate(ctx, doc)
	case EventValidate:
		return ctrl.Validate(ctx, doc)
	case EventBeforeSave:
		return ctrl.BeforeSave(ctx, doc)
	case EventAfterSave:
		return ctrl.AfterSave(ctx, doc)
	case EventOnUpdate:
		return ctrl.OnUpdate(ctx, doc)
	case EventBeforeSubmit:
		return ctrl.BeforeSubmit(ctx, doc)
	case EventOnSubmit:
		return ctrl.OnSubmit(ctx, doc)
	case EventBeforeCancel:
		return ctrl.BeforeCancel(ctx, doc)
	case EventOnCancel:
		return ctrl.OnCancel(ctx, doc)
	case EventOnTrash:
		return ctrl.OnTrash(ctx, doc)
	case EventAfterDelete:
		return ctrl.AfterDelete(ctx, doc)
	case EventOnChange:
		return ctrl.OnChange(ctx, doc)
	default:
		return fmt.Errorf("lifecycle: unknown event %q", event)
	}
}
