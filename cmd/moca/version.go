package main

import (
	"runtime"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/output"
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

			w := output.NewWriter(cmd)

			if err := w.PrintJSON(info); err != nil {
				return err
			}
			if w.Mode() == output.ModeJSON {
				return nil
			}

			c := w.Color()
			w.Print("%s", c.Bold("MOCA CLI"))
			w.Print("  %s    %s", c.Info("Version:"), info.Version)
			w.Print("  %s     %s", c.Info("Commit:"), info.Commit)
			w.Print("  %s      %s", c.Info("Built:"), info.BuildDate)
			w.Print("  %s %s", c.Info("Go version:"), info.GoVersion)
			w.Print("  %s    %s/%s", c.Info("OS/Arch:"), info.OS, info.Arch)
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
