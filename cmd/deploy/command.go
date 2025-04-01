package deploy

import (
	"io"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type Config struct {
	Logger *logrus.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Stderr must not be empty", config)
	}
	if config.Stdout == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Stdout must not be empty", config)
	}

	f := &flag{}

	r := &runner{
		Flag:   f,
		Logger: config.Logger,
		Stderr: config.Stderr,
		Stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an application to a GitOps repository",
		Long: `Deploy an application to a GitOps repository by:
		1. Cloning the GitOps repository
		2. Adding the application using 'kubectl gs gitops add app'
		3. Creating a branch and committing the changes
		4. Creating a pull request

Example:
    devctl deploy \
        --gitops-repo giantswarm/workload-clusters-fleet \
        --app-name hello-world \
        --app-version 0.1.0 \
        --app-catalog giantswarm \
        --workload-cluster demo \
        --organization giantswarm-production \
        --target-namespace default`,

		RunE:          r.Run,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	f.Init(c)

	return c, nil
}
