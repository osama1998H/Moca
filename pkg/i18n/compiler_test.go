package i18n

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMORoundTrip(t *testing.T) {
	translations := []Translation{
		{SourceText: "Hello", Language: "ar", TranslatedText: "\u0645\u0631\u062d\u0628\u0627", App: "core"},
		{SourceText: "Save", Language: "ar", TranslatedText: "\u062d\u0641\u0638", App: "core"},
		{SourceText: "Cancel", Language: "ar", TranslatedText: "\u0625\u0644\u063a\u0627\u0621", App: "core"},
	}

	var buf bytes.Buffer
	if err := CompileMO(translations, &buf); err != nil {
		t.Fatalf("CompileMO: %v", err)
	}

	got, err := LoadMO(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("LoadMO: %v", err)
	}

	if len(got) != len(translations) {
		t.Fatalf("expected %d entries, got %d", len(translations), len(got))
	}

	for _, tr := range translations {
		val, ok := got[tr.SourceText]
		if !ok {
			t.Errorf("missing key %q in loaded MO", tr.SourceText)
			continue
		}
		if val != tr.TranslatedText {
			t.Errorf("key %q: got %q, want %q", tr.SourceText, val, tr.TranslatedText)
		}
	}
}

func TestMOEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := CompileMO([]Translation{}, &buf); err != nil {
		t.Fatalf("CompileMO empty: %v", err)
	}

	got, err := LoadMO(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("LoadMO empty: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

func TestMOWithContext(t *testing.T) {
	translations := []Translation{
		{SourceText: "Name", Language: "ar", TranslatedText: "\u0627\u0633\u0645", Context: "DocType:User", App: "core"},
		{SourceText: "Name", Language: "ar", TranslatedText: "\u0627\u0644\u0627\u0633\u0645", Context: "DocType:Role", App: "core"},
	}

	var buf bytes.Buffer
	if err := CompileMO(translations, &buf); err != nil {
		t.Fatalf("CompileMO: %v", err)
	}

	// Verify the EOT separator (0x04) appears in the binary data,
	// indicating that context was encoded.
	if !bytes.Contains(buf.Bytes(), []byte{0x04}) {
		t.Error("expected EOT separator (0x04) in MO binary for context entries")
	}

	got, err := LoadMO(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("LoadMO: %v", err)
	}

	// LoadMO strips context and keys by msgid only; with two different contexts
	// for the same msgid, the last one wins in the map.
	val, ok := got["Name"]
	if !ok {
		t.Fatal("missing key \"Name\" in loaded MO")
	}
	// Verify we got one of the valid translations.
	if val != "\u0627\u0633\u0645" && val != "\u0627\u0644\u0627\u0633\u0645" {
		t.Errorf("unexpected translation for \"Name\": got %q", val)
	}
}

func TestMOMagicNumber(t *testing.T) {
	translations := []Translation{
		{SourceText: "Test", Language: "ar", TranslatedText: "\u0627\u062e\u062a\u0628\u0627\u0631", App: "core"},
	}

	var buf bytes.Buffer
	if err := CompileMO(translations, &buf); err != nil {
		t.Fatalf("CompileMO: %v", err)
	}

	data := buf.Bytes()
	if len(data) < 4 {
		t.Fatal("MO output too small to contain magic number")
	}

	magic := binary.LittleEndian.Uint32(data[:4])
	if magic != 0x950412de {
		t.Errorf("magic number: got 0x%08x, want 0x950412de", magic)
	}
}

func TestLoadMOInvalidMagic(t *testing.T) {
	// Create 28 bytes with an invalid magic number.
	data := make([]byte, 28)
	binary.LittleEndian.PutUint32(data[0:4], 0xDEADBEEF)

	_, err := LoadMO(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for invalid magic number, got nil")
	}
}

func TestLoadMOTooSmall(t *testing.T) {
	// MO header requires at least 28 bytes.
	tooSmall := []byte{0xde, 0x12, 0x04, 0x95, 0x00, 0x00}

	_, err := LoadMO(bytes.NewReader(tooSmall))
	if err == nil {
		t.Fatal("expected error for data smaller than 28 bytes, got nil")
	}
}
