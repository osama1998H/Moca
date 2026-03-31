package main

import "github.com/spf13/cobra"

// NewAPICommand returns the "moca api" command group with all subcommands.
// Includes nested "keys" and "webhooks" subgroups.
func NewAPICommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "API management",
		Long:  "List endpoints, test APIs, generate docs, and manage API keys and webhooks.",
	}

	// Nested: moca api keys {create,revoke,list,rotate}
	keys := &cobra.Command{
		Use:   "keys",
		Short: "Manage API keys",
	}
	keys.AddCommand(
		newSubcommand("create", "Create a new API key"),
		newSubcommand("revoke", "Revoke an API key"),
		newSubcommand("list", "List all API keys"),
		newSubcommand("rotate", "Rotate an API key's secret"),
	)

	// Nested: moca api webhooks {list,test,logs}
	webhooks := &cobra.Command{
		Use:   "webhooks",
		Short: "Manage webhooks",
	}
	webhooks.AddCommand(
		newSubcommand("list", "List configured webhooks"),
		newSubcommand("test", "Send a test webhook"),
		newSubcommand("logs", "Show webhook delivery logs"),
	)

	cmd.AddCommand(
		newSubcommand("list", "List all registered API endpoints"),
		newSubcommand("test", "Test an API endpoint"),
		newSubcommand("docs", "Generate OpenAPI/Swagger spec"),
		keys,
		webhooks,
	)

	return cmd
}
