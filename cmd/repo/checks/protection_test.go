package checks

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"
)

func TestCommandClassicChecksOnly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		method string
	}{
		{name: "case 0: check adds no unrelated drift", args: []string{"--checks=external gate"}},
		{name: "case 1: add a check", args: []string{"--update", "--checks=external gate"}, method: http.MethodPatch},
		{name: "case 2: remove the final check", args: []string{"--update", "--remove=pinned gate"}, method: http.MethodDelete},
		{name: "case 3: unchanged checks preserve all settings", args: []string{"--update"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChecksFixture(t)
			checks := []*github.RequiredStatusCheck{{Context: "pinned gate", AppID: new(int64(4321))}}
			f.protection = &github.Protection{
				RequiredStatusChecks: &github.RequiredStatusChecks{Strict: true, Checks: &checks},
				RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
					RequiredApprovingReviewCount: 2, DismissStaleReviews: true, RequireCodeOwnerReviews: true, RequireLastPushApproval: true,
				},
				EnforceAdmins:    &github.AdminEnforcement{Enabled: true},
				AllowForcePushes: &github.AllowForcePushes{Enabled: true},
				AllowDeletions:   &github.AllowDeletions{Enabled: true},
			}
			beforeReviews := *f.protection.RequiredPullRequestReviews
			output, err := f.execute(t.Context(), append(tc.args, "--output=json", "fixture/repository")...)
			require.NoError(t, err)
			if tc.method == "" {
				require.NotContains(t, output, "forbid force pushes")
				require.NotContains(t, output, "forbid deletions")
				require.Empty(t, f.writes)
				return
			}
			require.Len(t, f.writes, 1)
			require.Equal(t, tc.method, f.writes[0].method)
			require.Equal(t, "/branches/main/protection/required_status_checks", f.writes[0].path)
			require.Equal(t, beforeReviews, *f.protection.RequiredPullRequestReviews)
			require.True(t, f.protection.EnforceAdmins.Enabled)
			require.True(t, f.protection.AllowForcePushes.Enabled)
			require.True(t, f.protection.AllowDeletions.Enabled)
			if tc.method == http.MethodPatch {
				var body map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(f.writes[0].body, &body))
				require.Len(t, body, 2, "only checks and strict may be written")
				require.JSONEq(t, "true", string(body["strict"]))
				require.Equal(t, []*github.RequiredStatusCheck{{Context: "external gate"}, {Context: "pinned gate", AppID: new(int64(4321))}}, *f.protection.RequiredStatusChecks.Checks)
			} else {
				require.Nil(t, f.protection.RequiredStatusChecks)
			}
			_, err = f.execute(t.Context(), append(tc.args, "--output=json", "fixture/repository")...)
			require.NoError(t, err)
			require.Len(t, f.writes, 1, "a converged rerun writes nothing")
		})
	}
}

func TestCommandClassicWriteFailure(t *testing.T) {
	f := newChecksFixture(t)
	f.writeStatus = http.StatusForbidden
	output, err := f.execute(t.Context(), "--update", "--checks=external gate", "--output=json", "fixture/repository")
	require.Error(t, err)
	require.Contains(t, output, `"verdict": "failed"`)
	require.Contains(t, output, "fixture write refused")
	require.Len(t, f.writes, 1, "a failed mutation is not retried")
	require.Equal(t, http.MethodPatch, f.writes[0].method)
	require.Nil(t, f.protection.RequiredStatusChecks)
}

func TestCommandClassicProtectionDisappeared(t *testing.T) {
	f := newChecksFixture(t)
	reads := 0
	f.before = func(req *http.Request) {
		if req.Method == http.MethodGet && req.URL.Path == fixtureRepoPath+"/branches/main/protection" {
			reads++
			if reads == 2 {
				f.protection = nil
			}
		}
	}
	output, err := f.execute(t.Context(), "--update", "--checks=external gate", "--output=json", "fixture/repository")
	require.Error(t, err)
	require.Contains(t, output, `"verdict": "failed"`)
	require.Empty(t, f.writes, "a checks update must not create full branch protection")
}

func TestCommandClassicWriterLeavesRulesetUpdatesAlone(t *testing.T) {
	f := newChecksFixture(t)
	f.protection.RequiredPullRequestReviews = &github.PullRequestReviewsEnforcement{RequiredApprovingReviewCount: 2, RequireCodeOwnerReviews: true}
	f.ruleset = fixtureRuleset(&github.RuleStatusCheck{Context: "existing gate", IntegrationID: new(int64(4321))})
	f.reported = []string{"ci/circleci: build"}
	before := *f.protection
	_, err := f.execute(t.Context(), "--update", "--output=json", "fixture/repository")
	require.NoError(t, err)
	require.Len(t, f.writes, 1)
	require.Equal(t, http.MethodPut, f.writes[0].method)
	require.Equal(t, "/rulesets/7", f.writes[0].path, "when both exist the engine's ruleset remains the checks target")
	require.Equal(t, before, *f.protection, "classic protection is untouched when the ruleset carries the checks")
	require.Equal(t, []*github.RuleStatusCheck{{Context: "existing gate", IntegrationID: new(int64(4321))}, {Context: "ci/circleci: build"}}, f.ruleset.Rules.RequiredStatusChecks.RequiredStatusChecks)
}
