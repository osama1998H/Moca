package factory

import (
	"context"
	"fmt"
	"time"

	"github.com/brianvoe/gofakeit/v7"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

// InsertEnv provides the minimal interface needed by GenerateAndInsert to
// create documents. Both testutils.TestEnv and CLI adapters implement this.
type InsertEnv interface {
	GetDocManager() *document.DocManager
	GetDocContext() *document.DocContext
	GetSiteName() string
}

// DocFactory generates valid documents by introspecting MetaType field
// definitions and producing realistic values using gofakeit.
type DocFactory struct {
	registry  *meta.Registry
	faker     *gofakeit.Faker
	linkCache map[string][]string // doctype -> cached document names
}

// New creates a DocFactory with the given registry and options.
func New(registry *meta.Registry, opts ...Option) *DocFactory {
	cfg := &factoryConfig{
		seed: time.Now().UnixNano(),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return &DocFactory{
		registry:  registry,
		faker:     gofakeit.New(uint64(cfg.seed)),
		linkCache: make(map[string][]string),
	}
}

// Generate produces count valid document value maps for the given doctype.
// Values respect all MetaType validation constraints. Link fields are populated
// with placeholder empty strings — use GenerateAndInsert for full Link resolution.
func (f *DocFactory) Generate(ctx context.Context, site, doctype string, count int, opts ...GenOption) ([]map[string]any, error) {
	cfg := defaultGenConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	mt, err := f.registry.Get(ctx, site, doctype)
	if err != nil {
		return nil, fmt.Errorf("get MetaType %q: %w", doctype, err)
	}

	results := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		values, err := f.generateDoc(ctx, site, mt, i, cfg)
		if err != nil {
			return nil, fmt.Errorf("generate doc %d: %w", i, err)
		}
		results = append(results, values)
	}

	return results, nil
}

// GenerateAndInsert generates documents and inserts them via DocManager,
// handling Link field dependency resolution by creating referenced documents
// first. Returns the created DynamicDocs.
func (f *DocFactory) GenerateAndInsert(ctx context.Context, env InsertEnv, doctype string, count int, opts ...GenOption) ([]*document.DynamicDoc, error) {
	cfg := defaultGenConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	site := env.GetSiteName()

	// Build dependency graph and resolve in topological order.
	depG, err := buildDepGraph(ctx, f.registry, site, doctype)
	if err != nil {
		return nil, fmt.Errorf("build dependency graph for %q: %w", doctype, err)
	}

	order := depG.topoSort(doctype)

	// Generate and insert dependency doctypes first (1 each, unless it's the target).
	for _, dt := range order {
		if dt == doctype {
			continue
		}
		// Create at least 3 dependency documents so Link fields have variety.
		if _, ok := f.linkCache[dt]; !ok {
			depDocs, err := f.insertDocs(ctx, env, dt, 3, cfg, depG)
			if err != nil {
				return nil, fmt.Errorf("create dependency %q: %w", dt, err)
			}
			names := make([]string, len(depDocs))
			for j, d := range depDocs {
				names[j] = d.Name()
			}
			f.linkCache[dt] = names
		}
	}

	// Generate and insert the target doctype.
	return f.insertDocs(ctx, env, doctype, count, cfg, depG)
}

func (f *DocFactory) insertDocs(ctx context.Context, env InsertEnv, doctype string, count int, cfg *genConfig, depG *depGraph) ([]*document.DynamicDoc, error) {
	site := env.GetSiteName()
	mt, err := f.registry.Get(ctx, site, doctype)
	if err != nil {
		return nil, fmt.Errorf("get MetaType %q: %w", doctype, err)
	}

	docs := make([]*document.DynamicDoc, 0, count)
	docCtx := env.GetDocContext()
	dm := env.GetDocManager()

	for i := 0; i < count; i++ {
		values, err := f.generateDoc(ctx, site, mt, i, cfg)
		if err != nil {
			return nil, fmt.Errorf("generate %q #%d: %w", doctype, i, err)
		}

		// Fill Link fields from cache.
		if depG != nil {
			f.fillLinkFields(mt, values, depG)
		}

		doc, err := dm.Insert(docCtx, doctype, values)
		if err != nil {
			return nil, fmt.Errorf("insert %q #%d: %w", doctype, i, err)
		}
		docs = append(docs, doc)

		// Cache inserted document name for future Link field references.
		f.linkCache[doctype] = append(f.linkCache[doctype], doc.Name())
	}

	return docs, nil
}

func (f *DocFactory) generateDoc(ctx context.Context, site string, mt *meta.MetaType, seq int, cfg *genConfig) (map[string]any, error) {
	values := make(map[string]any)

	for _, field := range mt.Fields {
		// Skip layout-only fields.
		if !field.FieldType.IsStorable() {
			continue
		}

		// Apply overrides first.
		if cfg.overrides != nil {
			if v, ok := cfg.overrides[field.Name]; ok {
				values[field.Name] = v
				continue
			}
		}

		// Skip Table/TableMultiSelect — handled separately.
		if field.FieldType == meta.FieldTypeTable || field.FieldType == meta.FieldTypeTableMultiSelect {
			if cfg.withChildren && field.Options != "" {
				childValues, err := f.generateChildRows(ctx, site, field, seq, cfg)
				if err != nil {
					return nil, fmt.Errorf("generate children for %q: %w", field.Name, err)
				}
				values[field.Name] = childValues
			}
			continue
		}

		fctx := fieldGenContext{
			field:     field,
			seq:       seq,
			forceUniq: field.Unique,
		}
		val := generateFieldValue(f.faker, fctx)
		if val != nil {
			values[field.Name] = val
		}
	}

	return values, nil
}

func (f *DocFactory) generateChildRows(ctx context.Context, site string, field meta.FieldDef, seq int, cfg *genConfig) ([]any, error) {
	childMT, err := f.registry.Get(ctx, site, field.Options)
	if err != nil {
		return nil, fmt.Errorf("get child MetaType %q: %w", field.Options, err)
	}

	count := f.faker.IntRange(cfg.childCountMin, cfg.childCountMax)
	rows := make([]any, 0, count)

	for i := 0; i < count; i++ {
		childCfg := &genConfig{
			withChildren:  false, // no nested children
			childCountMin: cfg.childCountMin,
			childCountMax: cfg.childCountMax,
		}
		values, err := f.generateDoc(ctx, site, childMT, seq*100+i, childCfg)
		if err != nil {
			return nil, err
		}
		rows = append(rows, values)
	}

	return rows, nil
}

// fillLinkFields populates Link field values from the linkCache.
func (f *DocFactory) fillLinkFields(mt *meta.MetaType, values map[string]any, depG *depGraph) {
	for _, field := range mt.Fields {
		if field.FieldType != meta.FieldTypeLink {
			continue
		}

		target := depG.targetDoctype(mt.Name, field.Name)
		if target == "" {
			target = field.Options
		}
		if target == "" {
			continue
		}

		cached := f.linkCache[target]
		if len(cached) == 0 {
			continue
		}

		// Pick a random cached name.
		idx := f.faker.IntRange(0, len(cached)-1)
		values[field.Name] = cached[idx]
	}
}

// ClearCache resets the Link field name cache. Call between independent
// generation runs to avoid cross-contamination.
func (f *DocFactory) ClearCache() {
	f.linkCache = make(map[string][]string)
}
