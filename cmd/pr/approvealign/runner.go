package approvealign

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/pr/internal/checks"
	"github.com/giantswarm/devctl/v8/cmd/pr/internal/prcontext"
	"github.com/giantswarm/devctl/v8/internal/pr"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}
	return r.run(ctx, cmd, args)
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	token, err := authstore.ResolveGitHub(ctx)
	if err != nil {
		return err
	}
	token.WarnOnce(r.stderr)

	// Set logger to only show errors to avoid cluttering the table UI
	r.logger.SetLevel(logrus.ErrorLevel)

	if r.flag.DryRun {
		fmt.Fprintln(r.stdout, "🔍 DRY RUN MODE")
		fmt.Fprintln(r.stdout, "")
	}

	ghClientService, err := githubclient.New(githubclient.Config{
		Logger:      r.logger,
		AccessToken: token.Value,
		DryRun:      r.flag.DryRun,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	githubClient := ghClientService.GetUnderlyingClient(ctx)

	// Search for align-files PRs
	// Note: We don't filter by status:success here because we want to find all PRs
	// and then check/wait for their status in processPR (similar to approve-merge-renovate)
	searchQuery := `is:pr is:open archived:false org:giantswarm review-requested:@me in:title "` + alignTitle + `"`
	if r.flag.Team != "" {
		// Team scope: a repository that requests no reviewer never puts its
		// Align files PR into anyone's queue, so search without the review filter.
		searchQuery = `is:pr is:open archived:false org:giantswarm in:title "` + alignTitle + `"`
	}

	issues, err := r.searchIssues(ctx, githubClient, searchQuery)
	if err != nil {
		return err
	}

	if r.flag.Team != "" {
		repos, err := teamRepositories(ctx, githubClient, r.flag.Team)
		if err != nil {
			return err
		}
		issues = selectTeamPRs(issues, repos)
	}

	if len(issues) == 0 {
		fmt.Fprintln(r.stdout, "No PRs found.")
		return nil
	}

	// Initialize PR statuses with mutex protection for concurrent updates
	var prStatusesMu sync.Mutex
	prStatuses := make([]*pr.PRStatus, 0, len(issues))

	for _, issue := range issues {
		owner, repoName, err := pr.ParseRepoFromURL(issue.GetHTMLURL())
		if err != nil {
			continue
		}

		ps := &pr.PRStatus{
			Number:       issue.GetNumber(),
			Owner:        owner,
			Repo:         repoName,
			Title:        issue.GetTitle(),
			URL:          issue.GetHTMLURL(),
			Status:       "Queued",
			DisplayLabel: repoName,
			LastUpdate:   time.Now(),
		}
		prStatuses = append(prStatuses, ps)
	}

	displayedRows := 0

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
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			pr.UpdateTable(r.stdout, prStatuses)
			fmt.Fprintln(r.stdout, "")
			r.printSummary(prStatuses)
			return ctx.Err()

		case <-done:
			// Final update
			prStatusesMu.Lock()
			r.updateStatusTable(prStatuses, displayedRows, token.SourceName())
			prStatusesMu.Unlock()

			fmt.Fprintln(r.stdout, "")

			prStatusesMu.Lock()
			r.printSummary(prStatuses)
			prStatusesMu.Unlock()

			return ctx.Err()

		case <-ticker.C:
			prStatusesMu.Lock()
			displayedRows = r.updateStatusTable(prStatuses, displayedRows, token.SourceName())
			prStatusesMu.Unlock()
		}
	}
}

func (r *runner) searchIssues(ctx context.Context, githubClient *github.Client, query string) ([]*github.Issue, error) {
	opts := &github.SearchOptions{ListOptions: github.ListOptions{PerPage: 100}}

	var issues []*github.Issue
	for {
		results, resp, err := githubClient.Search.Issues(ctx, query, opts)
		if err != nil {
			return nil, microerror.Maskf(executionFailedError, "failed to search for PRs: %v", err)
		}
		for _, issue := range results.Issues {
			if strings.HasPrefix(issue.GetTitle(), alignTitle) {
				issues = append(issues, issue)
			}
		}
		if resp.NextPage == 0 {
			return issues, nil
		}
		opts.Page = resp.NextPage
	}
}

