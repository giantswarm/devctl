package reconcile

import (
	"context"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"
)

// ctxGatekeeper is a context another system (Tekton) reports, so no pipeline
// of the repository names it and it reports only on pull requests.
const ctxGatekeeper = "Heimdall - PR Gatekeeper"

// seedTeamProtection seeds a repository whose team requires a CircleCI job
// its repository's pipeline does not have and a context of another system,
// neither of them reported on the default branch.
func seedTeamProtection(h *harness) *fakeRepo {
	r := h.gh.addRepo(owner, name)
	r.files[".circleci/workflows.yml"] = generatedWorkflows
	r.statuses = []string{ctxSetup}
	r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxAppCatalog), statusCheck(ctxGatekeeper)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
	return r
}

// TestProtectionKeepRequiredChecks is the baseline `devctl repo checks`
// runs on: a context already required stays unless the run was told to
// remove it, whatever reported and whatever the repository's pipeline holds.
func TestProtectionKeepRequiredChecks(t *testing.T) {
	t.Run("neither an unreported context nor a CircleCI job the repository's pipeline lacks is removed", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.baseline.KeepRequiredChecks = true
		seedTeamProtection(h)

		check := h.run(ModeCheck, false, StepProtection)
		require.Equal(t, VerdictOK, check.Step(StepProtection).Verdict, "%+v", check.Step(StepProtection))
		h.run(ModeRepair, false, StepProtection)
		require.Equal(t, []string{ctxAppCatalog, ctxGatekeeper}, checkContexts(h.repo().ruleset(RulesetName)))
	})

	t.Run("a context the run is told to remove goes", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.baseline.KeepRequiredChecks = true
		h.baseline.IgnoredChecks = []string{"^" + ctxGatekeeper + "$"}
		seedTeamProtection(h)

		h.run(ModeRepair, false, StepProtection)
		require.Equal(t, []string{ctxAppCatalog}, checkContexts(h.repo().ruleset(RulesetName)))
	})

	t.Run("the pipeline the request hands in removes its missing CircleCI jobs, nothing else", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.baseline.KeepRequiredChecks = true
		seedTeamProtection(h)

		_, err := h.runner.Run(context.Background(), Request{Team: team, Entry: h.entry, Mode: ModeRepair, Steps: []Step{StepProtection}, Pipeline: [][]byte{[]byte(generatedWorkflows)}})
		require.NoError(t, err)
		require.Equal(t, []string{ctxGatekeeper}, checkContexts(h.repo().ruleset(RulesetName)))
	})

	t.Run("the company baseline still drops what has not reported and the stale CircleCI jobs", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		seedTeamProtection(h)

		h.run(ModeRepair, false, StepProtection)
		require.Empty(t, checkContexts(h.repo().ruleset(RulesetName)))
	})
}

func TestRequiredChecksKeep(t *testing.T) {
	b := DefaultBaseline()
	b.KeepRequiredChecks = true

	got, err := requiredChecks(b, nil, []string{ctxGoBuild, ctxGatekeeper}, []string{ctxSemantic}, true, []string{ctxBuildChart}, false)
	require.NoError(t, err)
	require.Equal(t, []string{ctxGoBuild, ctxGatekeeper, ctxSemantic}, got, "the unreported contexts stay; the reported candidate joins")
}
