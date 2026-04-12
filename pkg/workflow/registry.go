package workflow

import (
	"context"
	"sync"

	"github.com/osama1998H/moca/pkg/meta"
)

// WorkflowRegistry is an in-memory cache of active workflow definitions per
// site:doctype pair. It is safe for concurrent use.
type WorkflowRegistry struct {
	cache map[string]*meta.WorkflowMeta
	mu    sync.RWMutex
}

// NewWorkflowRegistry creates an empty WorkflowRegistry ready for use.
func NewWorkflowRegistry() *WorkflowRegistry {
	return &WorkflowRegistry{
		cache: make(map[string]*meta.WorkflowMeta),
	}
}

// cacheKey returns the canonical key used for cache lookups.
func cacheKey(site, doctype string) string {
	return site + ":" + doctype
}

// Set stores wf in the cache under the site:doctype key, replacing any prior
// entry.
func (r *WorkflowRegistry) Set(site, doctype string, wf *meta.WorkflowMeta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[cacheKey(site, doctype)] = wf
}

// Get returns the cached workflow for the given site and doctype. It returns
// ErrNoActiveWorkflow when no entry is found. ctx is reserved for future
// database-backed fallback lookups.
func (r *WorkflowRegistry) Get(_ context.Context, site, doctype string) (*meta.WorkflowMeta, error) {
	r.mu.RLock()
	wf, ok := r.cache[cacheKey(site, doctype)]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrNoActiveWorkflow
	}
	return wf, nil
}

// Invalidate removes the cached entry for the given site:doctype pair. A
// subsequent Get will return ErrNoActiveWorkflow until Set is called again.
func (r *WorkflowRegistry) Invalidate(site, doctype string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, cacheKey(site, doctype))
}

// FindTransition scans wf.Transitions for an edge where From == fromState and
// Action == action. branchName is reserved for future branch-aware lookup but
// is not matched against any field today. Returns a pointer to the first
// matching slice element, or nil if no match is found.
func FindTransition(wf *meta.WorkflowMeta, fromState, action, _ string) *meta.Transition {
	if wf == nil {
		return nil
	}
	for i := range wf.Transitions {
		t := &wf.Transitions[i]
		if t.From == fromState && t.Action == action {
			return t
		}
	}
	return nil
}

// FindState scans wf.States for a state whose Name equals name and returns a
// pointer to it. Returns nil when no match is found.
func FindState(wf *meta.WorkflowMeta, name string) *meta.WorkflowState {
	if wf == nil {
		return nil
	}
	for i := range wf.States {
		if wf.States[i].Name == name {
			return &wf.States[i]
		}
	}
	return nil
}

// FindBranches returns all states in wf where JoinTarget equals joinState.
// Used to discover the branches feeding into an AND-join gate.
func FindBranches(wf *meta.WorkflowMeta, joinState string) []meta.WorkflowState {
	if wf == nil {
		return nil
	}
	var branches []meta.WorkflowState
	for _, s := range wf.States {
		if s.JoinTarget == joinState {
			branches = append(branches, s)
		}
	}
	return branches
}
