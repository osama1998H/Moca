package hooks

import (
	"container/heap"
	"fmt"
	"sort"
	"strings"
)

// CircularDependencyError is returned when hook dependency declarations form a cycle.
type CircularDependencyError struct {
	Cycle []string
}

func (e *CircularDependencyError) Error() string {
	return fmt.Sprintf("hooks: circular dependency detected: %s", strings.Join(e.Cycle, " -> "))
}

// appNode groups all handlers from the same AppName into a single graph node.
type appNode struct {
	appName  string
	handlers []PrioritizedHandler
	inDegree int
}

// resolveOrder sorts handlers by topological dependency order with
// priority-aware tie-breaking using Kahn's algorithm with a min-heap.
//
// Handlers are grouped by AppName. DependsOn references app names, so
// all handlers from a dependency app run before any handler from the
// dependent app. Within a single app, handlers are stable-sorted by Priority.
//
// Empty AppName handlers are each treated as an independent node.
// DependsOn references to apps not in the handler set are silently ignored.
func resolveOrder(handlers []PrioritizedHandler) ([]PrioritizedHandler, error) {
	if len(handlers) == 0 {
		return nil, nil
	}
	if len(handlers) == 1 {
		return handlers, nil
	}

	// Step 1: Group by AppName.
	nodes := make(map[string]*appNode)
	var order []string // insertion order for determinism
	anonCount := 0

	for _, h := range handlers {
		key := h.AppName
		if key == "" {
			key = fmt.Sprintf("\x00anon_%d", anonCount)
			anonCount++
			nodes[key] = &appNode{appName: key, handlers: []PrioritizedHandler{h}}
			order = append(order, key)
			continue
		}
		if n, ok := nodes[key]; ok {
			n.handlers = append(n.handlers, h)
		} else {
			nodes[key] = &appNode{appName: key, handlers: []PrioritizedHandler{h}}
			order = append(order, key)
		}
	}

	// Fast path: single node, no topo sort needed.
	if len(nodes) == 1 {
		for _, n := range nodes {
			sortHandlersByPriority(n.handlers)
			return n.handlers, nil
		}
	}

	// Step 2: Build adjacency list with edge deduplication.
	// adj[dep] = set of app keys that depend on dep.
	adj := make(map[string]map[string]bool)

	for key, node := range nodes {
		seen := make(map[string]bool) // dedup deps within this node
		for _, h := range node.handlers {
			for _, dep := range h.DependsOn {
				if _, exists := nodes[dep]; !exists {
					continue // silently ignore missing deps
				}
				if dep == key {
					continue // self-dependency is a no-op
				}
				if seen[dep] {
					continue // already counted this edge
				}
				seen[dep] = true

				if adj[dep] == nil {
					adj[dep] = make(map[string]bool)
				}
				if !adj[dep][key] {
					adj[dep][key] = true
					node.inDegree++
				}
			}
		}
	}

	// Step 3: Initialize min-heap with zero-in-degree nodes.
	h := &nodeHeap{}
	heap.Init(h)
	for _, key := range order {
		if nodes[key].inDegree == 0 {
			heap.Push(h, nodes[key])
		}
	}

	// Step 4: Kahn's algorithm.
	result := make([]PrioritizedHandler, 0, len(handlers))
	emitted := 0

	for h.Len() > 0 {
		popped := heap.Pop(h)
		node, _ := popped.(*appNode)
		emitted++

		sortHandlersByPriority(node.handlers)
		result = append(result, node.handlers...)

		if dependents, ok := adj[node.appName]; ok {
			for depKey := range dependents {
				depNode := nodes[depKey]
				depNode.inDegree--
				if depNode.inDegree == 0 {
					heap.Push(h, depNode)
				}
			}
		}
	}

	// Step 5: Cycle detection.
	if emitted < len(nodes) {
		cycle := detectCycle(nodes, adj)
		return nil, &CircularDependencyError{Cycle: cycle}
	}

	return result, nil
}

// detectCycle finds one cycle among nodes with non-zero in-degree using DFS.
func detectCycle(nodes map[string]*appNode, adj map[string]map[string]bool) []string {
	// Build reverse adj: for each node, which nodes have edges pointing to it?
	// adj[dep] = dependents means dep -> dependent. We need dependent -> dep
	// to trace the cycle (follow what each node depends on).
	dependsOn := make(map[string][]string)
	for dep, dependents := range adj {
		for dependent := range dependents {
			dependsOn[dependent] = append(dependsOn[dependent], dep)
		}
	}

	const (
		white = 0 // unvisited
		gray  = 1 // in current path
		black = 2 // done
	)

	color := make(map[string]int)
	parent := make(map[string]string)

	var cyclePath []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = gray

		for _, dep := range dependsOn[node] {
			if _, ok := nodes[dep]; !ok {
				continue
			}
			if nodes[dep].inDegree <= 0 && color[dep] != gray {
				continue // already emitted, not part of cycle
			}

			if color[dep] == gray {
				// Found cycle: trace from dep back through parent to dep.
				cyclePath = []string{dep}
				cur := node
				for cur != dep {
					cyclePath = append(cyclePath, cur)
					cur = parent[cur]
				}
				cyclePath = append(cyclePath, dep)
				// Reverse to get natural order.
				for i, j := 0, len(cyclePath)-1; i < j; i, j = i+1, j-1 {
					cyclePath[i], cyclePath[j] = cyclePath[j], cyclePath[i]
				}
				return true
			}
			if color[dep] == white {
				parent[dep] = node
				if dfs(dep) {
					return true
				}
			}
		}

		color[node] = black
		return false
	}

	// Start DFS from nodes still in the graph (inDegree > 0).
	for key, node := range nodes {
		if node.inDegree > 0 && color[key] == white {
			if dfs(key) {
				return cyclePath
			}
		}
	}

	// Fallback: list remaining nodes.
	var remaining []string
	for key, node := range nodes {
		if node.inDegree > 0 {
			remaining = append(remaining, key)
		}
	}
	return remaining
}

// sortHandlersByPriority stable-sorts handlers by Priority (ascending).
// Preserves registration order for equal priorities.
func sortHandlersByPriority(handlers []PrioritizedHandler) {
	sort.SliceStable(handlers, func(i, j int) bool {
		return handlers[i].Priority < handlers[j].Priority
	})
}

// minPriority returns the lowest Priority value across all handlers.
func minPriority(handlers []PrioritizedHandler) int {
	if len(handlers) == 0 {
		return 0
	}
	m := handlers[0].Priority
	for _, h := range handlers[1:] {
		if h.Priority < m {
			m = h.Priority
		}
	}
	return m
}

// nodeHeap is a min-heap of *appNode ordered by minimum priority across
// the node's handlers, with alphabetical appName as tie-breaker.
type nodeHeap []*appNode

func (h nodeHeap) Len() int { return len(h) }

func (h nodeHeap) Less(i, j int) bool {
	pi, pj := minPriority(h[i].handlers), minPriority(h[j].handlers)
	if pi != pj {
		return pi < pj
	}
	return h[i].appName < h[j].appName
}

func (h nodeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *nodeHeap) Push(x any) {
	n, _ := x.(*appNode)
	*h = append(*h, n)
}

func (h *nodeHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return item
}
