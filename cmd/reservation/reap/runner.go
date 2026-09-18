package reap

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

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

// run sweeps --repo-dir directly: no clone. A GitHub token is only needed for
// the rename check; without one, HeadBranch stays nil and reservation.Reap
// still releases everything past its expiry.
func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	req := reservation.ReapRequest{
		RepoDir: r.flag.RepoDir,
		User:    r.flag.User,
	}

	if token := env.GitHubToken.Val(); token != "" {
		client, err := githubclient.New(githubclient.Config{
			Logger:      logrus.StandardLogger(),
			AccessToken: token,
		})
		if err != nil {
			return microerror.Mask(err)
		}
		req.HeadBranch = headBranchFunc(client)
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
		return microerror.Mask(reapErr)
	}

	return nil
}

// headBranchFunc adapts client into a reservation.HeadBranchFunc: pullRequest
// is "owner/repo#number", exactly as reserve's --pull-request stores it.
func headBranchFunc(client *githubclient.Client) reservation.HeadBranchFunc {
	return func(ctx context.Context, pullRequest string) (string, error) {
		owner, repo, number, err := splitPullRequest(pullRequest)
		if err != nil {
			return "", microerror.Mask(err)
		}

		pr, _, err := client.GetUnderlyingClient(ctx).PullRequests.Get(ctx, owner, repo, number)
		if err != nil {
			return "", microerror.Mask(err)
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
