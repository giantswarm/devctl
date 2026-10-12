package list

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
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

// run reads the checkout at --repo-dir directly: no clone, no GitHub token, so
// it works from a laptop exactly like release.
func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	all, err := reservation.List(reservation.ListRequest{
		RepoDir: r.flag.RepoDir,
		Cluster: r.flag.Cluster,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	// List returns every entry on record, including one already past its
	// Until that the reaper has not swept yet: that one is not active, and
	// must not be printed as if it still held the app.
	now := time.Now()
	var reservations []reservation.Reservation
	for _, res := range all {
		if res.Until.After(now) {
			reservations = append(reservations, res)
		}
	}

	if len(reservations) == 0 {
		_, _ = fmt.Fprintf(r.stdout, "No active reservations on %s.\n", r.flag.Cluster)
		return nil
	}

	w := tabwriter.NewWriter(r.stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "APP\tUSER\tBRANCH\tPULL REQUEST\tSCOPE\tEXPIRES")
	for _, res := range reservations {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			res.App, res.User, res.Branch, res.PullRequest, res.Scope, res.Until.Format("2006-01-02 15:04 MST"))
	}

	return microerror.Mask(w.Flush())
}
