package create

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name        = "create"
	description = `Creates and registers a new Giant Swarm platform release including Release CR and release notes.

Example: devctl release create \
	--name 12.0.1 \
	--provider kvm \
	--base 11.3.2 \
	--app cert-exporter@1.2.3 \
	--app cluster-autoscaler@1.16.0@1.16.5 \
	--component cluster-operator@0.23.9 \
	--component containerlinux@2512.2.1 \
	--overwrite
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
		Short: description,
		Long:  description,
		RunE:  r.Run,
	}

	f.Init(c)

	return c, nil
}
