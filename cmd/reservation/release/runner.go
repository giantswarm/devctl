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
	if r.flag.PullRequest != "" {
		return microerror.Mask(r.releaseAll(ctx))
	}

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
		return microerror.Mask(err)
	}

	_, _ = fmt.Fprintf(r.stdout, "Released %s on %s.\n", result.App, r.flag.Cluster)
	_, _ = fmt.Fprintf(r.stdout, "Commit %s\n", result.Commit)

	return nil
}

// releaseAll runs the --pull-request form: it releases every reservation the
// pull request holds, on every enabled cluster, and pushes each one. It
// prints what landed before it looks at the error, exactly as extend does: a
// cluster nobody can read must not hide the releases that did succeed, since
// a caller parses this to comment a line per release.
func (r *runner) releaseAll(ctx context.Context) error {
	released, releaseErr := reservation.ReleaseAll(ctx, reservation.ReleaseAllRequest{
		RepoDir:     r.flag.RepoDir,
		PullRequest: r.flag.PullRequest,
		User:        r.flag.User,
	})

	for _, entry := range released {
		_, _ = fmt.Fprintf(r.stdout, "%s\t%s\t%s\n", entry.Cluster, entry.App, entry.Commit)
	}

	if releaseErr != nil {
		return microerror.Mask(releaseErr)
	}

	return nil
}
