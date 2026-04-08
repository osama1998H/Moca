package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

const (
	rootModulePath   = "github.com/osama1998H/moca"
	coreModulePath   = "github.com/osama1998H/moca/apps/core"
	rootReplacePath  = "./apps/core"
	coreReplacePath  = "../.."
	defaultRootGoMod = "go.mod"
	defaultCoreGoMod = "apps/core/go.mod"
)

type moduleState struct {
	Requires   map[string]string
	Replaces   map[string]string
	ModulePath string
}

func main() {
	var version string
	var rootGoMod string
	var coreGoMod string

	flag.StringVar(&version, "version", "", "release version tag (for example v0.1.1-alpha.7)")
	flag.StringVar(&rootGoMod, "root-go-mod", defaultRootGoMod, "path to the root go.mod")
	flag.StringVar(&coreGoMod, "core-go-mod", defaultCoreGoMod, "path to the apps/core go.mod")
	flag.Parse()

	if err := run(version, rootGoMod, coreGoMod); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "release verification failed: %v\n", err)
		os.Exit(1)
	}

	_, _ = fmt.Fprintf(os.Stdout, "release verification passed for %s\n", version)
}

func run(version, rootGoMod, coreGoMod string) error {
	releaseVersion, err := normalizeReleaseVersion(version)
	if err != nil {
		return err
	}

	rootState, err := loadModuleState(rootGoMod)
	if err != nil {
		return fmt.Errorf("load root module state: %w", err)
	}

	coreState, err := loadModuleState(coreGoMod)
	if err != nil {
		return fmt.Errorf("load apps/core module state: %w", err)
	}

	if err := verifyModuleState("root module", rootState, rootModulePath, coreModulePath, releaseVersion, rootReplacePath); err != nil {
		return err
	}

	if err := verifyModuleState("apps/core module", coreState, coreModulePath, rootModulePath, releaseVersion, coreReplacePath); err != nil {
		return err
	}

	return nil
}

func normalizeReleaseVersion(version string) (string, error) {
	if version == "" {
		return "", fmt.Errorf("version is required")
	}

	version = strings.TrimPrefix(version, "refs/tags/")
	if strings.HasPrefix(version, "apps/core/") {
		return "", fmt.Errorf("version must be a root release tag like vX.Y.Z, got %q", version)
	}
	if !strings.HasPrefix(version, "v") || !semver.IsValid(version) {
		return "", fmt.Errorf("version must be a valid semantic version tag like vX.Y.Z, got %q", version)
	}

	return version, nil
}

func loadModuleState(goModPath string) (*moduleState, error) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", goModPath, err)
	}

	file, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", goModPath, err)
	}
	if file.Module == nil {
		return nil, fmt.Errorf("parse %s: module path not found", goModPath)
	}

	state := &moduleState{
		ModulePath: file.Module.Mod.Path,
		Requires:   make(map[string]string),
		Replaces:   make(map[string]string),
	}

	for _, req := range file.Require {
		state.Requires[req.Mod.Path] = req.Mod.Version
	}

	for _, rep := range file.Replace {
		replacement := normalizeReplaceTarget(rep.New.Path)
		if rep.New.Version != "" {
			replacement = replacement + "@" + rep.New.Version
		}
		state.Replaces[rep.Old.Path] = replacement
	}

	return state, nil
}

func normalizeReplaceTarget(target string) string {
	return path.Clean(strings.ReplaceAll(target, "\\", "/"))
}

func verifyModuleState(label string, state *moduleState, expectedModule, requiredModule, expectedVersion, expectedReplace string) error {
	if state.ModulePath != expectedModule {
		return fmt.Errorf("%s must declare module %q, got %q", label, expectedModule, state.ModulePath)
	}

	requiredVersion, ok := state.Requires[requiredModule]
	if !ok {
		return fmt.Errorf("%s must require %q", label, requiredModule)
	}
	if requiredVersion != expectedVersion {
		return fmt.Errorf("%s must require %q at %q, got %q", label, requiredModule, expectedVersion, requiredVersion)
	}

	replaceTarget, ok := state.Replaces[requiredModule]
	if !ok {
		return fmt.Errorf("%s must replace %q with local path %q", label, requiredModule, expectedReplace)
	}
	normalizedExpectedReplace := normalizeReplaceTarget(expectedReplace)
	if replaceTarget != normalizedExpectedReplace {
		return fmt.Errorf("%s must replace %q with %q, got %q", label, requiredModule, expectedReplace, replaceTarget)
	}

	return nil
}
