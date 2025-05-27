package approvealign

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

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
	searchResults, _, err := githubClient.Search.Issues(ctx, searchQuery, &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return microerror.Maskf(executionFailedError, "failed to search for PRs: %v", err)
	}

	if searchResults.GetTotal() == 0 {
		fmt.Fprintln(r.stdout, "No PRs found matching the criteria.")
	} else {
		fmt.Fprintf(r.stdout, "Found %d PRs to review.\n", searchResults.GetTotal())
	}
	approved := 0
	for _, issue := range searchResults.Issues {
		// Extract owner/repo from RepositoryURL: https://api.github.com/repos/{owner}/{repo}
		parts := strings.Split(issue.GetRepositoryURL(), "/")
		if len(parts) < 6 {
			continue
		}
		owner, repo := parts[4], parts[5]

		_, _, err = githubClient.PullRequests.CreateReview(ctx, owner, repo, issue.GetNumber(), &github.PullRequestReviewRequest{
			Event: github.String("APPROVE"),
		})
		if err != nil {
			fmt.Fprintf(r.stderr, "Failed to approve PR #%d: %v\n", issue.GetNumber(), err)
			continue
		}

		fmt.Fprintf(r.stdout, "Approved PR #%d in %s/%s\n", issue.GetNumber(), owner, repo)
		approved++
	}

	if approved > 0 {
		fmt.Fprintf(r.stdout, "\nApproved %d out of %d PRs.\n", approved, len(searchResults.Issues))
	}

	fmt.Fprintln(r.stdout, "\nAll remaining PRs (including any that failed approval or are pending) can be seen with this search query:")
	fmt.Fprintln(r.stdout, "https://github.com/pulls?q=sort%3Aupdated-desc+is%3Apr+is%3Aopen+archived%3Afalse+review-requested%3A%40me+status%3Apending+org%3Agiantswarm+%22Align+files%22")

	return nil
}
