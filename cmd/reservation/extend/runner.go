package extend

import (
	"context"
	"fmt"
	"io"
	"time"

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

// run sweeps --repo-dir directly: no clone, no GitHub token needed.
func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	extended, extendErr := reservation.Extend(ctx, reservation.ExtendRequest{
		RepoDir:     r.flag.RepoDir,
		PullRequest: r.flag.PullRequest,
		User:        r.flag.User,
	})

	// Print every extension that did land before ever looking at extendErr: a
	// broken cluster or an over-cap reservation among several must not hide
	// extensions that succeeded, since a caller parses this to post a comment
	// per line.
	for _, entry := range extended {
		_, _ = fmt.Fprintf(r.stdout, "%s\t%s\t%s\n", entry.Cluster, entry.App, entry.Until.Format(time.RFC3339))
	}

	if extendErr != nil {
		return microerror.Mask(extendErr)
	}

	return nil
}
