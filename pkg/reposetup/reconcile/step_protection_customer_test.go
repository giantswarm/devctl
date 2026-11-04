package reconcile

import (
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"
)

func TestCustomerProtection(t *testing.T) {
	testCases := []struct {
		name        string
		declaration string
		wantCheck   Verdict
		wantBypass  bool
	}{
		{
			name:      "case 0: alignment alone keeps the customer's protection",
			wantCheck: VerdictSkipped,
		},
		{
			name:        "case 1: baseline replaces classic protection with the ruleset",
			declaration: "  protection: baseline\n",
			wantCheck:   VerdictDrift,
			wantBypass:  true,
		},
		{
			name:        "case 2: baseline respects the agent merge opt-out",
			declaration: "  protection: baseline\n  agentMerge: false\n",
			wantCheck:   VerdictDrift,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, customerEntryYAML+"  align: true\n"+tc.declaration)
			r := h.gh.addRepo(owner, name)
			r.defaultBranch = "master"
			r.merged[0].Base.Ref = new("master")
			for _, path := range []string{".circleci/config.yml", ".circleci/workflows.yml", ".circleci/custom.yml"} {
				delete(r.files, path)
			}
			r.statuses = []string{ctxSemantic}
			classic := &fakeProtection{reviews: 1, enforceAdmins: true, checks: []string{ctxSemantic}}
			r.protection = classic

			steps := []Step{StepSettings, StepProtection, StepCircleCI, StepCodeowners, StepRelease}
			check := h.run(ModeCheck, false, steps...)
			require.Equal(t, tc.wantCheck, check.Step(StepProtection).Verdict, "%+v", check.Step(StepProtection))
			require.Empty(t, h.mutations(), "a check must not write")
			require.Equal(t, VerdictOK, check.Step(StepSettings).Verdict)
			for _, step := range []Step{StepCircleCI, StepCodeowners, StepRelease} {
				require.Equal(t, VerdictSkipped, check.Step(step).Verdict, "%+v", check.Step(step))
				require.Equal(t, "flavour customer", check.Step(step).Summary)
			}

			repair := h.run(ModeRepair, false, steps...)
			require.Equal(t, "master", r.defaultBranch, "the customer keeps its default branch")
			if tc.wantCheck == VerdictSkipped {
				require.Equal(t, VerdictSkipped, repair.Step(StepProtection).Verdict)
				require.Equal(t, "flavour customer", repair.Step(StepProtection).Summary)
				require.Equal(t, classic, r.protection)
				require.Empty(t, r.rulesets)
				require.Empty(t, h.mutations())
				return
			}

			require.Equal(t, VerdictRepaired, repair.Step(StepProtection).Verdict, "%+v", repair.Step(StepProtection))
			require.Nil(t, r.protection, "classic protection must stop blocking the bypass")
			rs := r.ruleset(RulesetName)
			require.NotNil(t, rs)
			require.Equal(t, github.RulesetEnforcementActive, rs.Enforcement)
			require.Equal(t, []string{"~DEFAULT_BRANCH"}, rs.Conditions.RefName.Include)
			require.Equal(t, 1, rs.Rules.PullRequest.RequiredApprovingReviewCount)
			require.Equal(t, []string{ctxSemantic}, checkContexts(rs))
			if tc.wantBypass {
				require.ElementsMatch(t, []*github.BypassActor{appBypass(testAppID), adminBypass(), teamBypass(testTeamID)}, rs.BypassActors)
			} else {
				require.Empty(t, rs.BypassActors)
			}
			require.Equal(t, []string{
				"POST /repos/" + owner + "/" + name + "/rulesets",
				"DELETE /repos/" + owner + "/" + name + "/branches/master/protection",
			}, h.mutations(), "write the ruleset before removing classic protection")

			again := h.run(ModeCheck, false, StepProtection)
			require.Equal(t, VerdictOK, again.Step(StepProtection).Verdict, "%+v", again.Step(StepProtection))
			require.Empty(t, again.Step(StepProtection).Changes)
			require.Empty(t, again.Step(StepProtection).Findings)
		})
	}
}

func TestCustomerProtectionPrerequisites(t *testing.T) {
	testCases := []struct {
		name    string
		seed    func(*fakeRepo)
		wantWhy string
	}{
		{
			name:    "case 0: an empty repository still waits for its scaffold",
			seed:    func(r *fakeRepo) { r.empty = true },
			wantWhy: "repository is empty: the scaffold comes first",
		},
		{
			name:    "case 1: an archived repository is still skipped",
			seed:    func(r *fakeRepo) { r.archived = true },
			wantWhy: "archived on GitHub",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, customerEntryYAML+"  protection: baseline\n")
			r := h.gh.addRepo(owner, name)
			tc.seed(r)

			res := h.run(ModeCheck, false, StepScaffold, StepProtection)
			require.Equal(t, VerdictSkipped, res.Step(StepProtection).Verdict)
			require.Equal(t, tc.wantWhy, res.Step(StepProtection).Summary)
			require.Empty(t, h.mutations())
		})
	}
}
