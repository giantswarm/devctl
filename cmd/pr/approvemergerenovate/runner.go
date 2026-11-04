package approvemergerenovate

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
	"github.com/manifoldco/promptui"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/pr/internal/checks"
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
	ctx := context.Background()
	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}
	return r.run(ctx, cmd, args)
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	token, err := authstore.ResolveGitHub(ctx)
	if err != nil {
		return err
	}
	token.WarnOnce(r.stderr)

	// Set logger to only show errors to avoid cluttering the table UI
	r.logger.SetLevel(logrus.ErrorLevel)

	ghClientService, err := githubclient.New(githubclient.Config{
		Logger:      r.logger,
		AccessToken: token.Value,
		DryRun:      r.flag.DryRun,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	githubClient := ghClientService.GetUnderlyingClient(ctx)

	var query string
	if len(args) == 0 {
		// Interactive mode: let user select from grouped PRs
		selectedQuery, err := r.selectGroupInteractively(ctx, githubClient)
		if err != nil {
			return microerror.Mask(err)
		}
		query = selectedQuery
	} else {
		// Direct mode: use provided query
		query = args[0]
	}

	if r.flag.DryRun {
		fmt.Fprintln(r.stdout, "🔍 DRY RUN MODE")
		fmt.Fprintln(r.stdout, "")
	}

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
		if !r.flag.Watch {
			fmt.Fprintln(r.stdout, "No PRs found.")
			return nil
		}
		// In watch mode, continue even if no PRs found initially
		fmt.Fprintln(r.stdout, "No PRs found yet. Watching for new PRs...")
		fmt.Fprintln(r.stdout, "")
	}

	// Initialize PR statuses with mutex protection for concurrent updates
	var prStatusesMu sync.Mutex
	prStatuses := make([]*pr.PRStatus, 0, len(searchResults.Issues))
	type prKey struct {
		owner, repo string
		number      int
	}
	seenPRs := make(map[prKey]bool)

	addPRs := func(issues []*github.Issue) []*pr.PRStatus {
		prStatusesMu.Lock()
		defer prStatusesMu.Unlock()

		newPRs := make([]*pr.PRStatus, 0)
		for _, issue := range issues {
			prNumber := issue.GetNumber()

			owner, repoName, err := pr.ParseRepoFromURL(issue.GetHTMLURL())
			if err != nil {
				continue
			}

			// PR numbers are unique only within a repository.
			key := prKey{owner: strings.ToLower(owner), repo: strings.ToLower(repoName), number: prNumber}
			if seenPRs[key] {
				continue
			}

			title := issue.GetTitle()
			displayLabel := repoName
			if r.flag.Grouping == GroupingRepo {
				displayLabel = pr.ExtractDependencyName(title)
			}

			ps := &pr.PRStatus{
				Number:       prNumber,
				Owner:        owner,
				Repo:         repoName,
				Title:        title,
				URL:          issue.GetHTMLURL(),
				Status:       "Queued",
				DisplayLabel: displayLabel,
				LastUpdate:   time.Now(),
			}
			prStatuses = append(prStatuses, ps)
			seenPRs[key] = true
			newPRs = append(newPRs, ps)
		}
		return newPRs
	}

	// Add initial PRs
	initialPRs := addPRs(searchResults.Issues)

	columnHeader := "Repository"
	if r.flag.Grouping == GroupingRepo {
		columnHeader = "Dependency"
	}
	pr.PrintTableHeader(r.stdout, columnHeader)

	// Print initial empty rows for all PRs
	for range initialPRs {
		fmt.Fprintln(r.stdout, "")
	}

	// Start processing initial PRs in parallel
	var wg sync.WaitGroup
	work := newActivity(len(initialPRs), r.flag.Watch)
	for _, ps := range initialPRs {
		wg.Add(1)
		go func(ps *pr.PRStatus) {
			defer wg.Done()
			defer work.finishPR()
			r.processPR(ctx, githubClient, ps)
		}(ps)
	}

	// Start background goroutine to poll for new PRs
	// Interval: 10 seconds in normal mode, 1 minute in watch mode
	pollInterval := 10 * time.Second
	if r.flag.Watch {
		pollInterval = 1 * time.Minute
	}

	stopPolling := make(chan bool)
	pollingDone := make(chan struct{})
	go func() {
		defer close(pollingDone)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-stopPolling:
				return
			case <-ticker.C:
				if !work.beginPoll() {
					return
				}
				// Re-run the search query
				newResults, _, err := githubClient.Search.Issues(ctx, searchQuery, searchOpts)
				if err != nil {
					work.endPoll(0)
					continue
				}

				// Add any new PRs found
				newPRs := addPRs(newResults.Issues)
				work.endPoll(len(newPRs))
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
						go func(ps *pr.PRStatus) {
							defer wg.Done()
							defer work.finishPR()
							r.processPR(ctx, githubClient, ps)
						}(ps)
					}
				}
			}
		}
	}()

	// Setup signal handling for graceful shutdown (Ctrl+C)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Update display periodically until all PRs are done (or forever in watch mode)
	done := work.done

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	if r.flag.Watch {
		fmt.Fprintln(r.stdout, "👁️  Watch mode enabled - monitoring for new PRs every minute (press Ctrl+C to exit)")
		fmt.Fprintln(r.stdout, "")
	}

	for {
		select {
		case <-sigChan:
			// User pressed Ctrl+C
			fmt.Fprintln(r.stdout, "\n\n⏹️  Interrupted by user")

			// Stop polling
			close(stopPolling)

			// Final update
			prStatusesMu.Lock()
			pr.UpdateTable(r.stdout, prStatuses)
			prStatusesMu.Unlock()

			fmt.Fprintln(r.stdout, "")

			prStatusesMu.Lock()
			r.printSummary(prStatuses)
			prStatusesMu.Unlock()

			return nil

		case <-done:
			// All current PRs processed (only happens in non-watch mode)
			// Stop polling for new PRs
			close(stopPolling)
			<-pollingDone
			wg.Wait()

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

func (r *runner) selectGroupInteractively(ctx context.Context, githubClient *github.Client) (string, error) {
	fmt.Fprintln(r.stdout, "Fetching Renovate PRs...")

	// Search for all Renovate PRs requesting review from the user
	searchQuery := "is:pr is:open archived:false review-requested:@me author:app/renovate"
	searchOpts := &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	searchResults, _, err := githubClient.Search.Issues(ctx, searchQuery, searchOpts)
	if err != nil {
		return "", microerror.Maskf(executionFailedError, "failed to search for PRs: %v", err)
	}

	if searchResults.GetTotal() == 0 {
		return "", microerror.Maskf(executionFailedError, "no Renovate PRs found requesting your review")
	}

	// Convert GitHub issues to PRInfo
	var prInfos []*pr.PRInfo
	for _, issue := range searchResults.Issues {
		owner, repoName, err := pr.ParseRepoFromURL(issue.GetHTMLURL())
		if err != nil {
			continue
		}

		prInfos = append(prInfos, &pr.PRInfo{
			Number: issue.GetNumber(),
			Owner:  owner,
			Repo:   repoName,
			Title:  issue.GetTitle(),
			URL:    issue.GetHTMLURL(),
		})
	}

	// Group PRs by dependency or repository
	var groups []*pr.PRGroup
	if r.flag.Grouping == GroupingRepo {
		groups = pr.GroupRenovatePRsByRepo(prInfos)
	} else {
		groups = pr.GroupRenovatePRs(prInfos)
	}

	if len(groups) == 0 {
		return "", microerror.Maskf(executionFailedError, "no PR groups found")
	}

	fmt.Fprintf(r.stdout, "Found %d PRs in %d groups.\n\n", len(prInfos), len(groups))

	// Create promptui selector
	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "▸ {{ .Name | cyan }} ({{ len .PRs }} PRs)",
		Inactive: "  {{ .Name }} ({{ len .PRs }} PRs)",
		Selected: "✓ {{ .Name | green }} ({{ len .PRs }} PRs)",
	}

	promptLabel := "Select a dependency group to process"
	if r.flag.Grouping == GroupingRepo {
		promptLabel = "Select a repository to process"
	}

	prompt := promptui.Select{
		Label:     promptLabel,
		Items:     groups,
		Templates: templates,
		Size:      15,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		if err == promptui.ErrInterrupt {
			return "", microerror.Maskf(executionFailedError, "selection cancelled by user")
		}
		return "", microerror.Maskf(executionFailedError, "selection failed: %v", err)
	}

	selectedGroup := groups[idx]
	fmt.Fprintln(r.stdout, "")

	return selectedGroup.SearchQuery, nil
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
		checkResult, err := checks.Read(ctx, githubClient, ps.Owner, ps.Repo, headSHA)
		if err != nil {
			ps.UpdateStatus("Failed to get checks: " + err.Error())
			return
		}

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
		var reviews []*github.PullRequestReview
		reviewOpts := &github.ListOptions{PerPage: 100}
		for {
			page, resp, err := githubClient.PullRequests.ListReviews(ctx, ps.Owner, ps.Repo, ps.Number, reviewOpts)
			if err != nil {
				ps.UpdateStatus("Failed to get reviews")
				return
			}
			reviews = append(reviews, page...)
			if resp.NextPage == 0 {
				break
			}
			reviewOpts.Page = resp.NextPage
		}

		alreadyApproved := false
		if len(reviews) > 0 {
			user, _, err := githubClient.Users.Get(ctx, "")
			if err != nil || user.GetLogin() == "" {
				ps.UpdateStatus("Failed to get reviewer")
				return
			}
			// Reviews are chronological. Only the caller's latest decisive
			// review state can replace their requested approval; comments do
			// not change that state.
			for _, review := range reviews {
				if !strings.EqualFold(review.GetUser().GetLogin(), user.GetLogin()) {
					continue
				}
				switch review.GetState() {
				case "APPROVED":
					alreadyApproved = true
				case "CHANGES_REQUESTED", "DISMISSED":
					alreadyApproved = false
				}
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
					Event:    new("APPROVE"),
					CommitID: new(headSHA),
				}
				_, _, err = githubClient.PullRequests.CreateReview(ctx, ps.Owner, ps.Repo, ps.Number, reviewRequest)
				if err != nil {
					ps.UpdateStatus("Failed to approve")
					return
				}

				if hasAutoMerge {
					// Approve and let auto-merge/merge queue handle it
					ps.UpdateStatus("Approved, checking merge status...")
					time.Sleep(5 * time.Second)

					// Check once if it merged immediately
					prCheck, _, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
					if err == nil && prCheck.GetMerged() {
						ps.UpdateStatus("Merged (auto-merge)")
						return
					}

					// Not merged yet - likely in merge queue or waiting for other reasons
					ps.UpdateStatus("Queued to merge")
					return
				}
				ps.UpdateStatus("Approved")
			}
		} else if hasAutoMerge {
			// Already approved with auto-merge - check once if merged
			prCheck, _, err := githubClient.PullRequests.Get(ctx, ps.Owner, ps.Repo, ps.Number)
			if err == nil && prCheck.GetMerged() {
				ps.UpdateStatus("Merged (auto-merge)")
				return
			}

			// Not merged yet - queued
			ps.UpdateStatus("Queued to merge")
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
			SHA:         headSHA,
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

func (r *runner) printSummary(prStatuses []*pr.PRStatus) {
	merged := 0
	approved := 0
	queued := 0
	skipped := 0
	failed := 0
	waiting := 0

	for _, ps := range prStatuses {
		status := ps.GetStatus()
		if strings.Contains(status, "Merged") {
			merged++
		} else if strings.Contains(status, "Queued to merge") {
			queued++
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
		if queued > 0 {
			fmt.Fprintf(r.stdout, "  PRs queued to merge: %d\n", queued)
		}
	}
	fmt.Fprintf(r.stdout, "  PRs skipped: %d\n", skipped)
	fmt.Fprintf(r.stdout, "  PRs failed: %d\n", failed)
	if waiting > 0 {
		fmt.Fprintf(r.stdout, "  PRs still waiting: %d\n", waiting)
	}
}
