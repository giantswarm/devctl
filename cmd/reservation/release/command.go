package release

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "release"
	shortDescription = "Undo a reservation on a management cluster app."
	longDescription  = `Undo a reservation on a management cluster app.

The command works on a fresh clone of the GitOps repository holding the
management cluster, given by --gitops-repo, so no checkout on disk is needed.
It deletes the reservation's Kustomize component, the line that references it,
and the entry in the cluster's reservations ConfigMap, commits the result, and
pushes. It needs a GitHub token (DEVCTL_GITHUB_TOKEN, GITHUB_TOKEN or
OPSCTL_GITHUB_TOKEN) to clone and push, and also runs from a laptop to free a
stuck reservation without CI.

The app is keyed on the resolved chart name, exactly as reserve resolves it:
pass the same --app (or --app-dir) as the reserve call, or the App field of its
Result.

The command refuses, and changes nothing, when the app holds no reservation on
the cluster.`
	example = `  devctl reservation release \
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
