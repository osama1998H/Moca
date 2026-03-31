package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewCompletionCommand returns the "moca completion" command.
func NewCompletionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for MOCA CLI.

To load completions:

Bash:
  $ source <(moca completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ moca completion bash > /etc/bash_completion.d/moca
  # macOS:
  $ moca completion bash > $(brew --prefix)/etc/bash_completion.d/moca

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc
  # To load completions for each session, execute once:
  $ moca completion zsh > "${fpath[1]}/_moca"
  # You will need to start a new shell for this setup to take effect.

Fish:
  $ moca completion fish | source
  # To load completions for each session, execute once:
  $ moca completion fish > ~/.config/fish/completions/moca.fish

PowerShell:
  PS> moca completion powershell | Out-String | Invoke-Expression
  # To load completions for every new session, add the output to your profile:
  PS> moca completion powershell >> $PROFILE
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}
	return cmd
}
