package list

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/internal/env"
	"github.com/giantswarm/microerror"
)

const (
	flagDependsOn         = "depends-on"
	flagGithubAccessToken = "github.access.token"
)

type flag struct {
	DependsOn         string
	GithubAccessToken string
}

func (f *flag) Init(cmd *cobra.Command, args []string) error {
	var err error

	err = f.init(cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	err = f.validate()
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (f *flag) init(cmd *cobra.Command, args []string) error {
	cmd.Flags().StringVar(&f.DependsOn, flagDependsOn, "", `Name of Go package, e.g. "github.com/giantswarm/helmclient". Filters listed repositories by those having dependency to the package.`)
	cmd.Flags().StringVar(&f.GithubAccessToken, flagGithubAccessToken, os.Getenv(env.DevctlGithubAccessToken), fmt.Sprintf(`Github access token. Defaults to %s environment variable.`, env.DevctlGithubAccessToken))

	return nil
}

func (f *flag) validate() error {
	if f.DependsOn == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDependsOn)
	}
	if f.GithubAccessToken == "" {
		return microerror.Maskf(invalidFlagError, "--%s or %s environment variable must not be empty", flagGithubAccessToken, env.DevctlGithubAccessToken)
	}

	return nil
}
