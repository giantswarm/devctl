package prmerge

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
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
// know: devctl acts as them, and which rulesets of the base their token
// cannot bypass. It reads each ruleset with a pull request rule, names the
// ones GitHub says the caller cannot bypass (current_user_can_bypass) as
// the blockers with their bypass actors, the ones the caller bypasses
// apart, the team whose file declares the entry, and what merges the pull
// request: past a blocker without bypass actors only an approving review
// or a change to that ruleset, otherwise a member of that team, a
// repository admin, or an approving review. A review rule no ruleset
// carries is classic branch protection: the reason names the entry whose
// alignment replaces it, since devctl never lifts enforce_admins. Rules the
// token cannot read are said so; nothing is written.
func (m *Merger) explainReviewRule(ctx context.Context, owner, repo, base, caller string, verdict Verdict) string {
	team := verdict.Team
	rules, _, err := m.github.GitHub().Repositories.ListRulesForBranch(ctx, owner, repo, base, &github.ListOptions{PerPage: 100})
	switch {
	case err != nil:
		return fmt.Sprintf("devctl acts as %s, who has no bypass on the rules of %s (they could not be read: %v)%s", caller, base, err, mergeAdvice(team))
	case len(rules.PullRequest) == 0:
		return fmt.Sprintf("devctl acts as %s, who has no bypass on the review rule of %s, which no ruleset carries: classic branch protection requires the review, and such a repository is aligned first, never merged past it: %s; devctl never lifts enforce_admins, and its token carries no Administration permission to do so", caller, base, alignAdvice(verdict.Entry, team))
	}
	var blocking, bypassed []rulesetView
	seen := map[string]bool{}
	for _, rule := range rules.PullRequest {
		key := fmt.Sprintf("%s/%d", rule.RulesetSourceType, rule.RulesetID)
		if seen[key] {
			continue
		}
		seen[key] = true
		v := m.describeRuleset(ctx, owner, repo, rule.BranchRuleMetadata)
		if v.bypass != "" {
			bypassed = append(bypassed, v)
		} else {
			blocking = append(blocking, v)
		}
	}
	var b strings.Builder
	if len(blocking) == 0 {
		fmt.Fprintf(&b, "devctl acts as %s, who bypasses every ruleset with a review rule on %s, yet GitHub declined the merge for it", caller, base)
	} else {
		fmt.Fprintf(&b, "devctl acts as %s, who has no bypass on %s", caller, joinViews(blocking, " nor on "))
	}
	if len(bypassed) > 0 {
		parts := make([]string, 0, len(bypassed))
		for _, v := range bypassed {
			parts = append(parts, fmt.Sprintf("%s (%s)", v.ref, v.bypass))
		}
		fmt.Fprintf(&b, "; %s bypasses %s", caller, strings.Join(parts, " and "))
	}
	var closed []rulesetView
	for _, v := range blocking {
		if v.noActors {
			closed = append(closed, v)
		}
	}
	if len(closed) == 0 {
		b.WriteString(mergeAdvice(team))
		return b.String()
	}
	names := make([]string, 0, len(closed))
	foreign := false
	for _, v := range closed {
		names = append(names, v.ref)
		foreign = foreign || !v.engine
	}
	fmt.Fprintf(&b, "; no team member or repository admin merges past %s: only an approving review by a reviewer with write access or a change to that ruleset does", strings.Join(names, " and "))
	if foreign {
		owner := "the owning team"
		if team != "" {
			owner += " (" + team + ")"
		}
		fmt.Fprintf(&b, "; a ruleset devctl did not create is a foreign-ruleset the reconciler leaves to %s to keep or remove", owner)
	}
	return b.String()
}

// alignAdvice is how a repository on classic branch protection gets the
// devctl ruleset: align: true on its entry, or a declaration first where no
// team file declares it.
func alignAdvice(entry, team string) string {
	if entry == "" {
		return "no team file declares the repository, so it is declared first (devctl repo adopt), then aligned with align: true on its entry"
	}
	return fmt.Sprintf("align: true on %s gives it the devctl ruleset, whose bypass lets the owning team (%s) and the repository admins merge their own green pull requests", entry, team)
}

// mergeAdvice is who merges a pull request the review rule declines when
// the blocking rulesets have bypass actors.
func mergeAdvice(team string) string {
	if team == "" {
		return "; a repository admin or another bypass actor merges it, or a reviewer with write access approves it first"
	}
	return fmt.Sprintf("; the entry's owning team is %s: one of its members or a repository admin merges it, or a reviewer with write access approves it first", team)
}

// rulesetView is a ruleset as the reason names it.
type rulesetView struct {
	// ref names the ruleset and where it is held: the ruleset "x" of o/r.
	ref string
	// text is ref with the bypass actors, or why they could not be read.
	text string
	// bypass is how the caller bypasses the ruleset, from GitHub's
	// current_user_can_bypass; empty when they cannot or it is unknown.
	bypass string
	// noActors: the ruleset was read and has no bypass actors.
	noActors bool
	// engine: the ruleset is the one devctl's reconciler writes.
	engine bool
}

func joinViews(views []rulesetView, sep string) string {
	texts := make([]string, 0, len(views))
	for _, v := range views {
		texts = append(texts, v.text)
	}
	return strings.Join(texts, sep)
}

// describeRuleset reads the ruleset a rule came from, from the repository
// or the organization that holds it: its name, its bypass actors and
// whether the caller bypasses it.
func (m *Merger) describeRuleset(ctx context.Context, owner, repo string, rule github.BranchRuleMetadata) rulesetView {
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
		ref := fmt.Sprintf("the ruleset %d of %s", rule.RulesetID, where)
		return rulesetView{ref: ref, text: fmt.Sprintf("%s (its bypass actors could not be read: %v)", ref, err)}
	}
	v := rulesetView{
		ref:    fmt.Sprintf("the ruleset %q of %s", rs.Name, where),
		bypass: callerBypass(rs.CurrentUserCanBypass),
		engine: rule.RulesetSourceType != github.RulesetSourceTypeOrganization && rs.Name == reconcile.RulesetName,
	}
	if len(rs.BypassActors) == 0 {
		v.noActors = true
		v.text = v.ref + " (no bypass actors)"
		return v
	}
	actors := make([]string, 0, len(rs.BypassActors))
	for _, a := range rs.BypassActors {
		actors = append(actors, describeActor(a))
	}
	v.text = fmt.Sprintf("%s (bypass actors: %s)", v.ref, strings.Join(actors, "; "))
	return v
}

// callerBypass is GitHub's current_user_can_bypass as the reason names it,
// empty for never or when GitHub does not say.
func callerBypass(mode *github.BypassMode) string {
	if mode == nil {
		return ""
	}
	switch *mode {
	case github.BypassModeNever, "":
		return ""
	case github.BypassModeAlways:
		return "always"
	case github.BypassModeExempt:
		return "exempt"
	case github.BypassModePullRequest, "pull_requests_only":
		return "pull requests only"
	}
	return string(*mode)
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
