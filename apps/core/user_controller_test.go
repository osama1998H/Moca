package core

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/meta"
)

// userMetaType returns a minimal MetaType for User, sufficient for DynamicDoc.
func userMetaType() *meta.MetaType {
	return &meta.MetaType{
		Name:   "User",
		Module: "Core",
		NamingRule: meta.NamingStrategy{Rule: meta.NamingByField, FieldName: "email"},
		Fields: []meta.FieldDef{
			{Name: "email", FieldType: meta.FieldTypeData, Required: true, Unique: true},
			{Name: "full_name", FieldType: meta.FieldTypeData},
			{Name: "password", FieldType: meta.FieldTypePassword},
			{Name: "enabled", FieldType: meta.FieldTypeCheck},
		},
	}
}

func TestUserController_BeforeSave_HashesPassword(t *testing.T) {
	mt := userMetaType()
	doc := document.NewDynamicDoc(mt, nil, true)
	if err := doc.Set("email", "test@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("password", "secret123"); err != nil {
		t.Fatal(err)
	}

	ctrl := &UserController{}
	if err := ctrl.BeforeSave(nil, doc); err != nil {
		t.Fatalf("BeforeSave error: %v", err)
	}

	hashed, ok := doc.Get("password").(string)
	if !ok {
		t.Fatal("password is not a string")
	}
	if !strings.HasPrefix(hashed, "$2a$") && !strings.HasPrefix(hashed, "$2b$") {
		t.Errorf("expected bcrypt hash, got: %s", hashed)
	}

	// Verify the hash is valid.
	if err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte("secret123")); err != nil {
		t.Errorf("bcrypt verification failed: %v", err)
	}
}

func TestUserController_BeforeSave_SkipsAlreadyHashed(t *testing.T) {
	mt := userMetaType()
	doc := document.NewDynamicDoc(mt, nil, true)
	if err := doc.Set("email", "test@example.com"); err != nil {
		t.Fatal(err)
	}

	// Set an already-hashed password.
	existing := "$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012"
	if err := doc.Set("password", existing); err != nil {
		t.Fatal(err)
	}

	ctrl := &UserController{}
	if err := ctrl.BeforeSave(nil, doc); err != nil {
		t.Fatalf("BeforeSave error: %v", err)
	}

	got := doc.Get("password").(string)
	if got != existing {
		t.Errorf("expected password unchanged, got: %s", got)
	}
}

func TestUserController_BeforeSave_NoOpWhenUnmodified(t *testing.T) {
	mt := userMetaType()
	// Create a non-new doc where password is in the original snapshot.
	doc := document.NewDynamicDoc(mt, nil, false)

	ctrl := &UserController{}
	if err := ctrl.BeforeSave(nil, doc); err != nil {
		t.Fatalf("BeforeSave error: %v", err)
	}

	// Password should still be nil (never set).
	if doc.Get("password") != nil {
		t.Error("expected password to remain nil")
	}
}

func TestNewUserControllerFactory(t *testing.T) {
	factory := NewUserController
	ctrl := factory()
	if ctrl == nil {
		t.Fatal("factory returned nil")
	}
	if _, ok := ctrl.(*UserController); !ok {
		t.Errorf("expected *UserController, got %T", ctrl)
	}
}
