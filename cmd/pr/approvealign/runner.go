package approvealign

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v83/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/internal/env"
	"github.com/giantswarm/devctl/v7/internal/pr"
	"github.com/giantswarm/devctl/v7/pkg/githubclient"
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
	// Set logger to only show errors to avoid cluttering the table UI
	r.logger.SetLevel(logrus.ErrorLevel)

	if r.flag.DryRun {
		fmt.Fprintln(r.stdout, "🔍 DRY RUN MODE")
		fmt.Fprintln(r.stdout, "")
	}

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

	// Search for "Align files" PRs
	// Note: We don't filter by status:success here because we want to find all PRs
	// and then check/wait for their status in processPR (similar to approve-merge-renovate)
	searchQuery := `is:pr is:open archived:false org:giantswarm review-requested:@me "Align files"`

	searchOpts := &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	searchResults, _, err := githubClient.Search.Issues(ctx, searchQuery, searchOpts)
	if err != nil {
		return microerror.Maskf(executionFailedError, "failed to search for PRs: %v", err)
	}

	if searchResults.GetTotal() == 0 {
		fmt.Fprintln(r.stdout, "No PRs found.")
		return nil
	}

	// Initialize PR statuses with mutex protection for concurrent updates
	var prStatusesMu sync.Mutex
	prStatuses := make([]*pr.PRStatus, 0, len(searchResults.Issues))

	for _, issue := range searchResults.Issues {
		owner, repoName, err := pr.ParseRepoFromURL(issue.GetHTMLURL())
		if err != nil {
			continue
		}

		ps := &pr.PRStatus{
			Number:     issue.GetNumber(),
			Owner:      owner,
			Repo:       repoName,
			Title:      issue.GetTitle(),
			URL:        issue.GetHTMLURL(),
			Status:     "Queued",
			LastUpdate: time.Now(),
		}
		prStatuses = append(prStatuses, ps)
	}

	// Print table header
	pr.PrintTableHeader(r.stdout)

	// Print initial empty rows for all PRs
	for range prStatuses {
		fmt.Fprintln(r.stdout, "")
	}

	// Start processing all PRs in parallel
	var wg sync.WaitGroup
	for _, ps := range prStatuses {
		wg.Add(1)
		go func(ps *pr.PRStatus) {
			defer wg.Done()
			r.processPR(ctx, githubClient, ps)
		}(ps)
	}

	// Update display periodically until all PRs are done
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			// Final update
			prStatusesMu.Lock()
			pr.UpdateTable(r.stdout, prStatuses)
			prStatusesMu.Unlock()

			fmt.Fprintln(r.stdout, "")

			prStatusesMu.Lock()
			r.printSummary(prStatuses)
			prStatusesMu.Unlock()

			return nil

		case <-ticker.C:
			prStatusesMu.Lock()
			pr.UpdateTable(r.stdout, prStatuses)
			prStatusesMu.Unlock()
		}
	}
}

