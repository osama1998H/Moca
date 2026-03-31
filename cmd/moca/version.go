package main

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// NewVersionCommand returns the "moca version" command.
func NewVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print MOCA CLI version information",
		Long:  "Display the version, commit hash, build date, Go version, and OS/architecture of the MOCA CLI binary.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := VersionInfo{
				Version:   Version,
				Commit:    Commit,
				BuildDate: BuildDate,
				GoVersion: runtime.Version(),
				OS:        runtime.GOOS,
				Arch:      runtime.GOARCH,
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			}

			w := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(w, "MOCA CLI\n")
			_, _ = fmt.Fprintf(w, "  Version:    %s\n", info.Version)
			_, _ = fmt.Fprintf(w, "  Commit:     %s\n", info.Commit)
			_, _ = fmt.Fprintf(w, "  Built:      %s\n", info.BuildDate)
			_, _ = fmt.Fprintf(w, "  Go version: %s\n", info.GoVersion)
			_, _ = fmt.Fprintf(w, "  OS/Arch:    %s/%s\n", info.OS, info.Arch)
			return nil
		},
	}
}

// VersionInfo holds version metadata for JSON output.
type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}
