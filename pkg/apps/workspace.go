package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// GoModConflict represents a dependency version conflict detected between
// an incoming app module's go.mod and existing workspace modules.
// Minor conflicts (v1.8 vs v1.9) are ignored — Go's MVS resolves them.
// Major conflicts (v1.x vs v2.x) require operator review.
//
// Design ref: docs/blocker-resolution-strategies.md (Phase 2, lines 31-52).
type GoModConflict struct {
	Package    string // e.g., "github.com/stretchr/testify"
	NewVersion string // version required by the incoming app
	OldVersion string // version required by an existing workspace module
	App        string // module path of the conflicting existing app
	IsMajor    bool   // true if major versions differ
}

// ValidateAppDependencies checks whether an incoming app module's go.mod
// dependencies create major-version conflicts with existing workspace modules.
//
// Minor conflicts are ignored — MVS resolves them automatically by picking the
// highest required version. Major conflicts are returned as GoModConflict values
// and must be reviewed by the operator before adding the app to the workspace.
//
// Promoted from spikes/go-workspace/main.go (MS-00 Spike 3, ADR-003).
func ValidateAppDependencies(appMod *modfile.File, workspaceMods []*modfile.File) []GoModConflict {
	var conflicts []GoModConflict

	for _, newReq := range appMod.Require {
		if newReq.Indirect {
			continue
		}
		for _, existingMod := range workspaceMods {
			for _, existingReq := range existingMod.Require {
				if existingReq.Indirect {
					continue
				}
				if newReq.Mod.Path != existingReq.Mod.Path {
					continue
				}
				newMajor := majorVersion(newReq.Mod.Version)
				existingMajor := majorVersion(existingReq.Mod.Version)
				if newMajor != existingMajor {
					conflicts = append(conflicts, GoModConflict{
						Package:    newReq.Mod.Path,
						NewVersion: newReq.Mod.Version,
						OldVersion: existingReq.Mod.Version,
						App:        existingMod.Module.Mod.Path,
						IsMajor:    true,
					})
				}
			}
		}
	}

	return conflicts
}

// LoadWorkspaceMods reads and parses go.mod files from all app directories
// under appsDir. Apps without a go.mod are silently skipped.
func LoadWorkspaceMods(appsDir string) ([]*modfile.File, error) {
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, fmt.Errorf("read apps directory: %w", err)
	}

	var mods []*modfile.File
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		goModPath := filepath.Join(appsDir, entry.Name(), "go.mod")
		data, err := os.ReadFile(goModPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", goModPath, err)
		}

		f, err := modfile.Parse(goModPath, data, nil)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", goModPath, err)
		}
		mods = append(mods, f)
	}

	return mods, nil
}

// ParseGoMod reads and parses a single go.mod file.
func ParseGoMod(path string) (*modfile.File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return f, nil
}

// majorVersion extracts the major version prefix from a semver string.
// Examples: "v1.9.0" -> "v1", "v2.3.0" -> "v2", "v0.22.0" -> "v0".
func majorVersion(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) == 0 {
		return v
	}
	return parts[0]
}
