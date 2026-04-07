package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/generate"
	"github.com/osama1998H/moca/internal/output"
)

// NewGenerateCommand returns the "moca generate" command group with all subcommands.
func NewGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "generate",
		Aliases: []string{"gen"},
		Short:   "Infrastructure config generation",
		Long:    "Generate reverse proxy configs, systemd units, Docker files, and Kubernetes manifests.",
	}

	cmd.AddCommand(
		newGenerateCaddyCommand(),
		newGenerateNginxCommand(),
		newGenerateSystemdCommand(),
		newGenerateDockerCommand(),
		newGenerateK8sCommand(),
		newGenerateSupervisorCommand(),
		newGenerateEnvCommand(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// Caddy
// ---------------------------------------------------------------------------

func newGenerateCaddyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "caddy",
		Short: "Generate Caddy reverse proxy config",
		Long:  "Generate a Caddyfile from moca.yaml for reverse-proxying to Moca processes.",
		RunE:  runGenerateCaddy,
	}
	f := cmd.Flags()
	f.String("output", "", "Output path (default: config/caddy/Caddyfile)")
	f.Bool("multitenant", false, "Generate wildcard config for subdomain multitenancy")
	f.Bool("reload", false, "Reload Caddy after generating")
	return cmd
}

func runGenerateCaddy(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)
	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	outputPath, _ := cmd.Flags().GetString("output")
	if outputPath == "" {
		outputPath = filepath.Join(ctx.ProjectRoot, "config", "caddy", "Caddyfile")
	}
	multitenant, _ := cmd.Flags().GetBool("multitenant")
	reload, _ := cmd.Flags().GetBool("reload")

	s := w.NewSpinner("Generating Caddyfile...")
	s.Start()

	paths, genErr := generate.GenerateCaddy(ctx.Project, ctx.ProjectRoot, generate.CaddyOptions{
		OutputPath:  outputPath,
		Multitenant: multitenant,
		Domain:      ctx.Project.Project.Name,
	})
	if genErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to generate Caddyfile").
			WithErr(genErr).
			WithFix("Check moca.yaml production config is valid.")
	}
	s.Stop("Caddyfile generated")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"paths": paths})
	}
	w.PrintSuccess(fmt.Sprintf("Caddyfile written to %s", paths[0]))

	if reload {
		if err := reloadService("caddy", "reload", "--config", paths[0]); err != nil {
			w.PrintWarning(fmt.Sprintf("Caddy reload failed: %v", err))
		} else {
			w.PrintSuccess("Caddy reloaded")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// NGINX
// ---------------------------------------------------------------------------

func newGenerateNginxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "nginx",
		Short: "Generate NGINX config",
		Long:  "Generate an NGINX reverse proxy configuration from moca.yaml.",
		RunE:  runGenerateNginx,
	}
	f := cmd.Flags()
	f.String("output", "", "Output path (default: config/nginx/moca.conf)")
	f.Bool("multitenant", false, "Subdomain-based multitenancy config")
	f.Bool("reload", false, "Reload NGINX after generating")
	return cmd
}

func runGenerateNginx(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)
	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	outputPath, _ := cmd.Flags().GetString("output")
	if outputPath == "" {
		outputPath = filepath.Join(ctx.ProjectRoot, "config", "nginx", "moca.conf")
	}
	multitenant, _ := cmd.Flags().GetBool("multitenant")
	reload, _ := cmd.Flags().GetBool("reload")

	s := w.NewSpinner("Generating NGINX config...")
	s.Start()

	paths, genErr := generate.GenerateNginx(ctx.Project, ctx.ProjectRoot, generate.NginxOptions{
		OutputPath:  outputPath,
		Multitenant: multitenant,
		Domain:      ctx.Project.Project.Name,
	})
	if genErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to generate NGINX config").
			WithErr(genErr).
			WithFix("Check moca.yaml production config is valid.")
	}
	s.Stop("NGINX config generated")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"paths": paths})
	}
	w.PrintSuccess(fmt.Sprintf("NGINX config written to %s", paths[0]))

	if reload {
		if err := reloadService("nginx", "-s", "reload"); err != nil {
			w.PrintWarning(fmt.Sprintf("NGINX reload failed: %v", err))
		} else {
			w.PrintSuccess("NGINX reloaded")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Systemd
// ---------------------------------------------------------------------------

func newGenerateSystemdCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "systemd",
		Short: "Generate systemd unit files",
		Long:  "Generate systemd unit files for all Moca processes.",
		RunE:  runGenerateSystemd,
	}
	f := cmd.Flags()
	f.String("output", "", "Output directory (default: config/systemd/)")
	f.String("user", "", "System user to run as (default: moca)")
	f.Bool("install", false, "Install units to /etc/systemd/system/")
	return cmd
}

