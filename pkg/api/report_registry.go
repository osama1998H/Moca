package api

import (
	"fmt"
	"sort"
	"sync"

	"github.com/osama1998H/moca/pkg/orm"
)

// ReportRegistry maps report names to ReportDef definitions.
// It is safe for concurrent use.
type ReportRegistry struct {
	reports sync.Map // name -> orm.ReportDef
}

// NewReportRegistry creates an empty report registry.
func NewReportRegistry() *ReportRegistry {
	return &ReportRegistry{}
}

// Register adds a report definition. Returns an error if the name is empty
// or already registered.
func (r *ReportRegistry) Register(def orm.ReportDef) error {
	if def.Name == "" {
		return fmt.Errorf("report: name is required")
	}
	if _, loaded := r.reports.LoadOrStore(def.Name, def); loaded {
		return fmt.Errorf("report %q already registered", def.Name)
	}
	return nil
}

// Get returns the report definition registered under name, or false if not found.
func (r *ReportRegistry) Get(name string) (orm.ReportDef, bool) {
	v, ok := r.reports.Load(name)
	if !ok {
		return orm.ReportDef{}, false
	}
	def, _ := v.(orm.ReportDef) //nolint:errcheck // type is guaranteed by Store
	return def, true
}

// List returns all registered report definitions sorted by name.
func (r *ReportRegistry) List() []orm.ReportDef {
	var defs []orm.ReportDef
	r.reports.Range(func(_, value any) bool {
		def, _ := value.(orm.ReportDef) //nolint:errcheck // type is guaranteed by Store
		defs = append(defs, def)
		return true
	})
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
	return defs
}
