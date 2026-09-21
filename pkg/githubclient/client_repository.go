package githubclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

func (c *Client) ListRepositories(ctx context.Context, owner string) ([]Repository, error) {
	c.logger.Infof("listing repositories for owner %#q", owner)

	underlyingClient := c.GetUnderlyingClient(ctx)

	var repos []Repository
	{
		opt := &github.RepositoryListByOrgOptions{
			ListOptions: github.ListOptions{
				PerPage: 500,
			},

			Type: "all",
		}
		for pageCnt := 0; ; pageCnt++ {
			c.logger.Infof("listing page %d of repositories for owner %#q", pageCnt, owner)

			pageRepos, resp, err := underlyingClient.Repositories.ListByOrg(ctx, owner, opt)
			if err != nil {
				return nil, microerror.Mask(err)
			}

			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage

			for _, pageRepo := range pageRepos {
				r, err := newRepository(pageRepo, owner)
				if err != nil {
					return nil, microerror.Mask(err)
				}

				repos = append(repos, r)
			}

			c.logger.Infof("listed page %d of %d repositories for owner %#q", pageCnt, len(pageRepos), owner)
		}
	}

	c.logger.Infof("listed %d repositories for owner %#q", len(repos), owner)

	return repos, nil
}

func (c *Client) GetRepository(ctx context.Context, owner, repo string) (*github.Repository, error) {
	c.logger.Infof("get repository details for \"%s/%s\"", owner, repo)

	underlyingClient := c.GetUnderlyingClient(ctx)

	repository, response, err := underlyingClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		if response != nil && response.Response != nil && response.StatusCode == http.StatusNotFound {
			return nil, microerror.Mask(notFoundError)
		}
		return nil, microerror.Mask(err)
	}

	b, _ := json.MarshalIndent(repository, "", "  ")
	c.logger.Debugf("repository details\n%s", b)

	return repository, nil
}

func (c *Client) SetRepositorySettings(ctx context.Context, repository, repositorySettings *github.Repository) (*github.Repository, error) {
	c.logger.Info("configure repository settings")
	b, _ := json.MarshalIndent(repositorySettings, "", "  ")
	c.logger.Debugf("repository settings\n%s", b)

	// Features
	repository.HasWiki = repositorySettings.HasWiki
	repository.HasIssues = repositorySettings.HasIssues
	repository.HasProjects = repositorySettings.HasProjects
	repository.Archived = repositorySettings.Archived

	// Merge settings
	repository.AllowMergeCommit = repositorySettings.AllowMergeCommit
	repository.AllowSquashMerge = repositorySettings.AllowSquashMerge
	repository.AllowRebaseMerge = repositorySettings.AllowRebaseMerge

	// Pull Requests
	repository.AllowUpdateBranch = repositorySettings.AllowUpdateBranch
	repository.AllowAutoMerge = repositorySettings.AllowAutoMerge
	repository.DeleteBranchOnMerge = repositorySettings.DeleteBranchOnMerge

	// This is required since Github does not allow overrides for flags specified
	// at organization level.
	// Otherwise you will run into the following error:
	// HTTP 422 This organization does not allow private repository forking
	repository.AllowForking = nil

	if c.dryRun {
		c.logger.Debug("configured repository settings")
		return repository, nil
	}

	underlyingClient := c.GetUnderlyingClient(ctx)
	repository, _, err := underlyingClient.Repositories.Edit(ctx, repository.GetOwner().GetLogin(), repository.GetName(), repository)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	c.logger.Debug("configured repository settings")

	return repository, nil
}

