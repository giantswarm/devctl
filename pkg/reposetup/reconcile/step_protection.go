package reconcile

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// RulesetName is the name of the repository ruleset the protection step
// writes: one per repository, the engine's own. Every other ruleset of the
// repository is left alone.
const RulesetName = "devctl: default branch"

// defaultBranchRef is the ruleset condition that follows the repository's
// default branch, so a rename or a fork line's declared branch needs no
// change to the ruleset.
const defaultBranchRef = "~DEFAULT_BRANCH"

// gitHubActionsAppID is the id of the GitHub Actions App on github.com. A
// GitHub Actions gate is pinned to it in the ruleset, so no other
// integration satisfies the context.
const gitHubActionsAppID int64 = 15368

// teamPrivacySecret is the privacy of a team GitHub shows to its members
// alone. Such a team cannot be a bypass actor of a ruleset: GitHub refuses
// it with 422.
const teamPrivacySecret = "secret"

// repositoryAdminRoleID is the id GitHub gives the repository role Admin in
// a bypass actor (Maintain is 2, Write 4).
const repositoryAdminRoleID int64 = 5

// stepProtection protects the default branch and keeps the required checks
// on the reported-only rule: a context is required once it has reported on
// the default branch or a recently merged pull request; a required context
// nothing reports, a CircleCI context without a job in the pipeline and an
// ignored context are removed; the entry's requiredChecks are required
// whatever reported and never removed. [Runner.DevctlAppID] is the switch
// between the two forms of protection: with the App id the protection is
// the repository ruleset [RulesetName] (stepRulesetProtection), without it
// classic branch protection as before, applied and verified in full, with
// the advisory finding [FindingRulesetsNotEnabled] naming the switch. The
// reconciler's wiring passes the id; a devctl release alone changes no
// repository.
func (r *Runner) stepProtection(ctx context.Context, s *run, sr *StepResult) error {
	if r.DevctlAppID == 0 {
		s.report(sr, FindingRulesetsNotEnabled,
			"classic branch protection: no devctl App id, so the ruleset with the App, the repository admins and the owning team as bypass actors is not written",
			fmt.Sprintf("pass the App's numeric id (its settings page; not the client id) with --devctl-app-id: the switch to the ruleset %q, which then replaces the classic protection", RulesetName))
		return r.stepClassicProtection(ctx, s, sr)
	}
	return r.stepRulesetProtection(ctx, s, sr)
}

