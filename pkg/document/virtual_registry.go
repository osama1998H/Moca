package document

import (
	"sort"
	"sync"
)

// VirtualSourceRegistry maps doctype names to their VirtualSource implementations.
// Thread-safe for concurrent registration and lookup.
type VirtualSourceRegistry struct {
	mu      sync.RWMutex
	sources map[string]VirtualSource
}

func NewVirtualSourceRegistry() *VirtualSourceRegistry {
	return &VirtualSourceRegistry{
		sources: make(map[string]VirtualSource),
	}
}

func (r *VirtualSourceRegistry) Register(doctype string, src VirtualSource) {
	r.mu.Lock()
	r.sources[doctype] = src
	r.mu.Unlock()
}

func (r *VirtualSourceRegistry) Get(doctype string) (VirtualSource, bool) {
	r.mu.RLock()
	src, ok := r.sources[doctype]
	r.mu.RUnlock()
	return src, ok
}

func (r *VirtualSourceRegistry) List() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.sources))
	for name := range r.sources {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}
