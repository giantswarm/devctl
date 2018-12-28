package dependent

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/internal/env"
	"github.com/giantswarm/microerror"
)

const (
	flagFrom              = "from"
	flagGithubAccessToken = "github.access.token"
)

type flag struct {
	From              string
	GithubAccessToken string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.From, flagFrom, "", `Fully qualifed name of github repository being a dependecy to listed repositories. E.g. "github.com/giantswarm/helmclient".`)
	cmd.Flags().StringVar(&f.GithubAccessToken, flagGithubAccessToken, os.Getenv(env.DevctlGithubAccessToken), fmt.Sprintf(`Github access token. Defaults to %s environment variable.`, env.DevctlGithubAccessToken))
}

func (f *flag) Validate() error {
	if f.From == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagFrom)
	}
	if f.GithubAccessToken == "" {
		return microerror.Maskf(invalidFlagError, "--%s or %s environment variable must not be empty", flagGithubAccessToken, env.DevctlGithubAccessToken)
	}

	return nil
}