// stepClassicProtection writes classic branch protection as the baseline
// says: the required reviews, administrators bound (EnforceAdmins), no force
// push, no deletion, the required checks on the reported-only rule with the
// baseline's strictness.
func (r *Runner) stepClassicProtection(ctx context.Context, s *run, sr *StepResult) error {
	b := s.baseline
	branch := s.branch()

	protection, err := r.classicProtection(ctx, s, branch)
	if err != nil {
		return err
	}

	var current []string
	if protection != nil && protection.RequiredStatusChecks != nil && protection.RequiredStatusChecks.Checks != nil {
		for _, c := range *protection.RequiredStatusChecks.Checks {
			current = append(current, c.GetContext())
		}
	}

	reported, reportedKnown := r.reportedChecks(ctx, s, sr, branch)
	gates, pipelineKnown, err := r.pipelineGates(ctx, s)
	if err != nil {
		return err
	}
	want, err := requiredChecks(b, s.fields.RequiredChecks, current, reported, reportedKnown, gates, pipelineKnown)
	if err != nil {
		return err
	}

	var changes []string
	if protection == nil {
		changes = append(changes, "protect "+branch)
	} else {
		if got := protection.GetRequiredPullRequestReviews().GetRequiredApprovingReviewCount(); got != b.RequiredReviews {
			changes = append(changes, fmt.Sprintf("required reviews %d → %d", got, b.RequiredReviews))
		}
		if got := protection.GetEnforceAdmins().GetEnabled(); got != b.EnforceAdmins {
			changes = append(changes, fmt.Sprintf("enforce admins %t → %t", got, b.EnforceAdmins))
		}
		if protection.GetAllowForcePushes().GetEnabled() {
			changes = append(changes, "forbid force pushes")
		}
		if protection.GetAllowDeletions().GetEnabled() {
			changes = append(changes, "forbid deletions")
		}
		if protection.RequiredStatusChecks != nil && protection.RequiredStatusChecks.Strict != b.StrictChecks && len(want) > 0 {
			changes = append(changes, fmt.Sprintf("strict checks %t → %t", protection.RequiredStatusChecks.Strict, b.StrictChecks))
		}
	}
	if !sameSet(current, want) {
		added, removed := diffNames(current, want)
		if len(added) > 0 {
			changes = append(changes, "require "+strings.Join(added, ", "))
		}
		if len(removed) > 0 {
			changes = append(changes, "stop requiring "+strings.Join(removed, ", "))
		}
	}
	if len(changes) == 0 {
		sr.Summary = fmt.Sprintf("%s protected; required: %s", branch, describe(want))
		return nil
	}

	return s.plan(sr, strings.Join(changes, "; "), func() error {
		req := &github.ProtectionRequest{
			RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
				RequiredApprovingReviewCount: b.RequiredReviews,
			},
			EnforceAdmins:    b.EnforceAdmins,
			AllowForcePushes: new(false),
			AllowDeletions:   new(false),
		}
		if len(want) > 0 {
			checks := make([]*github.RequiredStatusCheck, 0, len(want))
			for _, name := range want {
				checks = append(checks, &github.RequiredStatusCheck{Context: name})
			}
			req.RequiredStatusChecks = &github.RequiredStatusChecks{Strict: b.StrictChecks, Checks: &checks}
		}
		_, _, err := r.GitHub.Repositories.UpdateBranchProtection(ctx, s.owner, s.name, branch, req)
		return err
	})
}

