package prmerge

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v92/github"
)

// reviewRulePhrase is in GitHub's sentence when a merge is declined for
// the approving reviews a rule requires, classic protection and rulesets
// alike: "At least 1 approving review is required by reviewers with write
// access."
const reviewRulePhrase = "approving review"

// declinedByReviewRule says whether GitHub declined the merge for the
// review rule.
func declinedByReviewRule(err error) bool {
	return strings.Contains(err.Error(), reviewRulePhrase)
}

// explainReviewRule is what a caller declined by the review rule needs to
// know: devctl acts as them, and their token bypasses none of the rulesets
// of the base. It names each ruleset with a pull request rule and its
// bypass actors, the team whose file declares the entry, and what merges
// the pull request: a member of that team, a repository admin, or an
// approving review. Rules
// the token cannot read are said so; nothing is written.
func (m *Merger) explainReviewRule(ctx context.Context, owner, repo, base, caller, team string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "devctl acts as %s, who has no bypass on ", caller)
	rules, _, err := m.github.GitHub().Repositories.ListRulesForBranch(ctx, owner, repo, base, &github.ListOptions{PerPage: 100})
	switch {
	case err != nil:
		fmt.Fprintf(&b, "the rules of %s (they could not be read: %v)", base, err)
	case len(rules.PullRequest) == 0:
		fmt.Fprintf(&b, "the review rule of %s, which no ruleset carries: classic branch protection requires the review, and such a repository is aligned first (align: true on its entry), never merged past it", base)
	default:
		var named []string
		seen := map[string]bool{}
		for _, rule := range rules.PullRequest {
			key := fmt.Sprintf("%s/%d", rule.RulesetSourceType, rule.RulesetID)
			if seen[key] {
				continue
			}
			seen[key] = true
			named = append(named, m.describeRuleset(ctx, owner, repo, rule.BranchRuleMetadata))
		}
		b.WriteString(strings.Join(named, " nor on "))
	}
	if team == "" {
		b.WriteString("; a repository admin or another bypass actor merges it, or a reviewer with write access approves it first")
	} else {
		fmt.Fprintf(&b, "; the entry's owning team is %s: one of its members or a repository admin merges it, or a reviewer with write access approves it first", team)
	}
	return b.String()
}

// describeRuleset names the ruleset a rule came from and its bypass actors,
// read from the repository or the organization that holds it.
func (m *Merger) describeRuleset(ctx context.Context, owner, repo string, rule github.BranchRuleMetadata) string {
	gh := m.github.GitHub()
	var (
		rs    *github.RepositoryRuleset
		err   error
		where = owner + "/" + repo
	)
	switch rule.RulesetSourceType {
	case github.RulesetSourceTypeOrganization:
		where = "the organization " + owner
		rs, _, err = gh.Organizations.GetRepositoryRuleset(ctx, owner, rule.RulesetID)
	default:
		rs, _, err = gh.Repositories.GetRuleset(ctx, owner, repo, rule.RulesetID, false)
	}
	if err != nil {
		return fmt.Sprintf("the ruleset %d of %s (its bypass actors could not be read: %v)", rule.RulesetID, where, err)
	}
	if len(rs.BypassActors) == 0 {
		return fmt.Sprintf("the ruleset %q of %s (no bypass actors)", rs.Name, where)
	}
	actors := make([]string, 0, len(rs.BypassActors))
	for _, a := range rs.BypassActors {
		actors = append(actors, describeActor(a))
	}
	return fmt.Sprintf("the ruleset %q of %s (bypass actors: %s)", rs.Name, where, strings.Join(actors, "; "))
}

// Repository roles as GitHub numbers them in a bypass actor.
var repositoryRoles = map[int64]string{5: "repository admins", 2: "repository maintainers", 4: "repository writers"}

// describeActor is a bypass actor as the reason names it. An App's bypass
// covers the tokens of its installations, and devctl acts with a user
// token: the reason says so, lest the App on the list read as devctl's way
// past the rule.
func describeActor(a *github.BypassActor) string {
	mode := "for pull requests"
	if a.BypassMode != nil && *a.BypassMode == github.BypassModeAlways {
		mode = "always"
	}
	var kind github.BypassActorType
	if a.ActorType != nil {
		kind = *a.ActorType
	}
	switch kind {
	case github.BypassActorTypeIntegration:
		return fmt.Sprintf("App %d %s, whose bypass covers its installation tokens and not the user token devctl acts with", a.GetActorID(), mode)
	case github.BypassActorTypeTeam:
		return fmt.Sprintf("team %d %s", a.GetActorID(), mode)
	case github.BypassActorTypeRepositoryRole:
		if role, ok := repositoryRoles[a.GetActorID()]; ok {
			return role + " " + mode
		}
		return fmt.Sprintf("repository role %d %s", a.GetActorID(), mode)
	case github.BypassActorTypeOrganizationAdmin:
		return "organization admins " + mode
	case github.BypassActorTypeDeployKey:
		return "deploy keys " + mode
	}
	return fmt.Sprintf("%s %d %s", kind, a.GetActorID(), mode)
}
