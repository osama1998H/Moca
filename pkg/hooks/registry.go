package hooks

import (
	"sync"

	"github.com/osama1998H/moca/pkg/document"
)

// DefaultPriority is used when a PrioritizedHandler is registered with Priority == 0.
const DefaultPriority = 500

// DocEventHandler is the function signature for document lifecycle hooks.
// Hooks receive the same DocContext and Document interface as controllers.
type DocEventHandler func(ctx *document.DocContext, doc document.Document) error

// PrioritizedHandler wraps a DocEventHandler with ordering metadata.
// Priority is numeric (lower = runs first). A zero Priority is treated as
// DefaultPriority (500) during registration. DependsOn declares that this
// handler must run after all handlers from the named apps.
type PrioritizedHandler struct {
	Handler   DocEventHandler
	AppName   string
	DependsOn []string
	Priority  int
}

// HookRegistry stores document lifecycle event hooks registered by apps.
// Thread-safe for concurrent Register and Resolve calls.
//
// Field order: pointer-containing fields first so the GC scanner can stop
// before reaching the non-pointer sync.RWMutex tail.
type HookRegistry struct {
	docEvents       map[string]map[document.DocEvent][]PrioritizedHandler
	globalDocEvents map[document.DocEvent][]PrioritizedHandler
	mu              sync.RWMutex
}

// NewHookRegistry returns an empty HookRegistry ready for use.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		docEvents:       make(map[string]map[document.DocEvent][]PrioritizedHandler),
		globalDocEvents: make(map[document.DocEvent][]PrioritizedHandler),
	}
}

// Register adds a handler for a specific doctype and event.
// If handler.Priority is 0, it defaults to DefaultPriority (500).
// Safe to call concurrently.
func (r *HookRegistry) Register(doctype string, event document.DocEvent, handler PrioritizedHandler) {
	if handler.Priority == 0 {
		handler.Priority = DefaultPriority
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	byEvent, ok := r.docEvents[doctype]
	if !ok {
		byEvent = make(map[document.DocEvent][]PrioritizedHandler)
		r.docEvents[doctype] = byEvent
	}
	byEvent[event] = append(byEvent[event], handler)
}

// RegisterGlobal adds a cross-cutting handler that fires for all doctypes
// on the given event. If handler.Priority is 0, it defaults to DefaultPriority.
// Safe to call concurrently.
func (r *HookRegistry) RegisterGlobal(event document.DocEvent, handler PrioritizedHandler) {
	if handler.Priority == 0 {
		handler.Priority = DefaultPriority
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.globalDocEvents[event] = append(r.globalDocEvents[event], handler)
}

// ResolveGlobal returns only the global handlers for the given event, sorted by
// dependency order and priority. Per-doctype handlers are not included.
//
// Returns (nil, nil) when no global handlers are registered for the event.
// Returns a *CircularDependencyError if the dependency graph has a cycle.
// Safe to call concurrently.
func (r *HookRegistry) ResolveGlobal(event document.DocEvent) ([]PrioritizedHandler, error) {
	r.mu.RLock()
	var global []PrioritizedHandler
	if handlers, ok := r.globalDocEvents[event]; ok {
		global = make([]PrioritizedHandler, len(handlers))
		copy(global, handlers)
	}
	r.mu.RUnlock()

	if len(global) == 0 {
		return nil, nil
	}
	return resolveOrder(global)
}

// Resolve returns handlers for the given doctype and event, sorted by
// dependency order and priority. Global and per-doctype handlers are merged
// before sorting.
//
// Returns (nil, nil) when no handlers are registered.
// Returns a *CircularDependencyError if the dependency graph has a cycle.
// Safe to call concurrently.
func (r *HookRegistry) Resolve(doctype string, event document.DocEvent) ([]PrioritizedHandler, error) {
	r.mu.RLock()

	// Snapshot slices while holding the lock.
	var local []PrioritizedHandler
	if byEvent, ok := r.docEvents[doctype]; ok {
		if handlers, ok := byEvent[event]; ok {
			local = make([]PrioritizedHandler, len(handlers))
			copy(local, handlers)
		}
	}

	var global []PrioritizedHandler
	if handlers, ok := r.globalDocEvents[event]; ok {
		global = make([]PrioritizedHandler, len(handlers))
		copy(global, handlers)
	}

	r.mu.RUnlock()

	merged := append(local, global...)
	if len(merged) == 0 {
		return nil, nil
	}

	return resolveOrder(merged)
}
