package release

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

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
// token. It commits with go-git exactly as Reserve does, then pushes with the
// native git binary so the push uses whatever credentials are already
// configured for that checkout, on the engineer's own laptop or in CI.
func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	result, err := reservation.Release(reservation.ReleaseRequest{
		RepoDir: r.flag.RepoDir,
		Cluster: r.flag.Cluster,
		App:     r.flag.App,
		AppDir:  r.flag.AppDir,
		User:    r.flag.User,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	if err := gitPush(ctx, r.flag.RepoDir); err != nil {
		return microerror.Mask(err)
	}

	_, _ = fmt.Fprintf(r.stdout, "Released %s on %s.\n", result.App, r.flag.Cluster)
	_, _ = fmt.Fprintf(r.stdout, "Commit %s\n", result.Commit)

	return nil
}

// gitPush shells out to the native git binary rather than go-git, so the push
// uses the checkout's own configured remote and credentials (an SSH key, a
// credential helper) instead of requiring a GitHub token.
func gitPush(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "push") //nolint:gosec // dir and the git binary are trusted inputs, not user data
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard

	if err := cmd.Run(); err != nil {
		return microerror.Maskf(pushError, "git push in %s: %v\n%s", dir, err, strings.TrimSpace(stderr.String()))
	}

	return nil
}
