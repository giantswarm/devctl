package reap

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
)

const (
	name             = "reap"
	shortDescription = "Release every reservation that expired or whose pull request changed branch."
	longDescription  = `Release every reservation that expired or whose pull request changed branch.

` + gitops.CheckoutHelp + `

It sweeps every management cluster enabled for reservations, and skips a
cluster that never opted in. It releases every expired reservation, as the
release command does, one at a time, and commits and pushes each. With --user
set, that needs no GitHub token, so it also runs from a laptop. Flux applies
the change to the cluster a few minutes after the push.

It also releases an unexpired reservation when the head branch of its pull
request no longer matches the stored branch, as the old branch builds nothing.
That check reads the pull request through the GitHub API with the first of
DEVCTL_GITHUB_TOKEN, GITHUB_TOKEN and OPSCTL_GITHUB_TOKEN that is set, else
with the devctl auth login --github-only login (never under CI). Without a
token the command still releases every expired reservation, but it never
catches a rename.

For each release the command prints one tab-separated line to stdout: cluster,
app, user, branch, pull request, reason ("expired" or "renamed"), until (RFC
3339) and commit. It prints nothing when it finds nothing to release. With
--output json it prints the same entries as a JSON array, [] when there are
none. A failure on one cluster or reservation does not stop the sweep: the
command prints every release that landed, then reports the failure and exits
non-zero.`
	example = `  devctl reservation reap --gitops-dir ~/umbrella/cmc/giantswarm-management-clusters --user reservation-reaper`
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
