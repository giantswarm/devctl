package reconcile

import (
	"context"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"
)

const (
	ctxIntegration = "ci/circleci: mutation-integration-test"
	ctxPushCatalog = "ci/circleci: push-to-control-plane-catalog"
	ctxChartTests  = "ci/circleci: execute chart tests"

	// ownJobsConfig is a hand-kept pipeline: an integration test on every
	// branch, a push skipped on main, a tag-only release, an approval, a
	// nightly workflow and a conditional one. The first two gate pull
	// requests; the others never report on one.
	ownJobsConfig = `version: 2.1
workflows:
  version: 2
  build:
    jobs:
    - architect/go-build:
        name: go-build
    - architect/push-to-app-catalog:
        name: push-to-control-plane-catalog
        filters:
          branches:
            ignore:
            - main
    - architect/integration-test:
        name: mutation-integration-test
    - architect/push-to-app-catalog:
        name: release
        filters:
          branches:
            ignore: /.*/
    - hold:
        type: approval
  nightly:
    triggers:
    - schedule:
        cron: "0 3 * * *"
        filters:
          branches:
            only: main
    jobs:
    - nightly-e2e
  manual:
    when: << pipeline.parameters.manual >>
    jobs:
    - manual-e2e
`
	// customJobs is a generated repository's custom.yml.
	customJobs = `workflows:
  build:
    jobs:
    - architect/run-tests-with-ats:
        name: execute chart tests
`
)

