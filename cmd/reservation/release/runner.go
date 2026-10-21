package release

import (
	"context"
	"fmt"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
	"github.com/giantswarm/devctl/v8/internal/termout"
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

// run works on the GitOps checkout, synced to origin/main first: no clone, no
// GitHub token. It commits as Reserve does, then pushes with
// reservation.PushWithRetry, which uses whatever credentials are already
// configured for that checkout, on the engineer's own laptop or in CI. When
// the push is rejected, PushWithRetry rebases and reruns Release itself, so a
// reservation released by someone else in the meantime is not lost or
// replayed onto a stale tree.
func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) (err error) {
	user, err := gitops.User(ctx, r.flag.User, r.stderr)
	if err != nil {
		return microerror.Mask(err)
	}
	checkout, err := r.flag.GitOps.Open(ctx, r.stderr)
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() { err = checkout.Finish(ctx, r.stderr, err) }()

	if r.flag.Cluster == "" {
		return microerror.Mask(r.releaseAll(ctx, checkout, user))
	}

	// Empty --app-dir is the current directory, read only when --app is empty.
	appDir := r.flag.AppDir
	if r.flag.App == "" && appDir == "" {
		appDir = "."
	}

	var result reservation.ReleaseResult
	render := func() error {
		var err error
		result, err = reservation.Release(reservation.ReleaseRequest{
			RepoDir: checkout.Dir,
			Cluster: r.flag.Cluster,
			App:     r.flag.App,
			AppDir:  appDir,
			User:    user,
			// Empty unless the caller runs from a pull request. A human
			// clearing a stuck lock passes none and releases regardless.
			PullRequest: r.flag.PullRequest,
		})
		return microerror.Mask(err)
	}

	if err := reservation.PushWithRetry(ctx, checkout.Dir, r.flag.GitOps.DryRun, render); err != nil {
		return microerror.Mask(checkout.Explain(err))
	}
	checkout.Wrote(r.stderr)

	if r.flag.GitOps.JSON() {
		return gitops.WriteJSON(r.stdout, struct {
			Cluster string `json:"cluster"`
			reservation.ReleaseResult
		}{r.flag.Cluster, result})
	}

	_, _ = fmt.Fprintf(r.stdout, "Released %s on %s.\n", result.App, r.flag.Cluster)
	if termout.Debug(cmd) {
		_, _ = fmt.Fprintf(r.stdout, "Commit %s\n", result.Commit)
	}

	return nil
}

// releaseAll runs the --pull-request form: it releases every reservation the
// pull request holds, on every enabled cluster, and pushes each one. It
// prints what landed before it looks at the error, exactly as extend does: a
// cluster nobody can read must not hide the releases that did succeed, since
// a caller parses this to comment a line per release.
func (r *runner) releaseAll(ctx context.Context, checkout *gitops.Checkout, user string) error {
	released, releaseErr := reservation.ReleaseAll(ctx, reservation.ReleaseAllRequest{
		RepoDir:     checkout.Dir,
		PullRequest: r.flag.PullRequest,
		User:        user,
		DryRun:      r.flag.GitOps.DryRun,
	})

	if r.flag.GitOps.JSON() {
		if err := gitops.WriteJSON(r.stdout, append([]reservation.Released{}, released...)); err != nil {
			return microerror.Mask(err)
		}
	} else {
		for _, entry := range released {
			_, _ = fmt.Fprintf(r.stdout, "%s\t%s\t%s\n", entry.Cluster, entry.App, entry.Commit)
		}
	}

	if len(released) > 0 {
		checkout.Wrote(r.stderr)
	}
	// A success: the close hook runs this for every pull request, most of
	// which hold nothing.
	if len(released) == 0 && releaseErr == nil && !r.flag.GitOps.JSON() {
		_, _ = fmt.Fprintf(r.stdout, "Nothing to release: pull request %s holds no reservation.\n", r.flag.PullRequest)
	}
	if releaseErr != nil {
		return microerror.Mask(checkout.Explain(releaseErr))
	}

	return nil
}
