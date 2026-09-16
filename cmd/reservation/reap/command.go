package reap

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "reap"
	shortDescription = "Sweep a GitOps repo, releasing every reservation that ran out of time or was renamed out from under it."
	longDescription  = `Sweep a GitOps repo, releasing every reservation that ran out of time or was renamed out from under it.

The command works on an existing checkout of the GitOps repository, given by
--repo-dir (the current directory by default). It never clones. It sweeps
every management cluster enabled for reservations -- one that never opted in
is skipped, not a failure -- and releases every reservation whose expiry
passed, exactly as the release command does one at a time, committing and
pushing with the checkout's own git configuration: no GitHub token is needed
for that, so this also runs from a laptop.

It also releases a reservation that is not yet expired when its pull
request's current head branch no longer matches the reservation's stored
branch: a rename means the old branch builds nothing. That check needs a
GitHub token (DEVCTL_GITHUB_TOKEN, GITHUB_TOKEN or OPSCTL_GITHUB_TOKEN); with
none set, the command still releases every expired reservation, it just never
catches a rename.

For each release the command prints one tab-separated line to stdout:
cluster, app, user, branch, pull request, reason ("expired" or "renamed"),
until (RFC 3339) and commit. It prints nothing when it finds nothing to
release. A cluster or a reservation that fails does not stop the sweep from
reaching the next one; the command still prints every release that did land
before reporting the failure and exiting non-zero.`
	example = `  devctl reservation reap --repo-dir . --user reservation-reaper`
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
