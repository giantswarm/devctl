package githubclient

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// The settings and rules `devctl pr merge` reads beyond the wait: the merge
// methods a repository allows, and who may bypass which ruleset, to explain
// a merge GitHub declined for its review rule. Nothing here writes.

// Repository is GET /repos/{owner}/{repo} as GitHub answers it, its error
// GitHub's own.
func (c *Client) Repository(ctx context.Context, owner, repo string) (*github.Repository, error) {
	repository, _, err := c.ghClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return repository, nil
}

// RulesForBranch is every rule in effect on branch, from repository and
// organization rulesets alike (GET /repos/{owner}/{repo}/rules/branches/{branch}).
func (c *Client) RulesForBranch(ctx context.Context, owner, repo, branch string) (*github.BranchRules, error) {
	rules, _, err := c.ghClient.Repositories.ListRulesForBranch(ctx, owner, repo, branch, &github.ListOptions{PerPage: 100})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return rules, nil
}

// RepositoryRuleset is the repository's ruleset id, its bypass actors and
// whether the caller bypasses it included.
func (c *Client) RepositoryRuleset(ctx context.Context, owner, repo string, id int64) (*github.RepositoryRuleset, error) {
	rs, _, err := c.ghClient.Repositories.GetRuleset(ctx, owner, repo, id, false)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return rs, nil
}

// OrganizationRuleset is the organization's ruleset id, its bypass actors
// and whether the caller bypasses it included.
func (c *Client) OrganizationRuleset(ctx context.Context, org string, id int64) (*github.RepositoryRuleset, error) {
	rs, _, err := c.ghClient.Organizations.GetRepositoryRuleset(ctx, org, id)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return rs, nil
}
