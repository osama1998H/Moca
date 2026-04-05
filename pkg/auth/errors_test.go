package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestErrUserNotFound(t *testing.T) {
	if ErrUserNotFound == nil {
		t.Fatal("ErrUserNotFound should not be nil")
	}
	if !strings.Contains(ErrUserNotFound.Error(), "user not found") {
		t.Errorf("error message = %q, want 'user not found'", ErrUserNotFound.Error())
	}
}

func TestErrSessionNotFound(t *testing.T) {
	if ErrSessionNotFound == nil {
		t.Fatal("ErrSessionNotFound should not be nil")
	}
	if !strings.Contains(ErrSessionNotFound.Error(), "session not found") {
		t.Errorf("error message = %q, want 'session not found'", ErrSessionNotFound.Error())
	}
}

func TestErrUserNotFound_IsComparison(t *testing.T) {
	wrapped := errors.New("wrapper: " + ErrUserNotFound.Error())
	// Sentinel errors should be comparable.
	if !errors.Is(ErrUserNotFound, ErrUserNotFound) {
		t.Error("ErrUserNotFound should be equal to itself")
	}
	// Wrapped errors should NOT match unless using %w.
	if errors.Is(wrapped, ErrUserNotFound) {
		t.Error("simple wrapping should not match with errors.Is")
	}
}

func TestPermDeniedError_Message(t *testing.T) {
	tests := []struct {
		user    string
		doctype string
		perm    string
		want    string
	}{
		{"user@test.com", "SalesOrder", "write", `user "user@test.com" lacks "write" permission on SalesOrder`},
		{"admin@test.com", "User", "delete", `user "admin@test.com" lacks "delete" permission on User`},
		{"", "DocType", "read", `user "" lacks "read" permission on DocType`},
	}
	for _, tt := range tests {
		e := &PermDeniedError{User: tt.user, Doctype: tt.doctype, Perm: tt.perm}
		if got := e.Error(); got != tt.want {
			t.Errorf("PermDeniedError.Error() = %q, want %q", got, tt.want)
		}
	}
}

func TestPermDeniedError_ImplementsError(t *testing.T) {
	var err error = &PermDeniedError{User: "test", Doctype: "Doc", Perm: "read"}
	if err == nil {
		t.Fatal("should implement error interface")
	}
}
