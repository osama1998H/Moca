package apps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDeskManifest_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desk-manifest.json")
	content := `{
  "app": "crm",
  "version": "1.0.0",
  "extensions": {
    "field_types": {
      "Phone": "./fields/PhoneField.tsx"
    },
    "pages": [
      {
        "path": "/desk/app/crm-dashboard",
        "component": "./pages/CRMDashboard.tsx",
        "label": "CRM Dashboard",
        "icon": "Phone"
      }
    ],
    "sidebar_items": [
      {
        "label": "CRM",
        "icon": "Phone",
        "children": [
          { "label": "Dashboard", "path": "/desk/app/crm-dashboard" }
        ]
      }
    ],
    "dashboard_widgets": [
      {
        "name": "crm_pipeline",
        "component": "./widgets/PipelineWidget.tsx",
        "label": "Pipeline"
      }
    ]
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := ParseDeskManifest(path)
	if err != nil {
		t.Fatalf("ParseDeskManifest() error: %v", err)
	}

	if m.App != "crm" {
		t.Errorf("App = %q, want %q", m.App, "crm")
	}
	if m.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", m.Version, "1.0.0")
	}
	if len(m.Extensions.FieldTypes) != 1 {
		t.Errorf("FieldTypes count = %d, want 1", len(m.Extensions.FieldTypes))
	}
	if m.Extensions.FieldTypes["Phone"] != "./fields/PhoneField.tsx" {
		t.Errorf("FieldTypes[Phone] = %q", m.Extensions.FieldTypes["Phone"])
	}
	if len(m.Extensions.Pages) != 1 {
		t.Errorf("Pages count = %d, want 1", len(m.Extensions.Pages))
	}
	if len(m.Extensions.SidebarItems) != 1 {
		t.Errorf("SidebarItems count = %d, want 1", len(m.Extensions.SidebarItems))
	}
	if len(m.Extensions.DashboardWidgets) != 1 {
		t.Errorf("DashboardWidgets count = %d, want 1", len(m.Extensions.DashboardWidgets))
	}
}

func TestParseDeskManifest_FileNotFound(t *testing.T) {
	_, err := ParseDeskManifest("/nonexistent/desk-manifest.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseDeskManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desk-manifest.json")
	if err := os.WriteFile(path, []byte(`{invalid`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseDeskManifest(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateDeskManifest_Valid(t *testing.T) {
	m := &DeskManifest{
		App:     "crm",
		Version: "1.0.0",
		Extensions: DeskExtensions{
			FieldTypes: map[string]string{"Phone": "./fields/PhoneField.tsx"},
			Pages: []DeskPageDef{
				{Path: "/desk/app/crm-dash", Component: "./pages/Dash.tsx", Label: "Dash"},
			},
			SidebarItems: []DeskSidebarDef{
				{Label: "CRM", Children: []DeskSidebarChild{{Label: "Dash", Path: "/desk/app/crm-dash"}}},
			},
			DashboardWidgets: []DeskWidgetDef{
				{Name: "pipeline", Component: "./widgets/Pipeline.tsx"},
			},
		},
	}

	if err := ValidateDeskManifest(m); err != nil {
		t.Errorf("ValidateDeskManifest() unexpected error: %v", err)
	}
}

func TestValidateDeskManifest_EmptyExtensions(t *testing.T) {
	m := &DeskManifest{
		App:     "empty_app",
		Version: "0.1.0",
	}

	if err := ValidateDeskManifest(m); err != nil {
		t.Errorf("empty extensions should be valid, got: %v", err)
	}
}

func TestValidateDeskManifest_MissingRequired(t *testing.T) {
	m := &DeskManifest{}

	err := ValidateDeskManifest(m)
	if err == nil {
		t.Fatal("expected validation error for empty manifest")
	}

	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	fields := make(map[string]bool)
	for _, ve := range verrs {
		fields[ve.Field] = true
	}

	for _, required := range []string{"app", "version"} {
		if !fields[required] {
			t.Errorf("expected error for field %q", required)
		}
	}
}

func TestValidateDeskManifest_InvalidAppName(t *testing.T) {
	m := &DeskManifest{
		App:     "Invalid-Name",
		Version: "1.0.0",
	}

	err := ValidateDeskManifest(m)
	if err == nil {
		t.Fatal("expected validation error for invalid app name")
	}
}

func TestValidateDeskManifest_InvalidVersion(t *testing.T) {
	m := &DeskManifest{
		App:     "myapp",
		Version: "not-a-version",
	}

	err := ValidateDeskManifest(m)
	if err == nil {
		t.Fatal("expected validation error for invalid version")
	}
}

func TestValidateDeskManifest_AbsoluteComponentPath(t *testing.T) {
	m := &DeskManifest{
		App:     "myapp",
		Version: "1.0.0",
		Extensions: DeskExtensions{
			FieldTypes: map[string]string{"Bad": "/absolute/path.tsx"},
		},
	}

	err := ValidateDeskManifest(m)
	if err == nil {
		t.Fatal("expected validation error for absolute component path")
	}
}

func TestValidateDeskManifest_DuplicatePagePaths(t *testing.T) {
	m := &DeskManifest{
		App:     "myapp",
		Version: "1.0.0",
		Extensions: DeskExtensions{
			Pages: []DeskPageDef{
				{Path: "/desk/app/dup", Component: "./pages/A.tsx"},
				{Path: "/desk/app/dup", Component: "./pages/B.tsx"},
			},
		},
	}

	err := ValidateDeskManifest(m)
	if err == nil {
		t.Fatal("expected validation error for duplicate page paths")
	}
}

func TestValidateDeskManifest_DuplicateWidgetNames(t *testing.T) {
	m := &DeskManifest{
		App:     "myapp",
		Version: "1.0.0",
		Extensions: DeskExtensions{
			DashboardWidgets: []DeskWidgetDef{
				{Name: "dup", Component: "./widgets/A.tsx"},
				{Name: "dup", Component: "./widgets/B.tsx"},
			},
		},
	}

	err := ValidateDeskManifest(m)
	if err == nil {
		t.Fatal("expected validation error for duplicate widget names")
	}
}

func TestValidateDeskManifest_InvalidPagePath(t *testing.T) {
	m := &DeskManifest{
		App:     "myapp",
		Version: "1.0.0",
		Extensions: DeskExtensions{
			Pages: []DeskPageDef{
				{Path: "/wrong/prefix", Component: "./pages/A.tsx"},
			},
		},
	}

	err := ValidateDeskManifest(m)
	if err == nil {
		t.Fatal("expected validation error for page path not starting with /desk/app/")
	}
}

func TestValidateDeskManifest_SidebarChildMissingFields(t *testing.T) {
	m := &DeskManifest{
		App:     "myapp",
		Version: "1.0.0",
		Extensions: DeskExtensions{
			SidebarItems: []DeskSidebarDef{
				{Label: "Group", Children: []DeskSidebarChild{{}}},
			},
		},
	}

	err := ValidateDeskManifest(m)
	if err == nil {
		t.Fatal("expected validation error for empty sidebar child")
	}

	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	if len(verrs) < 2 {
		t.Errorf("expected at least 2 errors (label + path), got %d", len(verrs))
	}
}
