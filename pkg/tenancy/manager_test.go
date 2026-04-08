package tenancy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/sitepath"
)

func TestSanitizeForSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"dots to underscores", "acme.localhost", "acme_localhost"},
		{"dashes to underscores", "my-erp", "my_erp"},
		{"spaces to underscores", "my erp", "my_erp"},
		{"uppercase to lower", "Acme.Corp", "acme_corp"},
		{"leading digit gets s prefix", "123site", "s123site"},
		{"strip special chars", "acme@corp!", "acmecorp"},
		{"mixed", "My-ERP.v2", "my_erp_v2"},
		{"all special becomes site", "@#$", "site"},
		{"empty string", "", "site"},
		{"already clean", "acme", "acme"},
		{"underscores preserved", "my_erp", "my_erp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeForSchema(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeForSchema(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSchemaNameForSite(t *testing.T) {
	tests := []struct {
		site     string
		expected string
	}{
		{"acme.localhost", "tenant_acme_localhost"},
		{"my-erp", "tenant_my_erp"},
		{"simple", "tenant_simple"},
	}

	for _, tt := range tests {
		t.Run(tt.site, func(t *testing.T) {
			got := SchemaNameForSite(tt.site)
			if got != tt.expected {
				t.Errorf("SchemaNameForSite(%q) = %q, want %q", tt.site, got, tt.expected)
			}
		})
	}
}

func TestValidateSiteConfig(t *testing.T) {
	tests := []struct {
		cfg     SiteCreateConfig
		name    string
		wantErr bool
	}{
		{
			cfg:  SiteCreateConfig{Name: "acme", AdminEmail: "admin@acme.com", AdminPassword: "secret123"},
			name: "valid config",
		},
		{
			cfg:  SiteCreateConfig{Name: "acme.localhost", AdminEmail: "admin@acme.com", AdminPassword: "secret123"},
			name: "valid dotted name",
		},
		{
			cfg:     SiteCreateConfig{AdminEmail: "admin@acme.com", AdminPassword: "secret123"},
			name:    "missing name",
			wantErr: true,
		},
		{
			cfg:     SiteCreateConfig{Name: "acme", AdminPassword: "secret123"},
			name:    "missing email",
			wantErr: true,
		},
		{
			cfg:     SiteCreateConfig{Name: "acme", AdminEmail: "admin@acme.com"},
			name:    "missing password",
			wantErr: true,
		},
		{
			cfg:     SiteCreateConfig{Name: "../../../etc", AdminEmail: "admin@acme.com", AdminPassword: "secret123"},
			name:    "reject traversal",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSiteConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSiteConfig() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSiteName(t *testing.T) {
	valid := []string{"acme", "acme.localhost", "my-site", "my_site", "Acme-01"}
	for _, name := range valid {
		t.Run("valid/"+name, func(t *testing.T) {
			if err := sitepath.ValidateName(name); err != nil {
				t.Fatalf("ValidateName(%q) returned error: %v", name, err)
			}
		})
	}

	invalid := []string{"", " ", "../etc", "/tmp/evil", "acme/site", ".hidden", "trailing-", " leading"}
	for _, name := range invalid {
		t.Run("invalid/"+name, func(t *testing.T) {
			if err := sitepath.ValidateName(name); err == nil {
				t.Fatalf("ValidateName(%q) unexpectedly succeeded", name)
			}
		})
	}
}

func TestSitePath(t *testing.T) {
	projectRoot := t.TempDir()

	path, err := sitepath.Path(projectRoot, "acme.localhost", "backups")
	if err != nil {
		t.Fatalf("Path returned error: %v", err)
	}

	want := filepath.Join(projectRoot, "sites", "acme.localhost", "backups")
	if path != want {
		t.Fatalf("SitePath = %q, want %q", path, want)
	}
}

func TestSitePathRejectsTraversal(t *testing.T) {
	projectRoot := t.TempDir()

	if _, err := sitepath.Path(projectRoot, "../../../etc"); err == nil {
		t.Fatal("expected Path to reject traversal site name")
	}
}

func TestSetActiveSiteRejectsInvalidName(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := &SiteManager{}

	if err := mgr.SetActiveSite(tmpDir, "../etc"); err == nil {
		t.Fatal("expected SetActiveSite to reject invalid site name")
	}
}

func TestReorderChildrenFirst(t *testing.T) {
	child1 := &meta.MetaType{Name: "DocField", IsChildTable: true}
	child2 := &meta.MetaType{Name: "HasRole", IsChildTable: true}
	parent1 := &meta.MetaType{Name: "User"}
	parent2 := &meta.MetaType{Name: "DocType"}

	input := []*meta.MetaType{parent1, child1, parent2, child2}
	result := reorderChildrenFirst(input)

	if len(result) != 4 {
		t.Fatalf("expected 4 MetaTypes, got %d", len(result))
	}

	// First two should be children, last two should be parents.
	if !result[0].IsChildTable || !result[1].IsChildTable {
		t.Errorf("expected first two to be child tables, got %q and %q",
			result[0].Name, result[1].Name)
	}
	if result[2].IsChildTable || result[3].IsChildTable {
		t.Errorf("expected last two to be parent tables, got %q and %q",
			result[2].Name, result[3].Name)
	}
}

func TestReorderChildrenFirst_Empty(t *testing.T) {
	result := reorderChildrenFirst(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d", len(result))
	}
}

func TestReorderChildrenFirst_AllParents(t *testing.T) {
	mts := []*meta.MetaType{
		{Name: "User"},
		{Name: "Role"},
	}
	result := reorderChildrenFirst(mts)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Name != "User" || result[1].Name != "Role" {
		t.Errorf("expected order preserved for all-parent input")
	}
}

func TestSetActiveSite(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &SiteManager{}
	if err := mgr.SetActiveSite(tmpDir, "acme.localhost"); err != nil {
		t.Fatalf("SetActiveSite failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, ".moca", "current_site"))
	if err != nil {
		t.Fatalf("read current_site failed: %v", err)
	}
	if string(data) != "acme.localhost" {
		t.Errorf("current_site = %q, want %q", string(data), "acme.localhost")
	}
}

func TestSetActiveSite_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := &SiteManager{}

	_ = mgr.SetActiveSite(tmpDir, "site1")
	_ = mgr.SetActiveSite(tmpDir, "site2")

	data, _ := os.ReadFile(filepath.Join(tmpDir, ".moca", "current_site"))
	if string(data) != "site2" {
		t.Errorf("current_site = %q, want %q", string(data), "site2")
	}
}

func TestRandomID(t *testing.T) {
	id1, err := randomID("test")
	if err != nil {
		t.Fatalf("randomID failed: %v", err)
	}
	id2, err := randomID("test")
	if err != nil {
		t.Fatalf("randomID failed: %v", err)
	}

	if id1 == id2 {
		t.Errorf("randomID should produce unique IDs, got %q twice", id1)
	}
	if len(id1) < 10 {
		t.Errorf("randomID too short: %q", id1)
	}
}
