package reap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/termout"
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

// run sweeps the GitOps checkout, synced to origin/main first: no clone. A
// GitHub token is only needed for the rename check, the one GitHub API call;
// without one, HeadBranch stays nil and reservation.Reap still releases
// everything past its expiry.
func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	checkout, err := r.flag.GitOps.Open(ctx, r.stderr)
	if err != nil {
		return microerror.Mask(err)
	}

	req := reservation.ReapRequest{
		RepoDir: checkout.Dir,
		User:    r.flag.User,
	}

	token, err := authstore.ResolveGitHub(ctx)
	switch {
	case errors.Is(err, authstore.ErrAuthRequired):
		_, _ = fmt.Fprintf(r.stderr, "%s: no GitHub token found, so reap skips the rename check and releases only expired reservations. Set one of %s or run devctl auth login --github-only.\n",
			termout.Warning(r.stderr), strings.Join(authstore.GitHubEnvVars, ", "))
	case err != nil:
		return err
	default:
		token.WarnOnce(r.stderr)
		client, err := githubclient.New(githubclient.Config{
			Logger:      logrus.StandardLogger(),
			AccessToken: token.Value,
		})
		if err != nil {
			return microerror.Mask(err)
		}
		req.HeadBranch = headBranchFunc(client, authstore.GitHubNotFoundHint(token))
	}

	reaped, reapErr := reservation.Reap(ctx, req)

	// Print every release that did land before ever looking at reapErr: a
	// broken cluster or a failed push among several must not hide releases
	// that succeeded, since a caller parses this to post a comment per line.
	for _, entry := range reaped {
		_, _ = fmt.Fprintf(r.stdout, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			entry.Cluster, entry.App, entry.User, entry.Branch, entry.PullRequest,
			entry.Reason, entry.Until.Format(time.RFC3339), entry.Commit)
	}

	if len(reaped) > 0 {
		checkout.Wrote(r.stderr)
	}
	if reapErr != nil {
		return microerror.Mask(checkout.Explain(reapErr))
	}

	return nil
}

// headBranchFunc adapts client into a reservation.HeadBranchFunc: pullRequest
// is "owner/repo#number", exactly as reserve's --pull-request stores it.
// notFoundHint is added to GitHub's 404 for a pull request the token cannot
// read (authstore.GitHubNotFoundHint).
func headBranchFunc(client *githubclient.Client, notFoundHint string) reservation.HeadBranchFunc {
	return func(ctx context.Context, pullRequest string) (string, error) {
		owner, repo, number, err := reservation.SplitPullRequest(pullRequest)
		if err != nil {
			return "", microerror.Mask(err)
		}

		pr, _, err := client.GetUnderlyingClient(ctx).PullRequests.Get(ctx, owner, repo, number)
		if err != nil {
			return "", githubclient.ExplainNotFound(microerror.Mask(err), notFoundHint)
		}

		return pr.GetHead().GetRef(), nil
	}
}
