package approvemergerenovate

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v80/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/internal/env"
	"github.com/giantswarm/devctl/v7/pkg/githubclient"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
}

// parseRepoFromURL extracts owner and repo name from GitHub PR URL
// e.g., "https://github.com/giantswarm/backstage/pull/1033" -> "giantswarm", "backstage"
func parseRepoFromURL(url string) (string, string, error) {
	parts := strings.Split(url, "/")
	if len(parts) < 5 {
		return "", "", fmt.Errorf("invalid GitHub URL format: %s", url)
	}
	// URL format: https://github.com/{owner}/{repo}/pull/{number}
	owner := parts[3]
	repo := parts[4]
	return owner, repo, nil
}

// makeHyperlink creates an ANSI hyperlink (OSC 8) for terminals that support it
func makeHyperlink(url, text string) string {
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, text)
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}
	return r.run(ctx, cmd, args)
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	r.logger.Debug("running approve-merge-renovate command")

	if r.flag.DryRun {
		fmt.Fprintln(r.stdout, "🔍 DRY RUN MODE - No changes will be made")
	}

	fmt.Fprintf(r.stdout, "Searching for Renovate PRs matching: %s\n", r.flag.Query)

	githubToken := env.GitHubToken.Val()
	if githubToken == "" {
		return microerror.Maskf(executionFailedError, "environment variable GITHUB_TOKEN not found, please set it to your GitHub personal access token")
	}

	ghClientService, err := githubclient.New(githubclient.Config{
		Logger:      r.logger,
		AccessToken: githubToken,
		DryRun:      r.flag.DryRun,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	githubClient := ghClientService.GetUnderlyingClient(ctx)

	// Build search query - combining user query with Renovate-specific filters
	searchQuery := fmt.Sprintf("%s is:pr is:open archived:false review-requested:@me author:app/renovate", r.flag.Query)
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
		return nil
	}

	fmt.Fprintf(r.stdout, "Found %d PR(s) to process.\n\n", searchResults.GetTotal())

	approvedCount := 0
	mergedCount := 0
	skippedCount := 0

	for _, issue := range searchResults.Issues {
		prNumber := issue.GetNumber()
		title := issue.GetTitle()
		url := issue.GetHTMLURL()

		// Parse owner and repo from URL since repository object may not be fully populated
		owner, repoName, err := parseRepoFromURL(url)
		if err != nil {
			r.logger.Errorf("Failed to parse repository from URL %s: %v", url, err)
			fmt.Fprintf(r.stderr, "  ❌ Failed to parse repository from URL: %v\n\n", err)
			skippedCount++
			continue
		}

		prLink := makeHyperlink(url, fmt.Sprintf("PR #%d in %s/%s", prNumber, owner, repoName))
		fmt.Fprintf(r.stdout, "Processing %s\n", prLink)
		fmt.Fprintf(r.stdout, "  Title: %s\n", title)

		// Check status checks
		pr, _, err := githubClient.PullRequests.Get(ctx, owner, repoName, prNumber)
		if err != nil {
			r.logger.Errorf("Failed to get PR #%d in %s/%s: %v", prNumber, owner, repoName, err)
			fmt.Fprintf(r.stderr, "  ❌ Failed to get PR details: %v\n\n", err)
			skippedCount++
			continue
		}

		// Get combined status for the PR
		headSHA := pr.GetHead().GetSHA()
		combinedStatus, _, err := githubClient.Repositories.GetCombinedStatus(ctx, owner, repoName, headSHA, nil)
		if err != nil {
			r.logger.Warnf("Failed to get combined status for PR #%d: %v", prNumber, err)
		}

		// Also check check runs
		checkRuns, _, err := githubClient.Checks.ListCheckRunsForRef(ctx, owner, repoName, headSHA, nil)
		if err != nil {
			r.logger.Warnf("Failed to get check runs for PR #%d: %v", prNumber, err)
		}

		// Check if any checks are failing
		hasFailedChecks := false
		if combinedStatus != nil && combinedStatus.GetState() == "failure" {
			hasFailedChecks = true
		}
		if checkRuns != nil {
			for _, run := range checkRuns.CheckRuns {
				if run.GetStatus() == "completed" && run.GetConclusion() == "failure" {
					hasFailedChecks = true
					break
				}
			}
		}

		if hasFailedChecks {
			fmt.Fprintln(r.stdout, "  ❌ Skipping PR due to failed checks")
			fmt.Fprintln(r.stdout, "")
			skippedCount++
			continue
		}

		// Check if already approved
		reviews, _, err := githubClient.PullRequests.ListReviews(ctx, owner, repoName, prNumber, nil)
		if err != nil {
			r.logger.Errorf("Failed to get reviews for PR #%d: %v", prNumber, err)
			fmt.Fprintf(r.stderr, "  ❌ Failed to get PR reviews: %v\n\n", err)
			skippedCount++
			continue
		}

		alreadyApproved := false
		for _, review := range reviews {
			if review.GetState() == "APPROVED" {
				alreadyApproved = true
				break
			}
		}

		if !alreadyApproved {
			if r.flag.DryRun {
				fmt.Fprintln(r.stdout, "  ✅ Would approve PR")
			} else {
				r.logger.Infof("Approving PR #%d in %s/%s", prNumber, owner, repoName)
				reviewRequest := &github.PullRequestReviewRequest{
					Event: github.String("APPROVE"),
				}
				_, _, err = githubClient.PullRequests.CreateReview(ctx, owner, repoName, prNumber, reviewRequest)
				if err != nil {
					r.logger.Errorf("Failed to approve PR #%d: %v", prNumber, err)
					fmt.Fprintf(r.stderr, "  ❌ Failed to approve PR: %v\n\n", err)
					skippedCount++
					continue
				}
				fmt.Fprintln(r.stdout, "  ✅ Approved PR")
				approvedCount++
			}
		} else {
			fmt.Fprintln(r.stdout, "  ☑️  PR already approved")
		}

		// Check if already merged
		if pr.GetMerged() {
			fmt.Fprintln(r.stdout, "  ☑️  PR is already merged")
			fmt.Fprintln(r.stdout, "")
			continue
		}

		// Check if auto-merge is already enabled
		if pr.GetAutoMerge() != nil {
			fmt.Fprintln(r.stdout, "  ☑️  Auto-merge is already enabled")
			fmt.Fprintln(r.stdout, "")
			continue
		}

		// Get repository details to determine allowed merge methods
		repo, _, err := githubClient.Repositories.Get(ctx, owner, repoName)
		if err != nil {
			r.logger.Errorf("Failed to get repository details for %s/%s: %v", owner, repoName, err)
			fmt.Fprintf(r.stderr, "  ❌ Failed to get repository details: %v\n\n", err)
			skippedCount++
			continue
		}

		// Determine the appropriate merge method
		var mergeMethod string
		if r.flag.MergeMethod != "" {
			// User specified an override - validate it's allowed
			mergeMethod = r.flag.MergeMethod
			switch mergeMethod {
			case "squash":
				if !repo.GetAllowSquashMerge() {
					fmt.Fprintf(r.stdout, "  ⚠️  Squash merge not allowed in %s/%s\n\n", owner, repoName)
					skippedCount++
					continue
				}
			case "merge":
				if !repo.GetAllowMergeCommit() {
					fmt.Fprintf(r.stdout, "  ⚠️  Merge commit not allowed in %s/%s\n\n", owner, repoName)
					skippedCount++
					continue
				}
			case "rebase":
				if !repo.GetAllowRebaseMerge() {
					fmt.Fprintf(r.stdout, "  ⚠️  Rebase merge not allowed in %s/%s\n\n", owner, repoName)
					skippedCount++
					continue
				}
			}
			r.logger.Infof("Using override merge method: %s", mergeMethod)
		} else {
			// Use repository's default preference
			if repo.GetAllowSquashMerge() {
				mergeMethod = "squash"
			} else if repo.GetAllowMergeCommit() {
				mergeMethod = "merge"
			} else if repo.GetAllowRebaseMerge() {
				mergeMethod = "rebase"
			} else {
				fmt.Fprintf(r.stdout, "  ⚠️  No merge methods are enabled for %s/%s\n\n", owner, repoName)
				skippedCount++
				continue
			}
			r.logger.Infof("Using repository default merge method: %s", mergeMethod)
		}

		// Attempt to merge the PR
		if r.flag.DryRun {
			fmt.Fprintf(r.stdout, "  ✅ Would merge with method: %s\n\n", mergeMethod)
		} else {
			r.logger.Infof("Merging PR #%d in %s/%s with method %s", prNumber, owner, repoName, mergeMethod)

			mergeOpts := &github.PullRequestOptions{
				MergeMethod: mergeMethod,
			}

			mergeResult, _, err := githubClient.PullRequests.Merge(ctx, owner, repoName, prNumber, "", mergeOpts)
			if err != nil {
				// Check if error is because PR is not mergeable yet
				if strings.Contains(err.Error(), "merge conflict") {
					fmt.Fprintln(r.stdout, "  ⚠️  PR has merge conflicts")
				} else if strings.Contains(err.Error(), "required status check") {
					fmt.Fprintln(r.stdout, "  ⏳ PR is not ready to merge (waiting for checks)")
				} else {
					r.logger.Warnf("Could not merge PR #%d: %v", prNumber, err)
					fmt.Fprintf(r.stdout, "  ⚠️  Could not merge PR: %v\n", err)
				}
			} else if mergeResult.GetMerged() {
				fmt.Fprintln(r.stdout, "  ✅ Successfully merged PR")
				mergedCount++
			}
			fmt.Fprintln(r.stdout, "")
		}
	}

	// Print summary
	fmt.Fprintln(r.stdout, "─────────────────────────────")
	fmt.Fprintln(r.stdout, "Summary:")
	if r.flag.DryRun {
		fmt.Fprintf(r.stdout, "  PRs that would be processed: %d\n", searchResults.GetTotal()-skippedCount)
		fmt.Fprintf(r.stdout, "  PRs that would be skipped: %d\n", skippedCount)
	} else {
		fmt.Fprintf(r.stdout, "  PRs approved: %d\n", approvedCount)
		fmt.Fprintf(r.stdout, "  PRs merged: %d\n", mergedCount)
		fmt.Fprintf(r.stdout, "  PRs skipped: %d\n", skippedCount)
	}

	return nil
}

