package i18n

import (
	"bytes"
	"strings"
	"testing"
)

func sampleTranslations() []Translation {
	return []Translation{
		{SourceText: "Hello", Language: "ar", TranslatedText: "\u0645\u0631\u062d\u0628\u0627", Context: "", App: "core"},
		{SourceText: "Save", Language: "ar", TranslatedText: "\u062d\u0641\u0638", Context: "DocType:User", App: "core"},
	}
}

func TestPORoundTrip(t *testing.T) {
	orig := sampleTranslations()

	var buf bytes.Buffer
	if err := ExportPO(orig, &buf); err != nil {
		t.Fatalf("ExportPO: %v", err)
	}

	got, err := ImportPO(&buf)
	if err != nil {
		t.Fatalf("ImportPO: %v", err)
	}

	if len(got) != len(orig) {
		t.Fatalf("expected %d translations, got %d", len(orig), len(got))
	}

	for i, want := range orig {
		if got[i].SourceText != want.SourceText {
			t.Errorf("[%d] SourceText: got %q, want %q", i, got[i].SourceText, want.SourceText)
		}
		if got[i].TranslatedText != want.TranslatedText {
			t.Errorf("[%d] TranslatedText: got %q, want %q", i, got[i].TranslatedText, want.TranslatedText)
		}
		if got[i].Context != want.Context {
			t.Errorf("[%d] Context: got %q, want %q", i, got[i].Context, want.Context)
		}
	}
}

func TestCSVRoundTrip(t *testing.T) {
	orig := sampleTranslations()

	var buf bytes.Buffer
	if err := ExportCSV(orig, &buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	got, err := ImportCSV(&buf)
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}

	if len(got) != len(orig) {
		t.Fatalf("expected %d translations, got %d", len(orig), len(got))
	}

	for i, want := range orig {
		if got[i].SourceText != want.SourceText {
			t.Errorf("[%d] SourceText: got %q, want %q", i, got[i].SourceText, want.SourceText)
		}
		if got[i].Language != want.Language {
			t.Errorf("[%d] Language: got %q, want %q", i, got[i].Language, want.Language)
		}
		if got[i].TranslatedText != want.TranslatedText {
			t.Errorf("[%d] TranslatedText: got %q, want %q", i, got[i].TranslatedText, want.TranslatedText)
		}
		if got[i].Context != want.Context {
			t.Errorf("[%d] Context: got %q, want %q", i, got[i].Context, want.Context)
		}
		if got[i].App != want.App {
			t.Errorf("[%d] App: got %q, want %q", i, got[i].App, want.App)
		}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	orig := sampleTranslations()

	var buf bytes.Buffer
	if err := ExportJSON(orig, &buf); err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	got, err := ImportJSON(&buf)
	if err != nil {
		t.Fatalf("ImportJSON: %v", err)
	}

	if len(got) != len(orig) {
		t.Fatalf("expected %d translations, got %d", len(orig), len(got))
	}

	for i, want := range orig {
		if got[i].SourceText != want.SourceText {
			t.Errorf("[%d] SourceText: got %q, want %q", i, got[i].SourceText, want.SourceText)
		}
		if got[i].Language != want.Language {
			t.Errorf("[%d] Language: got %q, want %q", i, got[i].Language, want.Language)
		}
		if got[i].TranslatedText != want.TranslatedText {
			t.Errorf("[%d] TranslatedText: got %q, want %q", i, got[i].TranslatedText, want.TranslatedText)
		}
		if got[i].Context != want.Context {
			t.Errorf("[%d] Context: got %q, want %q", i, got[i].Context, want.Context)
		}
		if got[i].App != want.App {
			t.Errorf("[%d] App: got %q, want %q", i, got[i].App, want.App)
		}
	}
}

func TestPOWithContext(t *testing.T) {
	translations := []Translation{
		{SourceText: "Name", Language: "ar", TranslatedText: "\u0627\u0633\u0645", Context: "DocType:User", App: "core"},
	}

	var buf bytes.Buffer
	if err := ExportPO(translations, &buf); err != nil {
		t.Fatalf("ExportPO: %v", err)
	}

	po := buf.String()
	if !strings.Contains(po, "msgctxt") {
		t.Fatal("expected PO output to contain msgctxt")
	}
	if !strings.Contains(po, "DocType:User") {
		t.Fatal("expected PO output to contain context value")
	}

	got, err := ImportPO(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ImportPO: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 translation, got %d", len(got))
	}
	if got[0].Context != "DocType:User" {
		t.Errorf("Context: got %q, want %q", got[0].Context, "DocType:User")
	}
	if got[0].SourceText != "Name" {
		t.Errorf("SourceText: got %q, want %q", got[0].SourceText, "Name")
	}
}

func TestEmptyExport(t *testing.T) {
	empty := []Translation{}

	t.Run("PO", func(t *testing.T) {
		var buf bytes.Buffer
		if err := ExportPO(empty, &buf); err != nil {
			t.Fatalf("ExportPO empty: %v", err)
		}
		got, err := ImportPO(&buf)
		if err != nil {
			t.Fatalf("ImportPO empty: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected 0 translations, got %d", len(got))
		}
	})

	t.Run("CSV", func(t *testing.T) {
		var buf bytes.Buffer
		if err := ExportCSV(empty, &buf); err != nil {
			t.Fatalf("ExportCSV empty: %v", err)
		}
		got, err := ImportCSV(&buf)
		if err != nil {
			t.Fatalf("ImportCSV empty: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected 0 translations, got %d", len(got))
		}
	})

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		if err := ExportJSON(empty, &buf); err != nil {
			t.Fatalf("ExportJSON empty: %v", err)
		}
		got, err := ImportJSON(&buf)
		if err != nil {
			t.Fatalf("ImportJSON empty: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected 0 translations, got %d", len(got))
		}
	})
}

func TestPOSpecialChars(t *testing.T) {
	translations := []Translation{
		{SourceText: "Line1\nLine2", Language: "ar", TranslatedText: "\u0633\u0637\u06311\n\u0633\u0637\u06312", App: "core"},
		{SourceText: "Tab\there", Language: "ar", TranslatedText: "\u062a\u0628\u0648\u064a\u0628\t\u0647\u0646\u0627", App: "core"},
		{SourceText: `He said "hello"`, Language: "ar", TranslatedText: "\u0642\u0627\u0644 \"\u0645\u0631\u062d\u0628\u0627\"", App: "core"},
	}

	var buf bytes.Buffer
	if err := ExportPO(translations, &buf); err != nil {
		t.Fatalf("ExportPO: %v", err)
	}

	po := buf.String()

	// Verify escape sequences appear in the PO output.
	if !strings.Contains(po, `\n`) {
		t.Error("expected escaped newline (\\n) in PO output")
	}
	if !strings.Contains(po, `\t`) {
		t.Error("expected escaped tab (\\t) in PO output")
	}
	if !strings.Contains(po, `\"`) {
		t.Error("expected escaped quote (\\\") in PO output")
	}

	got, err := ImportPO(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ImportPO: %v", err)
	}

	if len(got) != len(translations) {
		t.Fatalf("expected %d translations, got %d", len(translations), len(got))
	}

	for i, want := range translations {
		if got[i].SourceText != want.SourceText {
			t.Errorf("[%d] SourceText: got %q, want %q", i, got[i].SourceText, want.SourceText)
		}
		if got[i].TranslatedText != want.TranslatedText {
			t.Errorf("[%d] TranslatedText: got %q, want %q", i, got[i].TranslatedText, want.TranslatedText)
		}
	}
}
