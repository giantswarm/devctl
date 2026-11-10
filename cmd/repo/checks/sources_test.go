package checks

import (
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"
)

func TestCommandGenericCheckSources(t *testing.T) {
	for _, tc := range []struct {
		name     string
		flag     string
		reported []string
		pin      *int64
	}{
		{name: "case 0: new unconditional CircleCI status", flag: "--checks"},
		{name: "case 1: reported conditional CircleCI status", flag: "--checks-if-reported", reported: []string{"ci/circleci: build"}},
		{name: "case 2: an existing integration pin", flag: "--checks", pin: new(int64(4321))},
		{name: "case 3: an existing any-app pin", flag: "--checks", pin: new(int64(-1))},
		{name: "case 4: an existing GitHub Actions pin", flag: "--checks", pin: new(int64(15368))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChecksFixture(t)
			f.protection = nil
			f.ruleset = fixtureRuleset(&github.RuleStatusCheck{Context: "existing gate", IntegrationID: new(int64(9876))})
			if tc.pin != nil {
				f.ruleset.Rules.RequiredStatusChecks.RequiredStatusChecks = append(f.ruleset.Rules.RequiredStatusChecks.RequiredStatusChecks,
					&github.RuleStatusCheck{Context: "ci/circleci: build", IntegrationID: tc.pin})
			}
			f.reported = tc.reported
			before := *f.ruleset
			_, err := f.execute(t.Context(), "--update", tc.flag+"=ci/circleci: build", "--output=json", "fixture/repository")
			require.NoError(t, err)
			var target *github.RuleStatusCheck
			for _, check := range f.ruleset.Rules.RequiredStatusChecks.RequiredStatusChecks {
				if check.Context == "ci/circleci: build" {
					target = check
				}
			}
			require.NotNil(t, target)
			require.Equal(t, tc.pin, target.IntegrationID, "a generic check flag must not select GitHub Actions as its source")
			require.Equal(t, before.BypassActors, f.ruleset.BypassActors)
			require.Equal(t, before.Conditions, f.ruleset.Conditions)
			require.Equal(t, before.Rules.PullRequest, f.ruleset.Rules.PullRequest)
			require.True(t, f.ruleset.Rules.RequiredStatusChecks.StrictRequiredStatusChecksPolicy)
			for _, check := range f.ruleset.Rules.RequiredStatusChecks.RequiredStatusChecks {
				if check.Context == "existing gate" {
					require.Equal(t, int64(9876), check.GetIntegrationID())
				}
			}
			writes := len(f.writes)
			if tc.pin == nil {
				require.Equal(t, 1, writes)
			} else {
				require.Zero(t, writes, "an explicitly named existing check needs no source change")
			}
			_, err = f.execute(t.Context(), "--update", tc.flag+"=ci/circleci: build", "--output=json", "fixture/repository")
			require.NoError(t, err)
			require.Len(t, f.writes, writes, "a converged rerun writes nothing")
		})
	}
}

func TestCommandUnreportedConditionalCheck(t *testing.T) {
	f := newChecksFixture(t)
	f.protection = nil
	f.ruleset = fixtureRuleset(&github.RuleStatusCheck{Context: "existing gate"})
	_, err := f.execute(t.Context(), "--update", "--checks-if-reported=ci/circleci: build", "--output=json", "fixture/repository")
	require.NoError(t, err)
	require.Empty(t, f.writes)
	require.Equal(t, []*github.RuleStatusCheck{{Context: "existing gate"}}, f.ruleset.Rules.RequiredStatusChecks.RequiredStatusChecks)
}
