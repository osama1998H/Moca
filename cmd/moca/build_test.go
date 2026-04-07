package main

import (
	"strings"
	"testing"

	"github.com/osama1998H/moca/pkg/apps"
	"github.com/spf13/cobra"
)

func TestBuildCommandSubcommands(t *testing.T) {
	cmd := NewBuildCommand()

	expected := []string{"desk", "portal", "assets", "app", "server"}
	subs := cmd.Commands()
	if len(subs) != len(expected) {
		t.Fatalf("expected %d subcommands, got %d", len(expected), len(subs))
	}

	names := make(map[string]bool)
	for _, sub := range subs {
		names[sub.Name()] = true
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing subcommand %q", name)
		}
	}

	// Verify app, server, and desk have real implementations (non-nil RunE).
	for _, sub := range subs {
		if sub.Name() == "app" || sub.Name() == "server" || sub.Name() == "desk" {
			if sub.RunE == nil {
				t.Errorf("subcommand %q has nil RunE (still a placeholder?)", sub.Name())
			}
		}
	}
}

func TestBuildAppFlags(t *testing.T) {
	cmd := NewBuildCommand()

	var appCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "app" {
			appCmd = sub
			break
		}
	}
	if appCmd == nil {
		t.Fatal("app subcommand not found")
	}

	// Verify flags.
	for _, flag := range []string{"race", "verbose"} {
		if appCmd.Flags().Lookup(flag) == nil {
			t.Errorf("app subcommand missing flag --%s", flag)
		}
	}

	// Verify requires exactly 1 arg.
	if appCmd.Args == nil {
		t.Error("app subcommand has no Args validator")
	}
}

func TestBuildDeskFlags(t *testing.T) {
	cmd := NewBuildCommand()

	var deskCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "desk" {
			deskCmd = sub
			break
		}
	}
	if deskCmd == nil {
		t.Fatal("desk subcommand not found")
	}

	if deskCmd.RunE == nil {
		t.Error("desk subcommand has nil RunE — still a placeholder")
	}

	if deskCmd.Flags().Lookup("verbose") == nil {
		t.Error("desk subcommand missing flag --verbose")
	}
}

func TestBuildServerFlags(t *testing.T) {
	cmd := NewBuildCommand()

	var serverCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "server" {
			serverCmd = sub
			break
		}
	}
	if serverCmd == nil {
		t.Fatal("server subcommand not found")
	}

	// Verify flags.
	for _, flag := range []string{"output", "race", "verbose", "ldflags"} {
		if serverCmd.Flags().Lookup(flag) == nil {
			t.Errorf("server subcommand missing flag --%s", flag)
		}
	}
}

func TestEnsureGoWork(t *testing.T) {
	tests := []struct {
		name        string
		projectRoot string
		wantGoWork  string
		env         []string
	}{
		{
			name:        "appends when not present",
			env:         []string{"PATH=/usr/bin", "HOME=/home/test"},
			projectRoot: "/projects/myapp",
			wantGoWork:  "GOWORK=/projects/myapp/go.work",
		},
		{
			name:        "replaces existing",
			env:         []string{"PATH=/usr/bin", "GOWORK=/old/go.work", "HOME=/home/test"},
			projectRoot: "/projects/myapp",
			wantGoWork:  "GOWORK=/projects/myapp/go.work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensureGoWork(tt.env, tt.projectRoot)

			found := false
			for _, e := range result {
				if e == tt.wantGoWork {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %q in env, got %v", tt.wantGoWork, result)
			}

			// Verify no duplicate GOWORK entries.
			count := 0
			for _, e := range result {
				if len(e) > 7 && e[:7] == "GOWORK=" {
					count++
				}
			}
			if count != 1 {
				t.Errorf("expected exactly 1 GOWORK entry, got %d", count)
			}
		})
	}
}

