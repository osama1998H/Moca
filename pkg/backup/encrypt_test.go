package backup

import (
	"bytes"
	"crypto/rand"
	"io"
	"strings"
	"testing"
)

var testMasterKey = bytes.Repeat([]byte{0xAB}, 32) // 32 bytes

func mustEncrypt(t *testing.T, plaintext []byte) []byte {
	t.Helper()
	var encrypted bytes.Buffer
	ew, err := EncryptStream(&encrypted, testMasterKey)
	if err != nil {
		t.Fatalf("EncryptStream: %v", err)
	}
	_, err = ew.Write(plaintext)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	err = ew.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	return encrypted.Bytes()
}

func TestEncryptDecryptStream_Roundtrip(t *testing.T) {
	cases := []struct {
		name string
		size int
	}{
		{"empty", 0},
		{"small", 13},
		{"exact_block", 16},
		{"medium", 1024},
		{"large", 1024 * 1024}, // 1 MB
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plaintext := make([]byte, tc.size)
			if tc.size > 0 {
				_, err := rand.Read(plaintext)
				if err != nil {
					t.Fatalf("rand.Read: %v", err)
				}
			}

			encrypted := mustEncrypt(t, plaintext)

			dr, err := DecryptStream(bytes.NewReader(encrypted), testMasterKey)
			if err != nil {
				t.Fatalf("DecryptStream: %v", err)
			}
			decrypted, err := io.ReadAll(dr)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}

			if !bytes.Equal(decrypted, plaintext) {
				t.Errorf("roundtrip mismatch: got %d bytes, want %d bytes", len(decrypted), len(plaintext))
			}
		})
	}
}

func TestDecryptStream_WrongKey(t *testing.T) {
	encrypted := mustEncrypt(t, []byte("secret backup data"))

	wrongKey := bytes.Repeat([]byte{0xCD}, 32)
	_, err := DecryptStream(bytes.NewReader(encrypted), wrongKey)
	if err == nil {
		t.Fatal("expected HMAC verification error with wrong key")
	}
	if !strings.Contains(err.Error(), "HMAC") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecryptStream_CorruptedData(t *testing.T) {
	encrypted := mustEncrypt(t, []byte("important data that must not be tampered with"))

	// Corrupt a byte in the encrypted data (after the header).
	data := make([]byte, len(encrypted))
	copy(data, encrypted)
	headerSize := 8 + saltSize + aesIVSize
	if len(data) > headerSize+5 {
		data[headerSize+5] ^= 0xFF
	}

	_, err := DecryptStream(bytes.NewReader(data), testMasterKey)
	if err == nil {
		t.Fatal("expected HMAC verification error for corrupted data")
	}
}

func TestDecryptStream_InvalidMagic(t *testing.T) {
	badData := []byte("NOT_MOCA_ENC_header_and_more_padding_data_here!!")
	_, err := DecryptStream(bytes.NewReader(badData), testMasterKey)
	if err == nil {
		t.Fatal("expected error for invalid magic header")
	}
	if !strings.Contains(err.Error(), "magic") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecryptStream_TruncatedFile(t *testing.T) {
	// Only the magic header, nothing else.
	_, err := DecryptStream(bytes.NewReader(fileMagic[:]), testMasterKey)
	if err == nil {
		t.Fatal("expected error for truncated file")
	}
}

func TestParseHexKey_Valid(t *testing.T) {
	hexKey := strings.Repeat("ab", 32) // 64 hex chars = 32 bytes
	key, err := ParseHexKey(hexKey)
	if err != nil {
		t.Fatalf("ParseHexKey: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(key))
	}
}

func TestParseHexKey_InvalidLength(t *testing.T) {
	_, err := ParseHexKey("tooshort")
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestParseHexKey_InvalidHex(t *testing.T) {
	_, err := ParseHexKey(strings.Repeat("zz", 32))
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestEncryptStream_MultipleWrites(t *testing.T) {
	var encrypted bytes.Buffer
	ew, err := EncryptStream(&encrypted, testMasterKey)
	if err != nil {
		t.Fatalf("EncryptStream: %v", err)
	}

	// Write in multiple chunks.
	chunks := []string{"hello ", "world ", "from ", "moca"}
	var expected bytes.Buffer
	for _, chunk := range chunks {
		expected.WriteString(chunk)
		_, err = ew.Write([]byte(chunk))
		if err != nil {
			t.Fatalf("Write(%q): %v", chunk, err)
		}
	}
	err = ew.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Decrypt and verify.
	dr, err := DecryptStream(bytes.NewReader(encrypted.Bytes()), testMasterKey)
	if err != nil {
		t.Fatalf("DecryptStream: %v", err)
	}
	decrypted, err := io.ReadAll(dr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(decrypted, expected.Bytes()) {
		t.Errorf("multi-write roundtrip failed: got %q, want %q", decrypted, expected.Bytes())
	}
}
