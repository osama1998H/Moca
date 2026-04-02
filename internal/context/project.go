// Package clicontext implements the CLI context detection and resolution
// pipeline. It resolves the current project, site, and environment from
// a 6-level priority chain: CLI flags → env vars → local state files →
// project config → directory auto-detection → sensible defaults.
//
// Design ref: MOCA_CLI_SYSTEM_DESIGN.md §6 (lines 3268–3293)
package clicontext

import (
	"os"
	"path/filepath"

	"github.com/osama1998H/moca/internal/config"
	"github.com/spf13/cobra"
)

const configFileName = "moca.yaml"

// resolveProject determines the project root directory and loads its
// configuration. Resolution priority:
//
//  1. --project flag
//  2. MOCA_PROJECT environment variable
//  3. Walk up from cwd looking for moca.yaml
//
// Returns ("", nil, nil) when no project is found — this is NOT an error.
// Commands that require a project check ctx.Project != nil themselves.
func resolveProject(cmd *cobra.Command) (string, *config.ProjectConfig, error) {
	// 1. --project flag (highest priority).
	if flag := cmd.Root().PersistentFlags().Lookup("project"); flag != nil && flag.Changed {
		return loadProjectFrom(flag.Value.String())
	}

	// 2. MOCA_PROJECT environment variable.
	if envDir := os.Getenv("MOCA_PROJECT"); envDir != "" {
		return loadProjectFrom(envDir)
	}

	// 3. Walk up directory tree from cwd.
	dir, err := os.Getwd()
	if err != nil {
		return "", nil, nil // can't determine cwd; treat as no project
	}
	return walkUpForProject(dir)
}

// loadProjectFrom loads a project configuration from the given directory.
// Returns an error if the directory contains a moca.yaml that is invalid.
func loadProjectFrom(dir string) (string, *config.ProjectConfig, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	cfgPath := filepath.Join(abs, configFileName)
	if _, statErr := os.Stat(cfgPath); os.IsNotExist(statErr) {
		return "", nil, nil
	}
	cfg, err := config.LoadAndResolve(cfgPath)
	if err != nil {
		return "", nil, err
	}
	return abs, cfg, nil
}

// walkUpForProject walks up the directory tree from start, looking for a
// moca.yaml file. Stops at the filesystem root.
func walkUpForProject(start string) (string, *config.ProjectConfig, error) {
	dir := start
	for {
		cfgPath := filepath.Join(dir, configFileName)
		if _, err := os.Stat(cfgPath); err == nil {
			cfg, err := config.LoadAndResolve(cfgPath)
			if err != nil {
				return "", nil, err
			}
			return dir, cfg, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			return "", nil, nil
		}
		dir = parent
	}
}
