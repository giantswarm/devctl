package list

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/internal/env"
	"github.com/giantswarm/microerror"
)

const (
	flagDependentFrom     = "dependent-from"
	flagGithubAccessToken = "github.access.token"
)

type flag struct {
	DependentFrom     string
	GithubAccessToken string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.DependentFrom, flagDependentFrom, "", `Fully qualifed name of github repository, e.g. "github.com/giantswarm/helmclient". With this flag set only repositories having dependency to the flag value will be listed.`)
	cmd.Flags().StringVar(&f.GithubAccessToken, flagGithubAccessToken, os.Getenv(env.DevctlGithubAccessToken), fmt.Sprintf(`Github access token. Defaults to %s environment variable.`, env.DevctlGithubAccessToken))
}

func (f *flag) Validate() error {
	if f.DependentFrom == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDependentFrom)
	}
	if f.GithubAccessToken == "" {
		return microerror.Maskf(invalidFlagError, "--%s or %s environment variable must not be empty", flagGithubAccessToken, env.DevctlGithubAccessToken)
	}

	return nil
}
