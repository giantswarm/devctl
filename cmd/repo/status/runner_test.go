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
