package i18n

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestExtractFromMetaTypes(t *testing.T) {
	ext := &Extractor{}

	mts := []*meta.MetaType{
		{
			Name:        "SalesOrder",
			Label:       "Sales Order",
			Description: "A document representing a customer order",
			Fields: []meta.FieldDef{
				{
					Name:      "customer_name",
					Label:     "Customer Name",
					FieldType: meta.FieldTypeData,
				},
				{
					Name:      "status",
					Label:     "Status",
					FieldType: meta.FieldTypeSelect,
					Options:   "Open\nClosed\nDraft",
				},
				{
					Name:      "details_section",
					Label:     "Details",
					FieldType: meta.FieldTypeSectionBreak,
					LayoutHint: meta.LayoutHint{
						Label: "Order Details",
					},
				},
			},
		},
	}

	result := ext.ExtractFromMetaTypes(mts)

	// Build a lookup by Source+Context for easy assertions.
	type key struct{ source, ctx string }
	found := make(map[key]bool, len(result))
	for _, ts := range result {
		found[key{ts.Source, ts.Context}] = true
	}

	expected := []struct {
		source  string
		context string
	}{
		{"Sales Order", "DocType:SalesOrder"},
		{"A document representing a customer order", "DocType:SalesOrder:description"},
		{"Customer Name", "DocType:SalesOrder:field:customer_name"},
		{"Status", "DocType:SalesOrder:field:status"},
		{"Open", "DocType:SalesOrder:option:status"},
		{"Closed", "DocType:SalesOrder:option:status"},
		{"Draft", "DocType:SalesOrder:option:status"},
		{"Details", "DocType:SalesOrder:field:details_section"},
		{"Order Details", "DocType:SalesOrder:section"},
	}

	for _, exp := range expected {
		k := key{exp.source, exp.context}
		if !found[k] {
			t.Errorf("missing expected string: source=%q context=%q", exp.source, exp.context)
		}
	}

	if len(result) != len(expected) {
		t.Errorf("expected %d translatable strings, got %d", len(expected), len(result))
		for _, ts := range result {
			t.Logf("  source=%q context=%q", ts.Source, ts.Context)
		}
	}
}

func TestExtractFromMetaTypes_Dedup(t *testing.T) {
	ext := &Extractor{}

	mts := []*meta.MetaType{
		{
			Name:  "DocA",
			Label: "Shared Label",
			Fields: []meta.FieldDef{
				{Name: "title", Label: "Title", FieldType: meta.FieldTypeData},
			},
		},
		{
			Name:  "DocB",
			Label: "Shared Label",
			Fields: []meta.FieldDef{
				{Name: "title", Label: "Title", FieldType: meta.FieldTypeData},
			},
		},
	}

	result := ext.ExtractFromMetaTypes(mts)

	// "Shared Label" appears in both MetaTypes but with different contexts
	// (DocType:DocA vs DocType:DocB), so both should be present.
	// "Title" also appears with different contexts per doctype.
	sourceCounts := make(map[string]int)
	for _, ts := range result {
		sourceCounts[ts.Source]++
	}

	// Each "Shared Label" has a unique context, so both are kept.
	if sourceCounts["Shared Label"] != 2 {
		t.Errorf("expected 2 entries for 'Shared Label' (different contexts), got %d", sourceCounts["Shared Label"])
	}
	if sourceCounts["Title"] != 2 {
		t.Errorf("expected 2 entries for 'Title' (different contexts), got %d", sourceCounts["Title"])
	}

	// Now test true dedup: same source AND same context.
	mts2 := []*meta.MetaType{
		{
			Name:  "DocC",
			Label: "Repeated",
			Fields: []meta.FieldDef{
				{Name: "f1", Label: "Repeated", FieldType: meta.FieldTypeData},
			},
		},
	}

	result2 := ext.ExtractFromMetaTypes(mts2)
	// "Repeated" with context "DocType:DocC" and "Repeated" with context
	// "DocType:DocC:field:f1" are different contexts, so both are kept.
	count := 0
	for _, ts := range result2 {
		if ts.Source == "Repeated" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 entries for 'Repeated' (label vs field context), got %d", count)
	}
}

func TestExtractFromTSX(t *testing.T) {
	dir := t.TempDir()

	tsxContent := `import React from 'react';

function App() {
  return (
    <div>
      <button>{t("Save")}</button>
      <button>{t('Cancel')}</button>
    </div>
  );
}
`
	tsxPath := filepath.Join(dir, "App.tsx")
	if err := os.WriteFile(tsxPath, []byte(tsxContent), 0644); err != nil {
		t.Fatal(err)
	}

	ext := &Extractor{}
	result, err := ext.ExtractFromTSX(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 strings, got %d", len(result))
	}

	// Verify "Save" extraction.
	var saveFound, cancelFound bool
	for _, ts := range result {
		switch ts.Source {
		case "Save":
			saveFound = true
			if ts.File != tsxPath {
				t.Errorf("Save: expected file %q, got %q", tsxPath, ts.File)
			}
			if ts.Line != 6 {
				t.Errorf("Save: expected line 6, got %d", ts.Line)
			}
		case "Cancel":
			cancelFound = true
			if ts.File != tsxPath {
				t.Errorf("Cancel: expected file %q, got %q", tsxPath, ts.File)
			}
			if ts.Line != 7 {
				t.Errorf("Cancel: expected line 7, got %d", ts.Line)
			}
		}
	}

	if !saveFound {
		t.Error("missing 'Save' string")
	}
	if !cancelFound {
		t.Error("missing 'Cancel' string")
	}
}

func TestExtractFromTSX_IgnoresNonTSX(t *testing.T) {
	dir := t.TempDir()

	cssContent := `.btn { content: t("DoNotExtract"); }
`
	if err := os.WriteFile(filepath.Join(dir, "style.css"), []byte(cssContent), 0644); err != nil {
		t.Fatal(err)
	}

	ext := &Extractor{}
	result, err := ext.ExtractFromTSX(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 strings from .css file, got %d", len(result))
		for _, ts := range result {
			t.Logf("  unexpected: source=%q file=%q", ts.Source, ts.File)
		}
	}
}

func TestExtractFromTemplates(t *testing.T) {
	dir := t.TempDir()

	htmlContent := `<html>
<body>
  <h1>{{ _("Submit") }}</h1>
  <p>{{ _("Confirm Action") }}</p>
</body>
</html>
`
	htmlPath := filepath.Join(dir, "page.html")
	if err := os.WriteFile(htmlPath, []byte(htmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	ext := &Extractor{}
	result, err := ext.ExtractFromTemplates(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 strings, got %d", len(result))
	}

	var submitFound, confirmFound bool
	for _, ts := range result {
		switch ts.Source {
		case "Submit":
			submitFound = true
			if ts.File != htmlPath {
				t.Errorf("Submit: expected file %q, got %q", htmlPath, ts.File)
			}
			if ts.Line != 3 {
				t.Errorf("Submit: expected line 3, got %d", ts.Line)
			}
		case "Confirm Action":
			confirmFound = true
			if ts.File != htmlPath {
				t.Errorf("Confirm Action: expected file %q, got %q", htmlPath, ts.File)
			}
			if ts.Line != 4 {
				t.Errorf("Confirm Action: expected line 4, got %d", ts.Line)
			}
		}
	}

	if !submitFound {
		t.Error("missing 'Submit' string")
	}
	if !confirmFound {
		t.Error("missing 'Confirm Action' string")
	}
}

func TestExtractFromTSX_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	ext := &Extractor{}
	result, err := ext.ExtractFromTSX(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 strings from empty directory, got %d", len(result))
	}
}
