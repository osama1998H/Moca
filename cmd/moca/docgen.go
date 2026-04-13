package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/osama1998H/moca/internal/docgen"
	"github.com/spf13/cobra"
)

func NewDocgenCommand() *cobra.Command {
	var wikiDir string

	cmd := &cobra.Command{
		Use:    "docgen",
		Short:  "Generate reference documentation",
		Hidden: true,
	}

	cmd.PersistentFlags().StringVar(&wikiDir, "wiki-dir", "wiki", "Path to wiki directory")

	cmd.AddCommand(
		newDocgenCLICommand(&wikiDir),
		newDocgenAPICommand(&wikiDir),
		newDocgenAllCommand(&wikiDir),
	)

	return cmd
}

func newDocgenCLICommand(wikiDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "cli",
		Short: "Generate CLI reference into wiki",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			md := docgen.GenerateCLIReference(root)
			path := filepath.Join(*wikiDir, "Reference-CLI-Commands.md")
			if err := docgen.InjectSection(path,
				"<!-- AUTO-GENERATED:START -->",
				"<!-- AUTO-GENERATED:END -->",
				md); err != nil {
				return fmt.Errorf("inject CLI reference: %w", err)
			}
			fmt.Fprintf(os.Stderr, "CLI reference injected into %s\n", path)
			return nil
		},
	}
}

func newDocgenAPICommand(wikiDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "api",
		Short: "Generate API reference into wiki",
		RunE: func(cmd *cobra.Command, args []string) error {
			md := docgen.GenerateAPIReference()
			path := filepath.Join(*wikiDir, "Reference-REST-API.md")
			if err := docgen.InjectSection(path,
				"<!-- AUTO-GENERATED:START -->",
				"<!-- AUTO-GENERATED:END -->",
				md); err != nil {
				return fmt.Errorf("inject API reference: %w", err)
			}
			fmt.Fprintf(os.Stderr, "API reference injected into %s\n", path)
			return nil
		},
	}
}

func newDocgenAllCommand(wikiDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "all",
		Short: "Generate all reference docs into wiki",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			cliMD := docgen.GenerateCLIReference(root)
			cliPath := filepath.Join(*wikiDir, "Reference-CLI-Commands.md")
			if err := docgen.InjectSection(cliPath,
				"<!-- AUTO-GENERATED:START -->",
				"<!-- AUTO-GENERATED:END -->",
				cliMD); err != nil {
				return fmt.Errorf("inject CLI reference: %w", err)
			}
			fmt.Fprintf(os.Stderr, "CLI reference injected into %s\n", cliPath)

			apiMD := docgen.GenerateAPIReference()
			apiPath := filepath.Join(*wikiDir, "Reference-REST-API.md")
			if err := docgen.InjectSection(apiPath,
				"<!-- AUTO-GENERATED:START -->",
				"<!-- AUTO-GENERATED:END -->",
				apiMD); err != nil {
				return fmt.Errorf("inject API reference: %w", err)
			}
			fmt.Fprintf(os.Stderr, "API reference injected into %s\n", apiPath)

			return nil
		},
	}
}
