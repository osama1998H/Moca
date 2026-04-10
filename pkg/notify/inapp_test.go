package notify

import (
	"testing"
)

func TestGenerateNotifID(t *testing.T) {
	id1, err := generateNotifID()
	if err != nil {
		t.Fatalf("generateNotifID() error = %v", err)
	}
	if len(id1) != 32 { // 16 bytes hex-encoded
		t.Errorf("id length = %d, want 32", len(id1))
	}

	id2, err := generateNotifID()
	if err != nil {
		t.Fatalf("generateNotifID() error = %v", err)
	}
	if id1 == id2 {
		t.Error("two generated IDs should not be equal")
	}
}

func TestNewInAppNotifier(t *testing.T) {
	n := NewInAppNotifier(nil)
	if n == nil {
		t.Fatal("NewInAppNotifier() returned nil")
	}
	if n.logger == nil {
		t.Fatal("logger should fall back to slog.Default()")
	}
}

func TestInAppNotifier_Create_NilPool(t *testing.T) {
	n := NewInAppNotifier(nil)
	_, err := n.Create(t.Context(), nil, "public", Notification{
		ForUser: "test@example.com",
		Subject: "Test",
	})
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}

func TestInAppNotifier_MarkRead_NilPool(t *testing.T) {
	n := NewInAppNotifier(nil)
	err := n.MarkRead(t.Context(), nil, "public", "test@example.com", "id1")
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}

func TestInAppNotifier_MarkRead_Empty(t *testing.T) {
	n := NewInAppNotifier(nil)
	err := n.MarkRead(t.Context(), nil, "public", "test@example.com")
	if err != nil {
		t.Fatalf("MarkRead with no names should be no-op, got error = %v", err)
	}
}

func TestInAppNotifier_GetUnread_NilPool(t *testing.T) {
	n := NewInAppNotifier(nil)
	_, _, err := n.GetUnread(t.Context(), nil, "public", "test@example.com", 20)
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}
