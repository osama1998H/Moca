package factory

import (
	"context"
	"fmt"

	"github.com/osama1998H/moca/pkg/meta"
)

// depGraph holds the dependency information for Link field resolution.
type depGraph struct {
	// edges maps doctype -> list of doctypes it depends on (via Link fields).
	edges map[string][]string
	// linkFields maps "doctype.fieldname" -> target doctype.
	linkFields map[string]string
}

// buildDepGraph walks the fields of the given doctype and all its Link field
// targets recursively, building a directed dependency graph.
func buildDepGraph(ctx context.Context, registry *meta.Registry, site, rootDoctype string) (*depGraph, error) {
	g := &depGraph{
		edges:      make(map[string][]string),
		linkFields: make(map[string]string),
	}

	visited := make(map[string]bool)
	if err := g.walk(ctx, registry, site, rootDoctype, visited); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *depGraph) walk(ctx context.Context, registry *meta.Registry, site, doctype string, visited map[string]bool) error {
	if visited[doctype] {
		return nil
	}
	visited[doctype] = true

	mt, err := registry.Get(ctx, site, doctype)
	if err != nil {
		return fmt.Errorf("resolve MetaType %q: %w", doctype, err)
	}

	for _, field := range mt.Fields {
		if field.FieldType != meta.FieldTypeLink {
			continue
		}
		if field.Options == "" {
			continue
		}

		target := field.Options
		key := doctype + "." + field.Name
		g.linkFields[key] = target

		// Avoid self-referential loops.
		if target == doctype {
			continue
		}

		g.edges[doctype] = append(g.edges[doctype], target)

		// Recursively resolve the target's dependencies.
		if err := g.walk(ctx, registry, site, target, visited); err != nil {
			// If a dependency can't be resolved, skip it rather than failing.
			// This handles cases where the referenced doctype isn't registered.
			continue
		}
	}

	return nil
}

// topoSort returns doctypes in dependency order (leaf doctypes first).
// Cycles are detected and broken by skipping back-edges.
func (g *depGraph) topoSort(root string) []string {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	var order []string

	var visit func(string)
	visit = func(dt string) {
		if visited[dt] {
			return
		}
		if inStack[dt] {
			// Cycle detected — skip this edge.
			return
		}
		inStack[dt] = true

		for _, dep := range g.edges[dt] {
			visit(dep)
		}

		inStack[dt] = false
		visited[dt] = true
		order = append(order, dt)
	}

	visit(root)
	return order
}

// targetDoctype returns the Link target for the given doctype + field name,
// or empty string if not a Link field.
func (g *depGraph) targetDoctype(doctype, fieldName string) string {
	return g.linkFields[doctype+"."+fieldName]
}
