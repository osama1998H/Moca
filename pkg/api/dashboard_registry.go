package api

import (
	"fmt"
	"sync"
)

// DashDef declares a dashboard layout with widgets.
type DashDef struct {
	Name    string       `json:"name"`
	Label   string       `json:"label"`
	Widgets []DashWidget `json:"widgets"`
}

// DashWidget is a single dashboard card/chart/list.
type DashWidget struct {
	Config map[string]any `json:"config"` // type-specific configuration
	Type   string         `json:"type"`   // "number_card", "chart", "list", "shortcut"
}

// DashboardRegistry maps dashboard names to DashDef definitions.
// It is safe for concurrent use.
type DashboardRegistry struct {
	dashboards sync.Map // name -> DashDef
}

// NewDashboardRegistry creates an empty dashboard registry.
func NewDashboardRegistry() *DashboardRegistry {
	return &DashboardRegistry{}
}

// Register adds a dashboard definition. Returns an error if the name is empty
// or already registered.
func (r *DashboardRegistry) Register(def DashDef) error {
	if def.Name == "" {
		return fmt.Errorf("dashboard: name is required")
	}
	if _, loaded := r.dashboards.LoadOrStore(def.Name, def); loaded {
		return fmt.Errorf("dashboard %q already registered", def.Name)
	}
	return nil
}

// Get returns the dashboard definition registered under name, or false if not found.
func (r *DashboardRegistry) Get(name string) (DashDef, bool) {
	v, ok := r.dashboards.Load(name)
	if !ok {
		return DashDef{}, false
	}
	def, _ := v.(DashDef) //nolint:errcheck // type is guaranteed by Store
	return def, true
}
