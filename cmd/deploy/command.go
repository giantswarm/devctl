package deploy

import (
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
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
2. Executing kubectl gs gitops add app
3. Creating a branch
4. Committing changes
5. Creating a pull request`,
		RunE:          r.Run,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	f.Init(c)

	return c, nil
}
