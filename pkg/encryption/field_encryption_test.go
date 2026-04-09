package encryption

import (
	"strings"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

const testHexKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newTestEncryptor(t *testing.T) *auth.FieldEncryptor {
	t.Helper()
	enc, err := auth.NewFieldEncryptor(testHexKey)
	if err != nil {
		t.Fatalf("NewFieldEncryptor: %v", err)
	}
	return enc
}

func testMetaWithPassword() *meta.MetaType {
	return &meta.MetaType{
		Name: "TestDoc",
		Fields: []meta.FieldDef{
			{Name: "username", FieldType: meta.FieldTypeData},
			{Name: "password", FieldType: meta.FieldTypePassword},
			{Name: "api_key", FieldType: meta.FieldTypePassword},
			{Name: "notes", FieldType: meta.FieldTypeText},
		},
	}
}

func testMetaNoPassword() *meta.MetaType {
	return &meta.MetaType{
		Name: "PlainDoc",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
			{Name: "body", FieldType: meta.FieldTypeText},
		},
	}
}

func newTestDoc(mt *meta.MetaType, values map[string]any) *document.DynamicDoc {
	doc := document.NewDynamicDoc(mt, nil, true)
	for k, v := range values {
		_ = doc.Set(k, v)
	}
	return doc
}

func TestEncryptBeforeSave_EncryptsPasswordFields(t *testing.T) {
	enc := newTestEncryptor(t)
	hook := NewFieldEncryptionHook(enc)

	mt := testMetaWithPassword()
	doc := newTestDoc(mt, map[string]any{
		"username": "admin",
		"password": "secret123",
		"api_key":  "key-abc-xyz",
		"notes":    "some notes",
	})

	if err := hook.EncryptBeforeSave(nil, doc); err != nil {
		t.Fatalf("EncryptBeforeSave: %v", err)
	}

	// Password fields should be encrypted.
	pw, _ := doc.Get("password").(string)
	if !auth.IsEncrypted(pw) {
		t.Errorf("password field not encrypted: %q", pw)
	}

	ak, _ := doc.Get("api_key").(string)
	if !auth.IsEncrypted(ak) {
		t.Errorf("api_key field not encrypted: %q", ak)
	}

	// Non-password fields should be unchanged.
	if doc.Get("username") != "admin" {
		t.Errorf("username was modified: %v", doc.Get("username"))
	}
	if doc.Get("notes") != "some notes" {
		t.Errorf("notes was modified: %v", doc.Get("notes"))
	}
}

func TestEncryptBeforeSave_SkipsAlreadyEncrypted(t *testing.T) {
	enc := newTestEncryptor(t)
	hook := NewFieldEncryptionHook(enc)

	encrypted, err := enc.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	mt := testMetaWithPassword()
	doc := newTestDoc(mt, map[string]any{
		"password": encrypted,
	})

	if err := hook.EncryptBeforeSave(nil, doc); err != nil {
		t.Fatalf("EncryptBeforeSave: %v", err)
	}

	// Value should be unchanged (not double-encrypted).
	if doc.Get("password") != encrypted {
		t.Error("already-encrypted value was modified")
	}
}

func TestEncryptBeforeSave_SkipsNilAndEmpty(t *testing.T) {
	enc := newTestEncryptor(t)
	hook := NewFieldEncryptionHook(enc)

	mt := testMetaWithPassword()
	doc := newTestDoc(mt, map[string]any{
		"password": "",
		// api_key is nil (not set)
	})

	if err := hook.EncryptBeforeSave(nil, doc); err != nil {
		t.Fatalf("EncryptBeforeSave: %v", err)
	}

	if pw := doc.Get("password"); pw != "" {
		t.Errorf("empty password was modified: %v", pw)
	}
}

func TestEncryptBeforeSave_NoPasswordFields(t *testing.T) {
	enc := newTestEncryptor(t)
	hook := NewFieldEncryptionHook(enc)

	mt := testMetaNoPassword()
	doc := newTestDoc(mt, map[string]any{
		"title": "Hello",
		"body":  "World",
	})

	if err := hook.EncryptBeforeSave(nil, doc); err != nil {
		t.Fatalf("EncryptBeforeSave: %v", err)
	}

	if doc.Get("title") != "Hello" {
		t.Error("title was modified")
	}
}

