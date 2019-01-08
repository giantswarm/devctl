package list

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/internal/env"
	"github.com/giantswarm/microerror"
)

const (
	flagDependsOn          = "depend-on"
	flagGithubAccessToken = "github.access.token"
)

type flag struct {
	DependsOn          string
	GithubAccessToken string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.DependsOn, flagDependsOn, "", `Name of Go package, e.g. "github.com/giantswarm/helmclient". Filters listed repositories by those having dependency to the package.`)
	cmd.Flags().StringVar(&f.GithubAccessToken, flagGithubAccessToken, os.Getenv(env.DevctlGithubAccessToken), fmt.Sprintf(`Github access token. Defaults to %s environment variable.`, env.DevctlGithubAccessToken))
}

func (f *flag) Validate() error {
	if f.DependsOn == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDependsOn)
	}
	if f.GithubAccessToken == "" {
		return microerror.Maskf(invalidFlagError, "--%s or %s environment variable must not be empty", flagGithubAccessToken, env.DevctlGithubAccessToken)
	}

	return nil
}
