// Package encryption provides transparent field-level encryption for Moca
// documents. It bridges pkg/auth (crypto primitives) and pkg/document
// (document lifecycle) without creating an import cycle.
package encryption

import (
	"fmt"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

// FieldEncryptionHook provides transparent encryption of Password-type fields.
// It implements both a BeforeSave hook (for encrypting on write) and the
// document.PostLoadTransformer interface (for decrypting on read).
type FieldEncryptionHook struct {
	encryptor *auth.FieldEncryptor
}

// NewFieldEncryptionHook creates a new field encryption hook.
func NewFieldEncryptionHook(enc *auth.FieldEncryptor) *FieldEncryptionHook {
	return &FieldEncryptionHook{encryptor: enc}
}

// EncryptBeforeSave is a DocEventHandler that encrypts all Password-type field
// values before the document is persisted. Already-encrypted values (with the
// enc:v1: prefix) are skipped to prevent double-encryption.
func (h *FieldEncryptionHook) EncryptBeforeSave(_ *document.DocContext, doc document.Document) error {
	mt := doc.Meta()
	for _, f := range mt.Fields {
		if f.FieldType != meta.FieldTypePassword {
			continue
		}
		raw := doc.Get(f.Name)
		if raw == nil {
			continue
		}
		val, ok := raw.(string)
		if !ok || val == "" {
			continue
		}
		if auth.IsEncrypted(val) {
			continue
		}

		encrypted, err := h.encryptor.Encrypt(val)
		if err != nil {
			return fmt.Errorf("encrypt field %q: %w", f.Name, err)
		}
		if err := doc.Set(f.Name, encrypted); err != nil {
			return fmt.Errorf("set encrypted field %q: %w", f.Name, err)
		}
	}
	return nil
}

// TransformAfterLoad implements document.PostLoadTransformer. It decrypts all
// Password-type fields that carry the enc:v1: prefix. The caller (DocManager)
// is responsible for calling resetDirtyState() after this returns.
func (h *FieldEncryptionHook) TransformAfterLoad(_ *document.DocContext, doc *document.DynamicDoc) error {
	mt := doc.Meta()
	for _, f := range mt.Fields {
		if f.FieldType != meta.FieldTypePassword {
			continue
		}
		raw := doc.Get(f.Name)
		if raw == nil {
			continue
		}
		val, ok := raw.(string)
		if !ok || val == "" {
			continue
		}
		if !auth.IsEncrypted(val) {
			continue
		}

		decrypted, err := h.encryptor.Decrypt(val)
		if err != nil {
			return fmt.Errorf("decrypt field %q: %w", f.Name, err)
		}
		if err := doc.Set(f.Name, decrypted); err != nil {
			return fmt.Errorf("set decrypted field %q: %w", f.Name, err)
		}
	}
	return nil
}
