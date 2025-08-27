package create

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "create"
	shortDescription = `Creates and registers a new Giant Swarm platform release.`
	longDescription  = `Creates and registers a new Giant Swarm platform release including Release CR and release notes.`
	example          = `  # Create a new release by manually specifying new app and component versions
  devctl release create \
        --name v30.1.0 \
        --provider capa \
        --base v30.0.0 \
        --app cert-manager@3.9.2 \
        --component cluster-aws@4.0.1 \
        --overwrite

  # Create a release by bumping all apps and components to their latest versions
  devctl release create \
        --name v30.1.0 \
        --provider capa \
        --base v30.0.0 \
        --bumpall

  # Bump all components, but override a specific app's version and add a dependency
  # This will overwrite any existing dependencies on 'coredns'.
  devctl release create \
        --name v30.1.0 \
        --provider capa \
        --base v30.0.0 \
        --bumpall \
        --app coredns@1.2.3@@cilium

  # Bump all components and remove all dependencies from a specific app
  # This will drop any existing dependencies on 'coredns'.
  devctl release create \
        --name v30.1.0 \
        --provider capa \
        --base v30.0.0 \
        --bumpall \
        --app cilium@1.2.3@@

  # Bump all components and override an app's component version and dependencies
  devctl release create \
        --name v30.1.0 \
        --provider capa \
        --base v30.0.0 \
        --bumpall \
        --app cluster-autoscaler@1.30.0-gs1@1.30.2@kyverno-crds`
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
		Use:     name,
		Short:   shortDescription,
		Long:    longDescription,
		Example: example,
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}
