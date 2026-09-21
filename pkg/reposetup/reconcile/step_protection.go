package reconcile

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/go-github/v92/github"
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

// stepProtection protects the default branch with the repository ruleset
// [RulesetName]: the baseline's review requirement; the required checks on
// the reported-only rule (a context is required once it has reported on the
// default branch or a recently merged pull request; a required context
// nothing reports, a CircleCI context without a job in the pipeline and an
// ignored context are removed; the entry's requiredChecks are required
// whatever reported and never removed); no deletion and no force push; and
// the devctl App as bypass actor for pull requests when the entry lets
// agents merge (agentMerge, true unless declared false) and
// [Runner.DevctlAppID] names the App. The ruleset targets the default branch
// wherever it moves. Classic branch protection gives way to the ruleset in
// the same run: its required checks are carried over, then it is removed.
// Rulesets the engine did not create are left alone and reported.
func (r *Runner) stepProtection(ctx context.Context, s *run, sr *StepResult) error {
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

	desired := rulesetState{
		enforcement: github.RulesetEnforcementActive,
		include:     []string{defaultBranchRef},
		reviews:     b.RequiredReviews,
		checks:      ruleChecks(want, actionsGates(b, s.fields.RequiredChecks), from.checks),
		strict:      b.StrictChecks,
		noDeletion:  true,
		noForcePush: true,
		bypass:      r.bypassActors(s, sr, have),
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
			body := desired.ruleset(have)
			if have == nil {
				_, _, err := r.GitHub.Repositories.CreateRuleset(ctx, s.owner, s.name, body)
				return err
			}
			_, _, err := r.GitHub.Repositories.UpdateRuleset(ctx, s.owner, s.name, have.GetID(), body)
			return err
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
// reported so that nothing is required or removed on a guess.
func (r *Runner) reportedChecks(ctx context.Context, s *run, sr *StepResult, branch string) (reported []string, known bool) {
	if r.Checks == nil {
		return nil, false
	}
	reported, err := r.Checks.ReportedChecks(ctx, s.repo, branch)
	switch code := statusCode(err); {
	case err == nil:
		return reported, true
	case isNotFound(nil, err):
		// No commit on the branch: nothing has reported yet.
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

// bypassActors is the ruleset's bypass list: the devctl App for pull
// requests when the entry lets agents merge and the App is configured; none
// when the entry opts out (agentMerge: false). Without the App id the step
// cannot manage the list — a fresh ruleset gets none, an existing one keeps
// its own — and reports the missing configuration.
func (r *Runner) bypassActors(s *run, sr *StepResult, have *github.RepositoryRuleset) []*github.BypassActor {
	if !s.agentMerge() {
		return nil
	}
	if r.DevctlAppID != 0 {
		return []*github.BypassActor{{
			ActorID:    new(r.DevctlAppID),
			ActorType:  new(github.BypassActorTypeIntegration),
			BypassMode: new(github.BypassModePullRequest),
		}}
	}
	var kept []*github.BypassActor
	if have != nil {
		kept = have.BypassActors
	}
	message := "no bypass actor: the devctl App id is not configured, so no agent merges past the required review"
	if len(kept) > 0 {
		message = fmt.Sprintf("%d bypass actor(s) not checked: the devctl App id is not configured", len(kept))
	}
	s.report(sr, FindingUnchecked, message,
		"pass the App's numeric id (its settings page; not the client id) with --devctl-app-id; the bypass actor is written on the next run")
	return kept
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
		if len(to.bypass) == 0 {
			changes = append(changes, "remove bypass actors")
		} else {
			changes = append(changes, "bypass actor: "+describeActors(to.bypass))
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
// modes, in any order.
func sameActors(a, b []*github.BypassActor) bool {
	return sameSet(describeEach(a), describeEach(b))
}

func describeEach(actors []*github.BypassActor) []string {
	out := make([]string, 0, len(actors))
	for _, a := range actors {
		out = append(out, describeActor(a))
	}
	return out
}

func describeActors(actors []*github.BypassActor) string {
	return strings.Join(describeEach(actors), ", ")
}

// describeActor is "App 123 on pull requests" for the devctl App, "<type> <id>
// (<mode>)" for any other actor.
func describeActor(a *github.BypassActor) string {
	actorType, mode := "", ""
	if a.ActorType != nil {
		actorType = string(*a.ActorType)
	}
	if a.BypassMode != nil {
		mode = string(*a.BypassMode)
	}
	if actorType == string(github.BypassActorTypeIntegration) && mode == string(github.BypassModePullRequest) {
		return fmt.Sprintf("App %d on pull requests", a.GetActorID())
	}
	return fmt.Sprintf("%s %d (%s)", actorType, a.GetActorID(), mode)
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
