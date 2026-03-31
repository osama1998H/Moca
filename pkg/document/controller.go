package document

import "sync"

// BaseController is an embeddable no-op implementation of DocLifecycle.
// Custom controllers embed it and override only the methods they need:
//
//	type OrderController struct {
//	    BaseController
//	}
//	func (c *OrderController) BeforeInsert(ctx *DocContext, doc Document) error {
//	    // set defaults, validate business rules, etc.
//	    return nil
//	}
//
// Embedding BaseController satisfies the full DocLifecycle interface without
// requiring every method to be implemented.
type BaseController struct{}

// compile-time assertion that BaseController satisfies DocLifecycle.
var _ DocLifecycle = BaseController{}

func (BaseController) BeforeInsert(_ *DocContext, _ Document) error    { return nil }
func (BaseController) AfterInsert(_ *DocContext, _ Document) error     { return nil }
func (BaseController) BeforeValidate(_ *DocContext, _ Document) error  { return nil }
func (BaseController) Validate(_ *DocContext, _ Document) error        { return nil }
func (BaseController) BeforeSave(_ *DocContext, _ Document) error      { return nil }
func (BaseController) AfterSave(_ *DocContext, _ Document) error       { return nil }
func (BaseController) OnUpdate(_ *DocContext, _ Document) error        { return nil }
func (BaseController) BeforeSubmit(_ *DocContext, _ Document) error    { return nil }
func (BaseController) OnSubmit(_ *DocContext, _ Document) error        { return nil }
func (BaseController) BeforeCancel(_ *DocContext, _ Document) error    { return nil }
func (BaseController) OnCancel(_ *DocContext, _ Document) error        { return nil }
func (BaseController) OnTrash(_ *DocContext, _ Document) error         { return nil }
func (BaseController) AfterDelete(_ *DocContext, _ Document) error     { return nil }
func (BaseController) OnChange(_ *DocContext, _ Document) error        { return nil }
func (BaseController) BeforeRename(_ *DocContext, _ Document, _, _ string) error { return nil }
func (BaseController) AfterRename(_ *DocContext, _ Document, _, _ string) error  { return nil }

// DocLifecycleFactory is a function that creates a fresh DocLifecycle instance.
// Factories are invoked on every DocManager CRUD call so each request gets its
// own controller instance (no shared mutable state between concurrent requests).
type DocLifecycleFactory func() DocLifecycle

// ControllerRegistry maps doctype names to DocLifecycle implementations.
// It supports two registration modes:
//
//   - TypeOverrides: fully replaces the controller for a specific doctype.
//     Registered via RegisterOverride. The factory is called fresh per request.
//
//   - TypeExtensions: wraps the resolved controller with additional behaviour.
//     Registered via RegisterExtension. Extensions are applied in registration
//     order (first registered = outermost wrapper).
//
// Resolution order: TypeOverrides[doctype] → BaseController (fallback) →
// TypeExtensions applied in order.
//
// ControllerRegistry is safe for concurrent use.
// ControllerRegistry field order: pointer-containing fields first so that the
// GC scanner can stop before reaching the non-pointer sync.RWMutex tail.
type ControllerRegistry struct {
	typeOverrides  map[string]DocLifecycleFactory
	typeExtensions map[string][]DocLifecycleExtension
	mu             sync.RWMutex
}

// NewControllerRegistry returns an empty ControllerRegistry ready for use.
func NewControllerRegistry() *ControllerRegistry {
	return &ControllerRegistry{
		typeOverrides:  make(map[string]DocLifecycleFactory),
		typeExtensions: make(map[string][]DocLifecycleExtension),
	}
}

// RegisterOverride registers factory as the primary controller for doctype.
// Registering the same doctype twice replaces the previous factory.
// Safe to call concurrently.
func (r *ControllerRegistry) RegisterOverride(doctype string, factory DocLifecycleFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.typeOverrides[doctype] = factory
}

// RegisterExtension appends ext to the extension chain for doctype.
// Extensions are applied in registration order (first = outermost wrapper).
// Safe to call concurrently.
func (r *ControllerRegistry) RegisterExtension(doctype string, ext DocLifecycleExtension) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.typeExtensions[doctype] = append(r.typeExtensions[doctype], ext)
}

// Resolve returns the DocLifecycle to use for doctype. A fresh controller
// instance is constructed on every call so controllers are not shared between
// concurrent requests.
//
// Resolution:
//  1. If TypeOverrides[doctype] exists, call the factory to get the base.
//  2. Otherwise, use BaseController{} as the base.
//  3. Apply TypeExtensions[doctype] in registration order (each wraps the current base).
func (r *ControllerRegistry) Resolve(doctype string) DocLifecycle {
	r.mu.RLock()
	factory, hasOverride := r.typeOverrides[doctype]
	// Copy the extension slice to avoid holding the lock during Wrap calls.
	exts := make([]DocLifecycleExtension, len(r.typeExtensions[doctype]))
	copy(exts, r.typeExtensions[doctype])
	r.mu.RUnlock()

	var base DocLifecycle
	if hasOverride {
		base = factory()
	} else {
		base = BaseController{}
	}

	// Build the wrapper chain from the inside out so that the first registered
	// extension becomes the outermost caller (first to execute).
	// Iteration in reverse means: last registered wraps base first, then each
	// prior registration wraps the previous result, putting first-registered on top.
	current := base
	for i := len(exts) - 1; i >= 0; i-- {
		current = exts[i].Wrap(current)
	}
	return current
}