func runGenerateSystemd(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)
	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	outputDir, _ := cmd.Flags().GetString("output")
	if outputDir == "" {
		outputDir = filepath.Join(ctx.ProjectRoot, "config", "systemd")
	}
	user, _ := cmd.Flags().GetString("user")
	install, _ := cmd.Flags().GetBool("install")

	s := w.NewSpinner("Generating systemd unit files...")
	s.Start()

	paths, genErr := generate.GenerateSystemd(ctx.Project, ctx.ProjectRoot, generate.SystemdOptions{
		OutputDir: outputDir,
		User:      user,
	})
	if genErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to generate systemd units").
			WithErr(genErr).
			WithFix("Check moca.yaml production config is valid.")
	}
	s.Stop(fmt.Sprintf("%d unit files generated", len(paths)))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"paths": paths})
	}
	for _, p := range paths {
		w.PrintSuccess(fmt.Sprintf("  %s", p))
	}

	if install {
		w.PrintInfo("Installing units to /etc/systemd/system/...")
		if err := installSystemdUnits(paths); err != nil {
			w.PrintWarning(fmt.Sprintf("Install failed: %v", err))
			w.PrintInfo("Try: sudo moca generate systemd --install")
		} else {
			w.PrintSuccess("Units installed. Run 'systemctl daemon-reload' to activate.")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Docker
// ---------------------------------------------------------------------------

func newGenerateDockerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docker",
		Short: "Generate Docker Compose files",
		Long:  "Generate Docker Compose configuration for the full Moca stack.",
		RunE:  runGenerateDocker,
	}
	f := cmd.Flags()
	f.String("output", "", "Output directory (default: config/docker/)")
	f.String("profile", "development", `"development" or "production"`)
	f.StringSlice("include", nil, `Extra services: "kafka", "meilisearch", "minio"`)
	return cmd
}

func runGenerateDocker(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)
	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	outputDir, _ := cmd.Flags().GetString("output")
	if outputDir == "" {
		outputDir = filepath.Join(ctx.ProjectRoot, "config", "docker")
	}
	profile, _ := cmd.Flags().GetString("profile")
	include, _ := cmd.Flags().GetStringSlice("include")

	s := w.NewSpinner("Generating Docker files...")
	s.Start()

	paths, genErr := generate.GenerateDocker(ctx.Project, ctx.ProjectRoot, generate.DockerOptions{
		OutputDir: outputDir,
		Profile:   profile,
		Include:   include,
	})
	if genErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to generate Docker files").
			WithErr(genErr).
			WithFix("Check moca.yaml infrastructure config is valid.")
	}
	s.Stop(fmt.Sprintf("%d files generated", len(paths)))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"paths": paths})
	}
	for _, p := range paths {
		w.PrintSuccess(fmt.Sprintf("  %s", p))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Kubernetes
// ---------------------------------------------------------------------------

func newGenerateK8sCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "k8s",
		Aliases: []string{"kubernetes"},
		Short:   "Generate Kubernetes manifests",
		Long:    "Generate Kubernetes deployment, service, ingress, and supporting manifests.",
		RunE:    runGenerateK8s,
	}
	f := cmd.Flags()
	f.String("output", "", "Output directory (default: config/k8s/)")
	f.String("namespace", "moca", "Kubernetes namespace")
	f.Int("replicas", 3, "Server replicas")
	f.Bool("helm", false, "Generate as Helm chart (planned for future release)")
	return cmd
}

