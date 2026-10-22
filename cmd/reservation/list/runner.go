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

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
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

// run reads the GitOps checkout, synced to origin/main first: never the
// cluster, and no GitHub token, so it works from a laptop exactly like release.
func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	checkout, err := r.flag.GitOps.Open(ctx, r.stderr)
	if err != nil {
		return microerror.Mask(err)
	}

	all, err := reservation.List(reservation.ListRequest{
		RepoDir: checkout.Dir,
		Cluster: r.flag.Cluster,
	})
	if err != nil {
		return microerror.Mask(checkout.Explain(err))
	}

	// An entry past its Until that the reaper has not swept yet is shown, marked
	// expired: it no longer holds the app, but it is still on record.
	now := time.Now()
	if r.flag.GitOps.JSON() {
		type entry struct {
			reservation.Reservation
			Status string `json:"status"`
		}
		entries := []entry{}
		for _, res := range all {
			entries = append(entries, entry{res, status(res, now)})
		}
		return gitops.WriteJSON(r.stdout, entries)
	}

	if len(all) == 0 {
		_, _ = fmt.Fprintf(r.stdout, "No reservations on %s.\n", r.flag.Cluster)
		return nil
	}

	w := tabwriter.NewWriter(r.stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "APP\tUSER\tBRANCH\tPULL REQUEST\tSCOPE\tEXPIRES\tSTATUS")
	for _, res := range all {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			res.App, res.User, res.Branch, res.PullRequest, res.Scope, res.Until.Format("2006-01-02 15:04 MST"), status(res, now))
	}

	return microerror.Mask(w.Flush())
}

func status(res reservation.Reservation, now time.Time) string {
	if res.Expired(now) {
		return "expired"
	}
	return "active"
}
