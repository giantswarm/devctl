package list

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// The arguments carry the scope and every filter that is set, the boolean
// ones as booleans; an enum outside the manager's is refused.
func TestFlagArgs(t *testing.T) {
	f := &flag{Flags: client.Flags{Output: client.OutputText}, Scope: "all", Team: "team-bumblebee", Fork: "false", InactiveDays: 365, Limit: 20, Signing: "unsigned"}
	require.NoError(t, f.Validate())
	require.Equal(t, map[string]any{"scope": "all", "team": "team-bumblebee", "fork": false, "inactiveDays": 365, "limit": 20, "signing": "unsigned"}, f.args())

	f = &flag{Flags: client.Flags{Output: client.OutputText}, Scope: "everything"}
	require.True(t, client.IsInvalidFlag(f.Validate()))
	f = &flag{Flags: client.Flags{Output: client.OutputText}, Scope: "mine", Archived: "yes"}
	require.True(t, client.IsInvalidFlag(f.Validate()))
}

// The table shows one row per repository with the page's marks for the
// set-up state, and the note when the manager gives one.
func TestPrint(t *testing.T) {
	converged, drifted := true, false
	var out bytes.Buffer
	print(&out, &manager.Listing{
		Scope: "mine", Teams: []string{"team-bumblebee"}, TeamsSource: "github", Total: 1790, Matched: 4, Shown: 4, Note: "one of your teams",
		Repositories: []manager.Row{
			{Repository: "giantswarm/a", Team: "team-bumblebee", Renovate: "active", LastPersonCommit: "2026-09-01", Setup: manager.RowSetup{Converged: &converged}},
			{Repository: "giantswarm/b", Team: "team-bumblebee", Lifecycle: "deprecated", Setup: manager.RowSetup{Converged: &drifted}, Findings: []string{"default-icon"}},
			{Repository: "giantswarm/c", Team: "team-bumblebee", Setup: manager.RowSetup{Refused: true}},
			{Repository: "giantswarm/d", Archived: true, Setup: manager.RowSetup{}},
		},
	})
	text := out.String()
	require.Contains(t, text, "scope mine (teams team-bumblebee, from github): 4 shown of 4 matched, 1790 in the inventory")
	require.Contains(t, text, "note: one of your teams")
	require.Contains(t, text, "last sweep: none yet")
	require.Regexp(t, `giantswarm/a\s+team-bumblebee\s+active\s+active\s+in sync\s+2026-09-01\s+-`, text)
	require.Regexp(t, `giantswarm/b\s+team-bumblebee\s+deprecated\s+-\s+not in sync\s+-\s+default-icon`, text)
	require.Regexp(t, `giantswarm/c\s+team-bumblebee\s+active\s+-\s+refused`, text)
	require.Regexp(t, `giantswarm/d\s+-\s+active \(archived on GitHub\)\s+-\s+undeclared`, text)
}
