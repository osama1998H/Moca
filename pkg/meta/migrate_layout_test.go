package meta_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

// writeTempJSON writes content to a new temporary file in dir and returns the path.
func writeTempJSON(t *testing.T, dir string, content string) string {
	t.Helper()
	f, err := os.CreateTemp(dir, "doctype-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}

// TestMigrateFile_FlatToTree verifies that a flat-format DocType JSON file is
// correctly migrated to tree-native format.
func TestMigrateFile_FlatToTree(t *testing.T) {
	dir := t.TempDir()
	flatJSON := `{
  "name": "Book",
  "module": "Library",
  "fields": [
    {"name": "title", "field_type": "Data", "label": "Title"},
    {"name": "author", "field_type": "Data", "label": "Author"}
  ]
}`
	path := writeTempJSON(t, dir, flatJSON)

	// First call: should migrate.
	migrated, err := meta.MigrateFileToTree(path)
	if err != nil {
		t.Fatalf("MigrateFileToTree: unexpected error: %v", err)
	}
	if !migrated {
		t.Fatal("expected migrated=true for flat input; got false")
	}

	// Re-read and verify the layout key is present.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}

	var probe struct {
		Layout json.RawMessage `json:"layout"`
	}
	if probeErr := json.Unmarshal(data, &probe); probeErr != nil {
		t.Fatalf("parse migrated file: %v", probeErr)
	}
	if len(probe.Layout) == 0 || probe.Layout[0] != '{' {
		t.Fatalf("migrated file missing tree-native layout object; got layout=%s", probe.Layout)
	}

	// File must compile successfully with meta.Compile.
	mt, err := meta.Compile(data)
	if err != nil {
		t.Fatalf("Compile migrated file: %v", err)
	}
	if mt.Name != "Book" {
		t.Errorf("Name: got %q, want %q", mt.Name, "Book")
	}
	if mt.Module != "Library" {
		t.Errorf("Module: got %q, want %q", mt.Module, "Library")
	}
	if len(mt.Fields) != 2 {
		t.Errorf("Fields count: got %d, want 2", len(mt.Fields))
	}
	t.Logf("migrated Book: %d fields, layout tabs=%d", len(mt.Fields), len(mt.Layout.Tabs))
}

// TestMigrateFile_AlreadyTree verifies that calling MigrateFileToTree on a
// file that is already in tree-native format returns (false, nil) and leaves
// the file content unchanged.
func TestMigrateFile_AlreadyTree(t *testing.T) {
	dir := t.TempDir()
	treeJSON := `{
  "name": "Book",
  "module": "Library",
  "layout": {
    "tabs": [{
      "label": "Details",
      "sections": [{
        "columns": [{"fields": ["title"]}]
      }]
    }]
  },
  "fields": {
    "title": {"field_type": "Data", "label": "Title"}
  }
}`
	path := writeTempJSON(t, dir, treeJSON)

	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original file: %v", err)
	}

	migrated, err := meta.MigrateFileToTree(path)
	if err != nil {
		t.Fatalf("MigrateFileToTree: unexpected error: %v", err)
	}
	if migrated {
		t.Fatal("expected migrated=false for already-tree-native input; got true")
	}

	// File content must be unchanged.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file after call: %v", err)
	}
	if string(original) != string(after) {
		t.Errorf("file content changed even though no migration was needed\nbefore:\n%s\nafter:\n%s", original, after)
	}
	t.Logf("already-tree-native file correctly left unchanged")
}

