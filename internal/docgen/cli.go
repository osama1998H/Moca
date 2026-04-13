package docgen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GenerateCLIReference walks the root command's top-level subcommands and
// produces a Markdown reference document. Each top-level group becomes a ##
// section with a table of its subcommands. Commands with non-inherited flags
// get an additional ### flags subsection.
func GenerateCLIReference(root *cobra.Command) string {
	var b strings.Builder

	// Collect and sort top-level groups alphabetically.
	groups := root.Commands()
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name() < groups[j].Name()
	})

	for _, group := range groups {
		if group.Hidden {
			continue
		}

		// Section heading.
		fmt.Fprintf(&b, "## %s\n\n", group.Name())
		if group.Short != "" {
			fmt.Fprintf(&b, "> %s\n\n", group.Short)
		}

		// Table of subcommands.
		subs := group.Commands()
		if len(subs) > 0 {
			sort.Slice(subs, func(i, j int) bool {
				return subs[i].Name() < subs[j].Name()
			})

			b.WriteString("| Command | Description |\n")
			b.WriteString("|---------|-------------|\n")

			for _, sub := range subs {
				if sub.Hidden {
					continue
				}
				fullPath := fmt.Sprintf("`%s`", sub.CommandPath())
				desc := sub.Short
				if len(sub.Aliases) > 0 {
					desc = fmt.Sprintf("%s (aliases: %s)", desc, strings.Join(sub.Aliases, ", "))
				}
				fmt.Fprintf(&b, "| %s | %s |\n", fullPath, desc)
			}
			b.WriteByte('\n')

			// Flag tables for subcommands that have non-inherited flags.
			for _, sub := range subs {
				if sub.Hidden {
					continue
				}
				flags := sub.NonInheritedFlags()
				if flags == nil {
					continue
				}
				var flagRows []string
				flags.VisitAll(func(f *pflag.Flag) {
					row := fmt.Sprintf("| `--%s` | %s | `%s` | %s |", f.Name, f.Value.Type(), f.DefValue, f.Usage)
					flagRows = append(flagRows, row)
				})
				if len(flagRows) == 0 {
					continue
				}
				fmt.Fprintf(&b, "### `%s` flags\n\n", sub.CommandPath())
				b.WriteString("| Flag | Type | Default | Description |\n")
				b.WriteString("|------|------|---------|-------------|\n")
				for _, row := range flagRows {
					b.WriteString(row)
					b.WriteByte('\n')
				}
				b.WriteByte('\n')
			}
		} else {
			// Group is itself a leaf command — show its own flags if any.
			flags := group.NonInheritedFlags()
			if flags != nil {
				var flagRows []string
				flags.VisitAll(func(f *pflag.Flag) {
					row := fmt.Sprintf("| `--%s` | %s | `%s` | %s |", f.Name, f.Value.Type(), f.DefValue, f.Usage)
					flagRows = append(flagRows, row)
				})
				if len(flagRows) > 0 {
					fmt.Fprintf(&b, "### `%s` flags\n\n", group.CommandPath())
					b.WriteString("| Flag | Type | Default | Description |\n")
					b.WriteString("|------|------|---------|-------------|\n")
					for _, row := range flagRows {
						b.WriteString(row)
						b.WriteByte('\n')
					}
					b.WriteByte('\n')
				}
			}
		}
	}

	return b.String()
}
