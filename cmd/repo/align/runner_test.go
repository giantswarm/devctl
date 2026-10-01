package align

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// The three modes print the entry's decision, the warning, the planned
// changes and what was dispatched or opened.
func TestPrint(t *testing.T) {
	var out bytes.Buffer
	print(&out, "my-service", &manager.Dispatch{
		Mode: "align", Declared: true, OptedIn: true, Team: "team-bumblebee", Warning: "An alignment changes the repository.",
		Planned: []manager.PlannedStep{{Step: "protection", Changes: []string{"require review", "enforce admins"}}}, CheckedAt: "2026-09-22T02:20:00Z",
		Dispatched: true, Workflow: "reconcile-repositories.yaml", As: "alice", RunsURL: "https://github.com/giantswarm/github/actions/workflows/reconcile-repositories.yaml",
		Then: "the record shows setup.pendingRun until the run has reported",
	}, false)
	text := out.String()
	require.Contains(t, text, "my-service: mode align (declared in team-bumblebee, opted in to alignment)")
	require.Contains(t, text, "An alignment changes the repository.")
	require.Contains(t, text, "planned changes (checked 2026-09-22T02:20:00Z):\n  protection   require review; enforce admins")
	require.Contains(t, text, "dispatched reconcile-repositories.yaml as alice: https://github.com/giantswarm/github/actions/workflows/reconcile-repositories.yaml")
	require.Contains(t, text, "then: the record shows setup.pendingRun")

	out.Reset()
	print(&out, "my-service", &manager.Dispatch{Mode: "opt-in", Declared: true, Team: "team-bumblebee", Warning: "w",
		OptIn: &manager.OptIn{Plan: manager.Plan{Repository: "giantswarm/my-service", Team: "team-bumblebee", Accepted: true, PullRequest: manager.PlannedPullRequest{Title: "chore: opt in", Branch: "reposetup/align-my-service", As: "alice"}}}}, true)
	text = out.String()
	require.Contains(t, text, "dry run: nothing dispatched, nothing opened")
	require.Contains(t, text, "my-service: mode opt-in (declared in team-bumblebee, not opted in)")
	require.Contains(t, text, "opt-in pull request:\ndry run: nothing written\ngiantswarm/my-service in team-bumblebee: accepted")
	require.NotContains(t, text, "nothing dispatched\n")

	out.Reset()
	print(&out, "orphan", &manager.Dispatch{Mode: "check", Team: "team-bumblebee", Warning: "w", Dispatched: true, Workflow: "reconcile-repositories.yaml", As: "alice", RunsURL: "u"}, false)
	require.Contains(t, out.String(), "orphan: mode check (not declared; checked from team-bumblebee)")
}
