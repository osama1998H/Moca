package document

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestBaseController_NoOpLifecycleMethods(t *testing.T) {
	bc := BaseController{}
	dc := NewDocContext(context.Background(), nil, nil)
	var doc Document // nil doc — testing no-op behavior

	methods := []struct {
		fn   func() error
		name string
	}{
		{name: "BeforeInsert", fn: func() error { return bc.BeforeInsert(dc, doc) }},
		{name: "AfterInsert", fn: func() error { return bc.AfterInsert(dc, doc) }},
		{name: "BeforeValidate", fn: func() error { return bc.BeforeValidate(dc, doc) }},
		{name: "Validate", fn: func() error { return bc.Validate(dc, doc) }},
		{name: "BeforeSave", fn: func() error { return bc.BeforeSave(dc, doc) }},
		{name: "AfterSave", fn: func() error { return bc.AfterSave(dc, doc) }},
		{name: "OnUpdate", fn: func() error { return bc.OnUpdate(dc, doc) }},
		{name: "BeforeSubmit", fn: func() error { return bc.BeforeSubmit(dc, doc) }},
		{name: "OnSubmit", fn: func() error { return bc.OnSubmit(dc, doc) }},
		{name: "BeforeCancel", fn: func() error { return bc.BeforeCancel(dc, doc) }},
		{name: "OnCancel", fn: func() error { return bc.OnCancel(dc, doc) }},
		{name: "OnTrash", fn: func() error { return bc.OnTrash(dc, doc) }},
		{name: "AfterDelete", fn: func() error { return bc.AfterDelete(dc, doc) }},
		{name: "OnChange", fn: func() error { return bc.OnChange(dc, doc) }},
		{name: "BeforeRename", fn: func() error { return bc.BeforeRename(dc, doc, "old", "new") }},
		{name: "AfterRename", fn: func() error { return bc.AfterRename(dc, doc, "old", "new") }},
	}

	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			if err := m.fn(); err != nil {
				t.Errorf("%s returned non-nil error: %v", m.name, err)
			}
		})
	}
}

func TestBaseController_SatisfiesDocLifecycle(t *testing.T) {
	var _ DocLifecycle = BaseController{}
}

func TestNewControllerRegistry(t *testing.T) {
	r := NewControllerRegistry()
	if r == nil {
		t.Fatal("expected non-nil ControllerRegistry")
	}
}

func TestControllerRegistry_Resolve_DefaultsToBaseController(t *testing.T) {
	r := NewControllerRegistry()
	lc := r.Resolve("SalesOrder")
	if lc == nil {
		t.Fatal("expected non-nil DocLifecycle")
	}
	// Should be a BaseController (or functional equivalent).
	dc := NewDocContext(context.Background(), nil, nil)
	if err := lc.BeforeInsert(dc, nil); err != nil {
		t.Errorf("default controller should return nil, got: %v", err)
	}
}

type testController struct {
	BaseController
	beforeInsertCalled bool
}

func (c *testController) BeforeInsert(_ *DocContext, _ Document) error {
	c.beforeInsertCalled = true
	return nil
}

func TestControllerRegistry_RegisterOverride(t *testing.T) {
	r := NewControllerRegistry()
	r.RegisterOverride("SalesOrder", func() DocLifecycle {
		return &testController{}
	})

	lc := r.Resolve("SalesOrder")
	dc := NewDocContext(context.Background(), nil, nil)
	_ = lc.BeforeInsert(dc, nil)

	// The resolved controller should be the override.
	tc, ok := lc.(*testController)
	if !ok {
		t.Fatal("expected *testController from override")
	}
	if !tc.beforeInsertCalled {
		t.Error("BeforeInsert should have been called")
	}
}

func TestControllerRegistry_OverrideReplacesExisting(t *testing.T) {
	r := NewControllerRegistry()
	calls := []string{}

	r.RegisterOverride("SalesOrder", func() DocLifecycle {
		return &stubLifecycle{onBeforeInsert: func() { calls = append(calls, "first") }}
	})
	r.RegisterOverride("SalesOrder", func() DocLifecycle {
		return &stubLifecycle{onBeforeInsert: func() { calls = append(calls, "second") }}
	})

	lc := r.Resolve("SalesOrder")
	dc := NewDocContext(context.Background(), nil, nil)
	_ = lc.BeforeInsert(dc, nil)

	if len(calls) != 1 || calls[0] != "second" {
		t.Errorf("expected only second override, got: %v", calls)
	}
}

