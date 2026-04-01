package hooks

import (
	"errors"
	"strings"
	"testing"

	"github.com/moca-framework/moca/pkg/document"
)

// recordingHandler returns a DocEventHandler that appends label to order.
func recordingHandler(order *[]string, label string) DocEventHandler {
	return func(_ *document.DocContext, _ document.Document) error {
		*order = append(*order, label)
		return nil
	}
}

// failingHandler returns a DocEventHandler that returns the given error.
func failingHandler(err error) DocEventHandler {
	return func(_ *document.DocContext, _ document.Document) error {
		return err
	}
}

func TestDocEventDispatcher_NoHandlers(t *testing.T) {
	reg := NewHookRegistry()
	d := NewDocEventDispatcher(reg)

	err := d.Dispatch(nil, nil, "TestDoc", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestDocEventDispatcher_SingleHandler(t *testing.T) {
	reg := NewHookRegistry()
	d := NewDocEventDispatcher(reg)

	called := false
	reg.Register("TestDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler: func(_ *document.DocContext, _ document.Document) error {
			called = true
			return nil
		},
		AppName: "myapp",
	})

	err := d.Dispatch(nil, nil, "TestDoc", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestDocEventDispatcher_PriorityOrder(t *testing.T) {
	reg := NewHookRegistry()
	d := NewDocEventDispatcher(reg)

	var order []string
	reg.Register("TestDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:  recordingHandler(&order, "high"),
		AppName:  "app_high",
		Priority: 300,
	})
	reg.Register("TestDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:  recordingHandler(&order, "low"),
		AppName:  "app_low",
		Priority: 100,
	})
	reg.Register("TestDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:  recordingHandler(&order, "mid"),
		AppName:  "app_mid",
		Priority: 200,
	})

	err := d.Dispatch(nil, nil, "TestDoc", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "low,mid,high"
	got := strings.Join(order, ",")
	if got != expected {
		t.Errorf("order = %q, want %q", got, expected)
	}
}

func TestDocEventDispatcher_FirstErrorStops(t *testing.T) {
	reg := NewHookRegistry()
	d := NewDocEventDispatcher(reg)

	var order []string
	sentinel := errors.New("hook failed")

	reg.Register("TestDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:  recordingHandler(&order, "first"),
		AppName:  "app_a",
		Priority: 100,
	})
	reg.Register("TestDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler: func(_ *document.DocContext, _ document.Document) error {
			order = append(order, "second")
			return sentinel
		},
		AppName:  "app_b",
		Priority: 200,
	})
	reg.Register("TestDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:  recordingHandler(&order, "third"),
		AppName:  "app_c",
		Priority: 300,
	})

	err := d.Dispatch(nil, nil, "TestDoc", document.EventBeforeSave)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error should wrap sentinel, got: %v", err)
	}

	got := strings.Join(order, ",")
	if got != "first,second" {
		t.Errorf("order = %q, want %q (third should not run)", got, "first,second")
	}
}

func TestDocEventDispatcher_ErrorWrapping(t *testing.T) {
	reg := NewHookRegistry()
	d := NewDocEventDispatcher(reg)

	sentinel := errors.New("boom")
	reg.Register("User", document.EventAfterSave, PrioritizedHandler{
		Handler: failingHandler(sentinel),
		AppName: "billing",
	})

	err := d.Dispatch(nil, nil, "User", document.EventAfterSave)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"User", "after_save", "billing", "boom"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should contain %q", msg, want)
		}
	}
	if !errors.Is(err, sentinel) {
		t.Error("error should wrap the original sentinel error")
	}
}

func TestDocEventDispatcher_CircularDependency(t *testing.T) {
	reg := NewHookRegistry()
	d := NewDocEventDispatcher(reg)

	reg.Register("TestDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:   func(_ *document.DocContext, _ document.Document) error { return nil },
		AppName:   "app_a",
		DependsOn: []string{"app_b"},
	})
	reg.Register("TestDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:   func(_ *document.DocContext, _ document.Document) error { return nil },
		AppName:   "app_b",
		DependsOn: []string{"app_a"},
	})

	err := d.Dispatch(nil, nil, "TestDoc", document.EventBeforeSave)
	if err == nil {
		t.Fatal("expected circular dependency error")
	}

	var cycleErr *CircularDependencyError
	if !errors.As(err, &cycleErr) {
		t.Errorf("expected *CircularDependencyError, got %T: %v", err, err)
	}
}

func TestDocEventDispatcher_GlobalAndLocal(t *testing.T) {
	reg := NewHookRegistry()
	d := NewDocEventDispatcher(reg)

	var order []string
	reg.Register("TestDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:  recordingHandler(&order, "local"),
		AppName:  "local_app",
		Priority: 200,
	})
	reg.RegisterGlobal(document.EventBeforeSave, PrioritizedHandler{
		Handler:  recordingHandler(&order, "global"),
		AppName:  "global_app",
		Priority: 100,
	})

	err := d.Dispatch(nil, nil, "TestDoc", document.EventBeforeSave)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "global,local"
	got := strings.Join(order, ",")
	if got != expected {
		t.Errorf("order = %q, want %q", got, expected)
	}
}