// stepRulesetProtection protects the default branch with the repository
// ruleset [RulesetName]: the baseline's review requirement, the required
// checks on the reported-only rule, no deletion and no force push, and the
// devctl App, the repository admins and the owning team as bypass actors
// for pull requests unless the entry opts out of agent merges (agentMerge:
// false; see bypassActors). The ruleset targets the default branch
// wherever it moves. Classic branch protection gives way to the ruleset in
// the same run: its required checks are carried over, then it is removed.
// Rulesets the engine did not create are left alone and reported.
func (r *Runner) stepRulesetProtection(ctx context.Context, s *run, sr *StepResult) error {
	b := s.baseline
	branch := s.branch()

	classic, err := r.classicProtection(ctx, s, branch)
	if err != nil {
		return err
	}
	have, err := r.ownRuleset(ctx, s, sr)
	if err != nil {
		return err
	}

	var from rulesetState
	switch {
	case have != nil:
		from = stateOfRuleset(have)
	case classic != nil:
		from = stateOfClassic(classic)
	}
	current := contexts(from.checks)

	reported, reportedKnown := r.reportedChecks(ctx, s, sr, branch)
	gates, pipelineKnown, err := r.pipelineGates(ctx, s)
	if err != nil {
		return err
	}
	want, err := requiredChecks(b, s.fields.RequiredChecks, current, reported, reportedKnown, gates, pipelineKnown)
	if err != nil {
		return err
	}
	bypass, err := r.bypassActors(ctx, s, sr, from.bypass)
	if err != nil {
		return err
	}

	desired := rulesetState{
		enforcement: github.RulesetEnforcementActive,
		include:     []string{defaultBranchRef},
		reviews:     b.RequiredReviews,
		checks:      ruleChecks(want, actionsGates(b, s.fields.RequiredChecks), from.checks),
		strict:      b.StrictChecks,
		noDeletion:  true,
		noForcePush: true,
		bypass:      bypass,
		team:        s.team,
	}

	var changes []string
	switch {
	case have == nil && classic == nil:
		// The creation carries the baseline; what the repository adds are
		// its checks and its bypass actor.
		base := desired
		base.checks, base.bypass = nil, nil
		changes = append([]string{fmt.Sprintf("create ruleset %q", RulesetName)}, diffRulesetStates(base, desired)...)
	case have == nil:
		changes = append([]string{fmt.Sprintf("create ruleset %q", RulesetName)}, diffRulesetStates(from, desired)...)
	default:
		changes = diffRulesetStates(from, desired)
	}
	if len(changes) > 0 {
		err := s.plan(sr, strings.Join(changes, "; "), func() error {
			err := r.writeRuleset(ctx, s, have, desired)
			if statusCode(err) != 422 || s.team == nil || !hasActor(desired.bypass, teamActor(s.team.GetID())) {
				return err
			}
			// GitHub's own judgment on the team, beyond what its privacy
			// shows: the App stands alone and the team is reported.
			s.report(sr, FindingTeamBypassRefused,
				fmt.Sprintf("GitHub refused team %s (privacy %s) as bypass actor of the ruleset: %v; the ruleset is written with the App and the repository admins", s.team.GetSlug(), s.team.GetPrivacy(), err),
				teamBypassFix(s.owner, s.team.GetSlug()))
			without := desired
			without.bypass = slices.DeleteFunc(slices.Clone(desired.bypass), func(a *github.BypassActor) bool {
				return actorKey(a) == actorKey(teamActor(s.team.GetID()))
			})
			return r.writeRuleset(ctx, s, have, without)
		})
		if err != nil {
			return err
		}
	}
	if classic != nil {
		err := s.plan(sr, "remove classic protection of "+branch, func() error {
			_, err := r.GitHub.Repositories.RemoveBranchProtection(ctx, s.owner, s.name, branch)
			return err
		})
		if err != nil {
			return err
		}
	}
	if len(sr.Changes) == 0 {
		sr.Summary = fmt.Sprintf("%s: ruleset %q; required: %s", branch, RulesetName, describe(want))
	}
	return nil
}

