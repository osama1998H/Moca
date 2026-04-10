package notify

import (
	"context"
	"fmt"
	"testing"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/queue"
)

func TestRegisterHooks(t *testing.T) {
	registry := hooks.NewHookRegistry()
	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, nil, nil)
	nd.RegisterHooks(registry)

	// Verify that handlers are registered for each notification event.
	for eventName, docEvent := range notificationEvents {
		handlers, err := registry.Resolve("SomeDocType", docEvent)
		if err != nil {
			t.Fatalf("Resolve(%q) error = %v", eventName, err)
		}
		if len(handlers) == 0 {
			t.Errorf("no handlers registered for event %q (%s)", eventName, docEvent)
		}

		found := false
		for _, h := range handlers {
			if h.AppName == notifyAppName {
				found = true
				if h.Priority != notifyHookPriority {
					t.Errorf("event %q: priority = %d, want %d", eventName, h.Priority, notifyHookPriority)
				}
			}
		}
		if !found {
			t.Errorf("event %q: no handler with appName %q", eventName, notifyAppName)
		}
	}
}

func TestNotificationEvents_Coverage(t *testing.T) {
	// Verify all expected events are mapped.
	expected := map[string]document.DocEvent{
		"on_create": document.EventAfterInsert,
		"on_update": document.EventAfterSave,
		"on_submit": document.EventOnSubmit,
		"on_cancel": document.EventOnCancel,
	}
	for name, event := range expected {
		mapped, ok := notificationEvents[name]
		if !ok {
			t.Errorf("missing event mapping for %q", name)
			continue
		}
		if mapped != event {
			t.Errorf("event %q: mapped to %q, want %q", name, mapped, event)
		}
	}
	if len(notificationEvents) != len(expected) {
		t.Errorf("notificationEvents has %d entries, want %d", len(notificationEvents), len(expected))
	}
}

func TestMakeHookHandler_NilContext(t *testing.T) {
	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, nil, nil)
	handler := nd.makeHookHandler("on_create")

	// nil context should return nil (no-op).
	err := handler(nil, nil)
	if err != nil {
		t.Errorf("expected nil error for nil context, got %v", err)
	}
}

func TestMakeHookHandler_NilSite(t *testing.T) {
	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, nil, nil)
	handler := nd.makeHookHandler("on_create")

	ctx := &document.DocContext{}
	err := handler(ctx, nil)
	if err != nil {
		t.Errorf("expected nil error for nil site, got %v", err)
	}
}

func TestBuildTemplateData(t *testing.T) {
	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, nil, nil)

	// Test with a nil-site DocContext and nil doc — should not panic.
	ctx := &document.DocContext{}
	data := nd.buildTemplateData(ctx, &stubDoc{name: "TEST-001"}, "on_create")
	if data["Event"] != "on_create" {
		t.Errorf("Event = %v, want on_create", data["Event"])
	}
	if data["Name"] != "TEST-001" {
		t.Errorf("Name = %v, want TEST-001", data["Name"])
	}
}

func TestGenerateJobID(t *testing.T) {
	id1, err := generateJobID()
	if err != nil {
		t.Fatalf("generateJobID() error = %v", err)
	}
	if len(id1) != 32 {
		t.Errorf("id length = %d, want 32", len(id1))
	}

	id2, err := generateJobID()
	if err != nil {
		t.Fatalf("generateJobID() error = %v", err)
	}
	if id1 == id2 {
		t.Error("two generated IDs should not be equal")
	}
}

func TestEmailDeliveryHandler_NilSender(t *testing.T) {
	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, nil, nil)

	// With nil emailSender, should ACK the job (return nil).
	err := nd.EmailDeliveryHandler(t.Context(), dummyJob("test-1"))
	if err != nil {
		t.Errorf("expected nil error for nil sender, got %v", err)
	}
}

func TestEmailDeliveryHandler_NoRecipients(t *testing.T) {
	sender := &mockEmailSender{}
	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, sender, nil)

	job := dummyJob("test-2")
	job.Payload = map[string]any{
		"to":      []any{}, // empty recipients
		"subject": "Test",
	}

	err := nd.EmailDeliveryHandler(t.Context(), job)
	if err != nil {
		t.Errorf("expected nil error for no recipients, got %v", err)
	}
	if sender.called {
		t.Error("sender.Send should not be called with no recipients")
	}
}

func TestEmailDeliveryHandler_Success(t *testing.T) {
	sender := &mockEmailSender{}
	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, sender, nil)

	job := dummyJob("test-3")
	job.Payload = map[string]any{
		"to":        []any{"user@example.com"},
		"subject":   "Test Subject",
		"html_body": "<p>Hello</p>",
		"text_body": "Hello",
	}

	err := nd.EmailDeliveryHandler(t.Context(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sender.called {
		t.Fatal("sender.Send should have been called")
	}
	if sender.lastMsg.Subject != "Test Subject" {
		t.Errorf("subject = %q, want %q", sender.lastMsg.Subject, "Test Subject")
	}
	if len(sender.lastMsg.To) != 1 || sender.lastMsg.To[0] != "user@example.com" {
		t.Errorf("to = %v, want [user@example.com]", sender.lastMsg.To)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

type mockEmailSender struct {
	err     error
	lastMsg EmailMessage
	called  bool
}

func (m *mockEmailSender) Send(_ context.Context, msg EmailMessage) error {
	m.called = true
	m.lastMsg = msg
	return m.err
}

func dummyJob(id string) queue.Job {
	return queue.Job{
		ID:      id,
		Site:    "test-site",
		Type:    JobTypeEmailDelivery,
		Payload: map[string]any{},
	}
}

// stubDoc implements document.Document for testing.
type stubDoc struct {
	name string
}

func (s *stubDoc) Meta() *meta.MetaType           { return nil }
func (s *stubDoc) Name() string                    { return s.name }
func (s *stubDoc) Get(_ string) any                { return nil }
func (s *stubDoc) Set(_ string, _ any) error       { return nil }
func (s *stubDoc) GetChild(_ string) []document.Document { return nil }
func (s *stubDoc) AddChild(_ string) (document.Document, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *stubDoc) IsNew() bool            { return true }
func (s *stubDoc) IsModified() bool       { return false }
func (s *stubDoc) ModifiedFields() []string { return nil }
func (s *stubDoc) AsMap() map[string]any  { return map[string]any{"name": s.name} }
func (s *stubDoc) ToJSON() ([]byte, error) { return nil, nil }