func runGenerateK8s(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)
	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	outputDir, _ := cmd.Flags().GetString("output")
	if outputDir == "" {
		outputDir = filepath.Join(ctx.ProjectRoot, "config", "k8s")
	}
	namespace, _ := cmd.Flags().GetString("namespace")
	replicas, _ := cmd.Flags().GetInt("replicas")
	helm, _ := cmd.Flags().GetBool("helm")

	if helm {
		w.PrintWarning("Helm chart generation is planned for a future release. Generating raw manifests.")
	}

	s := w.NewSpinner("Generating Kubernetes manifests...")
	s.Start()

	paths, genErr := generate.GenerateK8s(ctx.Project, ctx.ProjectRoot, generate.K8sOptions{
		OutputDir: outputDir,
		Namespace: namespace,
		Replicas:  replicas,
	})
	if genErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to generate Kubernetes manifests").
			WithErr(genErr).
			WithFix("Check moca.yaml production and infrastructure config.")
	}
	s.Stop(fmt.Sprintf("%d manifests generated", len(paths)))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"paths": paths})
	}
	for _, p := range paths {
		w.PrintSuccess(fmt.Sprintf("  %s", p))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Supervisor
// ---------------------------------------------------------------------------

func newGenerateSupervisorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "supervisor",
		Short: "Generate supervisor config",
		Long:  "Generate a supervisord configuration file for all Moca processes.",
		RunE:  runGenerateSupervisor,
	}
	f := cmd.Flags()
	f.String("output", "", "Output path (default: config/supervisor/supervisord.conf)")
	f.String("user", "", "System user to run as (default: moca)")
	return cmd
}

func runGenerateSupervisor(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)
	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	outputPath, _ := cmd.Flags().GetString("output")
	if outputPath == "" {
		outputPath = filepath.Join(ctx.ProjectRoot, "config", "supervisor", "supervisord.conf")
	}
	user, _ := cmd.Flags().GetString("user")

	s := w.NewSpinner("Generating supervisor config...")
	s.Start()

	paths, genErr := generate.GenerateSupervisor(ctx.Project, ctx.ProjectRoot, generate.SupervisorOptions{
		OutputPath: outputPath,
		User:       user,
	})
	if genErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to generate supervisor config").
			WithErr(genErr).
			WithFix("Check moca.yaml production config is valid.")
	}
	s.Stop("Supervisor config generated")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"paths": paths})
	}
	w.PrintSuccess(fmt.Sprintf("Supervisor config written to %s", paths[0]))
	return nil
}

// ---------------------------------------------------------------------------
// Env
// ---------------------------------------------------------------------------

func newGenerateEnvCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Generate .env file from moca.yaml",
		Long:  "Generate an environment variable file from moca.yaml configuration.",
		RunE:  runGenerateEnv,
	}
	f := cmd.Flags()
	f.String("output", "", "Output path (default: .env)")
	f.String("format", "dotenv", `Output format: "dotenv", "docker", "systemd"`)
	return cmd
}

func runGenerateEnv(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)
	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	outputPath, _ := cmd.Flags().GetString("output")
	if outputPath == "" {
		outputPath = filepath.Join(ctx.ProjectRoot, ".env")
	}
	format, _ := cmd.Flags().GetString("format")

	s := w.NewSpinner(fmt.Sprintf("Generating .env (%s format)...", format))
	s.Start()

	paths, genErr := generate.GenerateEnv(ctx.Project, ctx.ProjectRoot, generate.EnvOptions{
		OutputPath: outputPath,
		Format:     format,
	})
	if genErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to generate .env file").
			WithErr(genErr).
			WithFix("Check moca.yaml config is valid.")
	}
	s.Stop(".env generated")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"paths": paths, "format": format})
	}
	w.PrintSuccess(fmt.Sprintf("Environment file written to %s (format: %s)", paths[0], format))
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// reloadService runs the given binary with args to reload a service.
func reloadService(bin string, args ...string) error {
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// installSystemdUnits copies unit files to /etc/systemd/system/.
func installSystemdUnits(paths []string) error {
	for _, p := range paths {
		dest := filepath.Join("/etc/systemd/system", filepath.Base(p))
		out, err := exec.Command("cp", p, dest).CombinedOutput()
		if err != nil {
			return fmt.Errorf("copy %s: %s: %w", filepath.Base(p), strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}