type stubLifecycle struct {
	BaseController
	onBeforeInsert func()
}

func (s *stubLifecycle) BeforeInsert(_ *DocContext, _ Document) error {
	if s.onBeforeInsert != nil {
		s.onBeforeInsert()
	}
	return nil
}

type testExtension struct {
	calls *[]string
	name  string
}

func (e *testExtension) Wrap(inner DocLifecycle) DocLifecycle {
	return &wrappedLifecycle{inner: inner, extName: e.name, calls: e.calls}
}

type wrappedLifecycle struct {
	BaseController
	inner   DocLifecycle
	extName string
	calls   *[]string
}

func (w *wrappedLifecycle) BeforeInsert(dc *DocContext, doc Document) error {
	*w.calls = append(*w.calls, w.extName+":before")
	err := w.inner.BeforeInsert(dc, doc)
	*w.calls = append(*w.calls, w.extName+":after")
	return err
}

func TestControllerRegistry_RegisterExtension(t *testing.T) {
	r := NewControllerRegistry()
	calls := []string{}

	r.RegisterExtension("SalesOrder", &testExtension{name: "ext1", calls: &calls})
	r.RegisterExtension("SalesOrder", &testExtension{name: "ext2", calls: &calls})

	lc := r.Resolve("SalesOrder")
	dc := NewDocContext(context.Background(), nil, nil)
	_ = lc.BeforeInsert(dc, nil)

	// First registered = outermost, so execution order should be:
	// ext1:before → ext2:before → base → ext2:after → ext1:after
	expected := []string{"ext1:before", "ext2:before", "ext2:after", "ext1:after"}
	if len(calls) != len(expected) {
		t.Fatalf("calls = %v, want %v", calls, expected)
	}
	for i, c := range calls {
		if c != expected[i] {
			t.Errorf("calls[%d] = %q, want %q", i, c, expected[i])
		}
	}
}

func TestControllerRegistry_Resolve_FreshInstancePerCall(t *testing.T) {
	r := NewControllerRegistry()
	r.RegisterOverride("SalesOrder", func() DocLifecycle {
		return &testController{}
	})

	lc1 := r.Resolve("SalesOrder")
	lc2 := r.Resolve("SalesOrder")

	if lc1 == lc2 {
		t.Error("Resolve should return fresh instances, not the same pointer")
	}
}

func TestControllerRegistry_Resolve_UnknownDoctype(t *testing.T) {
	r := NewControllerRegistry()
	lc := r.Resolve("Nonexistent")
	if lc == nil {
		t.Fatal("should return BaseController for unknown doctypes")
	}
}

func TestControllerRegistry_ConcurrentAccess(t *testing.T) {
	r := NewControllerRegistry()

	var wg sync.WaitGroup
	// Concurrent registrations.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.RegisterOverride("SalesOrder", func() DocLifecycle {
				return BaseController{}
			})
		}(i)
	}

	// Concurrent resolutions.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lc := r.Resolve("SalesOrder")
			if lc == nil {
				t.Error("Resolve returned nil")
			}
		}()
	}
	wg.Wait()
}

func TestControllerRegistry_ExtensionDenyError(t *testing.T) {
	r := NewControllerRegistry()
	r.RegisterExtension("SalesOrder", &denyExtension{})

	lc := r.Resolve("SalesOrder")
	dc := NewDocContext(context.Background(), nil, nil)
	err := lc.BeforeInsert(dc, nil)
	if err == nil {
		t.Fatal("expected error from deny extension")
	}
	if !errors.Is(err, errDenied) {
		t.Errorf("expected errDenied, got: %v", err)
	}
}

var errDenied = errors.New("denied by extension")

type denyExtension struct{}

func (d *denyExtension) Wrap(inner DocLifecycle) DocLifecycle {
	return &denyLifecycle{}
}

type denyLifecycle struct{ BaseController }

func (d *denyLifecycle) BeforeInsert(_ *DocContext, _ Document) error {
	return errDenied
}
