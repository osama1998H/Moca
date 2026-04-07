package apps

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// DeskManifest represents the contents of an app's desk/desk-manifest.json file.
// It declares desk UI extensions (custom field types, pages, sidebar items, widgets)
// that the build system uses to generate the .moca-extensions.ts import manifest.
type DeskManifest struct {
	App        string         `json:"app"`
	Version    string         `json:"version"`
	Extensions DeskExtensions `json:"extensions"`
}

// DeskExtensions groups all desk extension declarations.
type DeskExtensions struct {
	FieldTypes       map[string]string    `json:"field_types"`
	Pages            []DeskPageDef        `json:"pages"`
	SidebarItems     []DeskSidebarDef     `json:"sidebar_items"`
	DashboardWidgets []DeskWidgetDef      `json:"dashboard_widgets"`
}

// DeskPageDef declares a custom page route in the desk.
type DeskPageDef struct {
	Path      string `json:"path"`
	Component string `json:"component"`
	Label     string `json:"label,omitempty"`
	Icon      string `json:"icon,omitempty"`
}

// DeskSidebarDef declares a custom sidebar navigation entry.
type DeskSidebarDef struct {
	Label    string             `json:"label"`
	Icon     string             `json:"icon,omitempty"`
	Path     string             `json:"path,omitempty"`
	Children []DeskSidebarChild `json:"children,omitempty"`
	Order    int                `json:"order,omitempty"`
}

// DeskSidebarChild is a leaf item inside a sidebar group.
type DeskSidebarChild struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// DeskWidgetDef declares a custom dashboard widget component.
type DeskWidgetDef struct {
	Name      string `json:"name"`
	Component string `json:"component"`
	Label     string `json:"label,omitempty"`
}

// ParseDeskManifest reads and unmarshals a desk-manifest.json file at the given path.
// It does not validate the manifest contents; call ValidateDeskManifest separately.
func ParseDeskManifest(path string) (*DeskManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ManifestError{
			File:    path,
			Message: fmt.Sprintf("cannot read desk manifest: %v", err),
			Err:     err,
		}
	}

	var m DeskManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, &ManifestError{
			File:    path,
			Message: fmt.Sprintf("invalid JSON: %v", err),
			Err:     err,
		}
	}

	return &m, nil
}

// ValidateDeskManifest checks all required fields and constraints on a parsed desk manifest.
// Returns nil if valid, or a ValidationErrors value listing all problems found.
func ValidateDeskManifest(m *DeskManifest) error {
	v := &manifestValidator{}

	// app: required, valid identifier
	v.requireNonEmpty("app", m.App)
	if m.App != "" && !appNamePattern.MatchString(m.App) {
		v.addError("app", "must be a valid identifier (lowercase letters, digits, underscores; must start with a letter)")
	}

	// version: required, valid semver
	v.requireNonEmpty("version", m.Version)
	if m.Version != "" {
		if _, err := semver.NewVersion(m.Version); err != nil {
			v.addError("version", fmt.Sprintf("must be a valid semantic version: %v", err))
		}
	}

	// field_types: component paths must be relative, no duplicate names
	for name, path := range m.Extensions.FieldTypes {
		field := fmt.Sprintf("extensions.field_types[%s]", name)
		if !strings.HasPrefix(path, "./") {
			v.addError(field, "component path must be relative (start with \"./\")")
		}
	}

	// pages: required fields, valid paths, no duplicates
	pagePaths := make(map[string]bool)
	for i, page := range m.Extensions.Pages {
		field := fmt.Sprintf("extensions.pages[%d]", i)
		if page.Path == "" {
			v.addError(field+".path", "required")
		} else {
			if !strings.HasPrefix(page.Path, "/desk/app/") {
				v.addError(field+".path", "must start with \"/desk/app/\"")
			}
			if pagePaths[page.Path] {
				v.addError(field+".path", fmt.Sprintf("duplicate page path %q", page.Path))
			}
			pagePaths[page.Path] = true
		}
		if page.Component == "" {
			v.addError(field+".component", "required")
		} else if !strings.HasPrefix(page.Component, "./") {
			v.addError(field+".component", "component path must be relative (start with \"./\")")
		}
	}

	// sidebar_items: required label
	for i, item := range m.Extensions.SidebarItems {
		field := fmt.Sprintf("extensions.sidebar_items[%d]", i)
		if item.Label == "" {
			v.addError(field+".label", "required")
		}
		for j, child := range item.Children {
			childField := fmt.Sprintf("%s.children[%d]", field, j)
			if child.Label == "" {
				v.addError(childField+".label", "required")
			}
			if child.Path == "" {
				v.addError(childField+".path", "required")
			}
		}
	}

	// dashboard_widgets: required fields, no duplicate names
	widgetNames := make(map[string]bool)
	for i, widget := range m.Extensions.DashboardWidgets {
		field := fmt.Sprintf("extensions.dashboard_widgets[%d]", i)
		if widget.Name == "" {
			v.addError(field+".name", "required")
		} else if widgetNames[widget.Name] {
			v.addError(field+".name", fmt.Sprintf("duplicate widget name %q", widget.Name))
		} else {
			widgetNames[widget.Name] = true
		}
		if widget.Component == "" {
			v.addError(field+".component", "required")
		} else if !strings.HasPrefix(widget.Component, "./") {
			v.addError(field+".component", "component path must be relative (start with \"./\")")
		}
	}

	if len(v.errs) > 0 {
		return ValidationErrors(v.errs)
	}
	return nil
}
