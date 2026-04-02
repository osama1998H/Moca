package hooks

import (
	"errors"
	"sync"
	"testing"

	"github.com/osama1998H/moca/pkg/document"
)

// noop is a DocEventHandler that does nothing.
var noop DocEventHandler = func(_ *document.DocContext, _ document.Document) error { return nil }

func TestHookRegistry_PriorityOrder(t *testing.T) {
	r := NewHookRegistry()
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler:  noop,
		Priority: 200,
		AppName:  "billing",
	})
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler:  noop,
		Priority: 100,
		AppName:  "crm",
	})

	got, err := r.Resolve("User", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(got))
	}
	if got[0].AppName != "crm" || got[1].AppName != "billing" {
		t.Errorf("expected crm(100) before billing(200), got %s(%d) then %s(%d)",
			got[0].AppName, got[0].Priority, got[1].AppName, got[1].Priority)
	}
}

func TestHookRegistry_DefaultPriority(t *testing.T) {
	r := NewHookRegistry()
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler: noop,
		AppName: "default_app",
		// Priority == 0, should become DefaultPriority (500)
	})
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler:  noop,
		Priority: 100,
		AppName:  "explicit_app",
	})

	got, err := r.Resolve("User", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0].AppName != "explicit_app" {
		t.Errorf("expected explicit_app (p=100) first, got %s (p=%d)", got[0].AppName, got[0].Priority)
	}
	if got[1].Priority != DefaultPriority {
		t.Errorf("expected default priority %d, got %d", DefaultPriority, got[1].Priority)
	}
}

func TestHookRegistry_GlobalMergedWithLocal(t *testing.T) {
	r := NewHookRegistry()
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler:  noop,
		Priority: 200,
		AppName:  "local_app",
	})
	r.RegisterGlobal(document.EventBeforeSave, PrioritizedHandler{
		Handler:  noop,
		Priority: 100,
		AppName:  "global_app",
	})

	got, err := r.Resolve("User", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0].AppName != "global_app" || got[1].AppName != "local_app" {
		t.Errorf("expected global_app(100) before local_app(200), got %s then %s",
			got[0].AppName, got[1].AppName)
	}
}

func TestHookRegistry_GlobalOnly(t *testing.T) {
	r := NewHookRegistry()
	r.RegisterGlobal(document.EventAfterSave, PrioritizedHandler{
		Handler:  noop,
		Priority: 100,
		AppName:  "audit",
	})

	got, err := r.Resolve("AnyDoctype", document.EventAfterSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].AppName != "audit" {
		t.Errorf("expected single audit handler, got %+v", got)
	}
}

func TestHookRegistry_EmptyReturnsNil(t *testing.T) {
	r := NewHookRegistry()

	got, err := r.Resolve("User", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestHookRegistry_DependencyOrdering(t *testing.T) {
	r := NewHookRegistry()
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler:   noop,
		Priority:  100,
		AppName:   "billing",
		DependsOn: []string{"crm"},
	})
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler:  noop,
		Priority: 200,
		AppName:  "crm",
	})

	got, err := r.Resolve("User", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	// crm must come first despite higher priority number.
	if got[0].AppName != "crm" || got[1].AppName != "billing" {
		t.Errorf("expected crm before billing (dependency), got %s then %s",
			got[0].AppName, got[1].AppName)
	}
}

func TestHookRegistry_CircularDependency(t *testing.T) {
	r := NewHookRegistry()
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler:   noop,
		Priority:  100,
		AppName:   "a",
		DependsOn: []string{"b"},
	})
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler:   noop,
		Priority:  100,
		AppName:   "b",
		DependsOn: []string{"a"},
	})

	_, err := r.Resolve("User", document.EventBeforeSave)
	if err == nil {
		t.Fatal("expected circular dependency error")
	}

	var cycleErr *CircularDependencyError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("expected *CircularDependencyError, got %T", err)
	}
}

func TestHookRegistry_MissingDependencyIgnored(t *testing.T) {
	r := NewHookRegistry()
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler:   noop,
		Priority:  100,
		AppName:   "a",
		DependsOn: []string{"nonexistent"},
	})

	got, err := r.Resolve("User", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("missing dep should be ignored: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
}

func TestHookRegistry_Concurrency(t *testing.T) {
	r := NewHookRegistry()
	const goroutines = 10
	const ops = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // half register, half resolve

	for range goroutines {
		go func() {
			defer wg.Done()
			for j := range ops {
				r.Register("User", document.EventBeforeSave, PrioritizedHandler{
					Handler:  noop,
					Priority: j,
					AppName:  "app",
				})
			}
		}()

		go func() {
			defer wg.Done()
			for range ops {
				_, _ = r.Resolve("User", document.EventBeforeSave)
			}
		}()
	}

	wg.Wait()
	// No panic or race detector failure = pass.
}

func TestHookRegistry_MultipleHandlersSameApp(t *testing.T) {
	r := NewHookRegistry()
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler: noop, Priority: 300, AppName: "crm",
	})
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler: noop, Priority: 100, AppName: "crm",
	})
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler: noop, Priority: 200, AppName: "crm",
	})

	got, err := r.Resolve("User", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	if got[0].Priority != 100 || got[1].Priority != 200 || got[2].Priority != 300 {
		t.Errorf("expected within-app sort 100,200,300 got %d,%d,%d",
			got[0].Priority, got[1].Priority, got[2].Priority)
	}
}

func TestHookRegistry_AnonymousHandlers(t *testing.T) {
	r := NewHookRegistry()
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler: noop, Priority: 300,
	})
	r.Register("User", document.EventBeforeSave, PrioritizedHandler{
		Handler: noop, Priority: 100,
	})

	got, err := r.Resolve("User", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0].Priority != 100 || got[1].Priority != 300 {
		t.Errorf("expected 100 before 300, got %d then %d", got[0].Priority, got[1].Priority)
	}
}
