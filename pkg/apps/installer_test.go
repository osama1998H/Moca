package apps

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"User", "user"},
		{"DocType", "doc_type"},
		{"HasRole", "has_role"},
		{"DocField", "doc_field"},
		{"ModuleDef", "module_def"},
		{"SystemSettings", "system_settings"},
		{"simple", "simple"},
		{"HTMLEditor", "h_t_m_l_editor"},
		{"", ""},
		{"Library Management", "library_management"},
		{"Sales-Order", "sales_order"},
		{"Multi Word Name", "multi_word_name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toSnakeCase(tt.input)
			if got != tt.expected {
				t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFindApp(t *testing.T) {
	apps := []AppInfo{
		{Name: "core", Manifest: &AppManifest{Name: "core"}},
		{Name: "crm", Manifest: &AppManifest{Name: "crm"}},
		{Name: "accounting", Manifest: &AppManifest{Name: "accounting"}},
	}

	t.Run("found", func(t *testing.T) {
		app := findApp(apps, "crm")
		if app == nil {
			t.Fatal("expected to find crm")
		}
		if app.Name != "crm" {
			t.Errorf("Name = %q, want %q", app.Name, "crm")
		}
	})

	t.Run("not found", func(t *testing.T) {
		app := findApp(apps, "nonexistent")
		if app != nil {
			t.Error("expected nil for nonexistent app")
		}
	})
}

func TestConvertMigrations(t *testing.T) {
	input := []Migration{
		{Version: "001", Up: "CREATE TABLE x (id INT)", Down: "DROP TABLE x", DependsOn: nil},
		{Version: "002", Up: "ALTER TABLE x ADD col TEXT", Down: "ALTER TABLE x DROP col", DependsOn: []string{"myapp:001"}},
	}

	result := convertMigrations("myapp", input)

	if len(result) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(result))
	}

	if result[0].AppName != "myapp" || result[0].Version != "001" {
		t.Errorf("migration 0: AppName=%q Version=%q", result[0].AppName, result[0].Version)
	}
	if result[0].UpSQL != "CREATE TABLE x (id INT)" {
		t.Errorf("migration 0 UpSQL = %q", result[0].UpSQL)
	}

	if result[1].Version != "002" || len(result[1].DependsOn) != 1 {
		t.Errorf("migration 1: Version=%q DependsOn=%v", result[1].Version, result[1].DependsOn)
	}
}

func TestReorderChildrenFirst(t *testing.T) {
	child := &meta.MetaType{Name: "HasRole", IsChildTable: true}
	parent := &meta.MetaType{Name: "User"}

	result := reorderChildrenFirst([]*meta.MetaType{parent, child})
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Name != "HasRole" {
		t.Errorf("expected child first, got %q", result[0].Name)
	}
	if result[1].Name != "User" {
		t.Errorf("expected parent second, got %q", result[1].Name)
	}
}

func TestLoadAppMetaTypes(t *testing.T) {
	// Create a minimal test app structure in testdata.
	app := &AppInfo{
		Name: "testapp",
		Path: "testdata/test_app_metatypes",
		Manifest: &AppManifest{
			Name: "testapp",
			Modules: []ModuleDef{
				{
					Name:     "TestModule",
					DocTypes: []string{"TestDoc"},
				},
			},
		},
	}

	mts, err := loadAppMetaTypes(app)
	if err != nil {
		t.Fatalf("loadAppMetaTypes failed: %v", err)
	}

	if len(mts) != 1 {
		t.Fatalf("expected 1 MetaType, got %d", len(mts))
	}

	if mts[0].Name != "TestDoc" {
		t.Errorf("MetaType name = %q, want %q", mts[0].Name, "TestDoc")
	}
}

func TestLoadAppMetaTypes_MissingFile(t *testing.T) {
	app := &AppInfo{
		Name: "testapp",
		Path: "testdata/nonexistent_app",
		Manifest: &AppManifest{
			Name: "testapp",
			Modules: []ModuleDef{
				{Name: "Mod", DocTypes: []string{"Missing"}},
			},
		},
	}

	_, err := loadAppMetaTypes(app)
	if err == nil {
		t.Fatal("expected error for missing MetaType file")
	}
}
