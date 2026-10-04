package exec

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
)

const (
	name        = "exec"
	description = "Run a command with the App login's GitHub token in $GH_TOKEN; never prints it."
	long        = `Run a command with the GitHub token of the App login in $GH_TOKEN.

The token is the giantswarm-devctl App's user token from the keychain, the one
the agent-facing commands use: it acts as you, capped by the App's permissions,
and expires after eight hours. A token that expires within ten minutes is
refreshed first. devctl hands it to the command's environment only and never
prints it; gh reads $GH_TOKEN in preference to its own login.

devctl replaces itself with the command, so the exit code, signals and terminal
are the command's. Without a usable App login it exits 8 naming
` + "`devctl auth login --github-only`" + `; a command not found is exit 7.

A link named gh to devctl does the same for gh: put it in a directory first on
an agent's PATH, and every gh the agent runs uses the App token while your own
shell keeps your gh login. The link skips itself on PATH and runs the next gh.

gh acting on a repository of an owner the App is not installed on (anything
but giantswarm) runs without the token, on your own gh login: the App does not
reach that repository.

No version check precedes it: an outdated devctl still hands over the token.`
	example = `  devctl auth exec -- gh api user
  ln -s "$(command -v devctl)" ~/.local/share/devctl/agent-bin/gh`
)

type Config struct {
	Stderr io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}

	c := &cobra.Command{
		Use:         name + " -- <command> [args...]",
		Short:       description,
		Long:        long,
		Example:     example,
		Annotations: versiongate.Exempt(),
		Args:        cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ex := authexec.Default()
			ex.Stderr = config.Stderr
			if code := authexec.Run(context.Background(), ex, args[0], args[1:]); code != 0 {
				return &agentcli.ExitError{Code: code}
			}
			return nil
		},
	}
	// Everything after the command's name is the command's own.
	c.Flags().SetInterspersed(false)

	return c, nil
}