// classicProtection reads the classic branch protection of branch, nil when
// the branch has none.
func (r *Runner) classicProtection(ctx context.Context, s *run, branch string) (*github.Protection, error) {
	protection, resp, err := r.GitHub.Repositories.GetBranchProtection(ctx, s.owner, s.name, branch)
	switch {
	case errors.Is(err, github.ErrBranchNotProtected) || isNotFound(resp, err):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return protection, nil
}

// ownRuleset reads the repository's own rulesets and returns the engine's
// with its rules, nil when there is none. Every other one is reported and
// left alone; the organization's are not read.
func (r *Runner) ownRuleset(ctx context.Context, s *run, sr *StepResult) (*github.RepositoryRuleset, error) {
	list, _, err := r.GitHub.Repositories.GetAllRulesets(ctx, s.owner, s.name, &github.RepositoryListRulesetsOptions{IncludesParents: new(false)})
	if err != nil {
		return nil, err
	}
	var own *github.RepositoryRuleset
	for _, rs := range list {
		if rs.Name != RulesetName {
			s.report(sr, FindingForeignRuleset,
				fmt.Sprintf("ruleset %q is not the engine's and is left alone", rs.Name),
				fmt.Sprintf("declare what it enforces in the entry and delete it, or keep it knowingly; the engine manages %q alone", RulesetName))
			continue
		}
		own = rs
	}
	if own == nil {
		return nil, nil
	}
	// The list carries the summary; the rules, conditions and bypass actors
	// come with the ruleset itself.
	full, _, err := r.GitHub.Repositories.GetRuleset(ctx, s.owner, s.name, own.GetID(), false)
	if err != nil {
		return nil, err
	}
	return full, nil
}

// reportedChecks asks Checks which contexts have reported on branch; known
// is false when the answer is not to be had, and the reason is logged or
// reported so that nothing is required or removed on a guess. The
// discovery is read once per run and shared by every step that asks, the
// error included: one page of the recently merged pull requests, and the
// statuses and check runs of the newest head.
func (r *Runner) reportedChecks(ctx context.Context, s *run, sr *StepResult, branch string) (reported []string, known bool) {
	if r.Checks == nil {
		return nil, false
	}
	if !s.reportedRead {
		s.reported, s.reportedErr = r.Checks.ReportedChecks(ctx, s.repo, branch)
		s.reportedRead = true
	}
	reported, err := s.reported, s.reportedErr
	switch code := statusCode(err); {
	case err == nil:
		return reported, true
	case isNotFound(nil, err), githubclient.IsNotFound(err):
		// No commit on the branch, or no pull request merged into it:
		// nothing has reported yet.
		fmt.Fprintf(s.log, "%s/%s %s: nothing has reported on %s yet\n", s.owner, s.name, sr.Step, branch)
	case code == 401 || code == 403:
		// Discovery reads statuses and check runs, which an App token can
		// only do with the statuses and checks read permissions.
		s.report(sr, FindingUnchecked,
			fmt.Sprintf("cannot read the checks reported on %s with this token: %v", branch, err),
			"grant the token the commit-status and checks read permissions (statuses: read, checks: read); the conditional checks are required on a later run")
	default:
		s.report(sr, FindingUnchecked,
			fmt.Sprintf("cannot read the checks reported on %s: %v", branch, err),
			"the read failed for a reason other than access; the conditional checks are required on a later run")
	}
	return nil, false
}

// writeRuleset creates the engine's ruleset from st, or updates have to it.
func (r *Runner) writeRuleset(ctx context.Context, s *run, have *github.RepositoryRuleset, st rulesetState) error {
	body := st.ruleset(have)
	if have == nil {
		_, _, err := r.GitHub.Repositories.CreateRuleset(ctx, s.owner, s.name, body)
		return err
	}
	_, _, err := r.GitHub.Repositories.UpdateRuleset(ctx, s.owner, s.name, have.GetID(), body)
	return err
}

// bypassActors is the ruleset's bypass list, none when the entry opts out
// of agent merges (agentMerge: false): the devctl App for pull requests
// and, beside it, the repository admins and the owning team in the same
// mode. GitHub evaluates a request under the App's user access token as the
// person, not as the App, so the App's bypass covers the App acting as
// itself — which devctl never does — while the admins' and the team's cover
// a person merging their own green pull request through their token: a
// member of the owning team in its repositories, an admin in every aligned
// repository, as classic protection without enforce_admins let them; direct
// pushes stay forbidden and every bypass is audited. A secret team cannot
// be a bypass actor: it is reported with the fix and the App and the admins
// stand. A run without a team (an undeclared entry) keeps the team actors
// the ruleset has. current is the bypass list of the ruleset as it is.
func (r *Runner) bypassActors(ctx context.Context, s *run, sr *StepResult, current []*github.BypassActor) ([]*github.BypassActor, error) {
	if !s.agentMerge() {
		return nil, nil
	}
	actors := []*github.BypassActor{appActor(r.DevctlAppID), adminActor()}
	team, err := r.owningTeam(ctx, s)
	if err != nil {
		return nil, err
	}
	switch {
	case team == nil:
		for _, a := range current {
			if actorType(a) == github.BypassActorTypeTeam {
				actors = append(actors, a)
			}
		}
	case team.GetPrivacy() == teamPrivacySecret:
		s.report(sr, FindingTeamBypassRefused,
			fmt.Sprintf("team %s is secret and cannot be a bypass actor of the ruleset: the App and the repository admins stand, so a member's own pull request does not merge through the API without a second review unless they are an admin", team.GetSlug()),
			teamBypassFix(s.owner, team.GetSlug()))
	default:
		actors = append(actors, teamActor(team.GetID()))
	}
	return actors, nil
}

// owningTeam reads the team whose file declares the entry — the
// organization's team of the team file's slug — once per run; nil when the
// run has no team (an undeclared entry).
func (r *Runner) owningTeam(ctx context.Context, s *run) (*github.Team, error) {
	if s.req.Team == "" {
		return nil, nil
	}
	if !s.teamRead {
		s.team, _, s.teamErr = r.GitHub.Teams.GetTeamBySlug(ctx, s.owner, s.req.Team)
		s.teamRead = true
	}
	if s.teamErr != nil {
		return nil, fmt.Errorf("team %s/%s: %w", s.owner, s.req.Team, s.teamErr)
	}
	return s.team, nil
}

// teamBypassFix is the fix of a team GitHub does not take as bypass actor.
func teamBypassFix(owner, slug string) string {
	return fmt.Sprintf("make %s/%s a visible team of the organization (privacy: closed, in the team's settings); the next run adds it beside the App and the repository admins", owner, slug)
}

// appActor is a GitHub App as bypass actor for pull requests.
func appActor(id int64) *github.BypassActor {
	return &github.BypassActor{
		ActorID:    new(id),
		ActorType:  new(github.BypassActorTypeIntegration),
		BypassMode: new(github.BypassModePullRequest),
	}
}

// adminActor is the repository role Admin as bypass actor for pull
// requests: what classic protection without enforce_admins granted the
// administrators, on the record this time.
func adminActor() *github.BypassActor {
	return &github.BypassActor{
		ActorID:    new(repositoryAdminRoleID),
		ActorType:  new(github.BypassActorTypeRepositoryRole),
		BypassMode: new(github.BypassModePullRequest),
	}
}

// teamActor is a team as bypass actor for pull requests.
func teamActor(id int64) *github.BypassActor {
	return &github.BypassActor{
		ActorID:    new(id),
		ActorType:  new(github.BypassActorTypeTeam),
		BypassMode: new(github.BypassModePullRequest),
	}
}

// rulesetState is the ruleset as the step compares it: its rules as data,
// without ids, links and timestamps. The classic protection it replaces
// reads into the same shape.
type rulesetState struct {
	enforcement  github.RulesetEnforcement
	include      []string
	reviews      int
	dismissStale bool
	// checks is the required_status_checks rule; nil when there is none.
	checks      []*github.RuleStatusCheck
	strict      bool
	noDeletion  bool
	noForcePush bool
	bypass      []*github.BypassActor
	// team is the owning team, when the run read it: the name of its actor
	// in the description of a change. The comparison is by id.
	team *github.Team
}

func stateOfRuleset(rs *github.RepositoryRuleset) rulesetState {
	st := rulesetState{enforcement: rs.Enforcement, bypass: rs.BypassActors}
	if rs.Conditions != nil && rs.Conditions.RefName != nil {
		st.include = rs.Conditions.RefName.Include
	}
	if rules := rs.Rules; rules != nil {
		if pr := rules.PullRequest; pr != nil {
			st.reviews = pr.RequiredApprovingReviewCount
			st.dismissStale = pr.DismissStaleReviewsOnPush
		}
		if rc := rules.RequiredStatusChecks; rc != nil {
			st.checks = rc.RequiredStatusChecks
			st.strict = rc.StrictRequiredStatusChecksPolicy
		}
		st.noDeletion = rules.Deletion != nil
		st.noForcePush = rules.NonFastForward != nil
	}
	return st
}

// stateOfClassic reads a classic branch protection into the ruleset's shape,
// so that the migration is described as the differences between the two.
func stateOfClassic(p *github.Protection) rulesetState {
	st := rulesetState{
		enforcement:  github.RulesetEnforcementActive,
		include:      []string{defaultBranchRef},
		reviews:      p.GetRequiredPullRequestReviews().GetRequiredApprovingReviewCount(),
		dismissStale: p.GetRequiredPullRequestReviews().GetDismissStaleReviews(),
		noDeletion:   !p.GetAllowDeletions().GetEnabled(),
		noForcePush:  !p.GetAllowForcePushes().GetEnabled(),
	}
	if rc := p.RequiredStatusChecks; rc != nil {
		st.strict = rc.Strict
		if rc.Checks != nil {
			for _, c := range *rc.Checks {
				st.checks = append(st.checks, &github.RuleStatusCheck{Context: c.GetContext(), IntegrationID: c.AppID})
			}
		}
	}
	return st
}

// ruleset is the API object of the state: the engine's rules over the rules
// of have it does not manage, which stay. An empty bypass list is sent as
// such, since an omitted one keeps the actors the ruleset has.
func (st rulesetState) ruleset(have *github.RepositoryRuleset) github.RepositoryRuleset {
	var rules github.RepositoryRulesetRules
	if have != nil && have.Rules != nil {
		rules = *have.Rules
	}
	rules.PullRequest = &github.PullRequestRuleParameters{
		RequiredApprovingReviewCount: st.reviews,
		DismissStaleReviewsOnPush:    st.dismissStale,
	}
	rules.RequiredStatusChecks = nil
	if len(st.checks) > 0 {
		rules.RequiredStatusChecks = &github.RequiredStatusChecksRuleParameters{
			RequiredStatusChecks:             st.checks,
			StrictRequiredStatusChecksPolicy: st.strict,
		}
	}
	rules.Deletion, rules.NonFastForward = nil, nil
	if st.noDeletion {
		rules.Deletion = &github.EmptyRuleParameters{}
	}
	if st.noForcePush {
		rules.NonFastForward = &github.EmptyRuleParameters{}
	}
	bypass := st.bypass
	if bypass == nil {
		bypass = []*github.BypassActor{}
	}
	return github.RepositoryRuleset{
		Name:         RulesetName,
		Target:       new(github.RulesetTargetBranch),
		Enforcement:  st.enforcement,
		BypassActors: bypass,
		Conditions: &github.RepositoryRulesetConditions{
			RefName: &github.RepositoryRulesetRefConditionParameters{Include: st.include, Exclude: []string{}},
		},
		Rules: &rules,
	}
}

// diffRulesetStates describes what turns from into to, one change per
// difference; nothing when they are the same.
func diffRulesetStates(from, to rulesetState) []string {
	var changes []string
	if from.enforcement != to.enforcement {
		changes = append(changes, fmt.Sprintf("enforcement %s → %s", from.enforcement, to.enforcement))
	}
	if !slices.Equal(from.include, to.include) {
		changes = append(changes, fmt.Sprintf("target %s → %s", describe(from.include), describe(to.include)))
	}
	if from.reviews != to.reviews {
		changes = append(changes, fmt.Sprintf("required reviews %d → %d", from.reviews, to.reviews))
	}
	if from.dismissStale != to.dismissStale {
		changes = append(changes, fmt.Sprintf("dismiss stale reviews %t → %t", from.dismissStale, to.dismissStale))
	}
	if !from.noForcePush && to.noForcePush {
		changes = append(changes, "forbid force pushes")
	}
	if !from.noDeletion && to.noDeletion {
		changes = append(changes, "forbid deletions")
	}
	added, removed := diffNames(contexts(from.checks), contexts(to.checks))
	if len(added) > 0 {
		changes = append(changes, "require "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		changes = append(changes, "stop requiring "+strings.Join(removed, ", "))
	}
	pins := map[string]*int64{}
	for _, c := range from.checks {
		pins[c.Context] = c.IntegrationID
	}
	for _, c := range to.checks {
		was, ok := pins[c.Context]
		switch {
		case !ok || sameID(was, c.IntegrationID):
		case c.IntegrationID == nil:
			changes = append(changes, fmt.Sprintf("unpin %s", c.Context))
		default:
			changes = append(changes, fmt.Sprintf("pin %s to app %d", c.Context, *c.IntegrationID))
		}
	}
	if len(to.checks) > 0 && from.strict != to.strict {
		changes = append(changes, fmt.Sprintf("strict checks %t → %t", from.strict, to.strict))
	}
	if !sameActors(from.bypass, to.bypass) {
		switch len(to.bypass) {
		case 0:
			changes = append(changes, "remove bypass actors")
		case 1:
			changes = append(changes, "bypass actor: "+describeActors(to.bypass, to.team))
		default:
			changes = append(changes, "bypass actors: "+describeActors(to.bypass, to.team))
		}
	}
	return changes
}

// ruleChecks turns the required contexts into the rule's checks. A GitHub
// Actions gate is pinned to the GitHub Actions App, so no other integration
// satisfies it; any other context keeps the pin it has, none for a CircleCI
// status.
func ruleChecks(want []string, actions map[string]bool, current []*github.RuleStatusCheck) []*github.RuleStatusCheck {
	pins := map[string]*int64{}
	for _, c := range current {
		pins[c.Context] = c.IntegrationID
	}
	checks := make([]*github.RuleStatusCheck, 0, len(want))
	for _, name := range want {
		id := pins[name]
		if actions[name] {
			id = new(gitHubActionsAppID)
		}
		checks = append(checks, &github.RuleStatusCheck{Context: name, IntegrationID: id})
	}
	return checks
}

// actionsGates are the contexts known to be GitHub Actions checks: the
// baseline's gates and the entry's declared requiredChecks.
func actionsGates(b Baseline, declared []string) map[string]bool {
	return toSet(slices.Concat(b.RequiredChecks, b.RequiredChecksIfReported, declared))
}

// sameID says whether two optional ids are both absent or the same.
func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func contexts(checks []*github.RuleStatusCheck) []string {
	names := make([]string, 0, len(checks))
	for _, c := range checks {
		names = append(names, c.Context)
	}
	return names
}

// sameActors says whether two bypass lists name the same actors in the same
// modes, in any order: the lists are compared as sets.
func sameActors(a, b []*github.BypassActor) bool {
	return sameSet(actorKeys(a), actorKeys(b))
}

// actorKey identifies a bypass actor: its type, id and mode.
func actorKey(a *github.BypassActor) string {
	return fmt.Sprintf("%s/%d/%s", actorType(a), a.GetActorID(), bypassMode(a))
}

// actorType and bypassMode are the actor's type and mode, "" when unset.
func actorType(a *github.BypassActor) github.BypassActorType {
	if a.ActorType == nil {
		return ""
	}
	return *a.ActorType
}

func bypassMode(a *github.BypassActor) github.BypassMode {
	if a.BypassMode == nil {
		return ""
	}
	return *a.BypassMode
}

func actorKeys(actors []*github.BypassActor) []string {
	out := make([]string, 0, len(actors))
	for _, a := range actors {
		out = append(out, actorKey(a))
	}
	return out
}

// hasActor says whether actors holds actor: same type, id and mode.
func hasActor(actors []*github.BypassActor, actor *github.BypassActor) bool {
	return slices.Contains(actorKeys(actors), actorKey(actor))
}

func describeActors(actors []*github.BypassActor, team *github.Team) string {
	out := make([]string, 0, len(actors))
	for _, a := range actors {
		out = append(out, describeActor(a, team))
	}
	return strings.Join(out, ", ")
}

// describeActor is "App 123 on pull requests" for the devctl App,
// "repository admins on pull requests" for the admin role, "team <slug> on
// pull requests" for the owning team (by id for any other team), "<type>
// <id> (<mode>)" for any other actor.
func describeActor(a *github.BypassActor, team *github.Team) string {
	kind, mode := actorType(a), bypassMode(a)
	if mode != github.BypassModePullRequest {
		return fmt.Sprintf("%s %d (%s)", kind, a.GetActorID(), mode)
	}
	switch kind {
	case github.BypassActorTypeIntegration:
		return fmt.Sprintf("App %d on pull requests", a.GetActorID())
	case github.BypassActorTypeRepositoryRole:
		if a.GetActorID() == repositoryAdminRoleID {
			return "repository admins on pull requests"
		}
		return fmt.Sprintf("repository role %d on pull requests", a.GetActorID())
	case github.BypassActorTypeTeam:
		if team != nil && team.GetID() == a.GetActorID() {
			return fmt.Sprintf("team %s on pull requests", team.GetSlug())
		}
		return fmt.Sprintf("team %d on pull requests", a.GetActorID())
	}
	return fmt.Sprintf("%s %d (%s)", kind, a.GetActorID(), mode)
}

// pipelineGates reads the generated pipeline — the request's documents, or
// the repository's .circleci — and returns the contexts of its branch-side
// jobs; known is false when there is no pipeline file.
func (r *Runner) pipelineGates(ctx context.Context, s *run) (gates []string, known bool, err error) {
	files := s.req.Pipeline
	if files == nil {
		for _, name := range PipelineFiles {
			data, found, err := r.fileContent(ctx, s.owner, s.name, ".circleci/"+name, s.branch())
			if err != nil {
				return nil, false, err
			}
			if found {
				files = append(files, data)
			}
		}
	}
	if len(files) == 0 {
		return nil, false, nil
	}
	gates, err = GateContexts(files...)
	if err != nil {
		return nil, false, fmt.Errorf("%s: .circleci: %w", s.slug(), err)
	}
	return gates, true, nil
}

// requiredChecks computes the required contexts: the baseline's
// unconditional checks and the entry's declared ones, whatever reported;
// the currently required ones that are neither ignored, nor CircleCI jobs
// the pipeline no longer has, nor unreported (when what reported is known);
// and the candidates — the baseline's conditional checks and the pipeline's
// gates — once they have reported. A declared context is in the first set,
// so neither the ignore rule nor the ghost removal reaches it. Nothing is
// added or removed on a guess: an unknown pipeline keeps every CircleCI
// context, unknown reports keep every current context.
func requiredChecks(b Baseline, declared, current, reported []string, reportedKnown bool, gates []string, pipelineKnown bool) ([]string, error) {
	ignored := make([]*regexp.Regexp, 0, len(b.IgnoredChecks))
	for _, expr := range b.IgnoredChecks {
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("baseline IgnoredChecks %q: %w", expr, err)
		}
		ignored = append(ignored, re)
	}
	isIgnored := func(name string) bool {
		for _, re := range ignored {
			if re.MatchString(name) {
				return true
			}
		}
		return false
	}
	isReported := toSet(reported)
	stale := toSet(StaleCircleCIContexts(current, gates))

	var want []string
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			want = append(want, name)
		}
	}
	for _, name := range append(append([]string{}, b.RequiredChecks...), declared...) {
		add(name)
	}
	for _, name := range current {
		switch {
		case isIgnored(name):
		case pipelineKnown && stale[name]:
		case reportedKnown && !isReported[name]:
		default:
			add(name)
		}
	}
	if reportedKnown {
		for _, name := range append(append([]string{}, b.RequiredChecksIfReported...), gates...) {
			if isReported[name] && !isIgnored(name) {
				add(name)
			}
		}
	}
	return want, nil
}

// statusCode is the HTTP status of a go-github error, 0 for any other.
func statusCode(err error) int {
	var ghErr *github.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response != nil {
		return ghErr.Response.StatusCode
	}
	return 0
}

func toSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// diffNames returns what want adds to and removes from current.
func diffNames(current, want []string) (added, removed []string) {
	has, wants := toSet(current), toSet(want)
	for _, n := range want {
		if !has[n] {
			added = append(added, n)
		}
	}
	for _, n := range current {
		if !wants[n] {
			removed = append(removed, n)
		}
	}
	return added, removed
}

func describe(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
