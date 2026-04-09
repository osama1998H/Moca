package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"

	"golang.org/x/crypto/hkdf"
)

// Backup encryption file format:
//
//	MOCA_ENC (8-byte magic)
//	salt     (32 bytes, random)
//	IV       (16 bytes, random)
//	data     (variable, AES-256-CTR encrypted)
//	HMAC     (32 bytes, HMAC-SHA256 of magic+salt+IV+data)
//
// Key derivation: HKDF(SHA-256, masterKey, salt) → 32-byte encKey + 32-byte hmacKey.

var fileMagic = [8]byte{'M', 'O', 'C', 'A', '_', 'E', 'N', 'C'}

const (
	saltSize    = 32
	aesIVSize   = 16
	hmacSize    = 32
	hkdfKeySize = 64 // 32 for AES + 32 for HMAC
)

// ParseHexKey decodes a 64-character hex string into a 32-byte key.
func ParseHexKey(s string) ([]byte, error) {
	if len(s) != 64 {
		return nil, fmt.Errorf("encryption key must be 64 hex characters (32 bytes), got %d", len(s))
	}
	key, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid hex in encryption key: %w", err)
	}
	return key, nil
}

// deriveKeys uses HKDF to derive separate encryption and HMAC keys from the
// master key and a random salt.
func deriveKeys(masterKey, salt []byte) (encKey, hmacKey []byte, err error) {
	hkdfReader := hkdf.New(sha256.New, masterKey, salt, []byte("moca-backup-enc"))
	derived := make([]byte, hkdfKeySize)
	if _, err := io.ReadFull(hkdfReader, derived); err != nil {
		return nil, nil, fmt.Errorf("HKDF key derivation: %w", err)
	}
	return derived[:32], derived[32:], nil
}

// encryptWriter wraps an io.Writer with AES-256-CTR encryption and HMAC.
type encryptWriter struct {
	out    io.Writer
	stream cipher.Stream
	mac    hash.Hash
	closed bool
}

// EncryptStream writes the encrypted file header (magic + salt + IV) to w,
// then returns a WriteCloser. All data written to it is encrypted with
// AES-256-CTR. Close MUST be called to write the trailing HMAC.
func EncryptStream(w io.Writer, key []byte) (io.WriteCloser, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	iv := make([]byte, aesIVSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("generate IV: %w", err)
	}

	encKey, hmacKey, err := deriveKeys(key, salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	stream := cipher.NewCTR(block, iv)

	mac := hmac.New(sha256.New, hmacKey)

	// Write header: magic + salt + IV. Include in HMAC.
	header := make([]byte, 0, len(fileMagic)+saltSize+aesIVSize)
	header = append(header, fileMagic[:]...)
	header = append(header, salt...)
	header = append(header, iv...)

	if _, err := w.Write(header); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	mac.Write(header)

	return &encryptWriter{
		out:    w,
		stream: stream,
		mac:    mac,
	}, nil
}

func (ew *encryptWriter) Write(p []byte) (int, error) {
	if ew.closed {
		return 0, fmt.Errorf("write to closed encrypt writer")
	}

	encrypted := make([]byte, len(p))
	ew.stream.XORKeyStream(encrypted, p)

	n, err := ew.out.Write(encrypted)
	if n > 0 {
		ew.mac.Write(encrypted[:n])
	}
	return n, err
}

// Close writes the HMAC trailer and marks the writer as closed.
func (ew *encryptWriter) Close() error {
	if ew.closed {
		return nil
	}
	ew.closed = true

	tag := ew.mac.Sum(nil)
	if _, err := ew.out.Write(tag); err != nil {
		return fmt.Errorf("write HMAC trailer: %w", err)
	}
	return nil
}

// DecryptStream reads and validates the encrypted file header, reads all
// remaining data, verifies the HMAC trailer, decrypts, and returns a Reader
// over the plaintext. The entire ciphertext is buffered in memory, which is
// acceptable for backup files.
func DecryptStream(r io.Reader, key []byte) (io.Reader, error) {
	// Read and verify magic.
	var magic [8]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	if magic != fileMagic {
		return nil, fmt.Errorf("invalid backup encryption header (expected MOCA_ENC magic)")
	}

	// Read salt and IV.
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(r, salt); err != nil {
		return nil, fmt.Errorf("read salt: %w", err)
	}
	iv := make([]byte, aesIVSize)
	if _, err := io.ReadFull(r, iv); err != nil {
		return nil, fmt.Errorf("read IV: %w", err)
	}

	encKey, hmacKey, err := deriveKeys(key, salt)
	if err != nil {
		return nil, err
	}

	// Read all remaining data (ciphertext + HMAC trailer).
	rest, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read encrypted data: %w", err)
	}
	if len(rest) < hmacSize {
		return nil, fmt.Errorf("encrypted data truncated: expected at least %d bytes for HMAC, got %d", hmacSize, len(rest))
	}

	cipherData := rest[:len(rest)-hmacSize]
	expectedMAC := rest[len(rest)-hmacSize:]

	// Compute HMAC over header + ciphertext.
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write(magic[:])
	mac.Write(salt)
	mac.Write(iv)
	mac.Write(cipherData)
	computedMAC := mac.Sum(nil)

	if !hmac.Equal(computedMAC, expectedMAC) {
		return nil, fmt.Errorf("HMAC verification failed: backup data corrupted or wrong key")
	}

	// Decrypt in-place.
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(cipherData, cipherData)

	return bytes.NewReader(cipherData), nil
}
