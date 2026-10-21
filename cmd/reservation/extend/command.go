package extend

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
)

const (
	name             = "extend"
	shortDescription = "Reset the expiry of every reservation a pull request holds."
	longDescription  = `Reset the expiry of every reservation a pull request holds.

` + gitops.CheckoutHelp + `

It scans every management cluster enabled for reservations, and skips a
cluster that never opted in. It resets the window of every reservation whose
pull request matches --pull-request, one at a time, and commits and pushes
each. With --user set, it needs no GitHub token, so it also runs from a laptop.
Flux applies the change to the cluster a few minutes after the push.

Each reservation keeps its own stored cluster, app, scope and duration. Only
the window moves: it starts now and lasts as long as the existing record did.
An expired reservation counts as gone, as reviving it could silently break a
lock someone else took over the same cluster while the dead record sat
unswept.

The command refuses when the pull request holds no unexpired reservation on
any cluster, and names devctl reservation reserve as the way to create one. A
reservation whose stored duration now exceeds its cluster's cap is refused on
its own and does not stop the rest of the sweep.

For each reset reservation the command prints one tab-separated line to
stdout: cluster, app and the new expiry (RFC 3339). It prints nothing when it
finds nothing to extend. With --output json it prints the same entries as a
JSON array, [] when there are none. A failure on one cluster or reservation
does not stop the sweep: the command prints every extension that landed, then
reports the failure and exits non-zero.`
	example = `  devctl reservation extend --gitops-dir ~/umbrella/cmc/giantswarm-management-clusters --pull-request giantswarm/hello-world#123 --user reservation-extend`
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
