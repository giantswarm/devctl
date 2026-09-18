package list

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "list"
	shortDescription = "List the active reservations on a management cluster."
	longDescription  = `List the active reservations on a management cluster.

The command reads an existing checkout of the GitOps repository holding the
management cluster, given by --repo-dir (the current directory by default). It
never clones and needs no GitHub token, so it also runs from a laptop.

For each active reservation it prints the app, the user, the branch, the pull
request, the scope and the expiry, exactly as recorded in the cluster's
reservations ConfigMap.`
	example = `  devctl reservation list --repo-dir . --cluster graveler`
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
