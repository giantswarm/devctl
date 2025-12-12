package approvemergerenovate

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

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

type prStatus struct {
	Number     int
	Owner      string
	Repo       string
	Title      string
	URL        string
	Status     string
	LastUpdate time.Time
	mu         sync.Mutex
}

func (ps *prStatus) UpdateStatus(status string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.Status = status
	ps.LastUpdate = time.Now()
}

func (ps *prStatus) GetStatus() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.Status
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}
	return r.run(ctx, cmd, args)
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

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	// Set logger to only show errors to avoid cluttering the table UI
	r.logger.SetLevel(logrus.ErrorLevel)

	// Validate that a query argument was provided
	if len(args) == 0 {
		return microerror.Maskf(invalidFlagsError, "query argument is required\n\nUsage: devctl pr approve-merge-renovate <query>\n\nExample:\n  devctl pr approve-merge-renovate \"architect v1.2.3\"\n")
	}

	query := args[0]

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

	// Build search query
	searchQuery := fmt.Sprintf("%s is:pr is:open archived:false review-requested:@me author:app/renovate", query)

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
	prStatuses := make([]*prStatus, 0, len(searchResults.Issues))
	prNumbersMap := make(map[int]bool) // Track PR numbers to avoid duplicates

	addPRs := func(issues []*github.Issue) []*prStatus {
		prStatusesMu.Lock()
		defer prStatusesMu.Unlock()

		newPRs := make([]*prStatus, 0)
		for _, issue := range issues {
			prNumber := issue.GetNumber()

			// Skip if we already have this PR
			if prNumbersMap[prNumber] {
				continue
			}

			owner, repoName, err := parseRepoFromURL(issue.GetHTMLURL())
			if err != nil {
				continue
			}

			ps := &prStatus{
				Number:     prNumber,
				Owner:      owner,
				Repo:       repoName,
				Title:      issue.GetTitle(),
				URL:        issue.GetHTMLURL(),
				Status:     "Queued",
				LastUpdate: time.Now(),
			}
			prStatuses = append(prStatuses, ps)
			prNumbersMap[prNumber] = true
			newPRs = append(newPRs, ps)
		}
		return newPRs
	}

	// Add initial PRs
	initialPRs := addPRs(searchResults.Issues)

	// Print table header
	r.printTableHeader()

	// Print initial empty rows for all PRs
	for range initialPRs {
		fmt.Fprintln(r.stdout, "")
	}

	// Start processing initial PRs in parallel
	var wg sync.WaitGroup
	for _, ps := range initialPRs {
		wg.Add(1)
		go func(ps *prStatus) {
			defer wg.Done()
			r.processPR(ctx, githubClient, ps)
		}(ps)
	}

	// Start background goroutine to poll for new PRs every 10 seconds
	stopPolling := make(chan bool)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-stopPolling:
				return
			case <-ticker.C:
				// Re-run the search query
				newResults, _, err := githubClient.Search.Issues(ctx, searchQuery, searchOpts)
				if err != nil {
					continue
				}

				// Add any new PRs found
				newPRs := addPRs(newResults.Issues)
				if len(newPRs) > 0 {
					// Add empty rows for new PRs
					prStatusesMu.Lock()
					for range newPRs {
						fmt.Fprintln(r.stdout, "")
					}
					prStatusesMu.Unlock()

					// Start processing new PRs
					for _, ps := range newPRs {
						wg.Add(1)
						go func(ps *prStatus) {
							defer wg.Done()
							r.processPR(ctx, githubClient, ps)
						}(ps)
					}
				}
			}
		}
	}()

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
			// Stop polling for new PRs
			close(stopPolling)

			// Final update
			prStatusesMu.Lock()
			r.updateTable(prStatuses)
			prStatusesMu.Unlock()

			fmt.Fprintln(r.stdout, "")

			prStatusesMu.Lock()
			r.printSummary(prStatuses)
			prStatusesMu.Unlock()

			return nil
		case <-ticker.C:
			prStatusesMu.Lock()
			r.updateTable(prStatuses)
			prStatusesMu.Unlock()
		}
	}
}