func TestJoinArgs(t *testing.T) {
	tests := []struct {
		want string
		args []string
	}{
		{"build", []string{"build"}},
		{"build -race ./...", []string{"build", "-race", "./..."}},
		{"", []string{}},
	}

	for _, tt := range tests {
		got := joinArgs(tt.args)
		if got != tt.want {
			t.Errorf("joinArgs(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// desk extension codegen tests
// ---------------------------------------------------------------------------

func TestBuildExtensionContent_NoSources(t *testing.T) {
	content := buildExtensionContent(nil, "/project/desk")
	if !strings.Contains(content, "No app desk extensions discovered") {
		t.Error("expected empty comment for no sources")
	}
}

func TestBuildExtensionContent_ManifestAllTypes(t *testing.T) {
	sources := []extensionSource{
		{
			appName: "crm",
			manifest: &apps.DeskManifest{
				App:     "crm",
				Version: "1.0.0",
				Extensions: apps.DeskExtensions{
					FieldTypes: map[string]string{
						"Phone": "./fields/PhoneField.tsx",
					},
					Pages: []apps.DeskPageDef{
						{Path: "/desk/app/crm-dash", Component: "./pages/Dash.tsx", Label: "CRM Dash", Icon: "Phone"},
					},
					SidebarItems: []apps.DeskSidebarDef{
						{Label: "CRM", Icon: "Phone", Children: []apps.DeskSidebarChild{
							{Label: "Dashboard", Path: "/desk/app/crm-dash"},
						}},
					},
					DashboardWidgets: []apps.DeskWidgetDef{
						{Name: "pipeline", Component: "./widgets/Pipeline.tsx", Label: "Pipeline"},
					},
				},
			},
		},
	}

	content := buildExtensionContent(sources, "/project/desk")

	// Should have the @osama1998h/desk import with all 4 registration functions.
	if !strings.Contains(content, `import { registerFieldType, registerPage, registerSidebarItem, registerDashboardWidget } from "@osama1998h/desk"`) {
		t.Error("missing @osama1998h/desk import header")
	}

	// Field type registration.
	if !strings.Contains(content, `registerFieldType("Phone", CrmFieldPhoneField)`) {
		t.Error("missing field type registration")
	}

	// Page registration with options.
	if !strings.Contains(content, `registerPage("/desk/app/crm-dash", CrmPageDash, { label: "CRM Dash", icon: "Phone" })`) {
		t.Error("missing page registration")
	}

	// Sidebar item.
	if !strings.Contains(content, `registerSidebarItem({ label: "CRM"`) {
		t.Error("missing sidebar registration")
	}
	if !strings.Contains(content, `children: [{ label: "Dashboard", path: "/desk/app/crm-dash" }]`) {
		t.Error("missing sidebar children")
	}

	// Widget registration with label.
	if !strings.Contains(content, `registerDashboardWidget("pipeline", CrmWidgetPipeline, { label: "Pipeline" })`) {
		t.Error("missing widget registration")
	}
}

func TestBuildExtensionContent_LegacyFallback(t *testing.T) {
	sources := []extensionSource{
		{
			appName:      "legacy_app",
			legacyImport: "../apps/legacy_app/desk/setup",
		},
	}

	content := buildExtensionContent(sources, "/project/desk")

	if !strings.Contains(content, `import "../apps/legacy_app/desk/setup"`) {
		t.Error("missing legacy bare import")
	}
	// Should NOT have @osama1998h/desk import header for legacy-only.
	if strings.Contains(content, `from "@osama1998h/desk"`) {
		t.Error("unexpected @osama1998h/desk import for legacy-only sources")
	}
}

func TestBuildExtensionContent_ManifestWinsOverLegacy(t *testing.T) {
	// When a manifest source is present, it should generate structured calls,
	// not a bare import.
	sources := []extensionSource{
		{
			appName: "myapp",
			manifest: &apps.DeskManifest{
				App:     "myapp",
				Version: "1.0.0",
				Extensions: apps.DeskExtensions{
					FieldTypes: map[string]string{"Custom": "./fields/Custom.tsx"},
				},
			},
		},
	}

	content := buildExtensionContent(sources, "/project/desk")

	if !strings.Contains(content, "registerFieldType") {
		t.Error("expected structured registration, not bare import")
	}
}

func TestBuildExtensionContent_MultipleSources(t *testing.T) {
	sources := []extensionSource{
		{
			appName: "crm",
			manifest: &apps.DeskManifest{
				App:     "crm",
				Version: "1.0.0",
				Extensions: apps.DeskExtensions{
					FieldTypes: map[string]string{"Phone": "./fields/Phone.tsx"},
				},
			},
		},
		{
			appName:      "legacy",
			legacyImport: "../apps/legacy/desk/setup",
		},
	}

	content := buildExtensionContent(sources, "/project/desk")

	if !strings.Contains(content, "// === crm ===") {
		t.Error("missing crm section header")
	}
	if !strings.Contains(content, "// === legacy ===") {
		t.Error("missing legacy section header")
	}
}

func TestGenerateImportName(t *testing.T) {
	tests := []struct {
		appName, category, path, want string
	}{
		{"crm", "Field", "./fields/PhoneField.tsx", "CrmFieldPhoneField"},
		{"my_app", "Page", "./pages/Dashboard.tsx", "MyAppPageDashboard"},
		{"hr", "Widget", "./widgets/PipelineWidget.tsx", "HrWidgetPipelineWidget"},
	}

	for _, tt := range tests {
		got := generateImportName(tt.appName, tt.category, tt.path)
		if got != tt.want {
			t.Errorf("generateImportName(%q, %q, %q) = %q, want %q", tt.appName, tt.category, tt.path, got, tt.want)
		}
	}
}

func TestTitleCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"crm", "Crm"},
		{"my_app", "MyApp"},
		{"hr", "Hr"},
		{"phone_field", "PhoneField"},
	}

	for _, tt := range tests {
		got := titleCase(tt.input)
		if got != tt.want {
			t.Errorf("titleCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildOptionsLiteral(t *testing.T) {
	tests := []struct {
		label, icon, want string
	}{
		{"", "", ""},
		{"Hello", "", `{ label: "Hello" }`},
		{"", "Phone", `{ icon: "Phone" }`},
		{"Hello", "Phone", `{ label: "Hello", icon: "Phone" }`},
	}

	for _, tt := range tests {
		got := buildOptionsLiteral(tt.label, tt.icon)
		if got != tt.want {
			t.Errorf("buildOptionsLiteral(%q, %q) = %q, want %q", tt.label, tt.icon, got, tt.want)
		}
	}
}
