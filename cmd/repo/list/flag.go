package list

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/internal/env"
	"github.com/giantswarm/microerror"
)

const (
	flagDependOn          = "depend-on"
	flagGithubAccessToken = "github.access.token"
)

type flag struct {
	DependOn          string
	GithubAccessToken string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.DependOn, flagDependOn, "", `Name of Go package, e.g. "github.com/giantswarm/helmclient". Filters listed repositories by those having dependency to the package.`)
	cmd.Flags().StringVar(&f.GithubAccessToken, flagGithubAccessToken, os.Getenv(env.DevctlGithubAccessToken), fmt.Sprintf(`Github access token. Defaults to %s environment variable.`, env.DevctlGithubAccessToken))
}

func (f *flag) Validate() error {
	if f.DependOn == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDependOn)
	}
	if f.GithubAccessToken == "" {
		return microerror.Maskf(invalidFlagError, "--%s or %s environment variable must not be empty", flagGithubAccessToken, env.DevctlGithubAccessToken)
	}

	return nil
}
