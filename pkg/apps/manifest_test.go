package apps

import (
	"errors"
	"testing"
)

func TestParseManifest_ValidFull(t *testing.T) {
	m, err := ParseManifest("testdata/valid_full/manifest.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Name != "crm" {
		t.Errorf("Name = %q, want %q", m.Name, "crm")
	}
	if m.Title != "Moca CRM" {
		t.Errorf("Title = %q, want %q", m.Title, "Moca CRM")
	}
	if m.Version != "1.2.3" {
		t.Errorf("Version = %q, want %q", m.Version, "1.2.3")
	}
	if m.Publisher != "Acme Corp" {
		t.Errorf("Publisher = %q, want %q", m.Publisher, "Acme Corp")
	}
	if m.License != "MIT" {
		t.Errorf("License = %q, want %q", m.License, "MIT")
	}
	if m.MocaVersion != ">=0.1.0" {
		t.Errorf("MocaVersion = %q, want %q", m.MocaVersion, ">=0.1.0")
	}
	if len(m.Dependencies) != 1 {
		t.Fatalf("Dependencies count = %d, want 1", len(m.Dependencies))
	}
	if m.Dependencies[0].App != "core" {
		t.Errorf("Dependencies[0].App = %q, want %q", m.Dependencies[0].App, "core")
	}
	if len(m.Modules) != 2 {
		t.Fatalf("Modules count = %d, want 2", len(m.Modules))
	}
	if m.Modules[0].Name != "Selling" {
		t.Errorf("Modules[0].Name = %q, want %q", m.Modules[0].Name, "Selling")
	}
	if len(m.Modules[0].DocTypes) != 2 {
		t.Errorf("Modules[0].DocTypes count = %d, want 2", len(m.Modules[0].DocTypes))
	}
}

func TestParseManifest_ValidMinimal(t *testing.T) {
	m, err := ParseManifest("testdata/valid_minimal/manifest.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Name != "myapp" {
		t.Errorf("Name = %q, want %q", m.Name, "myapp")
	}
	if m.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", m.Version, "0.1.0")
	}
	if len(m.Modules) != 0 {
		t.Errorf("Modules count = %d, want 0", len(m.Modules))
	}
	if len(m.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(m.Dependencies))
	}
}

func TestParseManifest_FileNotFound(t *testing.T) {
	_, err := ParseManifest("testdata/nonexistent/manifest.yaml")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var me *ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("expected *ManifestError, got %T", err)
	}
	if me.File != "testdata/nonexistent/manifest.yaml" {
		t.Errorf("File = %q, want testdata path", me.File)
	}
}

func TestValidateManifest_ValidFull(t *testing.T) {
	m, err := ParseManifest("testdata/valid_full/manifest.yaml")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if err := ValidateManifest(m); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestValidateManifest_ValidMinimal(t *testing.T) {
	m, err := ParseManifest("testdata/valid_minimal/manifest.yaml")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if err := ValidateManifest(m); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestValidateManifest_MissingName(t *testing.T) {
	m, err := ParseManifest("testdata/missing_name/manifest.yaml")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	err = ValidateManifest(m)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	found := false
	for _, e := range ve {
		if e.Field == "name" && e.Message == "required" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'name: required' error, got: %v", ve)
	}
}

func TestValidateManifest_InvalidSemver(t *testing.T) {
	m, err := ParseManifest("testdata/invalid_semver/manifest.yaml")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	err = ValidateManifest(m)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	found := false
	for _, e := range ve {
		if e.Field == "version" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'version' error, got: %v", ve)
	}
}

func TestValidateManifest_InvalidName(t *testing.T) {
	m, err := ParseManifest("testdata/invalid_name/manifest.yaml")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	err = ValidateManifest(m)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	found := false
	for _, e := range ve {
		if e.Field == "name" && e.Message != "required" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected name identifier error, got: %v", ve)
	}
}

func TestValidateManifest_DuplicateModules(t *testing.T) {
	m, err := ParseManifest("testdata/duplicate_modules/manifest.yaml")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	err = ValidateManifest(m)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	found := false
	for _, e := range ve {
		if e.Field == "modules[1].name" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected duplicate module error at modules[1].name, got: %v", ve)
	}
}

func TestValidateManifest_DuplicateDocTypes(t *testing.T) {
	m, err := ParseManifest("testdata/duplicate_doctypes/manifest.yaml")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	err = ValidateManifest(m)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	found := false
	for _, e := range ve {
		if e.Field == "modules[1].doctypes[0]" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected duplicate doctype error at modules[1].doctypes[0], got: %v", ve)
	}
}

func TestValidateManifest_MissingAllRequired(t *testing.T) {
	m := &AppManifest{} // all fields empty

	err := ValidateManifest(m)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	// Should have at least name, version, moca_version errors.
	requiredFields := map[string]bool{"name": false, "version": false, "moca_version": false}
	for _, e := range ve {
		if _, ok := requiredFields[e.Field]; ok {
			requiredFields[e.Field] = true
		}
	}
	for field, found := range requiredFields {
		if !found {
			t.Errorf("expected error for required field %q", field)
		}
	}
}

func TestValidateManifest_InvalidDepConstraint(t *testing.T) {
	m := &AppManifest{
		Name:        "testapp",
		Version:     "1.0.0",
		MocaVersion: ">=0.1.0",
		Dependencies: []AppDep{
			{App: "core", MinVersion: "not-a-constraint!!!"},
		},
	}

	err := ValidateManifest(m)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	found := false
	for _, e := range ve {
		if e.Field == "dependencies[0].min_version" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dependency min_version error, got: %v", ve)
	}
}

func TestValidateManifest_EmptyDepApp(t *testing.T) {
	m := &AppManifest{
		Name:        "testapp",
		Version:     "1.0.0",
		MocaVersion: ">=0.1.0",
		Dependencies: []AppDep{
			{App: "", MinVersion: ">=1.0.0"},
		},
	}

	err := ValidateManifest(m)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	var ve ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	found := false
	for _, e := range ve {
		if e.Field == "dependencies[0].app" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dependencies[0].app error, got: %v", ve)
	}
}
