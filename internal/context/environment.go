package clicontext

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const defaultEnvironment = "development"

// resolveEnvironment determines the target environment. Resolution priority:
//
//  1. --env flag
//  2. MOCA_ENV environment variable
//  3. .moca/environment state file (within projectRoot)
//  4. "development" default
//
// Always returns a non-empty value.
func resolveEnvironment(cmd *cobra.Command, projectRoot string) string {
	// 1. --env flag (highest priority).
	if flag := cmd.Root().PersistentFlags().Lookup("env"); flag != nil && flag.Changed {
		return flag.Value.String()
	}

	// 2. MOCA_ENV environment variable.
	if env := os.Getenv("MOCA_ENV"); env != "" {
		return env
	}

	// 3. .moca/environment state file.
	if projectRoot != "" {
		if val := readStateFile(filepath.Join(projectRoot, ".moca", "environment")); val != "" {
			return val
		}
	}

	// 4. Default.
	return defaultEnvironment
}
