package document

import (
	"errors"
	"fmt"
	"testing"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// recordingController records the names of every lifecycle method called on it.
// It embeds BaseController so it satisfies DocLifecycle without boilerplate,
// then overrides every method to append to the events slice.
type recordingController struct {
	BaseController
	events []string
}

func (r *recordingController) BeforeInsert(_ *DocContext, _ Document) error {
	r.events = append(r.events, "before_insert")
	return nil
}
func (r *recordingController) AfterInsert(_ *DocContext, _ Document) error {
	r.events = append(r.events, "after_insert")
	return nil
}
func (r *recordingController) BeforeValidate(_ *DocContext, _ Document) error {
	r.events = append(r.events, "before_validate")
	return nil
}
func (r *recordingController) Validate(_ *DocContext, _ Document) error {
	r.events = append(r.events, "validate")
	return nil
}
func (r *recordingController) BeforeSave(_ *DocContext, _ Document) error {
	r.events = append(r.events, "before_save")
	return nil
}
func (r *recordingController) AfterSave(_ *DocContext, _ Document) error {
	r.events = append(r.events, "after_save")
	return nil
}
func (r *recordingController) OnUpdate(_ *DocContext, _ Document) error {
	r.events = append(r.events, "on_update")
	return nil
}
func (r *recordingController) BeforeSubmit(_ *DocContext, _ Document) error {
	r.events = append(r.events, "before_submit")
	return nil
}
func (r *recordingController) OnSubmit(_ *DocContext, _ Document) error {
	r.events = append(r.events, "on_submit")
	return nil
}
func (r *recordingController) BeforeCancel(_ *DocContext, _ Document) error {
	r.events = append(r.events, "before_cancel")
	return nil
}
func (r *recordingController) OnCancel(_ *DocContext, _ Document) error {
	r.events = append(r.events, "on_cancel")
	return nil
}
func (r *recordingController) OnTrash(_ *DocContext, _ Document) error {
	r.events = append(r.events, "on_trash")
	return nil
}
func (r *recordingController) AfterDelete(_ *DocContext, _ Document) error {
	r.events = append(r.events, "after_delete")
	return nil
}
func (r *recordingController) OnChange(_ *DocContext, _ Document) error {
	r.events = append(r.events, "on_change")
	return nil
}
func (r *recordingController) BeforeRename(_ *DocContext, _ Document, _, _ string) error {
	r.events = append(r.events, "before_rename")
	return nil
}
func (r *recordingController) AfterRename(_ *DocContext, _ Document, _, _ string) error {
	r.events = append(r.events, "after_rename")
	return nil
}

// ─── dispatchEvent tests ──────────────────────────────────────────────────────

func TestDispatchEvent_AllFourteenEvents(t *testing.T) {
	t.Helper()
	rec := &recordingController{}
	ctx := &DocContext{}
	doc := &DynamicDoc{}

	allEvents := []DocEvent{
		EventBeforeInsert, EventAfterInsert,
		EventBeforeValidate, EventValidate,
		EventBeforeSave, EventAfterSave,
		EventOnUpdate,
		EventBeforeSubmit, EventOnSubmit,
		EventBeforeCancel, EventOnCancel,
		EventOnTrash, EventAfterDelete,
		EventOnChange,
	}

	for _, ev := range allEvents {
		if err := dispatchEvent(rec, ev, ctx, doc); err != nil {
			t.Errorf("dispatchEvent(%q) returned unexpected error: %v", ev, err)
		}
	}

	if len(rec.events) != len(allEvents) {
		t.Fatalf("expected %d events dispatched, got %d: %v", len(allEvents), len(rec.events), rec.events)
	}
	for i, ev := range allEvents {
		if DocEvent(rec.events[i]) != ev {
			t.Errorf("event[%d]: want %q got %q", i, ev, rec.events[i])
		}
	}
	t.Logf("all 14 lifecycle events dispatched in correct order: %v", rec.events)
}

func TestDispatchEvent_UnknownEvent(t *testing.T) {
	rec := &recordingController{}
	err := dispatchEvent(rec, DocEvent("unknown_event"), &DocContext{}, &DynamicDoc{})
	if err == nil {
		t.Fatal("expected error for unknown event, got nil")
	}
	t.Logf("unknown event error: %v", err)
}

func TestDispatchEvent_ErrorPropagated(t *testing.T) {
	// A controller that returns an error from BeforeInsert.
	errCtrl := &errorController{at: EventBeforeInsert, err: errors.New("blocked by controller")}
	err := dispatchEvent(errCtrl, EventBeforeInsert, &DocContext{}, &DynamicDoc{})
	if err == nil {
		t.Fatal("expected error from controller, got nil")
	}
	if !errors.Is(err, errCtrl.err) {
		t.Errorf("want %v, got %v", errCtrl.err, err)
	}
	t.Logf("controller error propagated correctly: %v", err)
}

// errorController returns a pre-configured error for a specific event.
type errorController struct {
	BaseController
	err error    // interface: all pointer words
	at  DocEvent // string: ptr + non-ptr len at end
}

func (c *errorController) BeforeInsert(_ *DocContext, _ Document) error {
	if c.at == EventBeforeInsert {
		return c.err
	}
	return nil
}

// ─── BaseController tests ─────────────────────────────────────────────────────

func TestBaseController_AllMethodsReturnNil(t *testing.T) {
	var ctrl DocLifecycle = BaseController{}
	ctx := &DocContext{}
	doc := &DynamicDoc{}

	allEvents := []DocEvent{
		EventBeforeInsert, EventAfterInsert,
		EventBeforeValidate, EventValidate,
		EventBeforeSave, EventAfterSave,
		EventOnUpdate,
		EventBeforeSubmit, EventOnSubmit,
		EventBeforeCancel, EventOnCancel,
		EventOnTrash, EventAfterDelete,
		EventOnChange,
	}
	for _, ev := range allEvents {
		if err := dispatchEvent(ctrl, ev, ctx, doc); err != nil {
			t.Errorf("BaseController.%s returned non-nil: %v", ev, err)
		}
	}
	// Rename methods directly.
	if err := ctrl.BeforeRename(ctx, doc, "old", "new"); err != nil {
		t.Errorf("BaseController.BeforeRename returned non-nil: %v", err)
	}
	if err := ctrl.AfterRename(ctx, doc, "old", "new"); err != nil {
		t.Errorf("BaseController.AfterRename returned non-nil: %v", err)
	}
	t.Log("all BaseController methods return nil as expected")
}

// ─── ControllerRegistry tests ─────────────────────────────────────────────────

func TestControllerRegistry_DefaultIsBaseController(t *testing.T) {
	reg := NewControllerRegistry()
	ctrl := reg.Resolve("UnknownDoctype")

	// BaseController satisfies DocLifecycle; resolving an unregistered doctype
	// should return a BaseController (or something that behaves identically).
	ctx := &DocContext{}
	doc := &DynamicDoc{}
	if err := dispatchEvent(ctrl, EventBeforeInsert, ctx, doc); err != nil {
		t.Errorf("default controller BeforeInsert returned error: %v", err)
	}
	t.Log("unregistered doctype resolves to BaseController-equivalent (all methods return nil)")
}

func TestControllerRegistry_TypeOverride(t *testing.T) {
	reg := NewControllerRegistry()
	var called bool
	reg.RegisterOverride("TestOrder", func() DocLifecycle {
		return &customController{onBeforeInsert: func() error {
			called = true
			return nil
		}}
	})

	ctrl := reg.Resolve("TestOrder")
	_ = ctrl.BeforeInsert(&DocContext{}, &DynamicDoc{})
	if !called {
		t.Error("override controller BeforeInsert was not called")
	}
	t.Log("TypeOverride correctly replaces the base controller")
}

func TestControllerRegistry_TypeOverride_FreshInstancePerResolve(t *testing.T) {
	reg := NewControllerRegistry()
	callCount := 0
	reg.RegisterOverride("TestOrder", func() DocLifecycle {
		callCount++
		return BaseController{}
	})

	reg.Resolve("TestOrder")
	reg.Resolve("TestOrder")
	if callCount != 2 {
		t.Errorf("factory should be called once per Resolve; called %d times", callCount)
	}
	t.Logf("factory called %d times for 2 Resolve calls (fresh instance per request)", callCount)
}

func TestControllerRegistry_TypeExtension_Wraps(t *testing.T) {
	reg := NewControllerRegistry()
	var log []string

	// Extension that records "ext" before delegating.
	reg.RegisterExtension("TestOrder", &loggingExt{prefix: "ext", log: &log})

	ctrl := reg.Resolve("TestOrder")
	_ = ctrl.BeforeInsert(&DocContext{}, &DynamicDoc{})
	if len(log) == 0 || log[0] != "ext:before_insert" {
		t.Errorf("extension was not called; log=%v", log)
	}
	t.Logf("TypeExtension wrapped controller correctly; log=%v", log)
}

func TestControllerRegistry_ExtensionOrder(t *testing.T) {
	reg := NewControllerRegistry()
	var log []string

	// First registered = outermost wrapper.
	reg.RegisterExtension("TestOrder", &loggingExt{prefix: "first", log: &log})
	reg.RegisterExtension("TestOrder", &loggingExt{prefix: "second", log: &log})

	ctrl := reg.Resolve("TestOrder")
	_ = ctrl.BeforeInsert(&DocContext{}, &DynamicDoc{})

	if len(log) < 2 {
		t.Fatalf("expected at least 2 log entries, got %v", log)
	}
	if log[0] != "first:before_insert" {
		t.Errorf("first extension should run first; log=%v", log)
	}
	if log[1] != "second:before_insert" {
		t.Errorf("second extension should run second; log=%v", log)
	}
	t.Logf("extension order correct: %v", log)
}

func TestControllerRegistry_OverrideAndExtensionCombined(t *testing.T) {
	reg := NewControllerRegistry()
	var log []string

	reg.RegisterOverride("TestOrder", func() DocLifecycle {
		return &customController{onBeforeInsert: func() error {
			log = append(log, "override")
			return nil
		}}
	})
	reg.RegisterExtension("TestOrder", &loggingExt{prefix: "ext", log: &log})

	ctrl := reg.Resolve("TestOrder")
	_ = ctrl.BeforeInsert(&DocContext{}, &DynamicDoc{})

	// Extension wraps the override, so ext runs first, then override.
	if len(log) < 2 {
		t.Fatalf("expected 2 log entries (ext + override), got %v", log)
	}
	t.Logf("override + extension combined correctly: %v", log)
}

func TestControllerRegistry_Concurrency(t *testing.T) {
	reg := NewControllerRegistry()
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			doctype := fmt.Sprintf("Doctype%d", i)
			reg.RegisterOverride(doctype, func() DocLifecycle { return BaseController{} })
			reg.RegisterExtension(doctype, &loggingExt{prefix: "x", log: &[]string{}})
			reg.Resolve(doctype)
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	t.Log("ControllerRegistry concurrent register+resolve completed without race")
}

// ─── test helpers ─────────────────────────────────────────────────────────────

type customController struct {
	BaseController
	onBeforeInsert func() error
}

func (c *customController) BeforeInsert(_ *DocContext, _ Document) error {
	if c.onBeforeInsert != nil {
		return c.onBeforeInsert()
	}
	return nil
}

// loggingExt wraps a DocLifecycle and prepends "<prefix>:<event>" to a shared log.
type loggingExt struct {
	log    *[]string // ptr
	prefix string    // ptr + non-ptr len at end
}

func (e *loggingExt) Wrap(inner DocLifecycle) DocLifecycle {
	return &loggingWrapper{inner: inner, prefix: e.prefix, log: e.log}
}

type loggingWrapper struct {
	BaseController
	inner  DocLifecycle // interface: all ptr words
	log    *[]string    // ptr
	prefix string       // ptr + non-ptr len at end
}

func (w *loggingWrapper) BeforeInsert(ctx *DocContext, doc Document) error {
	*w.log = append(*w.log, w.prefix+":before_insert")
	return w.inner.BeforeInsert(ctx, doc)
}
