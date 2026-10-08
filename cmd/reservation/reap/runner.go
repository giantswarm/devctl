package reap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
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

// run sweeps a fresh clone of --gitops-repo, which needs a GitHub token. The
// token also serves the rename check; only with the hidden --repo-dir can it
// be missing, and then HeadBranch stays nil and reservation.Reap still
// releases everything past its expiry.
func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	ctx, dir, cleanup, err := r.flag.GitOps.Open(ctx, r.stderr)
	if err != nil {
		return microerror.Mask(err)
	}
	defer cleanup()

	req := reservation.ReapRequest{
		RepoDir: dir,
		User:    r.flag.User,
	}

	token, err := authstore.ResolveGitHub(ctx)
	switch {
	case errors.Is(err, authstore.ErrAuthRequired):
		// No token: skip the rename check.
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

	if reapErr != nil {
		return microerror.Mask(r.flag.GitOps.Explain(reapErr))
	}

	return nil
}

// headBranchFunc adapts client into a reservation.HeadBranchFunc: pullRequest
// is "owner/repo#number", exactly as reserve's --pull-request stores it.
// notFoundHint is added to GitHub's 404 for a pull request the token cannot
// read (authstore.GitHubNotFoundHint).
func headBranchFunc(client *githubclient.Client, notFoundHint string) reservation.HeadBranchFunc {
	return func(ctx context.Context, pullRequest string) (string, error) {
		owner, repo, number, err := splitPullRequest(pullRequest)
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

// splitPullRequest parses "owner/repo#number".
func splitPullRequest(pullRequest string) (owner, repo string, number int, err error) {
	repoPart, numberPart, ok := strings.Cut(pullRequest, "#")
	if !ok {
		return "", "", 0, microerror.Maskf(invalidPullRequestError, "pull request %q is not owner/repo#number", pullRequest)
	}
	owner, repo, ok = strings.Cut(repoPart, "/")
	if !ok || owner == "" || repo == "" {
		return "", "", 0, microerror.Maskf(invalidPullRequestError, "pull request %q is not owner/repo#number", pullRequest)
	}
	number, err = strconv.Atoi(numberPart)
	if err != nil {
		return "", "", 0, microerror.Maskf(invalidPullRequestError, "pull request %q is not owner/repo#number: %v", pullRequest, err)
	}

	return owner, repo, number, nil
}
