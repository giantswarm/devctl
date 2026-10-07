package sweep

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// Started, already running and the last summary each have their sentence.
func TestPrint(t *testing.T) {
	at := time.Date(2026, 9, 22, 3, 0, 0, 0, time.UTC)
	last := &manager.SweepSummary{FinishedAt: at, Duration: "41m2s", Repositories: 1790, Declared: 460, Undeclared: 1300, Gone: 3, Archived: 27, EngineChecks: 460, Errors: []string{"x"}}
	var out bytes.Buffer
	print(&out, &manager.Sweep{Started: true, Running: true, Login: "alice", Teams: []string{"team-bumblebee", "team-planeteers"}, Last: last})
	require.Contains(t, out.String(), "sweep started as alice (a member of team-bumblebee, team-planeteers)")
	require.Contains(t, out.String(), "last sweep: finished 2026-09-22T03:00:00Z in 41m2s: 1790 repositories (460 declared, 1300 undeclared, 3 gone, 27 archived), 460 engine checks, 1 errors")

	out.Reset()
	print(&out, &manager.Sweep{Running: true, Login: "alice"})
	require.Contains(t, out.String(), "a sweep is already running; none started (asked as alice)")
	require.Contains(t, out.String(), "last sweep: none yet")
}
