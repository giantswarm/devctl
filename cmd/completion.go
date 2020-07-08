package cmd

// The 'completion' command is defined on the top leve of the commands
// package, as it has to have access to the root command.

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate completion script",
	Long: `To load completions:
	
	Bash:
	
	$ source <(devctl completion bash)
	
	# To load completions for each session, execute once:
	Linux:
	  $ devctl completion bash > /etc/bash_completion.d/devctl
	MacOS:
	  $ devctl completion bash > /usr/local/etc/bash_completion.d/devctl
	
	Zsh:
	
	$ source <(devctl completion zsh)
	
	# To load completions for each session, execute once:
	$ devctl completion zsh > "${fpath[1]}/_devctl"
	
	Fish:
	
	$ devctl completion fish | source
	
	# To load completions for each session, execute once:
	$ devctl completion fish > ~/.config/fish/completions/devctl.fish
	`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.ExactValidArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			cmd.Root().GenPowerShellCompletion(os.Stdout)
		}
	},
}
