package status

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

func result() *reconcile.Result {
	return &reconcile.Result{
		Repository: "giantswarm/my-service",
		Declared:   "giantswarm/my-service",
		Team:       "team-bumblebee",
		Mode:       reconcile.ModeCheck,
		Converged:  true,
	}
}

// The text output names the repository's opt-in to alignment right under
// the declaration's line when the source read the entry, and says nothing
// about it when the source carries the set-up state alone.
func TestPrintAlign(t *testing.T) {
	optedIn, notOptedIn := true, false
	cases := []struct {
		name  string
		align *bool
		want  string
	}{
		{name: "opted in", align: &optedIn, want: "opted in to alignment (align: true): the reconciler changes this repository to its declared set-up on every trigger"},
		{name: "not opted in", align: &notOptedIn, want: "not opted in to alignment: the reconciler checks this repository and changes nothing; opt in with align: true in its entry"},
		{name: "unknown", align: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			r := &runner{flag: &flag{Output: outputText}, stdout: &out}
			require.NoError(t, r.print(&output{Source: sourceEngine, Result: result(), Align: tc.align}))

			lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			require.Equal(t, "giantswarm/my-service declared in team-bumblebee (check mode, from engine)", lines[0])
			if tc.want == "" {
				require.Equal(t, "converged: set up as declared", lines[1])
				require.NotContains(t, out.String(), "alignment")
				return
			}
			require.Equal(t, tc.want, lines[1])
		})
	}
}

// The text output names the declared default branch and flavours right
// under the alignment line when the source read the entry, and the JSON
// output carries them; a source without the entry says nothing about them.
func TestPrintDeclaration(t *testing.T) {
	optedIn := true
	for _, tc := range []struct {
		name     string
		branch   string
		flavours []string
		want     string
		wantJSON string
	}{
		{name: "fork line", branch: "giantswarm", flavours: []string{"fork"}, want: "default branch giantswarm, flavours fork", wantJSON: `"defaultBranch": "giantswarm"`},
		{name: "no gen block", branch: "main", want: "default branch main, flavours none", wantJSON: `"defaultBranch": "main"`},
		{name: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &output{Source: sourceEngine, Result: result(), Align: &optedIn, DefaultBranch: tc.branch, Flavours: tc.flavours}

			var text bytes.Buffer
			require.NoError(t, (&runner{flag: &flag{Output: outputText}, stdout: &text}).print(out))
			lines := strings.Split(strings.TrimSuffix(text.String(), "\n"), "\n")
			var js bytes.Buffer
			require.NoError(t, (&runner{flag: &flag{Output: outputJSON}, stdout: &js}).print(out))
			require.True(t, json.Valid(js.Bytes()), js.String())

			if tc.want == "" {
				require.Equal(t, "converged: set up as declared", lines[2])
				require.NotContains(t, js.String(), `"defaultBranch"`)
				require.NotContains(t, js.String(), `"flavours"`)
				return
			}
			require.Equal(t, tc.want, lines[2])
			require.Contains(t, js.String(), tc.wantJSON)
		})
	}
}

// The text output marks an advisory finding and, when a finding a person
// must fix remains, says why the run did not converge.
func TestPrintFindings(t *testing.T) {
	res := result()
	res.Converged = false
	res.Steps = []reconcile.StepResult{{Step: reconcile.StepScaffold, Verdict: reconcile.VerdictReported, Summary: "present", Findings: []reconcile.Finding{
		{Kind: reconcile.FindingDefaultIcon, Message: "the default icon", Fix: "replace it", Advisory: true},
		{Kind: reconcile.FindingABSPrerequisite, Message: "no schema", Fix: "add it"},
	}}}
	var out bytes.Buffer
	r := &runner{flag: &flag{Output: outputText}, stdout: &out}
	require.NoError(t, r.print(&output{Source: sourceEngine, Result: res}))
	require.Contains(t, out.String(), "default-icon (advisory): the default icon -- fix: replace it")
	require.Contains(t, out.String(), "abs-prerequisite: no schema -- fix: add it")
	require.True(t, strings.HasSuffix(out.String(), "not converged: drift, failed steps or findings to fix above\n"), out.String())
}

// The text output of a refused entry (the manager's record of one, or the
// engine's result) says the declaration is at fault and nothing was
// checked, never that something drifted.
func TestPrintRefused(t *testing.T) {
	res := result()
	res.Converged = false
	res.Steps = []reconcile.StepResult{{Step: reconcile.StepEntry, Verdict: reconcile.VerdictReported, Summary: "refused: 1 problem(s) with the entry in repositories/team-bumblebee.yaml", Findings: []reconcile.Finding{
		{Kind: reconcile.FindingEntryRefused, Message: "agentMerge: not a field of the repositories schema", Fix: `edit agentMerge of the entry "my-service" in repositories/team-bumblebee.yaml: not a field of the repositories schema`},
	}}}
	var out bytes.Buffer
	r := &runner{flag: &flag{Output: outputText}, stdout: &out}
	require.NoError(t, r.print(&output{Source: sourceManager, Endpoint: "https://muster.example", Result: res}))
	require.Contains(t, out.String(), "  entry        reported  refused: 1 problem(s)")
	require.Contains(t, out.String(), "entry-refused: agentMerge: not a field of the repositories schema -- fix: edit agentMerge")
	require.True(t, strings.HasSuffix(out.String(), "not converged: the entry is refused and nothing was checked; fix the declaration as the findings above say\n"), out.String())
	require.NotContains(t, out.String(), "drift")
}

// The JSON output carries align only when the source read the entry.
func TestPrintAlignJSON(t *testing.T) {
	optedIn := true
	for _, tc := range []struct {
		name  string
		align *bool
		want  string
	}{
		{name: "opted in", align: &optedIn, want: `"align": true`},
		{name: "unknown", align: nil, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			r := &runner{flag: &flag{Output: outputJSON}, stdout: &out}
			require.NoError(t, r.print(&output{Source: sourceEngine, Result: result(), Align: tc.align}))
			require.True(t, json.Valid(out.Bytes()), out.String())
			if tc.want == "" {
				require.NotContains(t, out.String(), `"align"`)
				return
			}
			require.Contains(t, out.String(), tc.want)
		})
	}
}