// seedOwnJobs seeds a repository on a hand-kept pipeline whose ruleset
// requires none of its jobs any more, every one of them reported.
func seedOwnJobs(h *harness) *fakeRepo {
	r := h.gh.addRepo(owner, name)
	delete(r.files, ".circleci/workflows.yml")
	r.files[".circleci/config.yml"] = ownJobsConfig
	r.statuses = []string{ctxGoBuild, ctxPushCatalog, ctxIntegration, "ci/circleci: nightly-e2e", "ci/circleci: manual-e2e", "ci/circleci: hold"}
	r.addRuleset(RulesetName, []*github.RuleStatusCheck{actionsCheck(ctxSemantic)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
	return r
}

// TestGateJobsSkipsWhatAPushDoesNotRun covers the jobs that never report on
// a pull request: tag-only and main-only ones, approvals, scheduled and
// conditional workflows.
func TestGateJobsSkipsWhatAPushDoesNotRun(t *testing.T) {
	got, err := GateContexts([]byte(ownJobsConfig))
	require.NoError(t, err)
	require.Equal(t, []string{ctxGoBuild, ctxIntegration, ctxPushCatalog}, got)

	unless, err := GateContexts([]byte("workflows:\n  skip:\n    unless: true\n    jobs:\n    - lint\n"))
	require.NoError(t, err)
	require.Empty(t, unless)
}

// TestProtectionRequiresTheRepositorysOwnJobs is `devctl repo checks`
// (KeepRequiredChecks, no devctl App id) requiring a repository's own
// CircleCI jobs again: a hand-kept config.yml's, and a generated pipeline's
// custom.yml jobs, on a repository the engine's ruleset protects.
func TestProtectionRequiresTheRepositorysOwnJobs(t *testing.T) {
	keep := func(h *harness) {
		h.baseline.KeepRequiredChecks = true
		h.runner.DevctlAppID = 0
	}

	t.Run("a hand-kept config.yml's reported branch-side jobs are required again; the rest of the ruleset stays", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		keep(h)
		r := seedOwnJobs(h)
		before := *r.ruleset(RulesetName)

		check := h.run(ModeCheck, false, StepProtection)
		sr := check.Step(StepProtection)
		require.Equal(t, VerdictDrift, sr.Verdict, "%+v", sr)
		require.Equal(t, []string{"require " + ctxGoBuild + ", " + ctxIntegration + ", " + ctxPushCatalog}, sr.Changes)
		require.Empty(t, h.mutations(), "a check must not write")

		repair := h.run(ModeRepair, false, StepProtection)
		require.Equal(t, VerdictRepaired, repair.Step(StepProtection).Verdict, "%+v", repair.Step(StepProtection))
		after := r.ruleset(RulesetName)
		require.Equal(t, []string{ctxSemantic, ctxGoBuild, ctxIntegration, ctxPushCatalog}, checkContexts(after))
		require.Equal(t, before.BypassActors, after.BypassActors, "the bypass actors are sent back as they were")
		require.Equal(t, before.Rules.PullRequest, after.Rules.PullRequest)
		require.Equal(t, before.Conditions, after.Conditions)
		require.NotNil(t, after.Rules.Deletion)
		require.NotNil(t, after.Rules.NonFastForward)
		require.Equal(t, int64(15368), after.Rules.RequiredStatusChecks.RequiredStatusChecks[0].GetIntegrationID(), "a pinned check keeps its pin")

		again := h.run(ModeCheck, false, StepProtection)
		require.Equal(t, VerdictOK, again.Step(StepProtection).Verdict, "%+v", again.Step(StepProtection))
	})

	t.Run("a hand-kept job that has not reported is not required", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		keep(h)
		r := seedOwnJobs(h)
		r.statuses = []string{ctxGoBuild}

		h.run(ModeRepair, false, StepProtection)
		require.Equal(t, []string{ctxSemantic, ctxGoBuild}, checkContexts(r.ruleset(RulesetName)))
	})

	t.Run("a hand-kept config.yml removes no CircleCI context, even under the company baseline", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		r := seedOwnJobs(h)
		r.ruleset(RulesetName).Rules.RequiredStatusChecks.RequiredStatusChecks = []*github.RuleStatusCheck{statusCheck(ctxChartTests)}
		r.statuses = append(r.statuses, ctxChartTests)

		check := h.run(ModeCheck, false, StepProtection)
		require.Equal(t, []string{"require " + ctxGoBuild + ", " + ctxIntegration + ", " + ctxPushCatalog}, check.Step(StepProtection).Changes,
			"the context the config.yml reading lacks stays")
	})

	t.Run("a generated pipeline's custom.yml jobs are required in the ruleset", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		keep(h)
		r := h.gh.addRepo(owner, name)
		r.files[".circleci/workflows.yml"] = generatedWorkflows
		r.files[".circleci/custom.yml"] = customJobs
		r.statuses = []string{ctxBuildChart, ctxChartTests}
		r.addRuleset(RulesetName, nil, adminBypass())

		h.run(ModeRepair, false, StepProtection)
		after := r.ruleset(RulesetName)
		require.Equal(t, []string{ctxBuildChart, ctxChartTests}, checkContexts(after))
		require.Equal(t, DefaultBaseline().StrictChecks, after.Rules.RequiredStatusChecks.StrictRequiredStatusChecksPolicy, "a new rule takes the baseline's strictness")
	})

	t.Run("the generated pipeline wins over config.yml", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		keep(h)
		r := seedOwnJobs(h)
		r.files[".circleci/workflows.yml"] = generatedWorkflows
		r.statuses = append(r.statuses, ctxBuildChart)

		h.run(ModeRepair, false, StepProtection)
		require.Equal(t, []string{ctxSemantic, ctxBuildChart}, checkContexts(r.ruleset(RulesetName)))
	})

	t.Run("classic protection requires a hand-kept config.yml's jobs", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		keep(h)
		r := h.gh.addRepo(owner, name)
		delete(r.files, ".circleci/workflows.yml")
		r.files[".circleci/config.yml"] = ownJobsConfig
		r.statuses = []string{ctxIntegration}
		r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, checks: []string{ctxSemantic}}

		h.run(ModeRepair, false, StepProtection)
		require.Equal(t, []string{ctxSemantic, ctxIntegration}, r.protection.checks)
		require.Empty(t, r.rulesets, "a run without the App id writes no ruleset")
	})

	t.Run("the request's pipeline still decides over the repository's", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		keep(h)
		r := seedOwnJobs(h)

		_, err := h.runner.Run(context.Background(), Request{Team: team, Entry: h.entry, Mode: ModeRepair, Steps: []Step{StepProtection}, Pipeline: [][]byte{[]byte(customJobs)}})
		require.NoError(t, err)
		require.Equal(t, []string{ctxSemantic}, checkContexts(r.ruleset(RulesetName)), "none of the handed-in pipeline's jobs reported")
	})
}

// TestProtectionRequiresHeimdallWithE2ETests covers a repository with a
// tests/e2e directory: Heimdall's gatekeeper is required once it has
// reported, and not without the directory.
func TestProtectionRequiresHeimdallWithE2ETests(t *testing.T) {
	seed := func(h *harness, e2e bool) *fakeRepo {
		r := h.gh.addRepo(owner, name)
		if e2e {
			r.files[E2ETestsDir+"/main_test.go"] = "package e2e\n"
		}
		r.checkRuns = []string{HeimdallContext}
		r.addRuleset(RulesetName, nil, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
		return r
	}

	t.Run("a reported gatekeeper is required with tests/e2e", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		r := seed(h, true)

		h.run(ModeRepair, false, StepProtection)
		require.Contains(t, checkContexts(r.ruleset(RulesetName)), HeimdallContext)
	})

	t.Run("a gatekeeper that has not reported is not required", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		r := seed(h, true)
		r.checkRuns = nil

		h.run(ModeRepair, false, StepProtection)
		require.NotContains(t, checkContexts(r.ruleset(RulesetName)), HeimdallContext)
	})

	t.Run("without tests/e2e the gatekeeper is not required", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		r := seed(h, false)

		h.run(ModeRepair, false, StepProtection)
		require.NotContains(t, checkContexts(r.ruleset(RulesetName)), HeimdallContext)
	})
}
