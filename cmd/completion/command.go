package completion

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "completion [bash|zsh|fish|powershell]"
	shortDescription = "Generate completion script."
	longDescription  = `To load completions:
	
	# Bash:
	
	$ source <(devctl completion bash)
	
	# To load completions for each session, execute once:
	# Linux:
	  $ devctl completion bash > /etc/bash_completion.d/devctl
	# MacOS:
	  $ devctl completion bash > /usr/local/etc/bash_completion.d/devctl
	
	# Zsh:
	
	$ source <(devctl completion zsh)
	
	# To load completions for each session, execute once:
	$ devctl completion zsh > "${fpath[1]}/_devctl"
	
	# Fish:
	
	$ devctl completion fish | source
	
	# To load completions for each session, execute once:
	$ devctl completion fish > ~/.config/fish/completions/devctl.fish`
)

type Config struct {
	Logger micrologger.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	r := &runner{
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:                   name,
		Short:                 shortDescription,
		Long:                  longDescription,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE:                  r.Run,
	}

	return c, nil
}
