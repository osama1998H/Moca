package docgen

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestGenerateCLIReference(t *testing.T) {
	root := &cobra.Command{Use: "moca", Short: "Test CLI"}

	site := &cobra.Command{Use: "site", Short: "Site management commands"}
	site.AddCommand(&cobra.Command{
		Use:     "create <name>",
		Short:   "Create a new site",
		Aliases: []string{"new"},
	})
	site.AddCommand(&cobra.Command{Use: "list", Short: "List all sites"})
	root.AddCommand(site)

	got := GenerateCLIReference(root)

	checks := []struct {
		desc    string
		present string
	}{
		{"site group heading", "## site"},
		{"site create full path", "moca site create"},
		{"site list full path", "moca site list"},
		{"create description", "Create a new site"},
	}

	for _, tc := range checks {
		if !strings.Contains(got, tc.present) {
			t.Errorf("%s: expected %q in output\nFull output:\n%s", tc.desc, tc.present, got)
		}
	}
}
