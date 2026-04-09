package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// encryptionPrefix marks encrypted values. The version allows future key
	// rotation without breaking existing ciphertext.
	encryptionPrefix = "enc:v1:"

	// gcmNonceSize is the standard 12-byte nonce for AES-GCM.
	gcmNonceSize = 12
)

// FieldEncryptor provides AES-256-GCM encryption and decryption for document
// field values. It is safe for concurrent use.
type FieldEncryptor struct {
	gcm cipher.AEAD
}

// NewFieldEncryptor creates a FieldEncryptor from a 64-character hex-encoded
// AES-256 key. The key can come from MOCA_ENCRYPTION_KEY env var or config.
func NewFieldEncryptor(hexKey string) (*FieldEncryptor, error) {
	if len(hexKey) != 64 {
		return nil, fmt.Errorf("encryption key must be 64 hex characters (32 bytes), got %d characters", len(hexKey))
	}

	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid hex in encryption key: %w", err)
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	return &FieldEncryptor{gcm: gcm}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM and returns a prefixed,
// base64-encoded string: "enc:v1:" + base64(nonce || ciphertext).
// If the value is already encrypted (has the enc:v1: prefix), it is returned
// unchanged to prevent double-encryption.
func (e *FieldEncryptor) Encrypt(plaintext string) (string, error) {
	if IsEncrypted(plaintext) {
		return plaintext, nil
	}

	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)

	return encryptionPrefix + encoded, nil
}

// Decrypt decrypts a value previously encrypted with Encrypt. It expects the
// "enc:v1:" prefix. Non-prefixed values are returned as-is (not encrypted).
func (e *FieldEncryptor) Decrypt(ciphertext string) (string, error) {
	if !IsEncrypted(ciphertext) {
		return ciphertext, nil
	}

	encoded := strings.TrimPrefix(ciphertext, encryptionPrefix)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	if len(data) < gcmNonceSize {
		return "", fmt.Errorf("ciphertext too short: expected at least %d bytes, got %d", gcmNonceSize, len(data))
	}

	nonce := data[:gcmNonceSize]
	encrypted := data[gcmNonceSize:]

	plaintext, err := e.gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted reports whether a string value carries the encryption prefix.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, encryptionPrefix)
}
