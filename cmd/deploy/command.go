package deploy

import (
	"fmt"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name             = "deploy"
	shortDescription = `Deploy an application to a GitOps repository`
	longDescription  = `Deploy an application to a GitOps repository by:
		1. Cloning the GitOps repository
		2. Adding the application using 'kubectl gs gitops add app'
		3. Creating a branch and committing the changes
		4. Creating a pull request
		5. Check the app is finally deployed

		You must have 'kubectl-gs' and 'tsh' installed in your machine to use this command.`
	example = `  devctl deploy --gitops-repo giantswarm/workload-clusters-fleet \
		--app-name hello-world \
		--app-version 2.8.1 \
		--app-catalog giantswarm \
		--workload-cluster operations \
		--organization giantswarm-production \
		--target-namespace default`
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
		Use:     fmt.Sprintf("%s [--gitops-repo] REPOSITORY", name),
		Short:   shortDescription,
		Long:    longDescription,
		Example: example,
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}
