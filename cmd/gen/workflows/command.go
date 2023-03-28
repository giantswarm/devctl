package workflows

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "workflows"
	shortDescription = `Generates GitHub workflows.`
	longDescription  = `Generates GitHub workflows.

There are different generation flavours:

  - app - project containing a helm chart
  - cli - project released with a downloadable binary
  - generic - everything else, i.e a project which simply needs to be released
  - k8sapi - project containing a Kubernetes API
  - cluster-app - project containing helm chart, that is a cluster app (e.g. cluster-aws, cluster-azure, ...)
`
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
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	f := &flag{}

	r := &runner{
		flag:   f,
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:   name,
		Short: shortDescription,
		Long:  longDescription,
		RunE:  r.Run,
	}

	f.Init(c)

	return c, nil
}
