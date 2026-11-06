package setup

import (
	"context"
	"fmt"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// checkRulesetDisable refuses when effective rules would outlive the classic
// protection removal. The branch-rules API includes inherited rules and
// evaluates conditions; unrelated branches and disabled rulesets stay alone.
func checkRulesetDisable(ctx context.Context, client *githubclient.Client, owner, repo string, branches ...string) error {
	seen := map[string]bool{}
	for _, branch := range branches {
		if seen[branch] {
			continue
		}
		seen[branch] = true
		for _, err := range client.GetUnderlyingClient(ctx).Repositories.ListRulesForBranchIter(ctx, owner, repo, branch, &github.ListOptions{PerPage: 100}) {
			if err != nil {
				return fmt.Errorf("checking ruleset protection of %q: %w", branch, err)
			}
			return fmt.Errorf("--disable-branch-protection cannot disable active ruleset protection of %q; manage the applicable repository or organization rulesets explicitly before retrying", branch)
		}
	}
	return nil
}
