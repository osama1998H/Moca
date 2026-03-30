// Package main implements a spike validating Go workspace multi-module
// composition with intentional dependency version conflicts.
//
// Spike: MS-00-T4 (Spike 3)
// Design ref: docs/blocker-resolution-strategies.md (Blocker 1, lines 7-63)
//             MOCA_SYSTEM_DESIGN.md lines 1380-1384 (Build Composition Model)
//
// Key architectural bet being validated:
//
//	Multiple app modules with conflicting dependency versions compile into
//	a single binary via go.work. Go's Minimal Version Selection (MVS)
//	resolves minor version conflicts by picking the highest required version.
//	Major version "conflicts" are not conflicts in Go -- pkg and pkg/v2 are
//	distinct module paths that coexist independently.
//
// This is throwaway spike code. Do not promote to pkg/.
package main

import (
	"fmt"
	"strings"

	"golang.org/x/mod/modfile"
)

func main() {
	fmt.Println("MS-00-T4 (Spike 3): Go Workspace Multi-Module Composition Spike")
	fmt.Println("Run: go test -v -count=1 -race ./...")
	fmt.Println("Or:  make spike-gowork  (from repo root)")
}

// Conflict represents a dependency version conflict detected between app modules.
// A minor conflict (IsMajor=false) is resolved automatically by MVS.
// A major conflict (IsMajor=true) means the apps require incompatible API versions.
type Conflict struct {
	Package    string // e.g., "github.com/stretchr/testify"
	NewVersion string // version required by the incoming app module
	OldVersion string // version required by an existing workspace module
	App        string // module path of the existing app that has the conflict
	IsMajor    bool   // true if major versions differ (v1 vs v2)
}

// ValidateAppDependencies checks whether an incoming app module's dependencies
// create major-version conflicts with any existing workspace modules.
//
// Minor conflicts (v1.8 vs v1.9) are ignored — MVS resolves them automatically.
// Major conflicts (v1.x vs v2.x) are returned as Conflicts and must be reviewed
// by the operator before adding the app to the workspace.
//
// This implements the pre-install validation described in
// docs/blocker-resolution-strategies.md (Phase 2, lines 31-52).
func ValidateAppDependencies(appMod *modfile.File, workspaceMods []*modfile.File) []Conflict {
	var conflicts []Conflict

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
					conflicts = append(conflicts, Conflict{
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

// majorVersion extracts the major version prefix from a semver string.
// Examples: "v1.9.0" -> "v1", "v2.3.0" -> "v2", "v0.22.0" -> "v0".
func majorVersion(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) == 0 {
		return v
	}
	return parts[0]
}
