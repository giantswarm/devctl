package githubclient

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// CommitsBetween returns every commit reachable from headSHA but not
// baseSHA, with its message. Both ends are immutable so pagination cannot
// mix different heads. Unlike the pull-request commits endpoint, a paged
// comparison can read beyond 250 commits. An incomplete listing is an error.
func (c *Client) CommitsBetween(ctx context.Context, owner, repo, baseSHA, headSHA string) ([]*github.RepositoryCommit, error) {
	if baseSHA == "" || headSHA == "" {
		return nil, microerror.Maskf(executionError, "cannot read commits without both base and head SHAs")
	}
	opts := &github.ListOptions{PerPage: 100, Page: 1}
	var commits []*github.RepositoryCommit
	for page := 0; page < maxPages; page++ {
		comparison, resp, err := c.ghClient.Repositories.CompareCommits(ctx, owner, repo, baseSHA, headSHA, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		commits = append(commits, comparison.Commits...)
		if resp.NextPage == 0 {
			if comparison.TotalCommits == nil || len(commits) != comparison.GetTotalCommits() {
				return nil, microerror.Maskf(executionError, "incomplete commit comparison of %s/%s %s...%s: read %d of %d commits", owner, repo, baseSHA, headSHA, len(commits), comparison.GetTotalCommits())
			}
			return commits, nil
		}
		opts.Page = resp.NextPage
	}
	return nil, microerror.Maskf(executionError, "commit comparison of %s/%s %s...%s exceeds %d pages", owner, repo, baseSHA, headSHA, maxPages)
}
