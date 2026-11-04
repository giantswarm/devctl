package reconcile

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProtectionClassicReadAccess(t *testing.T) {
	testCases := []struct {
		name          string
		mode          Mode
		status        int
		appID         int64
		ruleset       bool
		rulesetDrift  bool
		wantVerdict   Verdict
		wantFindings  []FindingKind
		wantChanges   []string
		wantConverged bool
	}{
		{
			name: "case 0: check compares an aligned ruleset without the App id after a forbidden classic read",
			mode: ModeCheck, status: http.StatusForbidden, ruleset: true,
			wantVerdict: VerdictReported, wantFindings: []FindingKind{FindingUnchecked}, wantConverged: true,
		},
		{
			name: "case 1: check compares an aligned ruleset with the App id after a forbidden classic read",
			mode: ModeCheck, status: http.StatusForbidden, ruleset: true, appID: testAppID,
			wantVerdict: VerdictReported, wantFindings: []FindingKind{FindingUnchecked}, wantConverged: true,
		},
		{
			name: "case 2: a forbidden classic read does not hide ruleset drift without the App id",
			mode: ModeCheck, status: http.StatusForbidden, ruleset: true, rulesetDrift: true,
			wantVerdict: VerdictReported, wantFindings: []FindingKind{FindingUnchecked, FindingRulesetPending},
		},
		{
			name: "case 3: a forbidden classic read does not hide ruleset drift with the App id",
			mode: ModeCheck, status: http.StatusForbidden, ruleset: true, rulesetDrift: true, appID: testAppID,
			wantVerdict: VerdictDrift, wantFindings: []FindingKind{FindingUnchecked}, wantChanges: []string{"required reviews 0 → 1"},
		},
		{
			name: "case 4: without a ruleset check does not guess classic protection is absent",
			mode: ModeCheck, status: http.StatusForbidden,
			wantVerdict: VerdictReported, wantFindings: []FindingKind{FindingRulesetsNotEnabled, FindingUnchecked}, wantConverged: true,
		},
		{
			name: "case 5: without a ruleset check with the App id does not plan a migration from unknown protection",
			mode: ModeCheck, status: http.StatusForbidden, appID: testAppID,
			wantVerdict: VerdictReported, wantFindings: []FindingKind{FindingUnchecked}, wantConverged: true,
		},
		{
			name: "case 6: repair without a ruleset still fails on a forbidden classic read",
			mode: ModeRepair, status: http.StatusForbidden,
			wantVerdict: VerdictFailed, wantFindings: []FindingKind{FindingRulesetsNotEnabled},
		},
		{
			name: "case 7: repair with the App id does not migrate unknown classic protection",
			mode: ModeRepair, status: http.StatusForbidden, appID: testAppID,
			wantVerdict: VerdictFailed,
		},
		{
			name: "case 8: repair still fails beside an existing ruleset",
			mode: ModeRepair, status: http.StatusForbidden, ruleset: true,
			wantVerdict: VerdictFailed,
		},
		{
			name: "case 9: repair with the App id does not remove unknown classic protection",
			mode: ModeRepair, status: http.StatusForbidden, ruleset: true, appID: testAppID,
			wantVerdict: VerdictFailed,
		},
		{
			name: "case 10: check still fails on unauthorized classic reads",
			mode: ModeCheck, status: http.StatusUnauthorized, ruleset: true,
			wantVerdict: VerdictFailed,
		},
		{
			name: "case 11: check still fails on server errors",
			mode: ModeCheck, status: http.StatusInternalServerError, ruleset: true,
			wantVerdict: VerdictFailed,
		},
		{
			name: "case 12: an admin check without the App id still reports classic protection beside the ruleset",
			mode: ModeCheck, ruleset: true,
			wantVerdict: VerdictReported, wantFindings: []FindingKind{FindingRulesetPending},
		},
		{
			name: "case 13: an admin check still plans removal of classic protection beside the ruleset",
			mode: ModeCheck, ruleset: true, appID: testAppID,
			wantVerdict: VerdictDrift, wantChanges: []string{"remove classic protection of main"},
		},
		{
			name: "case 14: an admin repair still removes classic protection beside the ruleset",
			mode: ModeRepair, ruleset: true, appID: testAppID,
			wantVerdict: VerdictRepaired, wantChanges: []string{"remove classic protection of main"}, wantConverged: true,
		},
		{
			name: "case 15: an absent classic protection is still known to be absent",
			mode: ModeCheck, status: http.StatusNotFound, ruleset: true, appID: testAppID,
			wantVerdict: VerdictOK, wantConverged: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, entryYAML)
			h.runner.DevctlAppID = tc.appID
			r := h.gh.addRepo(owner, name)
			r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, checks: []string{ctxGoBuild}}
			r.protectionStatus = tc.status
			if tc.status == http.StatusNotFound {
				r.protection = nil
			}
			if tc.ruleset {
				rs := r.addRuleset(RulesetName, nil, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
				if tc.rulesetDrift {
					rs.Rules.PullRequest.RequiredApprovingReviewCount = 0
				}
			}

			res := h.run(tc.mode, false, StepProtection)
			sr := res.Step(StepProtection)
			require.Equal(t, tc.wantVerdict, sr.Verdict, "%+v", sr)
			require.ElementsMatch(t, tc.wantFindings, kinds(sr.Findings))
			require.Equal(t, tc.wantChanges, sr.Changes)
			require.Equal(t, tc.wantConverged, res.Converged, "%+v", sr)
			require.Equal(t, 1, h.gh.reads("/repos/giantswarm/sample-service/branches/main/protection"))
			for _, f := range sr.Findings {
				if f.Kind == FindingUnchecked {
					require.True(t, f.Advisory)
					require.Contains(t, f.Message, "cannot read classic protection with this token")
					require.Contains(t, f.Message, "check skipped")
					require.Contains(t, f.Fix, "administration: read")
				}
			}
			if tc.mode == ModeCheck && tc.status == http.StatusForbidden {
				if tc.ruleset && len(sr.Changes) == 0 {
					require.Contains(t, sr.Summary, RulesetName, "the readable ruleset was checked")
				} else if !tc.ruleset {
					require.Equal(t, "classic protection check skipped", sr.Summary)
				}
			}
			if tc.wantVerdict == VerdictRepaired {
				require.Equal(t, []string{"DELETE /repos/giantswarm/sample-service/branches/main/protection"}, h.mutations())
				require.Nil(t, r.protection)
			} else {
				require.Empty(t, h.mutations(), "checks and failed repairs must not write")
				if tc.status == http.StatusNotFound {
					require.Nil(t, r.protection)
				} else {
					require.NotNil(t, r.protection)
				}
			}
		})
	}
}
