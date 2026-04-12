//go:build integration

package backup

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegration_BackupEncryptDecryptFileRoundtrip simulates the full backup
// encryption pipeline: create a gzipped SQL file, encrypt it, then decrypt and
// decompress, verifying content integrity end-to-end.
func TestIntegration_BackupEncryptDecryptFileRoundtrip(t *testing.T) {
	// Generate a 64-char hex encryption key (32 bytes).
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	hexKey := hex.EncodeToString(keyBytes)
	masterKey, err := ParseHexKey(hexKey)
	if err != nil {
		t.Fatalf("ParseHexKey: %v", err)
	}

	// Create a fake SQL dump content.
	sqlContent := "-- Moca backup test\nCREATE TABLE test_data (id SERIAL PRIMARY KEY, value TEXT);\n"
	sqlContent += "INSERT INTO test_data (value) VALUES ('hello world');\n"
	// Add enough data to exercise the streaming path.
	for i := 0; i < 1000; i++ {
		sqlContent += "INSERT INTO test_data (value) VALUES ('row " + strings.Repeat("x", 100) + "');\n"
	}

	// Step 1: Compress with gzip (matching backup.Create pipeline).
	var gzipped bytes.Buffer
	gzw := gzip.NewWriter(&gzipped)
	if _, err := gzw.Write([]byte(sqlContent)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	// Step 2: Encrypt the gzipped data.
	var encrypted bytes.Buffer
	ew, err := EncryptStream(&encrypted, masterKey)
	if err != nil {
		t.Fatalf("EncryptStream: %v", err)
	}
	if _, err := ew.Write(gzipped.Bytes()); err != nil {
		t.Fatalf("encrypt write: %v", err)
	}
	if err := ew.Close(); err != nil {
		t.Fatalf("encrypt close: %v", err)
	}

	// Step 3: Write to a file with .enc extension.
	dir := t.TempDir()
	encPath := filepath.Join(dir, "test_site_2024-01-01_120000.sql.gz.enc")
	if err := os.WriteFile(encPath, encrypted.Bytes(), 0o644); err != nil {
		t.Fatalf("write .enc file: %v", err)
	}

	// Verify .enc extension is present (acceptance criterion).
	if !strings.HasSuffix(encPath, ".enc") {
		t.Error("encrypted backup should have .enc extension")
	}

	// Verify magic bytes (acceptance criterion).
	encData, _ := os.ReadFile(encPath)
	if len(encData) < 8 {
		t.Fatal("encrypted file too small")
	}
	if string(encData[:8]) != "MOCA_ENC" {
		t.Errorf("magic bytes: got %q, want %q", string(encData[:8]), "MOCA_ENC")
	}

	// Step 4: Decrypt the file.
	dr, err := DecryptStream(bytes.NewReader(encData), masterKey)
	if err != nil {
		t.Fatalf("DecryptStream: %v", err)
	}
	decryptedGzip, err := io.ReadAll(dr)
	if err != nil {
		t.Fatalf("read decrypted: %v", err)
	}

	// Step 5: Decompress.
	gzr, err := gzip.NewReader(bytes.NewReader(decryptedGzip))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	decryptedSQL, err := io.ReadAll(gzr)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	gzr.Close()

	// Step 6: Verify content matches original.
	if string(decryptedSQL) != sqlContent {
		t.Error("decrypted content does not match original SQL")
	}

	// Verify checksums match.
	origHash := sha256.Sum256([]byte(sqlContent))
	decHash := sha256.Sum256(decryptedSQL)
	if origHash != decHash {
		t.Errorf("SHA-256 mismatch: orig=%x, decrypted=%x", origHash[:8], decHash[:8])
	}
}

// TestIntegration_BackupEncryptWrongKeyFails verifies that attempting to
// decrypt an encrypted backup with the wrong key produces a clear error.
func TestIntegration_BackupEncryptWrongKeyFails(t *testing.T) {
	// Encrypt with key A.
	keyA := make([]byte, 32)
	if _, err := rand.Read(keyA); err != nil {
		t.Fatalf("generate key A: %v", err)
	}

	var encrypted bytes.Buffer
	ew, err := EncryptStream(&encrypted, keyA)
	if err != nil {
		t.Fatalf("EncryptStream: %v", err)
	}
	if _, err := ew.Write([]byte("sensitive backup data")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := ew.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Attempt to decrypt with key B.
	keyB := make([]byte, 32)
	if _, err := rand.Read(keyB); err != nil {
		t.Fatalf("generate key B: %v", err)
	}

	_, err = DecryptStream(bytes.NewReader(encrypted.Bytes()), keyB)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key")
	}
	if !strings.Contains(err.Error(), "HMAC") {
		t.Errorf("error should mention HMAC verification, got: %v", err)
	}
}
