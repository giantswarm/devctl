package release

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
)

const (
	name             = "release"
	shortDescription = "Undo a reservation on a management cluster app."
	longDescription  = `Undo a reservation on a management cluster app.

` + gitops.CheckoutHelp + `

It deletes the reservation's Kustomize component, the line that references
it, and the entry in the cluster's reservations ConfigMap, commits the result,
and pushes. With --user set, it needs no GitHub token, so it also runs from a
laptop to free a stuck reservation by hand. Flux applies the change to the
cluster a few minutes after the push.

With --cluster, it releases one app, keyed on the chart name as reserve
resolves it: pass the same --app or --app-dir as the reserve call. It refuses,
and changes nothing, when the app holds no reservation on the cluster. It
prints the released app and the commit.

Without --cluster, it releases every reservation that --pull-request holds, on
every enabled cluster. It prints one tab-separated line per release to stdout:
cluster, app and commit. It exits 0 when the pull request holds no
reservation. With --output json it prints the same entries as a JSON array, []
when there are none. In the --cluster form, the JSON output is one object.`
	example = `  devctl reservation release \
    --gitops-dir ~/umbrella/cmc/giantswarm-management-clusters \
    --cluster graveler \
    --app hello-world \
    --user alice`
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
