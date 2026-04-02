package hooks

import (
	"fmt"

	"github.com/osama1998H/moca/pkg/document"
)

// DocEventDispatcher implements document.HookDispatcher by resolving handlers
// from a HookRegistry and executing them sequentially in priority/dependency
// order.
type DocEventDispatcher struct {
	registry *HookRegistry
}

// NewDocEventDispatcher returns a dispatcher backed by the given registry.
func NewDocEventDispatcher(registry *HookRegistry) *DocEventDispatcher {
	return &DocEventDispatcher{registry: registry}
}

// Dispatch resolves all handlers for doctype+event from the registry and calls
// them sequentially. Returns the first non-nil error. Returns nil if no
// handlers are registered.
func (d *DocEventDispatcher) Dispatch(ctx *document.DocContext, doc document.Document, doctype string, event document.DocEvent) error {
	handlers, err := d.registry.Resolve(doctype, event)
	if err != nil {
		return fmt.Errorf("hooks: resolve %s/%s: %w", doctype, event, err)
	}
	for _, h := range handlers {
		if err := h.Handler(ctx, doc); err != nil {
			return fmt.Errorf("hooks: %s/%s handler %q: %w", doctype, event, h.AppName, err)
		}
	}
	return nil
}
