package status

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const (
	name        = "status"
	description = "Show the GitHub and CircleCI identities in the keychain: login, expiry, refreshable; never a token."
	long        = `Show the identities the agent-facing commands act with.

One JSON document: for GitHub and CircleCI whether a token is in the keychain,
the login it acts as, when it expires, whether the commands can refresh it
themselves (GitHub only) and the warnings (a CircleCI token within seven days of
its expiry; a GitHub token in $DEVCTL_GITHUB_TOKEN, $GITHUB_TOKEN or
$OPSCTL_GITHUB_TOKEN, which the commands for people use in place of the App
login). No token material is printed.

Exit 0 when both identities are usable without a human; exit 8 with the
` + "`devctl auth login`" + ` invocation that fixes it otherwise. Nothing is contacted.`
)

type Config struct {
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	f := &flag{}

	r := &runner{
		gate:           versiongate.Check,
		flag:           f,
		stderr:         config.Stderr,
		stdout:         config.Stdout,
		open:           authstore.Open,
		githubOverride: authstore.GitHubOverrideWarning,
	}

	c := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  long,
		// The arguments are checked by the runner: a usage error is exit 7
		// with a document, like every other outcome.
		Args:        cobra.ArbitraryArgs,
		RunE:        r.Run,
		Annotations: agentcli.AgentFacing(),
	}
	c.SetFlagErrorFunc(r.FlagError)

	f.Init(c)

	return c, nil
}
