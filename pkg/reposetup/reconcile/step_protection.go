package reconcile

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/go-github/v92/github"
)

// stepProtection protects the default branch as the baseline says and
// keeps the required checks on the reported-only rule: a context is
// required once it has reported on the default branch or a recently merged
// pull request; a required context nothing reports, a CircleCI context
// without a job in the pipeline and an ignored context are removed.
func (r *Runner) stepProtection(ctx context.Context, s *run, sr *StepResult) error {
	b := s.baseline
	branch := s.branch()

	protection, resp, err := r.GitHub.Repositories.GetBranchProtection(ctx, s.owner, s.name, branch)
	protected := true
	switch {
	case errors.Is(err, github.ErrBranchNotProtected) || isNotFound(resp, err):
		protected = false
		protection = nil
	case err != nil:
		return err
	}

	var current []string
	if protection != nil && protection.RequiredStatusChecks != nil && protection.RequiredStatusChecks.Checks != nil {
		for _, c := range *protection.RequiredStatusChecks.Checks {
			current = append(current, c.GetContext())
		}
	}

	var reported []string
	reportedKnown := false
	if r.Checks != nil {
		reported, err = r.Checks.ReportedChecks(ctx, s.repo, branch)
		if err != nil {
			// Discovery reads statuses and check runs, which an App token
			// can only do with the Checks permission; nothing is required or
			// removed on a guess.
			s.report(sr, FindingUnchecked,
				fmt.Sprintf("cannot read the checks reported on %s: %v", branch, err),
				"the conditional checks are required on a later run; grant the token the commit-status and checks read permissions")
		} else {
			reportedKnown = true
		}
	}

	gates, pipelineKnown, err := r.pipelineGates(ctx, s)
	if err != nil {
		return err
	}

	want, err := requiredChecks(b, current, reported, reportedKnown, gates, pipelineKnown)
	if err != nil {
		return err
	}

	var changes []string
	if !protected {
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
			changes = append(changes, fmt.Sprintf("strict checks → %t", b.StrictChecks))
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
// unconditional checks; the currently required ones that are neither
// ignored, nor CircleCI jobs the pipeline no longer has, nor unreported
// (when what reported is known); and the candidates — the baseline's
// conditional checks and the pipeline's gates — once they have reported.
// Nothing is added or removed on a guess: an unknown pipeline keeps every
// CircleCI context, unknown reports keep every current context.
func requiredChecks(b Baseline, current, reported []string, reportedKnown bool, gates []string, pipelineKnown bool) ([]string, error) {
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
	for _, name := range b.RequiredChecks {
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