func (c *Client) SetRepositoryPermissions(ctx context.Context, repository *github.Repository, permissions map[string]string) error {
	org := repository.GetOrganization().GetLogin()
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()

	c.logger.Info("grant permission on repository")
	c.logger.Debugf("permissions\n%v", permissions)

	underlyingClient := c.GetUnderlyingClient(ctx)

	for teamSlug, permission := range permissions {

		opt := &github.TeamAddTeamRepoOptions{Permission: permission}

		c.logger.Debugf("grant %q permission to %q", permission, teamSlug)

		_, err := underlyingClient.Teams.AddTeamRepoBySlug(ctx, org, teamSlug, owner, repo, opt)
		if err != nil {
			return microerror.Mask(err)
		}

		c.logger.Debugf("granted %q permission to %q", permission, teamSlug)
	}

	input := &github.DefaultWorkflowPermissionRepository{
		DefaultWorkflowPermissions: new("write"),
	}
	_, _, err := underlyingClient.Repositories.UpdateDefaultWorkflowPermissions(ctx, owner, repo, *input)
	if err != nil {
		return microerror.Mask(err)
	}
	c.logger.Debug("set default workflow permissions to write")

	c.logger.Debug("granted permission on repository")

	return nil
}

func (c *Client) SetRepositoryBranchProtection(ctx context.Context, repository *github.Repository, checkNames []string, checksFilter *regexp.Regexp) (err error) {
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()
	default_branch := repository.GetDefaultBranch()

	False := false

	c.logger.Infof("configure protection for %q branch", default_branch)

	opts := &github.ProtectionRequest{
		RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
			RequiredApprovingReviewCount: 1,
		},
		AllowForcePushes: &False,
		AllowDeletions:   &False,
		EnforceAdmins:    true,
	}

	if checkNames == nil {
		checkNames, err = c.getGithubChecks(ctx, repository, default_branch, checksFilter)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// We can only set RequiredStatusChecks when there is at least 1 check available.
	// Otherwise we hit a HTTP 422 Invalid request.
	if len(checkNames) > 0 {
		var checks []*github.RequiredStatusCheck
		for _, checkName := range checkNames {
			c := &github.RequiredStatusCheck{
				Context: checkName,
			}
			checks = append(checks, c)
		}

		opts.RequiredStatusChecks = &github.RequiredStatusChecks{
			Strict: true,
			Checks: &checks,
		}
	}

	b, _ := json.MarshalIndent(opts, "", "  ")
	c.logger.Debugf("branch protection settings\n%s", b)

	underlyingClient := c.GetUnderlyingClient(ctx)
	_, _, err = underlyingClient.Repositories.UpdateBranchProtection(ctx, owner, repo, default_branch, opts)
	if err != nil {
		return microerror.Mask(err)
	}

	c.logger.Debugf("configured protection for %q branch", default_branch)

	return nil
}

func (c *Client) RemoveRepositoryBranchProtection(ctx context.Context, repository *github.Repository) (err error) {
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()
	default_branch := repository.GetDefaultBranch()

	c.logger.Info("disable branch protection")

	underlyingClient := c.GetUnderlyingClient(ctx)
	_, _, err = underlyingClient.Repositories.GetBranchProtection(ctx, owner, repo, default_branch)
	if err != nil {
		if errors.Is(err, github.ErrBranchNotProtected) {
			// Branch has no protection set, no need to remove it.
			c.logger.Debugf("leaving branch %q without protection", default_branch)
			return nil
		}
		return microerror.Mask(err)
	}

	_, err = underlyingClient.Repositories.RemoveBranchProtection(ctx, owner, repo, default_branch)
	if err != nil {
		return microerror.Mask(err)
	}

	c.logger.Debugf("disabled protection for %q branch", default_branch)

	return nil
}

// ReportedChecks returns the names of the commit status contexts and the
// completed, non-skipped check runs observed on the heads of the most
// recently merged pull requests of branch: the checks that demonstrably gate
// a pull request in this repository. Callers use it to require a check only
// once it exists (devctl repo checks --checks-if-reported). notFoundError
// when no pull request has been merged yet: nothing has reported.
func (c *Client) ReportedChecks(ctx context.Context, repository *github.Repository, branch string) ([]string, error) {
	checks, err := c.getGithubChecks(ctx, repository, branch, nil)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return checks, nil
}

