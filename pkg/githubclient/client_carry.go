package githubclient

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// CommitsBetween returns every commit unique to headSHA compared with
// baseSHA, oldest first. Callers use immutable SHAs to keep pagination bound
// to the head they judged. A partial comparison is an error, never evidence
// that every commit has the same author.
func (c *Client) CommitsBetween(ctx context.Context, owner, repo, baseSHA, headSHA string) ([]*github.RepositoryCommit, error) {
	opts := &github.ListOptions{PerPage: 100}
	var commits []*github.RepositoryCommit
	for page := 0; page < maxPages; page++ {
		comparison, response, err := c.ghClient.Repositories.CompareCommits(ctx, owner, repo, baseSHA, headSHA, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		commits = append(commits, comparison.Commits...)
		if response.NextPage == 0 {
			if len(commits) != comparison.GetTotalCommits() {
				return nil, microerror.Maskf(executionError, "incomplete commit comparison: read %d of %d commits", len(commits), comparison.GetTotalCommits())
			}
			return commits, nil
		}
		opts.Page = response.NextPage
	}
	return nil, microerror.Maskf(executionError, "commit comparison exceeds %d pages", maxPages)
}