func (r *runner) processPR(ctx context.Context, githubClient *github.Client, ps *pr.PRStatus) {
	maxRetries := 60 // Poll for up to 5 minutes (60 * 5 seconds)
	retryDelay := 5 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if ctx.Err() != nil {
			ps.UpdateStatus("Cancelled")
			return
		}
		if attempt > 0 {
			if err := prcontext.Sleep(ctx, retryDelay); err != nil {
				ps.UpdateStatus("Cancelled")
				return
			}
		}

		// Get PR details
		ps.UpdateStatus("Checking...")
		prData, resp, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
		if err != nil {
			ps.UpdateStatus(readErrorStatus(failedPRRead, resp, err))
			return
		}

		// Check if already merged
		if prData.GetMerged() {
			ps.UpdateStatus("Already merged")
			return
		}

		// Search results can be stale when a PR has been renamed.
		if !strings.HasPrefix(prData.GetTitle(), alignTitle) {
			ps.UpdateStatus("Skipped: not an Align files PR")
			return
		}

		// Check if auto-merge is enabled (for status display later)
		hasAutoMerge := prData.GetAutoMerge() != nil

		// Check status checks BEFORE checking if already approved
		headSHA := prData.GetHead().GetSHA()
		checkResult, err := checks.Read(ctx, githubClient, ps.Owner, ps.Repo, headSHA)
		if err != nil {
			ps.UpdateStatus("Failed to get checks: " + err.Error())
			return
		}
		combinedStatus, checkRuns := checkResult.Combined, checkResult.Runs

		if checkResult.Failed {
			ps.UpdateStatus("Failed checks")
			return
		}

		if checkResult.Pending {
			ps.UpdateStatus(fmt.Sprintf("Waiting for checks (%d/%d)", attempt+1, maxRetries))
			continue
		}

		// Checks are passing, proceed with approval
		// Check if already approved
		reviews, resp, err := allReviews(ctx, githubClient, ps.Owner, ps.Repo, ps.Number)
		if err != nil {
			ps.UpdateStatus(readErrorStatus(failedReviewRead, resp, err))
			return
		}

		alreadyApproved := hasCurrentApproval(reviews)

		if alreadyApproved {
			// Check if it merged after being approved
			prCheck, _, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
			if err == nil && prCheck.GetMerged() {
				ps.UpdateStatus("Merged (auto-merge)")
				return
			}

			if prCheck != nil && prCheck.GetHead().GetSHA() != headSHA {
				ps.UpdateStatus("Head changed; run again")
				return
			}

			// Check if PR needs to be updated with base branch
			if prCheck != nil && prCheck.GetMergeableState() == "behind" {
				if r.flag.DryRun {
					ps.UpdateStatus("Would update branch")
					return
				}

				ps.UpdateStatus("Updating branch...")
				_, _, err := githubClient.PullRequests.UpdateBranch(ctx, ps.Owner, ps.Repo, ps.Number, github.UpdatePullRequestBranchRequest{ExpectedHeadSHA: new(headSHA)})
				if err != nil && !isGitHubAccepted(err) {
					ps.UpdateStatus("Failed to update branch")
					return
				}

				// Wait a bit for the update to process, then continue to check if it merged
				if err := prcontext.Sleep(ctx, 3*time.Second); err != nil {
					ps.UpdateStatus("Cancelled")
					return
				}
				prCheck, _, err = githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
				if err == nil && prCheck.GetMerged() {
					ps.UpdateStatus("Merged (auto-merge)")
					return
				}

				if err != nil || prCheck.GetHead().GetSHA() == "" || prCheck.GetHead().GetSHA() == headSHA {
					ps.UpdateStatus("Waiting for branch update")
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
				return
			}
			if r.flag.Team != "" {
				r.mergeApproved(ctx, githubClient, ps, prCheck, combinedStatus, checkRuns, headSHA)
				return
			}
			ps.UpdateStatus("Already approved")
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
			Event:    new("APPROVE"),
			CommitID: new(headSHA),
		}
		_, _, err = githubClient.PullRequests.CreateReview(ctx, ps.Owner, ps.Repo, ps.Number, reviewRequest)
		if err != nil {
			ps.UpdateStatus("Failed to approve")
			return
		}

		// After approval, check if PR needs to be updated with base branch
		if err := prcontext.Sleep(ctx, 2*time.Second); err != nil {
			ps.UpdateStatus("Cancelled")
			return
		}
		prCheck, _, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
		if err == nil && prCheck.GetMerged() {
			ps.UpdateStatus("Merged (auto-merge)")
			return
		}

		if prCheck != nil && prCheck.GetHead().GetSHA() != headSHA {
			ps.UpdateStatus("Head changed; run again")
			return
		}

		// Check if PR branch is behind and needs updating
		if prCheck != nil && prCheck.GetMergeableState() == "behind" {
			ps.UpdateStatus("Updating branch...")
			_, _, err := githubClient.PullRequests.UpdateBranch(ctx, ps.Owner, ps.Repo, ps.Number, github.UpdatePullRequestBranchRequest{ExpectedHeadSHA: new(headSHA)})
			if err != nil && !isGitHubAccepted(err) {
				ps.UpdateStatus("Failed to update branch")
				return
			}

			// Wait a bit for the update to process, then check if it merged
			if err := prcontext.Sleep(ctx, 3*time.Second); err != nil {
				ps.UpdateStatus("Cancelled")
				return
			}
			prCheck, _, err = githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
			if err == nil && prCheck.GetMerged() {
				ps.UpdateStatus("Merged (auto-merge)")
				return
			}

			if err != nil || prCheck.GetHead().GetSHA() == "" || prCheck.GetHead().GetSHA() == headSHA {
				ps.UpdateStatus("Waiting for branch update")
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

// mergeApproved merges an approved, green PR that carries no auto-merge, or
// names the required check that nothing produces when that is what blocks it.
func (r *runner) mergeApproved(ctx context.Context, githubClient *github.Client, ps *pr.PRStatus, prData *github.PullRequest, combined *github.CombinedStatus, runs *github.ListCheckRunsResults, headSHA string) {
	if prData == nil {
		ps.UpdateStatus("Already approved")
		return
	}

	switch prData.GetMergeableState() {
	case "clean":
	case "blocked":
		required, err := requiredContexts(ctx, githubClient, ps.Owner, ps.Repo, prData.GetBase().GetRef())
		if err == nil {
			if missing := missingContexts(required, combined, runs); len(missing) > 0 {
				ps.UpdateStatus("Blocked: required check never reported: " + strings.Join(missing, ", "))
				return
			}
		}
		ps.UpdateStatus("Already approved, blocked")
		return
	default:
		ps.UpdateStatus("Already approved, not mergeable (" + prData.GetMergeableState() + ")")
		return
	}

	if r.flag.DryRun {
		ps.UpdateStatus("Would merge")
		return
	}

	ps.UpdateStatus("Merging...")
	_, _, err := githubClient.PullRequests.Merge(ctx, ps.Owner, ps.Repo, ps.Number, "", &github.PullRequestOptions{MergeMethod: "squash", SHA: headSHA})
	if err != nil {
		ps.UpdateStatus("Failed to merge")
		return
	}
	ps.UpdateStatus("Merged")
}

func (r *runner) printSummary(prStatuses []*pr.PRStatus) {
	merged := 0
	approved := 0
	queued := 0
	updated := 0
	skipped := 0
	failed := 0
	waiting := 0
	blocked := 0
	wouldApprove := 0
	wouldMerge := 0
	wouldUpdate := 0

	for _, ps := range prStatuses {
		status := ps.GetStatus()
		// Error messages may themselves contain words such as "Merged" or
		// "Already"; classify the failure before inspecting the rest of the text.
		if strings.HasPrefix(status, "Failed") {
			failed++
		} else if strings.HasPrefix(status, "Would approve") {
			wouldApprove++
		} else if status == "Would merge" {
			wouldMerge++
		} else if status == "Would update branch" {
			wouldUpdate++
		} else if strings.Contains(status, "Merged") {
			merged++
		} else if strings.Contains(status, "Updated, queued") {
			updated++
			queued++
		} else if strings.Contains(status, "Branch updated") {
			updated++
			approved++
		} else if strings.Contains(status, "queued to merge") || status == "Already approved, queued" {
			queued++
		} else if strings.Contains(status, "Approved") && !strings.Contains(status, "Would") && !strings.Contains(status, "Already") {
			approved++
		} else if strings.Contains(status, "Blocked") || strings.Contains(status, "not mergeable") || strings.HasSuffix(status, "blocked") {
			blocked++
		} else if strings.Contains(status, "Already") || strings.HasPrefix(status, "Skipped:") {
			skipped++
		} else if strings.Contains(status, "Waiting") || strings.Contains(status, "Timeout") {
			waiting++
		}
	}

	fmt.Fprintln(r.stdout, "─────────────────────────────")
	fmt.Fprintln(r.stdout, "Summary:")
	if r.flag.DryRun {
		fmt.Fprintf(r.stdout, "  PRs that would be approved: %d\n", wouldApprove)
		if wouldMerge > 0 {
			fmt.Fprintf(r.stdout, "  PRs that would be merged: %d\n", wouldMerge)
		}
		if wouldUpdate > 0 {
			fmt.Fprintf(r.stdout, "  PR branches that would be updated: %d\n", wouldUpdate)
		}
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
	if blocked > 0 {
		fmt.Fprintf(r.stdout, "  PRs blocked: %d\n", blocked)
	}
	groups, _ := groupReadErrors(prStatuses)
	for _, group := range groups {
		fmt.Fprintf(r.stdout, "  %d PR(s): %s\n", group.count, group.status)
	}
}