// recentMergedPRsForChecks is how many recently merged pull request heads
// are inspected for the reported checks: the newest one. The checks that
// gated the last merge are the checks the repository has; a head costs two
// requests, its statuses and its check runs, and a check of a repository
// has a budget of twenty. A check a single pull request may skip is
// declared in the entry's requiredChecks, which needs no report.
const recentMergedPRsForChecks = 1

func (c *Client) getGithubChecks(ctx context.Context, repository *github.Repository, branch string, checksFilter *regexp.Regexp) ([]string, error) {
	seen := make(map[string]bool)
	var checks []string

	addCheck := func(name string) {
		if seen[name] {
			return
		}
		if checksFilter != nil && checksFilter.MatchString(name) {
			return
		}
		seen[name] = true
		checks = append(checks, name)
	}

	refs, err := c.collectChecksRefs(ctx, repository, branch)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	for _, ref := range refs {
		c.logger.Debugf("get commit statuses and check runs for ref: %q", ref)
		if err := c.collectChecksForRef(ctx, repository, ref, addCheck); err != nil {
			return nil, microerror.Mask(err)
		}
	}

	c.logger.Debugf("found %d checks across %d refs:", len(checks), len(refs))
	for id, check := range checks {
		c.logger.Debugf(" - checks[%d] = %q", id, check)
	}

	return checks, nil
}

// collectChecksRefs returns the commit SHAs to inspect for the reported
// checks: the heads of the most recently merged pull requests, newest first.
// A pull request's head carries every check that gates a pull request, the
// CircleCI statuses of its branch and the check runs of the `pull_request`
// and `push` workflows alike; the default branch is not inspected, since a
// check that reports only on a push to it or on a tag never gates a pull
// request, and on an auto-released repository its every commit is a tag.
// notFoundError when no pull request has been merged.
func (c *Client) collectChecksRefs(ctx context.Context, repository *github.Repository, branch string) ([]string, error) {
	refs, err := c.getRecentMergedPRHeads(ctx, repository, branch, recentMergedPRsForChecks)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if len(refs) == 0 {
		return nil, microerror.Maskf(notFoundError, "%s/%s: no pull request merged into %s yet", repository.GetOwner().GetLogin(), repository.GetName(), branch)
	}
	return refs, nil
}

func (c *Client) collectChecksForRef(ctx context.Context, repository *github.Repository, ref string, addCheck func(string)) error {
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()
	underlyingClient := c.GetUnderlyingClient(ctx)

	// Legacy commit statuses (CircleCI, etc.)
	{
		opt := &github.ListOptions{PerPage: 100}
		for {
			combinedStatus, resp, err := underlyingClient.Repositories.GetCombinedStatus(ctx, owner, repo, ref, opt)
			if err != nil {
				return microerror.Mask(err)
			}
			for _, status := range combinedStatus.Statuses {
				addCheck(status.GetContext())
			}
			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage
		}
	}

	// GitHub Actions check runs.
	{
		completed := "completed"
		opt := &github.ListCheckRunsOptions{
			Status:      &completed,
			ListOptions: github.ListOptions{PerPage: 100},
		}
		for {
			results, resp, err := underlyingClient.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, opt)
			if err != nil {
				return microerror.Mask(err)
			}
			for _, run := range results.CheckRuns {
				// A check_run reported as `skipped` on the observed commit gives us no
				// signal that it gates PRs -- reusable release workflows (release-please,
				// auto-release) commonly emit skipped jobs on every push to the default
				// branch. Requiring such a check produces an unsatisfiable gate.
				if run.GetConclusion() == "skipped" {
					continue
				}
				addCheck(run.GetName())
			}
			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage
		}
	}

	return nil
}

