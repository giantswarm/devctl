package reconcile

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The six merge settings reach GET /repos/{owner}/{repo} for an admin
// identity only; a read identity (the inventory App, a member with read
// access) gets them as null. The step reads them through GraphQL then, and
// reports them unchecked — never as drift to false — when that fails too.
func TestSettingsMergeSettingsReadIdentity(t *testing.T) {
	t.Run("an admin identity reads them with the repository, no GraphQL", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.gh.addRepo(owner, name)
		res := h.run(ModeCheck, false, StepSettings)
		sr := res.Step(StepSettings)
		require.Equal(t, VerdictOK, sr.Verdict, "%+v", sr)
		require.Zero(t, h.gh.reads("/graphql"))
	})
	t.Run("a read identity reads them through GraphQL", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.gh.addRepo(owner, name)
		h.gh.readOnly = true
		res := h.run(ModeCheck, false, StepSettings)
		sr := res.Step(StepSettings)
		require.Equal(t, VerdictOK, sr.Verdict, "a repository at the baseline is at the baseline for a read identity too: %+v", sr)
		require.Empty(t, sr.Changes)
		require.Equal(t, "baseline", sr.Summary)
		require.True(t, res.Converged)
		require.Equal(t, 1, h.gh.reads("/graphql"), "one GraphQL read for the six settings")
		require.Empty(t, h.mutations(), "a check must not write")
	})
	t.Run("drift in a merge setting is found through GraphQL", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		r := h.gh.addRepo(owner, name)
		r.allowSquash, r.allowAuto = false, false
		h.gh.readOnly = true
		res := h.run(ModeCheck, false, StepSettings)
		sr := res.Step(StepSettings)
		require.Equal(t, VerdictDrift, sr.Verdict, "%+v", sr)
		require.Equal(t, []string{"settings: allow_squash_merge false → true, allow_auto_merge false → true"}, sr.Changes)
		require.False(t, res.Converged)
	})
	// A one-commit pull request squash-merges under its commit's own subject
	// while the repository names the squash commit COMMIT_OR_PR_TITLE: the
	// title the title check accepted is not what lands, and an unconventional
	// subject is neither released nor listed by auto-release. The baseline
	// wants PR_TITLE, read with the repository or through GraphQL.
	t.Run("a squash commit named after the commit rather than the title is drift", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.gh.addRepo(owner, name).squashTitle = "COMMIT_OR_PR_TITLE"
		res := h.run(ModeCheck, false, StepSettings)
		sr := res.Step(StepSettings)
		require.Equal(t, VerdictDrift, sr.Verdict, "%+v", sr)
		require.Equal(t, []string{"settings: squash_merge_commit_title COMMIT_OR_PR_TITLE → PR_TITLE"}, sr.Changes)
		require.Empty(t, h.mutations(), "a check must not write")

		res = h.run(ModeRepair, false, StepSettings)
		require.Equal(t, VerdictRepaired, res.Step(StepSettings).Verdict, "%+v", res.Step(StepSettings))
		require.Equal(t, "PR_TITLE", h.repo().squashTitle)
		require.True(t, res.Converged)
	})
	t.Run("the squash title is read through GraphQL for a read identity", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.gh.addRepo(owner, name).squashTitle = "COMMIT_OR_PR_TITLE"
		h.gh.readOnly = true
		res := h.run(ModeCheck, false, StepSettings)
		sr := res.Step(StepSettings)
		require.Equal(t, VerdictDrift, sr.Verdict, "%+v", sr)
		require.Equal(t, []string{"settings: squash_merge_commit_title COMMIT_OR_PR_TITLE → PR_TITLE"}, sr.Changes)
		require.Equal(t, 1, h.gh.reads("/graphql"))
	})
	t.Run("unreadable both ways is unchecked, never drift", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.gh.addRepo(owner, name)
		h.gh.readOnly, h.gh.graphqlStatus = true, http.StatusForbidden
		res := h.run(ModeCheck, false, StepSettings)
		sr := res.Step(StepSettings)
		require.Equal(t, VerdictReported, sr.Verdict, "%+v", sr)
		require.Empty(t, sr.Changes, "an absent field is not false")
		require.Equal(t, []FindingKind{FindingUnchecked}, kinds(sr.Findings))
		for _, field := range mergeSettingFields {
			require.Contains(t, sr.Findings[0].Message, field)
		}
		require.Contains(t, sr.Findings[0].Message, "403")
		require.NotEmpty(t, sr.Findings[0].Fix)
		require.Equal(t, "baseline, the merge settings unchecked", sr.Summary)
		require.False(t, res.Converged, "a person must look")
		require.Empty(t, h.mutations())
	})
	t.Run("the readable settings are still compared beside an unchecked finding", func(t *testing.T) {
		h := newHarness(t, entryYAML)
		h.gh.addRepo(owner, name).hasWiki = true
		h.gh.readOnly, h.gh.graphqlStatus = true, http.StatusBadGateway
		res := h.run(ModeCheck, false, StepSettings)
		sr := res.Step(StepSettings)
		require.Equal(t, VerdictDrift, sr.Verdict, "%+v", sr)
		require.Equal(t, []string{"settings: has_wiki true → false"}, sr.Changes)
		require.Equal(t, []FindingKind{FindingUnchecked}, kinds(sr.Findings))
	})
}
