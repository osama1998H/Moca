package auth

import (
	"context"
	"fmt"
	"sync"
)

// CustomRuleFunc is a function that evaluates a custom permission rule.
// It receives the context, user, and doctype, and returns nil to allow
// or an error to deny.
type CustomRuleFunc func(ctx context.Context, user *User, doctype string) error

// CustomRuleRegistry holds named custom rule functions.
// It is safe for concurrent use; rules are registered at init time
// and evaluated at request time.
type CustomRuleRegistry struct {
	rules map[string]CustomRuleFunc
	mu    sync.RWMutex
}

// NewCustomRuleRegistry creates an empty custom rule registry.
func NewCustomRuleRegistry() *CustomRuleRegistry {
	return &CustomRuleRegistry{
		rules: make(map[string]CustomRuleFunc),
	}
}

// Register adds a named custom rule function to the registry.
// Returns an error if a rule with the same name is already registered.
func (r *CustomRuleRegistry) Register(name string, fn CustomRuleFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.rules[name]; exists {
		return fmt.Errorf("custom rule %q already registered", name)
	}
	r.rules[name] = fn
	return nil
}

// Evaluate runs the named custom rule. Returns an error if the rule is
// not registered or if the rule function denies access.
func (r *CustomRuleRegistry) Evaluate(ctx context.Context, name string, user *User, doctype string) error {
	r.mu.RLock()
	fn, ok := r.rules[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("custom rule %q not registered", name)
	}
	return fn(ctx, user, doctype)
}

// EvaluateAll runs all named custom rules in order. Returns the first
// error encountered (fail-fast). Returns nil if all rules pass.
func (r *CustomRuleRegistry) EvaluateAll(ctx context.Context, names []string, user *User, doctype string) error {
	for _, name := range names {
		if err := r.Evaluate(ctx, name, user, doctype); err != nil {
			return fmt.Errorf("custom rule %q denied: %w", name, err)
		}
	}
	return nil
}
