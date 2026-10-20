package extend

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "extend"
	shortDescription = "Reset the expiry of every reservation a pull request holds."
	longDescription  = `Reset the expiry of every reservation a pull request holds.

The command works on a checkout of the GitOps repository, found through
--gitops-dir or, when that is empty, $UMBRELLA_TARGET_DIR/cmc/<repo> (<repo> is
the name part of --gitops-repo). It never clones: with neither it fails. Before
anything else it fast-forwards the checkout to origin/main, and it stops when
the checkout is not on main, has changes to tracked files (untracked files are
left alone), or has local commits that are not on
origin/main. Git uses the checkout's own credentials.
It scans every management cluster enabled for reservations -- one that never
opted in is skipped, not a failure -- and resets the window of every
reservation whose pull request matches --pull-request, one at a time,
committing and pushing. No GitHub token is needed, so this also runs from a
laptop. Flux applies the change to the cluster a few minutes after the push.

Each reservation keeps its own stored cluster, app, scope and duration --
only the window moves, starting now and lasting as long as the existing
record already did. A reservation whose expiry already passed is treated as
if it did not exist, since reviving it could silently break a lock someone
else legally took over the same cluster while the dead record sat unswept.

The command refuses when the pull request holds no matching, unexpired
reservation on any cluster, naming devctl reservation reserve as the way to create one. A single
reservation whose stored duration now exceeds its cluster's cap is refused on
its own and does not stop the rest of the sweep.

For each reservation reset the command prints one tab-separated line to
stdout: cluster, app and the new expiry (RFC 3339). It prints nothing when it
finds nothing to extend. A cluster or a reservation that fails does not stop
the sweep from reaching the next one; the command still prints every
extension that did land before reporting the failure and exiting non-zero.`
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
