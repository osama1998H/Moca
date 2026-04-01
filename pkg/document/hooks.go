package document

// HookDispatcher resolves and executes registered hooks for a given doctype
// and lifecycle event. Implementations live outside this package (e.g.
// pkg/hooks) to avoid import cycles.
//
// Dispatch calls all resolved handlers sequentially in priority/dependency
// order and returns the first non-nil error. The caller (DocManager) decides
// whether that error is fatal or logged.
type HookDispatcher interface {
	Dispatch(ctx *DocContext, doc Document, doctype string, event DocEvent) error
}
