package githubclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v71/github"
)

func (c *Client) ListRepositories(ctx context.Context, owner string) ([]Repository, error) {
	c.logger.Infof("listing repositories for owner %#q", owner)

	underlyingClient := c.getUnderlyingClient(ctx)

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

	underlyingClient := c.getUnderlyingClient(ctx)

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

	if !c.dryRun {
		var err error

		underlyingClient := c.getUnderlyingClient(ctx)
		repository, _, err = underlyingClient.Repositories.Edit(ctx, repository.GetOwner().GetLogin(), repository.GetName(), repository)
		if err != nil {
			return nil, microerror.Mask(err)
		}
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

	underlyingClient := c.getUnderlyingClient(ctx)

	for teamSlug, permission := range permissions {

		opt := &github.TeamAddTeamRepoOptions{Permission: permission}

		c.logger.Debugf("grant %q permission to %q", permission, teamSlug)

		if !c.dryRun {
			_, err := underlyingClient.Teams.AddTeamRepoBySlug(ctx, org, teamSlug, owner, repo, opt)
			if err != nil {
				return microerror.Mask(err)
			}
		}

		c.logger.Debugf("granted %q permission to %q", permission, teamSlug)
	}

	input := &github.DefaultWorkflowPermissionRepository{
		DefaultWorkflowPermissions: github.Ptr("write"),
	}
	_, _, err := underlyingClient.Repositories.EditDefaultWorkflowPermissions(ctx, owner, repo, *input)
	if err != nil {
		return microerror.Mask(err)
	}

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

	if !c.dryRun {
		underlyingClient := c.getUnderlyingClient(ctx)
		_, _, err = underlyingClient.Repositories.UpdateBranchProtection(ctx, owner, repo, default_branch, opts)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	c.logger.Debugf("configured protection for %q branch", default_branch)

	return nil
}

func (c *Client) RemoveRepositoryBranchProtection(ctx context.Context, repository *github.Repository) (err error) {
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()
	default_branch := repository.GetDefaultBranch()

	c.logger.Info("disable branch protection")

	underlyingClient := c.getUnderlyingClient(ctx)
	_, _, err = underlyingClient.Repositories.GetBranchProtection(ctx, owner, repo, default_branch)
	if err != nil {
		if errors.Is(err, github.ErrBranchNotProtected) {
			// Branch has no protection set, no need to remove it.
			c.logger.Debugf("leaving branch %q without protection", default_branch)
			return nil
		}
		return microerror.Mask(err)
	}

	if !c.dryRun {
		_, err = underlyingClient.Repositories.RemoveBranchProtection(ctx, owner, repo, default_branch)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	c.logger.Debugf("disabled protection for %q branch", default_branch)

	return nil
}

func (c *Client) getGithubChecks(ctx context.Context, repository *github.Repository, branch string, checksFilter *regexp.Regexp) ([]string, error) {
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()

	// Tags have specific workflows, that are not run in PRs.
	// So, we need to find checks for a commit that is not tagged.
	// Otherwise PRs would be blocked by not-run checks.
	allTags, err := c.getTags(ctx, repository)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	ref, err := c.getLatestNonTagCommit(ctx, repository, branch, allTags)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	c.logger.Debugf("get commit statuses for ref: %q", ref)

	var allCombinedStatus []*github.CombinedStatus
	{
		opt := &github.ListOptions{
			PerPage: 10,
		}

		underlyingClient := c.getUnderlyingClient(ctx)

		for {
			combinedStatus, resp, err := underlyingClient.Repositories.GetCombinedStatus(ctx, owner, repo, ref, opt)
			if err != nil {
				return nil, microerror.Mask(err)
			}
			allCombinedStatus = append(allCombinedStatus, combinedStatus)
			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage

		}
	}

	var checks []string
	for _, combinedStatus := range allCombinedStatus {
		for _, status := range combinedStatus.Statuses {
			if checksFilter == nil || !checksFilter.MatchString(status.GetContext()) {
				checks = append(checks, status.GetContext())
			}
		}
	}

	c.logger.Debugf("found %d commit statuses for ref %q:", len(checks), ref)
	for id, check := range checks {
		c.logger.Debugf(" - checks[%d] = %q", id, check)
	}

	return checks, nil
}

func (c *Client) SetRepositoryDefaultBranch(ctx context.Context, repository *github.Repository, newDefaultBranch string) (err error) {
	currentDefaultBranch := repository.GetDefaultBranch()
	if currentDefaultBranch != newDefaultBranch {
		owner := repository.GetOwner().GetLogin()
		repo := repository.GetName()

		c.logger.Infof("renaming default branch from %q to %q", currentDefaultBranch, newDefaultBranch)

		if !c.dryRun {
			underlyingClient := c.getUnderlyingClient(ctx)
			_, _, err := underlyingClient.Repositories.RenameBranch(ctx, owner, repo, currentDefaultBranch, newDefaultBranch)
			if err != nil {
				return microerror.Mask(err)
			}
		}

		*repository.DefaultBranch = newDefaultBranch

		c.logger.Infof("renamed default branch from %q to %q", currentDefaultBranch, newDefaultBranch)
	}

	return nil
}

// getTags retrieves list of tags
func (c *Client) getTags(ctx context.Context, repository *github.Repository) ([]*github.RepositoryTag, error) {
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()

	underlyingClient := c.getUnderlyingClient(ctx)

	var allTags []*github.RepositoryTag
	opt := &github.ListOptions{
		PerPage: 10,
	}
	for {
		tags, resp, err := underlyingClient.Repositories.ListTags(ctx, owner, repo, opt)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		allTags = append(allTags, tags...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	for _, tag := range allTags {
		c.logger.Debugf("Found tag: %s / commit: %s\n", tag.GetName(), tag.GetCommit().GetSHA())
	}
	return allTags, nil
}

// getLatestNonTagCommit gets latest commit that is not tagged
// because we want one that is not a release
func (c *Client) getLatestNonTagCommit(ctx context.Context, repository *github.Repository, branch string, tags []*github.RepositoryTag) (string, error) {
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()

	underlyingClient := c.getUnderlyingClient(ctx)

	opt := &github.CommitsListOptions{
		SHA: branch,
		ListOptions: github.ListOptions{
			PerPage: 10,
		},
	}

	// Loop through commits
	for {
		commits, resp, err := underlyingClient.Repositories.ListCommits(ctx, owner, repo, opt)
		if err != nil {
			return "", microerror.Mask(err)
		}
		for _, commit := range commits {
			c.logger.Debugf("Checking commit: %s\n", commit.GetSHA())
			// Is this commit tagged?
			if !isCommitTagged(commit, tags) {
				return commit.GetSHA(), nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return "", microerror.Mask(notFoundError)
}

// Returns true if the commit has an associated tag
func isCommitTagged(commit *github.RepositoryCommit, tags []*github.RepositoryTag) bool {
	for _, tag := range tags {
		if commit.GetSHA() == tag.GetCommit().GetSHA() {
			return true
		}
	}
	return false
}

func (c *Client) SetRepositoryWebhooks(ctx context.Context, repository *github.Repository, hook *github.Hook) error {
	owner := repository.GetOwner().GetLogin()
	repo := repository.GetName()

	underlyingClient := c.getUnderlyingClient(ctx)

	hooks, _, err := underlyingClient.Repositories.ListHooks(ctx, owner, repo, &github.ListOptions{PerPage: 50})
	if err != nil {
		return microerror.Mask(err)
	}

	c.logger.Debugf("Checking for existing webhook\n")
	for _, existingHook := range hooks {

		if *existingHook.Config.URL == *hook.Config.URL {
			c.logger.Debugf("found existing webhook. ID=%d\n", *existingHook.ID)

			if !c.dryRun {
				hook.ID = existingHook.ID

				hook, _, err = underlyingClient.Repositories.EditHook(ctx, owner, repo, *hook.ID, hook)
				if err != nil {
					return microerror.Mask(err)
				}
			}
			c.logger.Infof("updated existing webhook. ID=%d\n", *hook.ID)

			return nil
		}
	}

	c.logger.Debugf("Creating new webhook\n")
	if !c.dryRun {
		hook, _, err = underlyingClient.Repositories.CreateHook(ctx, owner, repo, hook)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	c.logger.Infof("new webhook added. ID=%d\n", *hook.ID)

	return nil
}

func (c *Client) CreateFromTemplate(ctx context.Context, templateOwner, templateRepo, newOwner string, repository *github.Repository) (*github.Repository, error) {
	c.logger.Infof("creating repository %s/%s from template %s/%s", newOwner, repository.GetName(), templateOwner, templateRepo)

	underlyingClient := c.getUnderlyingClient(ctx)

	req := &github.TemplateRepoRequest{
		Name:        repository.Name,
		Owner:       github.Ptr(newOwner),
		Description: repository.Description,
		Private:     repository.Private,
	}

	repo, _, err := underlyingClient.Repositories.CreateFromTemplate(ctx, templateOwner, templateRepo, req)
	if err != nil {
		if c.dryRun {
			c.logger.Infof("[dry-run] would have created repository %s/%s from template %s/%s", newOwner, repository.GetName(), templateOwner, templateRepo)
			return repository, nil
		}
		return nil, microerror.Mask(err)
	}

	c.logger.Infof("created repository %s/%s from template %s/%s", newOwner, repository.GetName(), templateOwner, templateRepo)

	return repo, nil
}
