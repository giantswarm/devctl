package list

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
)

const (
	name             = "list"
	shortDescription = "List the reservations on a management cluster."
	longDescription  = `List the reservations on a management cluster.

` + gitops.CheckoutHelp + `

It reads the tip of main, never the cluster, and needs no GitHub token, so it
also runs from a laptop.

For each reservation it prints the app, the user, the branch, the pull
request, the scope, the expiry and the status, as recorded in the cluster's
reservations ConfigMap. The status is "active" until the expiry passes, then
"expired" until the reaper sweeps the reservation. With --output json it
prints the same entries as a JSON array, [] when there are none.`
	example = `  devctl reservation list --gitops-dir ~/umbrella/cmc/giantswarm-management-clusters --cluster graveler`
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
		Args:    cobra.NoArgs,
		Short:   shortDescription,
		Long:    longDescription,
		Example: example,
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}