// getRecentMergedPRHeads returns the head SHAs of up to n most recently merged
// pull requests, newest first. Closed-without-merge PRs are skipped: their
// check_runs typically reflect a failed gate and we don't want failed checks
// leaking into the required-checks list.
// getRecentMergedPRHeads returns the head SHAs of up to n pull requests
// merged into branch, newest first, from one page of the thirty most
// recently updated closed pull requests: one request, whatever the
// repository's history.
func (c *Client) getRecentMergedPRHeads(ctx context.Context, repository *github.Repository, branch string, n int) ([]string, error) {
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()

	prs, _, err := c.GetUnderlyingClient(ctx).PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		State:       "closed",
		Base:        branch,
		Sort:        "updated",
		Direction:   "desc",
		ListOptions: github.ListOptions{PerPage: 30},
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	var heads []string
	for _, pr := range prs {
		if pr.MergedAt == nil {
			continue
		}
		heads = append(heads, pr.GetHead().GetSHA())
		if len(heads) >= n {
			break
		}
	}
	return heads, nil
}

func (c *Client) SetRepositoryDefaultBranch(ctx context.Context, repository *github.Repository, newDefaultBranch string) (err error) {
	currentDefaultBranch := repository.GetDefaultBranch()
	if currentDefaultBranch != newDefaultBranch {
		owner := repository.GetOwner().GetLogin()
		repo := repository.GetName()

		c.logger.Infof("renaming default branch from %q to %q", currentDefaultBranch, newDefaultBranch)

		underlyingClient := c.GetUnderlyingClient(ctx)
		_, _, err := underlyingClient.Repositories.RenameBranch(ctx, owner, repo, currentDefaultBranch, newDefaultBranch)
		if err != nil {
			return microerror.Mask(err)
		}

		*repository.DefaultBranch = newDefaultBranch

		c.logger.Infof("renamed default branch from %q to %q", currentDefaultBranch, newDefaultBranch)
	}

	return nil
}

func (c *Client) SetRepositoryWebhooks(ctx context.Context, repository *github.Repository, hook *github.Hook) error {
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()

	underlyingClient := c.GetUnderlyingClient(ctx)

	hooks, _, err := underlyingClient.Repositories.ListHooks(ctx, owner, repo, &github.ListOptions{PerPage: 50})
	if err != nil {
		return microerror.Mask(err)
	}

	c.logger.Debugf("Checking for existing webhook\n")
	for _, existingHook := range hooks {

		if *existingHook.Config.URL == *hook.Config.URL {
			c.logger.Debugf("found existing webhook. ID=%d\n", *existingHook.ID)

			if c.dryRun {
				return nil
			}

			hook.ID = existingHook.ID

			hook, _, err = underlyingClient.Repositories.EditHook(ctx, owner, repo, *hook.ID, hook)
			if err != nil {
				return microerror.Mask(err)
			}
			c.logger.Infof("updated existing webhook. ID=%d\n", *hook.ID)

			return nil
		}
	}

	c.logger.Debugf("Creating new webhook\n")
	if c.dryRun {
		return nil
	}

	hook, _, err = underlyingClient.Repositories.CreateHook(ctx, owner, repo, hook)
	if err != nil {
		return microerror.Mask(err)
	}

	c.logger.Infof("new webhook added. ID=%d\n", *hook.ID)

	return nil
}

func (c *Client) CreateFromTemplate(ctx context.Context, templateOwner, templateRepo, newOwner string, repository *github.Repository) (*github.Repository, error) {
	c.logger.Infof("creating repository %s/%s from template %s/%s", newOwner, repository.GetName(), templateOwner, templateRepo)

	if c.dryRun {
		return repository, nil
	}

	underlyingClient := c.GetUnderlyingClient(ctx)

	req := github.TemplateRepoRequest{
		Name:        repository.GetName(),
		Owner:       new(newOwner),
		Description: repository.Description,
		Private:     repository.Private,
	}

	repo, _, err := underlyingClient.Repositories.CreateFromTemplate(ctx, templateOwner, templateRepo, req)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	c.logger.Infof("created repository %s/%s from template %s/%s", newOwner, repository.GetName(), templateOwner, templateRepo)

	return repo, nil
}