func (r *runner) printTableHeader() {
	header := fmt.Sprintf("%-7s %-40s %-30s", "PR", "Repository", "Status")
	fmt.Fprintln(r.stdout, header)
	fmt.Fprintln(r.stdout, strings.Repeat("─", 80))
}

func (r *runner) updateTable(prStatuses []*prStatus) {
	// Move cursor up to redraw table
	if len(prStatuses) > 0 {
		fmt.Fprintf(r.stdout, "\033[%dA", len(prStatuses))
	}

	for _, ps := range prStatuses {
		// Pad PR number to consistent width (6 chars for "#12345")
		prText := fmt.Sprintf("#%-5d", ps.Number)
		prLink := makeHyperlink(ps.URL, prText)
		status := ps.GetStatus()

		// Don't use padding in format string for hyperlink, just add spaces after
		line := fmt.Sprintf("%s  %-40s %-30s", prLink, ps.Repo, status)
		// Clear line and print
		fmt.Fprintf(r.stdout, "\033[2K%s\n", line)
	}
}

func (r *runner) processPR(ctx context.Context, githubClient *github.Client, ps *prStatus) {
	maxRetries := 60 // Poll for up to 5 minutes (60 * 5 seconds)
	retryDelay := 5 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		// Get PR details
		ps.UpdateStatus("Checking...")
		pr, _, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
		if err != nil {
			ps.UpdateStatus("Failed to get PR")
			return
		}

		// Check if already merged
		if pr.GetMerged() {
			ps.UpdateStatus("Already merged")
			return
		}

		// Check status checks BEFORE checking auto-merge
		// This way we report failed checks even if auto-merge is enabled
		headSHA := pr.GetHead().GetSHA()
		combinedStatus, _, _ := githubClient.Repositories.GetCombinedStatus(ctx, ps.Owner, ps.Repo, headSHA, nil)
		checkRuns, _, _ := githubClient.Checks.ListCheckRunsForRef(ctx, ps.Owner, ps.Repo, headSHA, nil)

		// Check if any checks are failing
		hasFailedChecks := false
		checksPending := false

		if combinedStatus != nil {
			if combinedStatus.GetState() == "failure" {
				hasFailedChecks = true
			} else if combinedStatus.GetState() == "pending" {
				checksPending = true
			}
		}

		if checkRuns != nil {
			for _, run := range checkRuns.CheckRuns {
				if run.GetStatus() == "completed" && run.GetConclusion() == "failure" {
					hasFailedChecks = true
					break
				}
				if run.GetStatus() != "completed" {
					checksPending = true
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

		hasAutoMerge := pr.GetAutoMerge() != nil

		if !alreadyApproved {
			if r.flag.DryRun {
				if hasAutoMerge {
					ps.UpdateStatus("Would approve (auto-merge)")
					return
				}
				ps.UpdateStatus("Would approve & merge")
			} else {
				ps.UpdateStatus("Approving...")
				reviewRequest := &github.PullRequestReviewRequest{
					Event: github.String("APPROVE"),
				}
				_, _, err = githubClient.PullRequests.CreateReview(ctx, ps.Owner, ps.Repo, ps.Number, reviewRequest)
				if err != nil {
					ps.UpdateStatus("Failed to approve")
					return
				}

				if hasAutoMerge {
					// Wait for auto-merge to complete
					ps.UpdateStatus("Approved, waiting for auto-merge...")
					for waitAttempt := 0; waitAttempt < 12; waitAttempt++ { // Wait up to 1 minute
						time.Sleep(5 * time.Second)

						// Check if merged
						prCheck, _, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
						if err == nil && prCheck.GetMerged() {
							ps.UpdateStatus("Merged (auto-merge)")
							return
						}

						ps.UpdateStatus(fmt.Sprintf("Waiting for auto-merge (%d/12)", waitAttempt+1))
					}
					ps.UpdateStatus("Approved (auto-merge pending)")
					return
				}
				ps.UpdateStatus("Approved")
			}
		} else if hasAutoMerge {
			// Already approved and has auto-merge - check if it will merge
			ps.UpdateStatus("Waiting for auto-merge...")
			for waitAttempt := 0; waitAttempt < 12; waitAttempt++ {
				time.Sleep(5 * time.Second)

				prCheck, _, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
				if err == nil && prCheck.GetMerged() {
					ps.UpdateStatus("Merged (auto-merge)")
					return
				}

				ps.UpdateStatus(fmt.Sprintf("Waiting for auto-merge (%d/12)", waitAttempt+1))
			}
			ps.UpdateStatus("Auto-merge enabled")
			return
		}

		// Only proceed to merge if it doesn't have auto-merge
		// (auto-merge PRs are handled above after approval)
		if hasAutoMerge {
			// Should not reach here, but just in case
			ps.UpdateStatus("Auto-merge enabled")
			return
		}

		// Get repository details for merge method
		repo, _, err := githubClient.Repositories.Get(ctx, ps.Owner, ps.Repo)
		if err != nil {
			ps.UpdateStatus("Failed to get repo")
			return
		}

		// Determine merge method from repository settings
		var mergeMethod string
		if repo.GetAllowSquashMerge() {
			mergeMethod = "squash"
		} else if repo.GetAllowMergeCommit() {
			mergeMethod = "merge"
		} else if repo.GetAllowRebaseMerge() {
			mergeMethod = "rebase"
		} else {
			ps.UpdateStatus("No merge methods")
			return
		}

		// Attempt to merge
		if r.flag.DryRun {
			ps.UpdateStatus(fmt.Sprintf("Would merge (%s)", mergeMethod))
			return
		}

		ps.UpdateStatus(fmt.Sprintf("Merging (%s)...", mergeMethod))
		mergeOpts := &github.PullRequestOptions{
			MergeMethod: mergeMethod,
		}

		mergeResult, _, err := githubClient.PullRequests.Merge(ctx, ps.Owner, ps.Repo, ps.Number, "", mergeOpts)
		if err != nil {
			if strings.Contains(err.Error(), "merge conflict") {
				ps.UpdateStatus("Merge conflicts")
				return
			} else if strings.Contains(err.Error(), "required status check") {
				ps.UpdateStatus(fmt.Sprintf("Waiting checks (%d/%d)", attempt+1, maxRetries))
				continue
			} else {
				ps.UpdateStatus("Merge failed")
				return
			}
		}

		if mergeResult.GetMerged() {
			ps.UpdateStatus(fmt.Sprintf("Merged (%s)", mergeMethod))
			return
		}
	}

	ps.UpdateStatus("Timeout waiting")
}

func (r *runner) printSummary(prStatuses []*prStatus) {
	merged := 0
	approved := 0
	skipped := 0
	failed := 0
	waiting := 0

	for _, ps := range prStatuses {
		status := ps.GetStatus()
		if strings.Contains(status, "Merged") {
			merged++
		} else if strings.Contains(status, "Approved") && !strings.Contains(status, "Would") {
			approved++
		} else if strings.Contains(status, "Already") || strings.Contains(status, "Auto-merge enabled") {
			skipped++
		} else if strings.Contains(status, "Failed") || strings.Contains(status, "conflicts") || strings.Contains(status, "not allowed") {
			failed++
		} else if strings.Contains(status, "Waiting") || strings.Contains(status, "Timeout") {
			waiting++
		}
	}

	fmt.Fprintln(r.stdout, "─────────────────────────────")
	fmt.Fprintln(r.stdout, "Summary:")
	if r.flag.DryRun {
		fmt.Fprintf(r.stdout, "  PRs that would be processed: %d\n", len(prStatuses)-skipped-failed)
	} else {
		fmt.Fprintf(r.stdout, "  PRs merged: %d\n", merged)
		fmt.Fprintf(r.stdout, "  PRs approved: %d\n", approved)
	}
	fmt.Fprintf(r.stdout, "  PRs skipped: %d\n", skipped)
	fmt.Fprintf(r.stdout, "  PRs failed: %d\n", failed)
	if waiting > 0 {
		fmt.Fprintf(r.stdout, "  PRs still waiting: %d\n", waiting)
	}
}
