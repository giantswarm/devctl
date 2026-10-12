package reconcile

import (
	"testing"

	"github.com/google/go-github/v92/github"
	"github.com/stretchr/testify/require"
)

const (
	ctxAppCatalog = "ci/circleci: app-catalog"
	ctxBuildChart = "ci/circleci: build-chart"

	// handKeptConfig is a hand-kept pipeline without the generated files:
	// its job reports on pull requests as ctxAppCatalog.
	handKeptConfig = `version: 2.1
workflows:
  package:
    jobs:
    - architect/push-to-app-catalog:
        name: app-catalog
`
	generatedWorkflows = `version: 2.1
workflows:
  build:
    jobs:
    - architect/run-tests-with-ats:
        name: build-chart
`
)

// seedMigration seeds a repository on a hand-kept pipeline whose ruleset
// requires the old pipeline's context, reported on the default branch.
func seedMigration(h *harness) *fakeRepo {
	r := h.gh.addRepo(owner, name)
	r.files[".circleci/config.yml"] = handKeptConfig
	delete(r.files, ".circleci/workflows.yml")
	r.statuses = []string{ctxAppCatalog}
	r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxAppCatalog)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
	return r
}

// openAlignPullRequest opens the align-files pull request that replaces the
// hand-kept pipeline with the generated one.
func openAlignPullRequest(r *fakeRepo) {
	r.branchFiles[AlignFilesBranch] = map[string]string{
		".circleci/config.yml":    "version: 2.1\nsetup: true\n",
		".circleci/workflows.yml": generatedWorkflows,
	}
	r.prs = append(r.prs, &github.PullRequest{
		Number: new(7), State: new("open"),
		Head: &github.PullRequestBranch{SHA: new("align-head"), Ref: new(AlignFilesBranch)},
		Base: &github.PullRequestBranch{Ref: new("main")},
	})
}

// TestProtectionFollowsTheAlignPullRequestsPipeline is the move from a
// hand-kept pipeline to the generated one: the align-files pull request
// carries the generated pipeline, whose jobs report other contexts than the
// hand-kept one's. The old context is stale against the pipeline the pull
// request brings and stops being required, so the pull request can merge;
// the generated jobs are required once they have reported on the default
// branch.
func TestProtectionFollowsTheAlignPullRequestsPipeline(t *testing.T) {
	t.Run("the open align-files pull request's pipeline makes the old context stale", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		openAlignPullRequest(seedMigration(h))

		check := h.run(ModeCheck, false, StepProtection)
		sr := check.Step(StepProtection)
		require.Equal(t, VerdictDrift, sr.Verdict, "%+v", sr)
		require.Equal(t, []string{"stop requiring " + ctxAppCatalog}, sr.Changes)
		require.Empty(t, h.mutations(), "a check must not write")

		repair := h.run(ModeRepair, false, StepProtection)
		require.Equal(t, VerdictRepaired, repair.Step(StepProtection).Verdict, "%+v", repair.Step(StepProtection))
		require.Empty(t, checkContexts(h.repo().ruleset(RulesetName)), "the generated jobs have not reported on main yet")

		again := h.run(ModeCheck, false, StepProtection)
		require.Equal(t, VerdictOK, again.Step(StepProtection).Verdict, "%+v", again.Step(StepProtection))
	})

	t.Run("without an align-files pull request the hand-kept pipeline's context stays", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		seedMigration(h)

		check := h.run(ModeCheck, false, StepProtection)
		require.Equal(t, VerdictOK, check.Step(StepProtection).Verdict, "%+v", check.Step(StepProtection))
		require.Equal(t, []string{ctxAppCatalog}, checkContexts(h.repo().ruleset(RulesetName)))
	})

	t.Run("after the merge the generated jobs are required once they reported", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		r := seedMigration(h)
		openAlignPullRequest(r)
		h.run(ModeRepair, false, StepProtection)

		// The pull request merged: main carries the generated pipeline, and
		// its jobs report there.
		for p, c := range r.branchFiles[AlignFilesBranch] {
			r.files[p] = c
		}
		r.prs = nil
		r.statuses = []string{ctxSetup, ctxBuildChart}

		check := h.run(ModeCheck, false, StepProtection)
		require.Equal(t, []string{"require " + ctxBuildChart}, check.Step(StepProtection).Changes)
		h.run(ModeRepair, false, StepProtection)
		require.Equal(t, []string{ctxBuildChart}, checkContexts(h.repo().ruleset(RulesetName)))
	})

	t.Run("a default branch with the generated pipeline does not look for the pull request", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		r := h.gh.addRepo(owner, name)
		r.statuses = []string{ctxGoBuild}
		r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
		openAlignPullRequest(r)

		check := h.run(ModeCheck, false, StepProtection)
		require.Equal(t, VerdictOK, check.Step(StepProtection).Verdict, "%+v", check.Step(StepProtection))
		require.Equal(t, []string{ctxGoBuild}, checkContexts(h.repo().ruleset(RulesetName)), "main's pipeline decides")
	})
}
