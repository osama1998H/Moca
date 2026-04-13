package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/apps"
	"github.com/spf13/cobra"
)

func newAppPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish APP_NAME",
		Short: "Publish app to registry",
		Long: `Publish an app to GitHub Releases.

Validates the manifest, creates a tarball, and creates a GitHub Release
with the tarball as an asset.

Requires: gh CLI authenticated (gh auth login) or GITHUB_TOKEN env var.`,
		Args: cobra.ExactArgs(1),
		RunE: runAppPublish,
	}

	cmd.Flags().String("tag", "", "Release tag (default: v{manifest.version})")
	cmd.Flags().Bool("dry-run", false, "Validate without publishing")
	cmd.Flags().String("notes", "", "Release notes")

	return cmd
}

func runAppPublish(cmd *cobra.Command, args []string) error {
	appName := args[0]
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	tagOverride, _ := cmd.Flags().GetString("tag")
	notes, _ := cmd.Flags().GetString("notes")

	appDir := filepath.Join(cliCtx.ProjectRoot, "apps", appName)
	manifestPath := filepath.Join(appDir, "manifest.yaml")

	manifest, err := apps.ParseManifest(manifestPath)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Cannot read manifest for app %q", appName)).
			WithErr(err).
			WithFix(fmt.Sprintf("Ensure %s exists and is valid YAML.", manifestPath))
	}

	if manifest.Repository == "" {
		return output.NewCLIError("Manifest missing 'repository' field").
			WithFix("Add 'repository: github.com/org/repo' to manifest.yaml.")
	}
	if manifest.Version == "" {
		return output.NewCLIError("Manifest missing 'version' field").
			WithFix("Add 'version: 1.0.0' to manifest.yaml.")
	}

	tag := tagOverride
	if tag == "" {
		tag = "v" + manifest.Version
	}

	// Create tarball.
	tarballName := fmt.Sprintf("%s-%s.tar.gz", appName, manifest.Version)
	tarballPath := filepath.Join(os.TempDir(), tarballName)
	if err := createAppTarball(appDir, tarballPath, appName); err != nil {
		return output.NewCLIError("Failed to create tarball").WithErr(err)
	}
	defer func() { _ = os.Remove(tarballPath) }()

	info, _ := os.Stat(tarballPath)

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"app": appName, "version": manifest.Version,
			"repository": manifest.Repository, "tag": tag,
			"archive": tarballName, "size_bytes": info.Size(),
			"dry_run": dryRun,
		})
	}

	w.Print("App:         %s", appName)
	w.Print("Version:     %s", manifest.Version)
	w.Print("Repository:  %s", manifest.Repository)
	w.Print("Tag:         %s", tag)
	w.Print("Archive:     %s (%s)", tarballName, formatAppSize(info.Size()))

	if dryRun {
		w.Print("")
		w.PrintSuccess("Dry run complete. Run without --dry-run to publish.")
		return nil
	}

	if _, err := exec.LookPath("gh"); err != nil {
		return output.NewCLIError("GitHub CLI (gh) not found").
			WithFix("Install gh: https://cli.github.com/ and run 'gh auth login'.")
	}

	authCmd := exec.Command("gh", "auth", "status")
	if err := authCmd.Run(); err != nil {
		return output.NewCLIError("GitHub CLI not authenticated").
			WithFix("Run 'gh auth login' or set GITHUB_TOKEN.")
	}

	repo := manifest.Repository
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "github.com/")

	if notes == "" {
		notes = fmt.Sprintf("Release %s of %s", tag, appName)
	}

	ghCmd := exec.Command("gh", "release", "create", tag,
		"--repo", repo, "--title", fmt.Sprintf("%s %s", appName, tag),
		"--notes", notes, tarballPath)
	ghCmd.Stdout = os.Stdout
	ghCmd.Stderr = os.Stderr

	if err := ghCmd.Run(); err != nil {
		return output.NewCLIError("Failed to create GitHub release").
			WithErr(err).WithFix("Check repository access and gh auth status.")
	}

	w.PrintSuccess(fmt.Sprintf("Published %s %s to %s", appName, tag, manifest.Repository))
	return nil
}

func createAppTarball(srcDir, destPath, prefix string) error {
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	defer func() { _ = gw.Close() }()

	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	excludeDirs := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".moca": true, "dist": true, "build": true,
	}

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(srcDir, path)
		if relPath == "." {
			return nil
		}
		if info.IsDir() && excludeDirs[filepath.Base(path)] {
			return filepath.SkipDir
		}
		if strings.HasSuffix(filepath.Base(path), "_test.go") {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.Join(prefix, relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		_, err = io.Copy(tw, file)
		return err
	})
}

func formatAppSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
	)
	switch {
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
