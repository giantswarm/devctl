package release

import (
	"context"
	"fmt"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(context.Background(), cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// run works on the checkout at --repo-dir directly: no clone, no GitHub
// token. It commits with go-git exactly as Reserve does, then pushes with
// reservation.PushWithRetry, which uses whatever credentials are already
// configured for that checkout, on the engineer's own laptop or in CI. When
// the push is rejected, PushWithRetry rebases and reruns Release itself, so a
// reservation released by someone else in the meantime is not lost or
// replayed onto a stale tree.
func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	var result reservation.ReleaseResult
	render := func() error {
		var err error
		result, err = reservation.Release(reservation.ReleaseRequest{
			RepoDir: r.flag.RepoDir,
			Cluster: r.flag.Cluster,
			App:     r.flag.App,
			AppDir:  r.flag.AppDir,
			User:    r.flag.User,
		})
		return microerror.Mask(err)
	}

	if err := reservation.PushWithRetry(ctx, r.flag.RepoDir, render); err != nil {
		return microerror.Maskf(pushError, "%s", err)
	}

	_, _ = fmt.Fprintf(r.stdout, "Released %s on %s.\n", result.App, r.flag.Cluster)
	_, _ = fmt.Fprintf(r.stdout, "Commit %s\n", result.Commit)

	return nil
}
