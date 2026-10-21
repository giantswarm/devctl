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

The command works on a checkout of the GitOps repository, found through
--gitops-dir or, when that is empty, $UMBRELLA_TARGET_DIR/cmc/<repo> (<repo> is
the name part of --gitops-repo). It never clones: with neither it fails. Before
anything else it fast-forwards the checkout to origin/main, and it stops when
the checkout is not on main, has changes to tracked files (untracked files are
left alone), or has local commits that are not on
origin/main. Git uses the checkout's own credentials.
It deletes the
reservation's Kustomize component, the line that references it, and the entry
in the cluster's reservations ConfigMap, commits the result, and pushes. No
GitHub token is needed, so this also runs from a laptop to free a stuck
reservation by hand. Flux applies the change to the cluster a few minutes
after the push.

The app is keyed on the resolved chart name, exactly as reserve resolves it:
pass the same --app (or --app-dir) as the reserve call, or the App field of its
Result.

The command refuses, and changes nothing, when the app holds no reservation on
the cluster.`
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
