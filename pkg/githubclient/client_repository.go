package githubclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v44/github"
)

func (c *Client) ListRepositories(ctx context.Context, owner string) ([]Repository, error) {
	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("listing repositories for owner %#q", owner))

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
			c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("listing page %d of repositories for owner %#q", pageCnt, owner))

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

			c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("listed page %d of %d repositories for owner %#q", pageCnt, len(pageRepos), owner))
		}
	}

	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("listed %d repositories for owner %#q", len(repos), owner))

	return repos, nil
}

func (c *Client) GetRepository(ctx context.Context, owner, repo string) (*github.Repository, error) {
	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("get repository %s/%s", owner, repo))

	underlyingClient := c.getUnderlyingClient(ctx)

	repository, response, err := underlyingClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		if response != nil && response.Response != nil && response.Response.StatusCode == http.StatusNotFound {
			return nil, microerror.Mask(notFoundError)
		}
		return nil, microerror.Mask(err)
	}

	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("got repository %s", *repository.FullName))

	return repository, nil
}

func (c *Client) SetRepositorySettings(ctx context.Context, repository, repositorySettings *github.Repository) (*github.Repository, error) {
	owner := *repository.Owner.Login
	repo := *repository.Name

	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("setting repository %s/%s settings", owner, repo))

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

	underlyingClient := c.getUnderlyingClient(ctx)
	repository, _, err := underlyingClient.Repositories.Edit(ctx, *repository.Owner.Login, *repository.Name, repository)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("set repository %s/%s settings", owner, repo))

	return repository, nil
}

func (c *Client) SetRepositoryPermissions(ctx context.Context, repository *github.Repository, permissions map[string]string) error {
	org := *repository.Organization.Login
	owner := *repository.Owner.Login
	repo := *repository.Name

	//data, err := json.MarshalIndent(permissions, "", "  ")
	//if err != nil {
	//	return microerror.Mask(err)
	//}
	//fmt.Printf("permissions\n%s\n", data)

	underlyingClient := c.getUnderlyingClient(ctx)

	for teamSlug, permission := range permissions {
		c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("grant %s permission to %s on %s/%s repository", permission, teamSlug, owner, repo))

		opt := &github.TeamAddTeamRepoOptions{Permission: permission}

		_, err := underlyingClient.Teams.AddTeamRepoBySlug(ctx, org, teamSlug, owner, repo, opt)
		if err != nil {
			return microerror.Mask(err)
		}

		c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("granted %s permission to %s on %s/%s repository", permission, teamSlug, owner, repo))
	}

	return nil
}

func (c *Client) SetRepositoryBranchProtection(ctx context.Context, repository *github.Repository, checkNames []string) (err error) {
	owner := *repository.Owner.Login
	repo := *repository.Name
	default_branch := *repository.DefaultBranch

	False := false

	opts := &github.ProtectionRequest{
		RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
			RequiredApprovingReviewCount: 1,
		},
		AllowForcePushes: &False,
		AllowDeletions:   &False,
		EnforceAdmins:    true,
	}

	if checkNames == nil {
		checkNames, err = c.getGithubChecks(ctx, repository, default_branch)
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
			Checks: checks,
		}
	}

	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("configure protection for %s branch on %s/%s repository", default_branch, owner, repo))

	underlyingClient := c.getUnderlyingClient(ctx)
	_, _, err = underlyingClient.Repositories.UpdateBranchProtection(ctx, owner, repo, default_branch, opts)
	if err != nil {
		return microerror.Mask(err)
	}

	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("configured protection for %s branch on %s/%s repository", default_branch, owner, repo))

	return nil
}

func (c *Client) getGithubChecks(ctx context.Context, repository *github.Repository, branch string) ([]string, error) {
	owner := *repository.Owner.Login
	repo := *repository.Name

	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("get repository commit statuses for %s branch on %s/%s repository", branch, owner, repo))

	var allCombinedStatus []*github.CombinedStatus
	{
		opt := &github.ListOptions{
			PerPage: 10,
		}

		underlyingClient := c.getUnderlyingClient(ctx)

		for {
			combinedStatus, resp, err := underlyingClient.Repositories.GetCombinedStatus(ctx, owner, repo, branch, opt)
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
			checks = append(checks, *status.Context)
		}
	}

	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("got %d repository commit statuses for %s branch on %s/%s repository", len(checks), branch, owner, repo))

	return checks, nil
}
