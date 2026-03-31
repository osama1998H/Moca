package main

import "github.com/spf13/cobra"

// NewGenerateCommand returns the "moca generate" command group with all subcommands.
func NewGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "generate",
		Aliases: []string{"gen"},
		Short:   "Infrastructure config generation",
		Long:    "Generate reverse proxy configs, systemd units, Docker files, and Kubernetes manifests.",
	}

	cmd.AddCommand(
		newSubcommand("caddy", "Generate Caddy reverse proxy config"),
		newSubcommand("nginx", "Generate NGINX config"),
		newSubcommand("systemd", "Generate systemd unit files"),
		newSubcommand("docker", "Generate Docker Compose files"),
		newSubcommand("k8s", "Generate Kubernetes manifests"),
		newSubcommand("supervisor", "Generate supervisor config"),
		newSubcommand("env", "Generate .env file from moca.yaml"),
	)

	return cmd
}
