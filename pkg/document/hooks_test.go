package document

import (
	"errors"
	"testing"
)

// mockDispatcher implements HookDispatcher for testing.
type mockDispatcher struct {
	err         error
	lastDoctype string
	lastEvent   DocEvent
}

func (m *mockDispatcher) Dispatch(_ *DocContext, _ Document, doctype string, event DocEvent) error {
	m.lastDoctype = doctype
	m.lastEvent = event
	return m.err
}

func TestDispatchHooks_NilDispatcher(t *testing.T) {
	dm := &DocManager{} // hookDispatcher is nil
	err := dm.dispatchHooks(nil, nil, "TestDoc", EventBeforeSave)
	if err != nil {
		t.Fatalf("nil dispatcher should return nil, got %v", err)
	}
}

func TestDispatchHooks_Delegates(t *testing.T) {
	mock := &mockDispatcher{}
	dm := &DocManager{}
	dm.SetHookDispatcher(mock)

	err := dm.dispatchHooks(nil, nil, "User", EventAfterInsert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastDoctype != "User" {
		t.Errorf("doctype = %q, want %q", mock.lastDoctype, "User")
	}
	if mock.lastEvent != EventAfterInsert {
		t.Errorf("event = %q, want %q", mock.lastEvent, EventAfterInsert)
	}
}

func TestDispatchHooks_PropagatesError(t *testing.T) {
	sentinel := errors.New("hook error")
	mock := &mockDispatcher{err: sentinel}
	dm := &DocManager{}
	dm.SetHookDispatcher(mock)

	err := dm.dispatchHooks(nil, nil, "User", EventBeforeSave)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}