func TestTransformAfterLoad_DecryptsPasswordFields(t *testing.T) {
	enc := newTestEncryptor(t)
	hook := NewFieldEncryptionHook(enc)

	encPW, _ := enc.Encrypt("secret123")
	encKey, _ := enc.Encrypt("key-abc-xyz")

	mt := testMetaWithPassword()
	doc := newTestDoc(mt, map[string]any{
		"username": "admin",
		"password": encPW,
		"api_key":  encKey,
		"notes":    "some notes",
	})

	if err := hook.TransformAfterLoad(nil, doc); err != nil {
		t.Fatalf("TransformAfterLoad: %v", err)
	}

	if doc.Get("password") != "secret123" {
		t.Errorf("password not decrypted: %v", doc.Get("password"))
	}
	if doc.Get("api_key") != "key-abc-xyz" {
		t.Errorf("api_key not decrypted: %v", doc.Get("api_key"))
	}
	if doc.Get("username") != "admin" {
		t.Errorf("username was modified: %v", doc.Get("username"))
	}
	if doc.Get("notes") != "some notes" {
		t.Errorf("notes was modified: %v", doc.Get("notes"))
	}
}

func TestTransformAfterLoad_SkipsNonEncryptedPasswords(t *testing.T) {
	enc := newTestEncryptor(t)
	hook := NewFieldEncryptionHook(enc)

	mt := testMetaWithPassword()
	doc := newTestDoc(mt, map[string]any{
		"password": "plaintext-password",
	})

	if err := hook.TransformAfterLoad(nil, doc); err != nil {
		t.Fatalf("TransformAfterLoad: %v", err)
	}

	// Non-encrypted password should be left as-is.
	if doc.Get("password") != "plaintext-password" {
		t.Errorf("non-encrypted password was modified: %v", doc.Get("password"))
	}
}

func TestFieldEncryption_Roundtrip(t *testing.T) {
	enc := newTestEncryptor(t)
	hook := NewFieldEncryptionHook(enc)

	mt := testMetaWithPassword()
	doc := newTestDoc(mt, map[string]any{
		"username": "admin",
		"password": "my-secret",
		"api_key":  "api-token-123",
		"notes":    "keep me",
	})

	// Encrypt on save.
	if err := hook.EncryptBeforeSave(nil, doc); err != nil {
		t.Fatalf("EncryptBeforeSave: %v", err)
	}

	// Verify encrypted at rest.
	pwVal, _ := doc.Get("password").(string)
	if !auth.IsEncrypted(pwVal) {
		t.Fatal("password should be encrypted")
	}
	akVal, _ := doc.Get("api_key").(string)
	if !auth.IsEncrypted(akVal) {
		t.Fatal("api_key should be encrypted")
	}

	// Decrypt on load.
	if err := hook.TransformAfterLoad(nil, doc); err != nil {
		t.Fatalf("TransformAfterLoad: %v", err)
	}

	if doc.Get("password") != "my-secret" {
		t.Errorf("password roundtrip failed: %v", doc.Get("password"))
	}
	if doc.Get("api_key") != "api-token-123" {
		t.Errorf("api_key roundtrip failed: %v", doc.Get("api_key"))
	}
	if doc.Get("username") != "admin" {
		t.Error("username changed during roundtrip")
	}
	if doc.Get("notes") != "keep me" {
		t.Error("notes changed during roundtrip")
	}
}

func TestTransformAfterLoad_WrongKey(t *testing.T) {
	enc1 := newTestEncryptor(t)
	hook1 := NewFieldEncryptionHook(enc1)

	otherKey := strings.Repeat("ff", 32)
	enc2, err := auth.NewFieldEncryptor(otherKey)
	if err != nil {
		t.Fatalf("NewFieldEncryptor: %v", err)
	}
	hook2 := NewFieldEncryptionHook(enc2)

	mt := testMetaWithPassword()
	doc := newTestDoc(mt, map[string]any{
		"password": "secret",
	})

	// Encrypt with key 1.
	if encErr := hook1.EncryptBeforeSave(nil, doc); encErr != nil {
		t.Fatalf("EncryptBeforeSave: %v", encErr)
	}

	// Decrypt with key 2 should fail.
	err = hook2.TransformAfterLoad(nil, doc)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key")
	}
}
