package githubclient

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

// maxPages bounds every paged listing here: a head with more than 1000 check
// runs or statuses is not one the merge box shows either.
const maxPages = 10

// PullRequest returns the pull request as GitHub sees it now: state, draft,
// mergeable state, head and base.
func (c *Client) PullRequest(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error) {
	pr, _, err := c.ghClient.PullRequests.Get(ctx, owner, repo, number)
	if isGithub404(err) {
		return nil, microerror.Maskf(notFoundError, "pull request %s/%s#%d", owner, repo, number)
	}
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return pr, nil
}

// IsPullRequest reports whether number in owner/repo is a pull request
// rather than an issue: the issues endpoint answers for both and marks a pull
// request. A number that does not exist is notFoundError.
func (c *Client) IsPullRequest(ctx context.Context, owner, repo string, number int) (bool, error) {
	issue, _, err := c.ghClient.Issues.Get(ctx, owner, repo, number)
	if isGithub404(err) {
		return false, microerror.Maskf(notFoundError, "issue or pull request %s/%s#%d", owner, repo, number)
	}
	if err != nil {
		return false, microerror.Mask(err)
	}
	return issue.IsPullRequest(), nil
}

// CheckRunsForRef returns the check runs of ref as the merge box lists them:
// the latest run per check name and app (GitHub's filter=latest), every page.
func (c *Client) CheckRunsForRef(ctx context.Context, owner, repo, ref string) ([]*github.CheckRun, error) {
	filter := "latest"
	opts := &github.ListCheckRunsOptions{
		Filter:      &filter,
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var runs []*github.CheckRun
	for page := 0; page < maxPages; page++ {
		result, resp, err := c.ghClient.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		runs = append(runs, result.CheckRuns...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return runs, nil
}

// CommitStatuses returns the commit statuses of ref, the latest per context
// (the combined status), every page.
func (c *Client) CommitStatuses(ctx context.Context, owner, repo, ref string) ([]*github.RepoStatus, error) {
	opts := &github.ListOptions{PerPage: 100}
	var statuses []*github.RepoStatus
	for page := 0; page < maxPages; page++ {
		combined, resp, err := c.ghClient.Repositories.GetCombinedStatus(ctx, owner, repo, ref, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		statuses = append(statuses, combined.Statuses...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return statuses, nil
}

// WorkflowRunsForSHA returns every GitHub Actions run of the head SHA,
// whatever its status, every page.
func (c *Client) WorkflowRunsForSHA(ctx context.Context, owner, repo, sha string) ([]*github.WorkflowRun, error) {
	opts := &github.ListWorkflowRunsOptions{
		HeadSHA:     sha,
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var runs []*github.WorkflowRun
	for page := 0; page < maxPages; page++ {
		result, resp, err := c.ghClient.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		runs = append(runs, result.WorkflowRuns...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return runs, nil
}

// RequiredStatusContexts returns the status contexts branch requires before a
// merge: the required status checks of its branch protection and of every
// ruleset in effect on it, sorted and without duplicates. A protection the
// token may not read (404: none, or 403: no admin access) contributes
// nothing; the rules endpoint answers for everyone with read access.
func (c *Client) RequiredStatusContexts(ctx context.Context, owner, repo, branch string) ([]string, error) {
	set := map[string]bool{}

	protection, _, err := c.ghClient.Repositories.GetBranchProtection(ctx, owner, repo, branch)
	switch status := githubStatus(err); {
	case err == nil:
		if protection.RequiredStatusChecks != nil {
			if protection.RequiredStatusChecks.Contexts != nil {
				for _, name := range *protection.RequiredStatusChecks.Contexts {
					set[name] = true
				}
			}
			if protection.RequiredStatusChecks.Checks != nil {
				for _, check := range *protection.RequiredStatusChecks.Checks {
					set[check.Context] = true
				}
			}
		}
	case status == http.StatusNotFound, status == http.StatusForbidden:
	default:
		return nil, microerror.Mask(err)
	}

	rules, _, err := c.ghClient.Repositories.ListRulesForBranch(ctx, owner, repo, branch, &github.ListOptions{PerPage: 100})
	switch status := githubStatus(err); {
	case err == nil:
		for _, rule := range rules.RequiredStatusChecks {
			for _, check := range rule.Parameters.RequiredStatusChecks {
				set[check.Context] = true
			}
		}
	case status == http.StatusNotFound, status == http.StatusForbidden:
	default:
		return nil, microerror.Mask(err)
	}

	contexts := make([]string, 0, len(set))
	for name := range set {
		contexts = append(contexts, name)
	}
	sort.Strings(contexts)
	return contexts, nil
}

// FileExists says whether path is a file at ref.
func (c *Client) FileExists(ctx context.Context, owner, repo, path, ref string) (bool, error) {
	file, _, _, err := c.ghClient.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
	if isGithub404(err) {
		return false, nil
	}
	if err != nil {
		return false, microerror.Mask(err)
	}
	return file != nil, nil
}

// githubStatus is the HTTP status of a go-github error, 0 for any other.
func githubStatus(err error) int {
	var errResponse *github.ErrorResponse
	if errors.As(err, &errResponse) && errResponse.Response != nil {
		return errResponse.Response.StatusCode
	}
	return 0
}