func (r *runner) processPR(ctx context.Context, githubClient *github.Client, ps *pr.PRStatus) {
	maxRetries := 60 // Poll for up to 5 minutes (60 * 5 seconds)
	retryDelay := 5 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		// Get PR details
		ps.UpdateStatus("Checking...")
		prData, _, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
		if err != nil {
			ps.UpdateStatus("Failed to get PR")
			return
		}

		// Check if already merged
		if prData.GetMerged() {
			ps.UpdateStatus("Already merged")
			return
		}

		// Check if auto-merge is enabled (for status display later)
		hasAutoMerge := prData.GetAutoMerge() != nil

		// Check status checks BEFORE checking if already approved
		headSHA := prData.GetHead().GetSHA()
		combinedStatus, _, _ := githubClient.Repositories.GetCombinedStatus(ctx, ps.Owner, ps.Repo, headSHA, nil)
		checkRuns, _, _ := githubClient.Checks.ListCheckRunsForRef(ctx, ps.Owner, ps.Repo, headSHA, nil)

		// Check if any checks are failing or pending
		hasFailedChecks := false
		checksPending := false

		// Check combined status first - this is for traditional status checks
		if combinedStatus != nil {
			state := combinedStatus.GetState()
			totalCount := combinedStatus.GetTotalCount()

			if state == "success" {
				// All required checks passed
				checksPending = false
				hasFailedChecks = false
			} else if state == "failure" || state == "error" {
				hasFailedChecks = true
			} else if state == "pending" && totalCount > 0 {
				// Only treat as pending if there are actual status checks
				// pending with totalCount=0 just means no checks exist, not that checks are waiting
				checksPending = true
			}
		}

		// Check individual check runs (GitHub Actions checks)
		if checkRuns != nil && len(checkRuns.CheckRuns) > 0 && !hasFailedChecks {
			// Only check runs if combinedStatus didn't already give us a definitive answer
			if combinedStatus == nil || (combinedStatus.GetState() != "success" && combinedStatus.GetState() != "failure") {
				for _, run := range checkRuns.CheckRuns {
					conclusion := run.GetConclusion()
					status := run.GetStatus()

					if status == "completed" {
						if conclusion == "failure" || conclusion == "cancelled" || conclusion == "timed_out" {
							hasFailedChecks = true
							break
						}
						// success, neutral, skipped are OK
					} else {
						// Check is not completed (queued, in_progress)
						checksPending = true
					}
				}
			}
		}

		if hasFailedChecks {
			ps.UpdateStatus("Failed checks")
			return
		}

		if checksPending {
			ps.UpdateStatus(fmt.Sprintf("Waiting for checks (%d/%d)", attempt+1, maxRetries))
			continue
		}

		// Checks are passing, proceed with approval
		// Check if already approved
		reviews, _, err := githubClient.PullRequests.ListReviews(ctx, ps.Owner, ps.Repo, ps.Number, nil)
		if err != nil {
			ps.UpdateStatus("Failed to get reviews")
			return
		}

		alreadyApproved := false
		for _, review := range reviews {
			if review.GetState() == "APPROVED" {
				alreadyApproved = true
				break
			}
		}

		if alreadyApproved {
			// Check if it merged after being approved
			prCheck, _, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
			if err == nil && prCheck.GetMerged() {
				ps.UpdateStatus("Merged (auto-merge)")
				return
			}

			// Check if PR needs to be updated with base branch
			if prCheck != nil && prCheck.GetMergeableState() == "behind" {
				if r.flag.DryRun {
					ps.UpdateStatus("Would update branch")
					return
				}

				ps.UpdateStatus("Updating branch...")
				_, _, err := githubClient.PullRequests.UpdateBranch(ctx, ps.Owner, ps.Repo, ps.Number, nil)
				if err != nil {
					ps.UpdateStatus("Failed to update branch")
					return
				}

				// Wait a bit for the update to process, then continue to check if it merged
				time.Sleep(3 * time.Second)
				prCheck, _, err = githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
				if err == nil && prCheck.GetMerged() {
					ps.UpdateStatus("Merged (auto-merge)")
					return
				}

				if hasAutoMerge {
					ps.UpdateStatus("Updated, queued to merge")
				} else {
					ps.UpdateStatus("Branch updated")
				}
				return
			}

			if hasAutoMerge {
				ps.UpdateStatus("Already approved, queued")
			} else {
				ps.UpdateStatus("Already approved")
			}
			return
		}

		// Approve the PR
		if r.flag.DryRun {
			if hasAutoMerge {
				ps.UpdateStatus("Would approve (auto-merge)")
			} else {
				ps.UpdateStatus("Would approve")
			}
			return
		}

		ps.UpdateStatus("Approving...")
		reviewRequest := &github.PullRequestReviewRequest{
			Event: github.String("APPROVE"),
		}
		_, _, err = githubClient.PullRequests.CreateReview(ctx, ps.Owner, ps.Repo, ps.Number, reviewRequest)
		if err != nil {
			ps.UpdateStatus("Failed to approve")
			return
		}

		// After approval, check if PR needs to be updated with base branch
		time.Sleep(2 * time.Second)
		prCheck, _, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
		if err == nil && prCheck.GetMerged() {
			ps.UpdateStatus("Merged (auto-merge)")
			return
		}

		// Check if PR branch is behind and needs updating
		if prCheck != nil && prCheck.GetMergeableState() == "behind" {
			ps.UpdateStatus("Updating branch...")
			_, _, err := githubClient.PullRequests.UpdateBranch(ctx, ps.Owner, ps.Repo, ps.Number, nil)
			if err != nil {
				ps.UpdateStatus("Failed to update branch")
				return
			}

			// Wait a bit for the update to process, then check if it merged
			time.Sleep(3 * time.Second)
			prCheck, _, err = githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
			if err == nil && prCheck.GetMerged() {
				ps.UpdateStatus("Merged (auto-merge)")
				return
			}

			if hasAutoMerge {
				ps.UpdateStatus("Updated, queued to merge")
			} else {
				ps.UpdateStatus("Branch updated")
			}
			return
		}

		// Not merged yet, not behind
		if hasAutoMerge {
			ps.UpdateStatus("Approved, queued to merge")
		} else {
			ps.UpdateStatus("Approved")
		}
		return
	}

	// Timed out waiting for checks
	ps.UpdateStatus("Timeout waiting for checks")
}

func (r *runner) printSummary(prStatuses []*pr.PRStatus) {
	merged := 0
	approved := 0
	queued := 0
	updated := 0
	skipped := 0
	failed := 0
	waiting := 0

	for _, ps := range prStatuses {
		status := ps.GetStatus()
		if strings.Contains(status, "Merged") {
			merged++
		} else if strings.Contains(status, "Updated, queued") {
			updated++
			queued++
		} else if strings.Contains(status, "Branch updated") {
			updated++
			approved++
		} else if strings.Contains(status, "queued to merge") {
			queued++
		} else if strings.Contains(status, "Approved") && !strings.Contains(status, "Would") && !strings.Contains(status, "Already") {
			approved++
		} else if strings.Contains(status, "Already") {
			skipped++
		} else if strings.Contains(status, "Failed") {
			failed++
		} else if strings.Contains(status, "Waiting") || strings.Contains(status, "Timeout") {
			waiting++
		}
	}

	fmt.Fprintln(r.stdout, "─────────────────────────────")
	fmt.Fprintln(r.stdout, "Summary:")
	if r.flag.DryRun {
		fmt.Fprintf(r.stdout, "  PRs that would be approved: %d\n", len(prStatuses)-skipped-failed-waiting)
	} else {
		if merged > 0 {
			fmt.Fprintf(r.stdout, "  PRs merged: %d\n", merged)
		}
		fmt.Fprintf(r.stdout, "  PRs approved: %d\n", approved)
		if queued > 0 {
			fmt.Fprintf(r.stdout, "  PRs queued to merge: %d\n", queued)
		}
		if updated > 0 {
			fmt.Fprintf(r.stdout, "  PRs with branch updated: %d\n", updated)
		}
	}
	fmt.Fprintf(r.stdout, "  PRs skipped: %d\n", skipped)
	if failed > 0 {
		fmt.Fprintf(r.stdout, "  PRs failed: %d\n", failed)
	}
	if waiting > 0 {
		fmt.Fprintf(r.stdout, "  PRs still waiting: %d\n", waiting)
	}
}
