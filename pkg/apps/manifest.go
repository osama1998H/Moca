// Package apps provides manifest parsing, validation, and directory scanning
// for installable Moca apps. Installable apps live under apps/, carry their own
// go.mod, and declare metadata in a manifest.yaml file. Builtin framework apps
// such as pkg/builtin/core are part of the root module and are not discovered
// through apps/ scanning.
package apps

import (
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

// appNamePattern matches a valid app name: lowercase letter followed by lowercase
// letters, digits, or underscores.
var appNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// AppManifest describes an installed Moca app's identity, dependencies, and contents.
type AppManifest struct {
	// Identity
	Name        string `yaml:"name"`
	Title       string `yaml:"title"`
	Version     string `yaml:"version"`
	Publisher   string `yaml:"publisher"`
	License     string `yaml:"license"`
	Description string `yaml:"description"`

	// Dependencies
	MocaVersion  string   `yaml:"moca_version"`
	Dependencies []AppDep `yaml:"dependencies"`

	// Contents
	Modules []ModuleDef `yaml:"modules"`

	// Forward-compatible: declared but not validated in MS-08.
	Fixtures     []FixtureDef  `yaml:"fixtures,omitempty"`
	Migrations   []Migration   `yaml:"migrations,omitempty"`
	StaticAssets []AssetBundle `yaml:"static_assets,omitempty"`
	PortalPages  []PortalPage  `yaml:"portal_pages,omitempty"`

	// Publishing (used by moca app publish)
	Repository string   `yaml:"repository,omitempty"`
	Author     string   `yaml:"author,omitempty"`
	Keywords   []string `yaml:"keywords,omitempty"`
}

// AppDep declares a dependency on another app with a minimum version constraint.
type AppDep struct {
	App        string `yaml:"app"`
	MinVersion string `yaml:"min_version"`
}

// ModuleDef declares a module within an app and the doctypes it contains.
type ModuleDef struct {
	Name     string   `yaml:"name"`
	Label    string   `yaml:"label"`
	Icon     string   `yaml:"icon,omitempty"`
	Color    string   `yaml:"color,omitempty"`
	Category string   `yaml:"category,omitempty"`
	DocTypes []string `yaml:"doctypes"`
}

// Migration describes a database migration step.
type Migration struct {
	Version   string   `yaml:"version"`
	Up        string   `yaml:"up"`
	Down      string   `yaml:"down"`
	DependsOn []string `yaml:"depends_on,omitempty"`
}

// FixtureDef describes seed data to be loaded during app installation.
// Fields will be defined in MS-09.
type FixtureDef struct {
	Name string `yaml:"name,omitempty"`
	Path string `yaml:"path,omitempty"`
}

// AssetBundle describes static assets (JS, CSS) bundled with an app.
// Fields will be defined in a later milestone.
type AssetBundle struct {
	Name string `yaml:"name,omitempty"`
	Path string `yaml:"path,omitempty"`
}

// PortalPage describes a server-side rendered portal page.
// Fields will be defined in a later milestone.
type PortalPage struct {
	Name string `yaml:"name,omitempty"`
	Path string `yaml:"path,omitempty"`
}

// ParseManifest reads and unmarshals a manifest.yaml file at the given path.
// It does not validate the manifest contents; call ValidateManifest separately.
func ParseManifest(path string) (*AppManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &ManifestError{
			File:    path,
			Message: fmt.Sprintf("cannot open manifest: %v", err),
			Err:     err,
		}
	}
	defer f.Close() //nolint:errcheck

	m, err := parseManifest(f)
	if err != nil {
		if me, ok := err.(*ManifestError); ok {
			me.File = path
			return nil, me
		}
		return nil, &ManifestError{
			File:    path,
			Message: err.Error(),
			Err:     err,
		}
	}
	return m, nil
}

// parseManifest is the shared implementation that reads from an io.Reader.
func parseManifest(r io.Reader) (*AppManifest, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, &ManifestError{
			Message: fmt.Sprintf("cannot read manifest: %v", err),
			Err:     err,
		}
	}

	var m AppManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, &ManifestError{
			Message: fmt.Sprintf("invalid YAML: %v", err),
			Err:     err,
		}
	}

	return &m, nil
}

// ValidateManifest checks all required fields and constraints on a parsed manifest.
// Returns nil if valid, or a ValidationErrors value listing all problems found.
// All fields are checked even after the first failure (accumulation pattern).
func ValidateManifest(m *AppManifest) error {
	v := &manifestValidator{}

	// name: required, valid identifier
	v.requireNonEmpty("name", m.Name)
	if m.Name != "" && !appNamePattern.MatchString(m.Name) {
		v.addError("name", `must be a valid identifier (lowercase letters, digits, underscores; must start with a letter)`)
	}

	// version: required, valid semver
	v.requireNonEmpty("version", m.Version)
	if m.Version != "" {
		if _, err := semver.NewVersion(m.Version); err != nil {
			v.addError("version", fmt.Sprintf("must be a valid semantic version: %v", err))
		}
	}

	// moca_version: required, valid semver constraint
	v.requireNonEmpty("moca_version", m.MocaVersion)
	if m.MocaVersion != "" {
		if _, err := semver.NewConstraint(m.MocaVersion); err != nil {
			v.addError("moca_version", fmt.Sprintf("must be a valid semver constraint: %v", err))
		}
	}

	// dependencies: each must have app name and valid min_version constraint
	for i, dep := range m.Dependencies {
		field := fmt.Sprintf("dependencies[%d]", i)
		if dep.App == "" {
			v.addError(field+".app", "required")
		}
		if dep.MinVersion != "" {
			if _, err := semver.NewConstraint(dep.MinVersion); err != nil {
				v.addError(field+".min_version", fmt.Sprintf("must be a valid semver constraint: %v", err))
			}
		}
	}

	// modules: no duplicate names, no duplicate doctypes across modules
	moduleNames := make(map[string]bool)
	doctypeNames := make(map[string]string) // doctype -> module that declared it
	for i, mod := range m.Modules {
		field := fmt.Sprintf("modules[%d]", i)
		if mod.Name == "" {
			v.addError(field+".name", "required")
			continue
		}
		if moduleNames[mod.Name] {
			v.addError(field+".name", fmt.Sprintf("duplicate module name %q", mod.Name))
		}
		moduleNames[mod.Name] = true

		for j, dt := range mod.DocTypes {
			if dt == "" {
				v.addError(fmt.Sprintf("%s.doctypes[%d]", field, j), "required")
				continue
			}
			if prevMod, exists := doctypeNames[dt]; exists {
				v.addError(fmt.Sprintf("%s.doctypes[%d]", field, j),
					fmt.Sprintf("duplicate doctype %q (already declared in module %q)", dt, prevMod))
			}
			doctypeNames[dt] = mod.Name
		}
	}

	if len(v.errs) > 0 {
		return ValidationErrors(v.errs)
	}
	return nil
}

// manifestValidator accumulates validation errors.
type manifestValidator struct {
	errs []ValidationError
}

func (v *manifestValidator) addError(field, message string) {
	v.errs = append(v.errs, ValidationError{Field: field, Message: message})
}

func (v *manifestValidator) requireNonEmpty(field, value string) {
	if value == "" {
		v.addError(field, "required")
	}
}
