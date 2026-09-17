package reconcile

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWriteTable(t *testing.T) {
	start := time.Date(2026, 9, 16, 20, 0, 0, 0, time.UTC)
	res := &Result{
		Repository: "giantswarm/my-repo-v2",
		Declared:   "giantswarm/my-repo",
		Mode:       ModeRepair,
		StartedAt:  start,
		FinishedAt: start.Add(1500 * time.Millisecond),
		Steps: []StepResult{
			{Step: StepSettings, Verdict: VerdictRepaired, Changes: []string{"settings: has_wiki true → false"}},
			{Step: StepProtection, Verdict: VerdictOK, Summary: "main protected; required: pre-commit"},
			{Step: StepRenovate, Verdict: VerdictReported, Findings: []Finding{{Kind: FindingRenovateNotScanned, Message: "not covered", Fix: "add it"}}},
			{Step: StepRelease, Verdict: VerdictFailed, Summary: "boom"},
		},
	}

	var b strings.Builder
	require.NoError(t, res.WriteTable(&b))
	out := b.String()

	require.Contains(t, out, "giantswarm/my-repo-v2 (repair) — declared as giantswarm/my-repo: not converged in 1.5s")
	require.Contains(t, out, "STEP")
	require.Contains(t, out, "settings")
	require.Contains(t, out, "repaired")
	require.Contains(t, out, "has_wiki true → false")
	require.Contains(t, out, "1 finding")
	require.Contains(t, out, "- [renovate-not-scanned] not covered\n  fix: add it")
	require.Equal(t, []StepResult{res.Steps[3]}, res.Failed())

	res.Steps = res.Steps[:2]
	res.Converged = true
	res.Declared = res.Repository
	b.Reset()
	require.NoError(t, res.WriteTable(&b))
	require.True(t, strings.HasPrefix(b.String(), "giantswarm/my-repo-v2 (repair): converged in 1.5s\n"))
	require.NotContains(t, b.String(), "Findings:")
	require.Empty(t, res.Failed())
}
