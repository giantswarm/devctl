package reserve

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
)

const (
	name             = "reserve"
	shortDescription = "Point one management cluster app at the dev builds of a branch."
	longDescription  = `Point one management cluster app at the dev builds of a branch.

` + gitops.CheckoutHelp + `

The command adds a Kustomize component that serves the app from the dev
builds of the branch, records the reservation in the cluster's reservations
ConfigMap, renders the result to check that the reservation really takes
effect, and pushes one commit. With --user set, it needs no GitHub token. Flux
applies the change to the cluster a few minutes after the push.

The command finds the app by rendering the cluster's collections and matching
the rendered source objects on the chart their OCI URL serves, never on the
object name: collection names repeat across collections, so a name match is
ambiguous. The chart name comes from the app repository's helm/*/Chart.yaml
unless --app gives it. The command patches every instance of the app on the
cluster, and all of them share one source object.

The reservation lasts 10 hours unless --duration says otherwise. Accepted
durations are 30m, 4h, 4h30m or 2d. The longest is 7d, or less when the
cluster's reservations ConfigMap carries a lower
reservations.giantswarm.io/max-duration annotation.

The cluster has to be enabled for reservations first; the command says how
when it is not. With --output json the command prints the reservation as one
JSON object.`
	example = `  devctl reservation reserve \
    --gitops-dir ~/umbrella/cmc/giantswarm-management-clusters \
    --cluster graveler \
    --app hello-world \
    --branch fix/crash \
    --user alice \
    --duration 4h \
    --pull-request giantswarm/hello-world#123`
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
