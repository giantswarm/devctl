package approvemergerenovate

import (
	"context"
	"fmt"

	"github.com/google/go-github/v93/github"
)

// searchPRs gathers a complete discovery batch before any PR is admitted.
// GitHub search exposes at most 1,000 matches, even with pagination.
func searchPRs(ctx context.Context, client *github.Client, query string) (*github.IssuesSearchResult, error) {
	opts := &github.SearchOptions{ListOptions: github.ListOptions{PerPage: 100}}
	all := &github.IssuesSearchResult{}
	for {
		result, resp, err := client.Search.Issues(ctx, query, opts)
		if err != nil {
			return nil, err
		}
		if result.GetIncompleteResults() {
			return nil, fmt.Errorf("GitHub returned incomplete search results; narrow the query or retry")
		}
		if result.GetTotal() > 1000 {
			return nil, fmt.Errorf("GitHub search has %d matches and exposes at most 1000; narrow the query", result.GetTotal())
		}
		all.Total = result.Total
		all.Issues = append(all.Issues, result.Issues...)
		if resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}
