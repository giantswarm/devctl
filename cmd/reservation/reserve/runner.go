package reserve

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/env"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
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

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	token := env.GitHubToken.Val()
	if token == "" {
		return microerror.Maskf(envVarNotFoundError,
			"no GitHub token found. Set DEVCTL_GITHUB_TOKEN, GITHUB_TOKEN or OPSCTL_GITHUB_TOKEN")
	}

	owner, repo, err := splitRepo(r.flag.GitOpsRepo)
	if err != nil {
		return microerror.Mask(err)
	}

	// Already accepted by Validate; the cluster's own maximum is checked once the
	// clone is on disk, in Reserve.
	duration, err := reservation.ParseDuration(r.flag.Duration)
	if err != nil {
		return microerror.Mask(err)
	}

	client, err := githubclient.New(githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	// A throwaway clone, so a failed run leaves nothing behind to clean up and a
	// half-written reservation can never be pushed.
	dir, err := os.MkdirTemp("", "devctl-reservation-*")
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	err = client.CloneRepository(ctx, owner, repo, dir)
	if err != nil {
		return microerror.Mask(err)
	}

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
			AppDir:      r.flag.AppDir,
			Branch:      r.flag.Branch,
			User:        r.flag.User,
			PullRequest: r.flag.PullRequest,
			Duration:    duration,
			Scope:       scope,
		})
		return microerror.Mask(err)
	}

	if err := reservation.PushWithRetry(ctx, dir, render); err != nil {
		return microerror.Mask(err)
	}

	_, _ = fmt.Fprintf(r.stdout, "Reserved %s on %s for %s until %s.\n",
		result.App, r.flag.Cluster, r.flag.User, result.Until.Format("2006-01-02 15:04 MST"))
	_, _ = fmt.Fprintf(r.stdout, "Source %s follows %s\n", result.SourceName, result.SemverFilter)
	_, _ = fmt.Fprintf(r.stdout, "Commit %s on %s/%s\n", result.Commit, owner, repo)

	return nil
}
