package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

const (
	rootModulePath       = "github.com/osama1998H/moca"
	legacyCoreModulePath = "github.com/osama1998H/moca/apps/core"
	legacyCoreWorkUse    = "./apps/core"
	defaultRootGoMod     = "go.mod"
	defaultGoWork        = "go.work"
	defaultBuiltinCore   = "pkg/builtin/core"
	defaultLegacyCoreMod = "apps/core/go.mod"
)

type moduleState struct {
	Requires   map[string]string
	Replaces   map[string]string
	ModulePath string
}

func main() {
	var rootGoMod string
	var goWorkPath string
	var builtinCorePath string
	var legacyCoreGoMod string

	flag.StringVar(&rootGoMod, "root-go-mod", defaultRootGoMod, "path to the root go.mod")
	flag.StringVar(&goWorkPath, "go-work", defaultGoWork, "path to the workspace go.work")
	flag.StringVar(&builtinCorePath, "builtin-core", defaultBuiltinCore, "path to the builtin core package directory")
	flag.StringVar(&legacyCoreGoMod, "legacy-core-go-mod", defaultLegacyCoreMod, "path where the legacy apps/core go.mod must not exist")
	flag.Parse()

	if err := run(rootGoMod, goWorkPath, builtinCorePath, legacyCoreGoMod); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "release verification failed: %v\n", err)
		os.Exit(1)
	}

	_, _ = fmt.Fprintln(os.Stdout, "release verification passed")
}

func run(rootGoMod, goWorkPath, builtinCorePath, legacyCoreGoMod string) error {
	rootState, err := loadModuleState(rootGoMod)
	if err != nil {
		return fmt.Errorf("load root module state: %w", err)
	}
	if rootState.ModulePath != rootModulePath {
		return fmt.Errorf("root module must declare %q, got %q", rootModulePath, rootState.ModulePath)
	}
	if version, ok := rootState.Requires[legacyCoreModulePath]; ok {
		return fmt.Errorf("root module must not require legacy core module %q (found %q)", legacyCoreModulePath, version)
	}
	if replacement, ok := rootState.Replaces[legacyCoreModulePath]; ok {
		return fmt.Errorf("root module must not replace legacy core module %q (found %q)", legacyCoreModulePath, replacement)
	}

	if _, err := os.Stat(legacyCoreGoMod); err == nil {
		return fmt.Errorf("legacy core go.mod must not exist at %q", legacyCoreGoMod)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat legacy core go.mod %q: %w", legacyCoreGoMod, err)
	}

	if err := verifyBuiltinCoreDir(builtinCorePath); err != nil {
		return err
	}

	if err := verifyGoWork(goWorkPath); err != nil {
		return err
	}

	return nil
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
		state.Replaces[rep.Old.Path] = rep.New.Path
	}

	return state, nil
}

func verifyBuiltinCoreDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("builtin core package %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("builtin core package %q must be a directory", path)
	}

	manifestPath := filepath.Join(path, "manifest.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("builtin core manifest %q: %w", manifestPath, err)
	}

	legacyNestedGoMod := filepath.Join(path, "go.mod")
	if _, err := os.Stat(legacyNestedGoMod); err == nil {
		return fmt.Errorf("builtin core package must not contain nested go.mod at %q", legacyNestedGoMod)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat builtin core nested go.mod %q: %w", legacyNestedGoMod, err)
	}

	return nil
}

func verifyGoWork(goWorkPath string) error {
	data, err := os.ReadFile(goWorkPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", goWorkPath, err)
	}
	if strings.Contains(string(data), legacyCoreWorkUse) {
		return fmt.Errorf("go.work must not reference legacy core workspace entry %q", legacyCoreWorkUse)
	}
	return nil
}
