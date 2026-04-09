package apps

import (
	"fmt"
	"sync"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
)

// InitFunc is the signature every app's Initialize function must match.
// It receives the shared controller and hook registries so the app can
// register its document controllers and lifecycle hooks.
type InitFunc func(cr *document.ControllerRegistry, hr *hooks.HookRegistry)

// initEntry pairs an app name with its Initialize function.
type initEntry struct {
	fn   InitFunc
	name string
}

var (
	initMu       sync.Mutex
	initRegistry []initEntry
	initNames    map[string]struct{}
)

func init() {
	initNames = make(map[string]struct{})
}

// RegisterInit queues an app's Initialize function for later invocation by
// InitializeAll. It is safe to call from init() functions.
// Returns an error if an app with the same name has already been registered.
func RegisterInit(name string, fn InitFunc) error {
	initMu.Lock()
	defer initMu.Unlock()

	if _, exists := initNames[name]; exists {
		return fmt.Errorf("app %q already registered", name)
	}
	initNames[name] = struct{}{}
	initRegistry = append(initRegistry, initEntry{name: name, fn: fn})
	return nil
}

// MustRegisterInit calls RegisterInit and panics on duplicate registration.
// This is the intended function for app init() hooks: a duplicate app name
// is a configuration error that must surface at program startup.
func MustRegisterInit(name string, fn InitFunc) {
	if err := RegisterInit(name, fn); err != nil {
		panic("moca/apps: " + err.Error())
	}
}

// InitializeAll invokes every registered Initialize function in registration
// order, passing the shared controller and hook registries. Call this once
// during server or CLI startup after constructing the registries.
func InitializeAll(cr *document.ControllerRegistry, hr *hooks.HookRegistry) error {
	initMu.Lock()
	entries := make([]initEntry, len(initRegistry))
	copy(entries, initRegistry)
	initMu.Unlock()

	for _, e := range entries {
		e.fn(cr, hr)
	}
	return nil
}

// RegisteredInitNames returns the names of all registered apps.
// Useful for tests and debug logging.
func RegisteredInitNames() []string {
	initMu.Lock()
	defer initMu.Unlock()

	names := make([]string, 0, len(initRegistry))
	for _, e := range initRegistry {
		names = append(names, e.name)
	}
	return names
}

// ResetForTesting clears all registrations. ONLY for use in tests.
func ResetForTesting() {
	initMu.Lock()
	defer initMu.Unlock()

	initRegistry = nil
	initNames = make(map[string]struct{})
}
