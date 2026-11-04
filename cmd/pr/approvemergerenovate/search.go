package approvemergerenovate

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// searchIssues reads the complete batch before any PR is processed or grouped.
// An unreadable page must not turn a full sweep into a silent partial sweep.
func (r *runner) searchIssues(ctx context.Context, client *github.Client, query string) ([]*github.Issue, error) {
	opts := &github.SearchOptions{ListOptions: github.ListOptions{PerPage: 100}}
	var issues []*github.Issue
	for {
		results, resp, err := client.Search.Issues(ctx, query, opts)
		if err != nil {
			return nil, microerror.Maskf(executionFailedError, "failed to search for PRs: %v", err)
		}
		if results.GetIncompleteResults() {
			return nil, microerror.Maskf(executionFailedError, "GitHub returned an incomplete PR search; narrow the query and retry")
		}
		issues = append(issues, results.Issues...)
		if resp.NextPage == 0 {
			return issues, nil
		}
		opts.Page = resp.NextPage
	}
}
