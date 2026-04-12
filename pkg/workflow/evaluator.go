package workflow

import (
	"fmt"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/osama1998H/moca/pkg/document"
)

// ConditionEvaluator evaluates workflow transition condition expressions against
// a document's current state. Expressions are compiled once via expr-lang/expr
// and cached in a sync.RWMutex-protected map for reuse across transitions.
//
// The evaluation environment exposes:
//
//   - doc      map[string]any   — document fields (via doc.AsMap())
//   - user     string           — authenticated user email
//   - roles    []string         — user's assigned roles
//   - now      time.Time        — current wall-clock time
//   - has_role func(string)bool — returns true if the user holds the given role
type ConditionEvaluator struct {
	cache map[string]*vm.Program
	mu    sync.RWMutex
}

// NewConditionEvaluator constructs a ConditionEvaluator with an empty program cache.
func NewConditionEvaluator() *ConditionEvaluator {
	return &ConditionEvaluator{
		cache: make(map[string]*vm.Program),
	}
}

// Eval evaluates condition against the given document and request context.
// An empty condition string is treated as always-passing (returns true, nil).
// Returns (false, ErrInvalidCondition) if the condition fails to compile.
// ctx may be nil; in that case user and roles will be empty.
func (e *ConditionEvaluator) Eval(condition string, doc document.Document, ctx *document.DocContext) (bool, error) {
	if condition == "" {
		return true, nil
	}

	prog, err := e.getOrCompile(condition)
	if err != nil {
		return false, err
	}

	env := e.buildEnv(doc, ctx)

	out, err := expr.Run(prog, env)
	if err != nil {
		return false, fmt.Errorf("workflow: condition evaluation failed: %w", err)
	}

	result, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("workflow: condition did not return a bool (got %T)", out)
	}
	return result, nil
}

// getOrCompile returns a cached compiled program for the condition, compiling
// it on the first call. Compilation is guarded by the write lock; subsequent
// reads use the read lock.
func (e *ConditionEvaluator) getOrCompile(condition string) (*vm.Program, error) {
	// Fast path: program already cached.
	e.mu.RLock()
	prog, ok := e.cache[condition]
	e.mu.RUnlock()
	if ok {
		return prog, nil
	}

	// Slow path: compile and cache.
	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock to avoid duplicate compilation.
	if prog, ok = e.cache[condition]; ok {
		return prog, nil
	}

	compiled, err := expr.Compile(condition, expr.AsBool())
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidCondition, err.Error())
	}

	e.cache[condition] = compiled
	return compiled, nil
}

// buildEnv constructs the expression environment map for a single Eval call.
// ctx may be nil; user/roles will be empty strings/slices in that case.
func (e *ConditionEvaluator) buildEnv(doc document.Document, ctx *document.DocContext) map[string]any {
	var userEmail string
	var roles []string

	if ctx != nil && ctx.User != nil {
		userEmail = ctx.User.Email
		roles = ctx.User.Roles
	}
	if roles == nil {
		roles = []string{}
	}

	// Capture roles in the closure so it doesn't escape via the env map.
	rolesCopy := roles
	hasRole := func(role string) bool {
		for _, r := range rolesCopy {
			if r == role {
				return true
			}
		}
		return false
	}

	return map[string]any{
		"doc":      doc.AsMap(),
		"user":     userEmail,
		"roles":    rolesCopy,
		"now":      time.Now(),
		"has_role": hasRole,
	}
}
