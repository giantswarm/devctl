package reserve

import (
	"context"
	"fmt"
	"io"
	"time"

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
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) (err error) {
	// Already accepted by Validate; the cluster's own maximum is checked once
	// the checkout is synced, in Reserve. Without --duration it stays zero, so
	// Reserve takes the default held to that maximum instead of refusing it.
	var duration time.Duration
	if r.flag.Duration != "" {
		duration, err = reservation.ParseDuration(r.flag.Duration)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	if r.flag.PullRequest == "" {
		_, _ = fmt.Fprintf(r.stderr, "%s: no --%s given. devctl does not check that a pull request exists for branch %s; you are responsible for builds of that branch to exist. extend --%s, release --%s and the reap rename check do not find this reservation.\n",
			termout.Warning(r.stderr), flagPullRequest, r.flag.Branch, flagPullRequest, flagPullRequest)
	}

	user, err := gitops.User(ctx, r.flag.User, r.stderr)
	if err != nil {
		return microerror.Mask(err)
	}
	// Empty --app-dir is the current directory, read only when --app is empty.
	appDir := r.flag.AppDir
	if r.flag.App == "" && appDir == "" {
		appDir = "."
	}

	checkout, err := r.flag.GitOps.Open(ctx, r.stderr)
	if err != nil {
		return microerror.Mask(err)
	}
	dir := checkout.Dir
	defer func() { err = checkout.Finish(ctx, r.stderr, err) }()

	scope := reservation.ScopeApp
	if r.flag.Exclusive {
		scope = reservation.ScopeExclusive
	}

	// render re-runs Reserve itself, so a retry after a rejected push (see
	// PushWithRetry) reserves against whatever another reservation just landed,
	// rather than replaying a commit made against a stale tree.
	var result reservation.Result
	render := func() error {
		var err error
		result, err = reservation.Reserve(reservation.Request{
			RepoDir:     dir,
			Cluster:     r.flag.Cluster,
			App:         r.flag.App,
			AppDir:      appDir,
			Branch:      r.flag.Branch,
			User:        user,
			PullRequest: r.flag.PullRequest,
			Duration:    duration,
			Scope:       scope,
		})
		return microerror.Mask(err)
	}

	if err := reservation.PushWithRetry(ctx, dir, r.flag.GitOps.DryRun, render); err != nil {
		return microerror.Mask(checkout.Explain(err))
	}
	checkout.Wrote(r.stderr)

	if r.flag.GitOps.JSON() {
		return gitops.WriteJSON(r.stdout, struct {
			Cluster     string `json:"cluster"`
			User        string `json:"user"`
			Branch      string `json:"branch"`
			PullRequest string `json:"pullRequest,omitempty"`
			Scope       string `json:"scope"`
			reservation.Result
		}{r.flag.Cluster, user, r.flag.Branch, r.flag.PullRequest, scope, result})
	}
	_, _ = fmt.Fprintf(r.stdout, "Reserved %s on %s for %s until %s.\n",
		result.App, r.flag.Cluster, user, result.Until.Format("2006-01-02 15:04 MST"))
	if termout.Debug(cmd) {
		_, _ = fmt.Fprintf(r.stdout, "Source %s follows %s\n", result.SourceName, result.SemverFilter)
		_, _ = fmt.Fprintf(r.stdout, "Commit %s on %s\n", result.Commit, r.flag.GitOps.Repo)
	}

	return nil
}
