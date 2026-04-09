package auth

import (
	"encoding/hex"
	"strings"
	"testing"
)

// testKey is a fixed 32-byte key for deterministic tests.
const testHexKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newTestEncryptor(t *testing.T) *FieldEncryptor {
	t.Helper()
	enc, err := NewFieldEncryptor(testHexKey)
	if err != nil {
		t.Fatalf("NewFieldEncryptor: %v", err)
	}
	return enc
}

func TestNewFieldEncryptor_ValidKey(t *testing.T) {
	enc, err := NewFieldEncryptor(testHexKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc == nil {
		t.Fatal("expected non-nil encryptor")
	}
}

func TestNewFieldEncryptor_InvalidKeyLength(t *testing.T) {
	_, err := NewFieldEncryptor("tooshort")
	if err == nil {
		t.Fatal("expected error for short key")
	}
	if !strings.Contains(err.Error(), "64 hex characters") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewFieldEncryptor_InvalidHex(t *testing.T) {
	// 64 chars but not valid hex
	badKey := strings.Repeat("zz", 32)
	_, err := NewFieldEncryptor(badKey)
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
	if !strings.Contains(err.Error(), "invalid hex") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	enc := newTestEncryptor(t)

	cases := []string{
		"hello world",
		"",
		"short",
		"unicode: 日本語テスト 🔑",
		strings.Repeat("a", 10000), // large string
		"special chars: !@#$%^&*()_+-=[]{}|;':\",./<>?",
	}

	for _, plaintext := range cases {
		encrypted, err := enc.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}

		if !IsEncrypted(encrypted) {
			t.Errorf("Encrypt(%q): result missing %q prefix", plaintext, encryptionPrefix)
		}

		decrypted, err := enc.Decrypt(encrypted)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}

		if decrypted != plaintext {
			t.Errorf("roundtrip failed: got %q, want %q", decrypted, plaintext)
		}
	}
}

func TestEncrypt_RandomNonce(t *testing.T) {
	enc := newTestEncryptor(t)
	plaintext := "same input"

	ct1, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	ct2, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if ct1 == ct2 {
		t.Error("two encryptions of the same plaintext produced identical ciphertext (nonce reuse)")
	}
}

func TestEncrypt_IdempotentOnEncryptedValue(t *testing.T) {
	enc := newTestEncryptor(t)

	encrypted, err := enc.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Encrypting an already-encrypted value should return it unchanged.
	doubleEncrypted, err := enc.Encrypt(encrypted)
	if err != nil {
		t.Fatalf("double Encrypt: %v", err)
	}

	if doubleEncrypted != encrypted {
		t.Error("double encryption was not prevented")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	enc1 := newTestEncryptor(t)

	otherKey := strings.Repeat("ff", 32)
	enc2, err := NewFieldEncryptor(otherKey)
	if err != nil {
		t.Fatalf("NewFieldEncryptor: %v", err)
	}

	encrypted, err := enc1.Encrypt("secret data")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, err = enc2.Decrypt(encrypted)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key")
	}
	if !strings.Contains(err.Error(), "decrypt") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDecrypt_NonEncryptedValue(t *testing.T) {
	enc := newTestEncryptor(t)

	// Non-prefixed strings should be returned as-is.
	plain := "not encrypted"
	result, err := enc.Decrypt(plain)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if result != plain {
		t.Errorf("got %q, want %q", result, plain)
	}
}

func TestDecrypt_CorruptedCiphertext(t *testing.T) {
	enc := newTestEncryptor(t)

	// Valid prefix but corrupted base64 payload.
	corrupted := encryptionPrefix + "not-valid-base64!!!"
	_, err := enc.Decrypt(corrupted)
	if err == nil {
		t.Fatal("expected error for corrupted ciphertext")
	}
}

func TestDecrypt_TruncatedCiphertext(t *testing.T) {
	enc := newTestEncryptor(t)

	// Valid prefix and base64, but too short (less than nonce size).
	short := hex.EncodeToString([]byte("tiny"))
	// Use raw base64 of something shorter than 12 bytes
	corrupted := encryptionPrefix + "dGlueQ==" // "tiny" in base64 = 4 bytes
	_ = short

	_, err := enc.Decrypt(corrupted)
	if err == nil {
		t.Fatal("expected error for truncated ciphertext")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIsEncrypted(t *testing.T) {
	if IsEncrypted("plaintext") {
		t.Error("plaintext should not be detected as encrypted")
	}
	if !IsEncrypted(encryptionPrefix + "anything") {
		t.Error("prefixed value should be detected as encrypted")
	}
	if IsEncrypted("") {
		t.Error("empty string should not be detected as encrypted")
	}
}
