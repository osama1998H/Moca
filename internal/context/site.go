package clicontext

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// resolveSite determines the target site name. Resolution priority:
//
//  1. --site flag
//  2. MOCA_SITE environment variable
//  3. .moca/current_site state file (within projectRoot)
//
// Returns "" when no site can be determined.
func resolveSite(cmd *cobra.Command, projectRoot string) string {
	// 1. --site flag (highest priority).
	if flag := cmd.Root().PersistentFlags().Lookup("site"); flag != nil && flag.Changed {
		return flag.Value.String()
	}

	// 2. MOCA_SITE environment variable.
	if env := os.Getenv("MOCA_SITE"); env != "" {
		return env
	}

	// 3. .moca/current_site state file.
	if projectRoot != "" {
		return readStateFile(filepath.Join(projectRoot, ".moca", "current_site"))
	}

	return ""
}

// readStateFile reads a single-line state file and returns its trimmed content.
// Returns "" if the file does not exist or cannot be read.
func readStateFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
