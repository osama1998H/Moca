package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/osama1998H/moca/internal/output"
	"github.com/spf13/cobra"
)

// NewDeskCommand returns the "moca desk" command group with subcommands for
// managing the React Desk frontend: install, update, dev.
func NewDeskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "desk",
		Short: "Desk frontend management",
		Long:  "Install, update, and develop the Moca Desk React frontend.",
	}

	cmd.AddCommand(
		newDeskInstallCmd(),
		newDeskUpdateCmd(),
		newDeskDevCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// desk install
// ---------------------------------------------------------------------------

func newDeskInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install desk npm dependencies",
		Long: `Install npm dependencies for the desk/ frontend project.
Runs 'npm install' in the desk/ directory.`,
		RunE: runDeskInstall,
	}

	f := cmd.Flags()
	f.Bool("verbose", false, "Show npm output")

	return cmd
}

func runDeskInstall(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}
	projectRoot := ctx.ProjectRoot
	verbose, _ := cmd.Flags().GetBool("verbose")

	deskDir := filepath.Join(projectRoot, "desk")
	if err := requireDeskDir(deskDir); err != nil {
		return err
	}

	npmCmd := exec.Command("npm", "install")
	npmCmd.Dir = deskDir

	if verbose {
		npmCmd.Stdout = os.Stdout
		npmCmd.Stderr = os.Stderr
	}

	s := w.NewSpinner("Installing desk dependencies...")
	if !verbose {
		s.Start()
	}

	out, runErr := runCmdCaptureStderr(npmCmd, verbose)

	if !verbose {
		if runErr != nil {
			s.Stop("Failed")
		} else {
			s.Stop("OK")
		}
	}

	if runErr != nil {
		if out != "" {
			w.Print(out)
		}
		return output.NewCLIError("npm install failed").
			WithErr(runErr).
			WithFix("Check that Node.js and npm are installed. Run 'node --version' to verify.")
	}

	// Report node_modules size.
	nmDir := filepath.Join(deskDir, "node_modules")
	if info, statErr := os.Stat(nmDir); statErr == nil && info.IsDir() {
		var totalSize int64
		_ = filepath.Walk(nmDir, func(_ string, fi os.FileInfo, _ error) error {
			if fi != nil && !fi.IsDir() {
				totalSize += fi.Size()
			}
			return nil
		})
		w.PrintSuccess(fmt.Sprintf("Desk dependencies installed (%s)", formatBytes(totalSize)))
	} else {
		w.PrintSuccess("Desk dependencies installed")
	}

	return nil
}

// ---------------------------------------------------------------------------
// desk update
// ---------------------------------------------------------------------------

func newDeskUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update @moca/desk to latest compatible version",
		Long: `Update the @moca/desk package to the latest compatible version
and regenerate desk extension imports.`,
		RunE: runDeskUpdate,
	}

	f := cmd.Flags()
	f.Bool("verbose", false, "Show npm output")

	return cmd
}

func runDeskUpdate(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}
	projectRoot := ctx.ProjectRoot
	verbose, _ := cmd.Flags().GetBool("verbose")

	deskDir := filepath.Join(projectRoot, "desk")
	if err := requireDeskDir(deskDir); err != nil {
		return err
	}

	// Update @moca/desk package.
	npmCmd := exec.Command("npm", "update", "@moca/desk")
	npmCmd.Dir = deskDir

	if verbose {
		npmCmd.Stdout = os.Stdout
		npmCmd.Stderr = os.Stderr
	}

	s := w.NewSpinner("Updating @moca/desk...")
	if !verbose {
		s.Start()
	}

	out, runErr := runCmdCaptureStderr(npmCmd, verbose)

	if !verbose {
		if runErr != nil {
			s.Stop("Failed")
		} else {
			s.Stop("OK")
		}
	}

	if runErr != nil {
		if out != "" {
			w.Print(out)
		}
		return output.NewCLIError("npm update failed").
			WithErr(runErr).
			WithFix("Check npm output above for details.")
	}

	// Regenerate desk extensions.
	if extErr := generateDeskExtensions(projectRoot, deskDir); extErr != nil {
		return extErr
	}

	w.PrintSuccess("@moca/desk updated and extensions regenerated")
	return nil
}

// ---------------------------------------------------------------------------
// desk dev
// ---------------------------------------------------------------------------

func newDeskDevCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Start Vite dev server for desk development",
		Long: `Start the Vite development server for the desk/ frontend.
This enables hot module replacement (HMR) for rapid UI development.
The dev server runs on the configured desk_port (default: 3000).`,
		RunE: runDeskDev,
	}

	f := cmd.Flags()
	f.Int("port", 0, "Dev server port (default: from config or 3000)")

	return cmd
}

func runDeskDev(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}
	projectRoot := ctx.ProjectRoot

	deskDir := filepath.Join(projectRoot, "desk")
	if err := requireDeskDir(deskDir); err != nil {
		return err
	}

	// Determine port: flag > config > default 3000.
	port, _ := cmd.Flags().GetInt("port")
	if port == 0 && ctx.Project != nil && ctx.Project.Development.DeskPort > 0 {
		port = ctx.Project.Development.DeskPort
	}
	if port == 0 {
		port = 3000
	}

	// Regenerate extensions before starting dev server.
	if extErr := generateDeskExtensions(projectRoot, deskDir); extErr != nil {
		w.PrintWarning(fmt.Sprintf("Extension generation failed: %v", extErr))
	}

	w.Print("Starting Vite dev server on port %d...", port)
	w.Print("Press Ctrl+C to stop.\n")

	viteCmd := exec.Command("npx", "vite", "--port", fmt.Sprintf("%d", port))
	viteCmd.Dir = deskDir
	viteCmd.Stdout = os.Stdout
	viteCmd.Stderr = os.Stderr
	viteCmd.Stdin = os.Stdin

	// Forward signals to the Vite process for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if err := viteCmd.Start(); err != nil {
		return output.NewCLIError("Failed to start Vite dev server").
			WithErr(err).
			WithFix("Ensure Node.js is installed and desk/ dependencies are installed (run 'moca desk install').")
	}

	// Wait for signal or process exit.
	done := make(chan error, 1)
	go func() {
		done <- viteCmd.Wait()
	}()

	select {
	case sig := <-sigCh:
		// Forward signal to child process.
		if viteCmd.Process != nil {
			_ = viteCmd.Process.Signal(sig)
		}
		<-done // wait for process to exit
	case err := <-done:
		signal.Stop(sigCh)
		if err != nil {
			return output.NewCLIError("Vite dev server exited with error").
				WithErr(err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// requireDeskDir checks that desk/package.json exists and returns a CLIError if not.
func requireDeskDir(deskDir string) error {
	pkgJSON := filepath.Join(deskDir, "package.json")
	if _, err := os.Stat(pkgJSON); err != nil {
		return output.NewCLIError("No desk/ project found").
			WithCause(fmt.Sprintf("%s does not exist", pkgJSON)).
			WithFix("Run 'moca init' to scaffold a project with a desk/ directory, or create one manually.")
	}
	return nil
}
