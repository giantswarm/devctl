package ciwebhooks

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name            = "ci-webhooks"
	description     = `Configure GitHub repository`
	longDescription = `Configure GitHub repository with:

 - Webhooks to Tekton

To be able to setup the webhooks, a shared secret must be provided using --webhook-secret
You can get the value of this secret from the github-webhook-secret Secret in the tekton-pipelines namespace of the gazelle/cicdprod cluster.
e.g.

  SHARED_SECRET="$(kubectl get secret -n tekton-pipelines github-webhook-secret -o jsonpath='{.data.token}' | base64 -d)"
`
	use = `ci-webhooks --webhook-secret ${SHARED_SECRET} [flags] ORG/REPOSITORY`
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
		Use:   use,
		Short: description,
		Long:  longDescription,
		RunE:  r.Run,
		Args:  cobra.ExactArgs(1),
	}

	f.Init(c)

	return c, nil
}