// TestMigrateFile_PreservesProperties verifies that all top-level MetaType
// properties (label, description, naming_rule, permissions, api_config,
// is_submittable, track_changes, etc.) are preserved verbatim after migration.
func TestMigrateFile_PreservesProperties(t *testing.T) {
	dir := t.TempDir()
	flatJSON := `{
  "name": "Invoice",
  "module": "accounts",
  "label": "Tax Invoice",
  "description": "A formal billing document.",
  "naming_rule": {"rule": "pattern", "pattern": "INV-.####"},
  "title_field": "customer_name",
  "sort_field": "creation",
  "sort_order": "desc",
  "search_fields": ["customer_name"],
  "is_submittable": true,
  "track_changes": true,
  "permissions": [
    {"role": "Accountant", "doctype_perm": 7}
  ],
  "api_config": {
    "enabled": true,
    "allow_list": true,
    "allow_get": true,
    "allow_create": true,
    "allow_update": true,
    "allow_delete": false,
    "default_page_size": 20,
    "max_page_size": 100,
    "base_path": "/api/v1/invoices",
    "always_include": [],
    "middleware": [],
    "computed_fields": [],
    "exclude_fields": [],
    "default_fields": [],
    "versions": []
  },
  "fields": [
    {"name": "customer_name", "field_type": "Data", "label": "Customer Name", "required": true},
    {"name": "amount", "field_type": "Currency", "label": "Amount"}
  ]
}`
	// Write the flat JSON under a stable path (no need for the temp path returned above).
	stablePath := filepath.Join(dir, "Invoice.json")
	if err := os.WriteFile(stablePath, []byte(flatJSON), 0o644); err != nil {
		t.Fatalf("write stable path: %v", err)
	}

	migrated, err := meta.MigrateFileToTree(stablePath)
	if err != nil {
		t.Fatalf("MigrateFileToTree: unexpected error: %v", err)
	}
	if !migrated {
		t.Fatal("expected migrated=true; got false")
	}

	data, err := os.ReadFile(stablePath)
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}

	mt, err := meta.Compile(data)
	if err != nil {
		t.Fatalf("Compile migrated file: %v", err)
	}

	// Verify all preserved properties.
	if mt.Name != "Invoice" {
		t.Errorf("Name: got %q, want %q", mt.Name, "Invoice")
	}
	if mt.Module != "accounts" {
		t.Errorf("Module: got %q, want %q", mt.Module, "accounts")
	}
	if mt.Label != "Tax Invoice" {
		t.Errorf("Label: got %q, want %q", mt.Label, "Tax Invoice")
	}
	if mt.Description != "A formal billing document." {
		t.Errorf("Description: got %q, want %q", mt.Description, "A formal billing document.")
	}
	if mt.NamingRule.Rule != meta.NamingByPattern {
		t.Errorf("NamingRule.Rule: got %q, want %q", mt.NamingRule.Rule, meta.NamingByPattern)
	}
	if mt.NamingRule.Pattern != "INV-.####" {
		t.Errorf("NamingRule.Pattern: got %q, want %q", mt.NamingRule.Pattern, "INV-.####")
	}
	if mt.TitleField != "customer_name" {
		t.Errorf("TitleField: got %q, want %q", mt.TitleField, "customer_name")
	}
	if mt.SortField != "creation" {
		t.Errorf("SortField: got %q, want %q", mt.SortField, "creation")
	}
	if mt.SortOrder != "desc" {
		t.Errorf("SortOrder: got %q, want %q", mt.SortOrder, "desc")
	}
	if len(mt.SearchFields) != 1 || mt.SearchFields[0] != "customer_name" {
		t.Errorf("SearchFields: got %v, want [customer_name]", mt.SearchFields)
	}
	if !mt.IsSubmittable {
		t.Error("IsSubmittable: got false, want true")
	}
	if !mt.TrackChanges {
		t.Error("TrackChanges: got false, want true")
	}
	if len(mt.Permissions) != 1 {
		t.Errorf("Permissions: got %d entries, want 1", len(mt.Permissions))
	} else {
		perm := mt.Permissions[0]
		if perm.Role != "Accountant" {
			t.Errorf("Permissions[0].Role: got %q, want %q", perm.Role, "Accountant")
		}
		if perm.DocTypePerm != 7 {
			t.Errorf("Permissions[0].DocTypePerm: got %d, want 7", perm.DocTypePerm)
		}
	}
	if mt.APIConfig == nil {
		t.Fatal("APIConfig: got nil, want non-nil")
	}
	if !mt.APIConfig.Enabled {
		t.Error("APIConfig.Enabled: got false, want true")
	}
	if mt.APIConfig.DefaultPageSize != 20 {
		t.Errorf("APIConfig.DefaultPageSize: got %d, want 20", mt.APIConfig.DefaultPageSize)
	}
	if mt.APIConfig.BasePath != "/api/v1/invoices" {
		t.Errorf("APIConfig.BasePath: got %q, want %q", mt.APIConfig.BasePath, "/api/v1/invoices")
	}

	// Fields must all be present and correct.
	if len(mt.Fields) != 2 {
		t.Fatalf("Fields: got %d, want 2", len(mt.Fields))
	}
	if mt.Fields[0].Name != "customer_name" {
		t.Errorf("Fields[0].Name: got %q, want %q", mt.Fields[0].Name, "customer_name")
	}
	if !mt.Fields[0].Required {
		t.Error("Fields[0].Required: got false, want true")
	}

	t.Logf("Invoice migrated: %d fields, naming_rule=%q, perms=%d, api_config.enabled=%v",
		len(mt.Fields), mt.NamingRule.Rule, len(mt.Permissions), mt.APIConfig.Enabled)
}
