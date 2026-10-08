package reserve

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
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
	token, err := authstore.ResolveGitHub(ctx)
	if err != nil {
		return err
	}
	token.WarnOnce(r.stderr)
	notFoundHint := authstore.GitHubNotFoundHint(token)

	owner, repo, err := splitRepo(r.flag.GitOpsRepo)
	if err != nil {
		return microerror.Mask(err)
	}

	// Already accepted by Validate; the cluster's own maximum is checked once the
	// clone is on disk, in Reserve. Without --duration it stays zero, so Reserve
	// takes the default held to that maximum instead of refusing it.
	var duration time.Duration
	if r.flag.Duration != "" {
		duration, err = reservation.ParseDuration(r.flag.Duration)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	client, err := githubclient.New(githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token.Value,
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
		return githubclient.ExplainNotFound(microerror.Mask(err), notFoundHint)
	}

	// The clone keeps the token out of its remote URL, so native git, which
	// pushes and fetches below, needs it handed over.
	ctx = reservation.WithGitHubToken(ctx, token.Value)

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
		if reservation.IsNotGitOpsRepo(err) {
			// The fresh clone has no --repo-dir to fix: the wrong repo came in
			// through --gitops-repo.
			return microerror.Maskf(invalidFlagError,
				"--gitops-repo %s is not the GitOps repo that holds the management cluster configuration: it has no management-clusters directory. Set --gitops-repo to that repo, for example giantswarm/giantswarm-management-clusters",
				r.flag.GitOpsRepo)
		}
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
