package reconcile

import (
	"context"

	"github.com/google/go-github/v93/github"
)

// ListRepositoryRulesets reads every page of repository-owned rulesets,
// excluding inherited organization rulesets. A failed page returns no partial
// list: callers must not infer that a ruleset is absent from incomplete data.
func ListRepositoryRulesets(ctx context.Context, gh *github.Client, owner, repo string) ([]*github.RepositoryRuleset, error) {
	opts := &github.RepositoryListRulesetsOptions{
		IncludesParents: new(false),
		ListOptions:     github.ListOptions{PerPage: 100},
	}
	var rulesets []*github.RepositoryRuleset
	for {
		page, resp, err := gh.Repositories.GetAllRulesets(ctx, owner, repo, opts)
		if err != nil {
			return nil, err
		}
		rulesets = append(rulesets, page...)
		if resp.NextPage == 0 {
			return rulesets, nil
		}
		opts.Page = resp.NextPage
	}
}
