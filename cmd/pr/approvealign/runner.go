package approvealign

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v72/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/githubclient"
)

const (
	githubTokenEnvVar = "GITHUB_TOKEN" // Standard environment variable for GitHub token
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}
	return r.run(ctx, cmd, args)
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	r.logger.Debug("running approvealign command")

	fmt.Fprintln(r.stdout, "Auto-approving all 'Align files' PRs that have passing status checks...")

	githubToken, found := os.LookupEnv(githubTokenEnvVar)
	if !found {
		return microerror.Maskf(executionFailedError, "environment variable %#q not found, please set it to your GitHub personal access token", githubTokenEnvVar)
	}

	ghClientService, err := githubclient.New(githubclient.Config{
		Logger:      r.logger,
		AccessToken: githubToken,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	githubClient := ghClientService.GetUnderlyingClient(ctx)

	searchQuery := `is:pr is:open status:success org:giantswarm review-requested:@me "Align files"`
	r.logger.Infof("Searching for PRs with query: %s", searchQuery)

	searchOpts := &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	searchResults, _, err := githubClient.Search.Issues(ctx, searchQuery, searchOpts)
	if err != nil {
		return microerror.Maskf(executionFailedError, "failed to search for PRs: %v", err)
	}

	if searchResults.GetTotal() == 0 {
		fmt.Fprintln(r.stdout, "No PRs found matching the criteria.")
	} else {
		fmt.Fprintf(r.stdout, "Found %d PRs to review.\n", searchResults.GetTotal())
	}

	approvedCount := 0
	for _, issue := range searchResults.Issues {
		prNumber := issue.GetNumber()

		owner := issue.GetRepository().GetOrganization().GetLogin()
		repoName := issue.GetRepository().GetName()

		r.logger.Infof("Attempting to approve PR #%d in %s/%s", prNumber, owner, repoName)

		reviewRequest := &github.PullRequestReviewRequest{
			Event: github.String("APPROVE"),
		}
		_, _, err = githubClient.PullRequests.CreateReview(ctx, owner, repoName, prNumber, reviewRequest)
		if err != nil {
			r.logger.Errorf("Failed to approve PR #%d in %s/%s: %v", prNumber, owner, repoName, err)
			fmt.Fprintf(r.stderr, "Failed to approve PR #%d in %s/%s: %v\n", prNumber, owner, repoName, err)
			continue
		}
			
	    fmt.Fprintf(r.stdout, "Successfully approved PR #%d in %s/%s\n", prNumber, owner, repoName)
		approvedCount++
	}

	if approvedCount > 0 {
		fmt.Fprintf(r.stdout, "Successfully approved %d PR(s).\n", approvedCount)
	}

	fmt.Fprintln(r.stdout, "\nAll remaining PRs (including any that failed approval or are pending) can be seen with this search query:")
	fmt.Fprintln(r.stdout, "https://github.com/pulls?q=sort%3Aupdated-desc+is%3Apr+is%3Aopen+archived%3Afalse+review-requested%3A%40me+status%3Apending+org%3Agiantswarm+%22Align+files%22")

	return nil
}
