package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Masterminds/semver/v3"
)

// AppInfo holds a discovered app's name, directory path, and parsed manifest.
type AppInfo struct {
	Manifest *AppManifest
	Name     string
	Path     string // absolute path to app directory
}

// ScanApps walks appsDir looking for subdirectories containing a manifest.yaml.
// Directories without a manifest are silently skipped. Returns an error if any
// discovered manifest fails to parse or validate.
func ScanApps(appsDir string) ([]AppInfo, error) {
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read apps directory %s: %w", appsDir, err)
	}

	var apps []AppInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appDir := filepath.Join(appsDir, entry.Name())
		manifestPath := filepath.Join(appDir, "manifest.yaml")

		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			continue // no manifest — skip silently
		}

		info, err := LoadApp(appDir)
		if err != nil {
			return nil, err
		}
		apps = append(apps, *info)
	}

	// Sort by name for deterministic ordering.
	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Name < apps[j].Name
	})

	return apps, nil
}

// LoadApp loads and validates a single app from the given directory.
// It expects a manifest.yaml file in appDir.
func LoadApp(appDir string) (*AppInfo, error) {
	absDir, err := filepath.Abs(appDir)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve path %s: %w", appDir, err)
	}

	manifestPath := filepath.Join(absDir, "manifest.yaml")
	m, err := ParseManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	if err := ValidateManifest(m); err != nil {
		return nil, &ManifestError{
			File:    manifestPath,
			Message: err.Error(),
			Err:     err,
		}
	}

	return &AppInfo{
		Name:     m.Name,
		Path:     absDir,
		Manifest: m,
	}, nil
}

// ValidateDependencies checks inter-app dependencies for the given set of apps.
// It verifies that:
//   - Every dependency references an app present in the set.
//   - Version constraints are satisfied.
//   - The dependency graph is acyclic (no circular dependencies).
func ValidateDependencies(apps []AppInfo) error {
	byName := make(map[string]*AppInfo, len(apps))
	for i := range apps {
		byName[apps[i].Name] = &apps[i]
	}

	// Check all declared dependencies exist and satisfy version constraints.
	for _, app := range apps {
		for _, dep := range app.Manifest.Dependencies {
			target, ok := byName[dep.App]
			if !ok {
				return &DependencyError{
					Message: fmt.Sprintf("app %q depends on %q, which is not installed", app.Name, dep.App),
				}
			}

			if dep.MinVersion != "" {
				constraint, err := semver.NewConstraint(dep.MinVersion)
				if err != nil {
					return &DependencyError{
						Message: fmt.Sprintf("app %q has invalid version constraint %q for dependency %q: %v",
							app.Name, dep.MinVersion, dep.App, err),
					}
				}

				ver, err := semver.NewVersion(target.Manifest.Version)
				if err != nil {
					return &DependencyError{
						Message: fmt.Sprintf("app %q has invalid version %q: %v",
							dep.App, target.Manifest.Version, err),
					}
				}

				if !constraint.Check(ver) {
					return &DependencyError{
						Message: fmt.Sprintf("app %q requires %q %s, but version %s is installed",
							app.Name, dep.App, dep.MinVersion, target.Manifest.Version),
					}
				}
			}
		}
	}

	// Detect cycles using Kahn's algorithm.
	if err := detectCycles(apps); err != nil {
		return err
	}

	return nil
}

// detectCycles uses Kahn's algorithm to detect circular dependencies in the app
// dependency graph. Returns a *DependencyError with the cycle path if found.
func detectCycles(apps []AppInfo) error {
	// Build adjacency list and in-degree counts.
	inDegree := make(map[string]int, len(apps))
	dependents := make(map[string][]string, len(apps)) // app -> apps that depend on it

	for _, app := range apps {
		if _, ok := inDegree[app.Name]; !ok {
			inDegree[app.Name] = 0
		}
		for _, dep := range app.Manifest.Dependencies {
			dependents[dep.App] = append(dependents[dep.App], app.Name)
			inDegree[app.Name]++
		}
	}

	// Seed queue with zero-in-degree nodes.
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue) // deterministic order

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++

		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if visited < len(apps) {
		// Cycle detected — find the cycle members for error reporting.
		var cycle []string
		for name, deg := range inDegree {
			if deg > 0 {
				cycle = append(cycle, name)
			}
		}
		sort.Strings(cycle)
		// Append the first element to close the cycle visually.
		cycle = append(cycle, cycle[0])
		return &DependencyError{
			Message: "circular app dependency detected",
			Cycle:   cycle,
		}
	}

	return nil
}
