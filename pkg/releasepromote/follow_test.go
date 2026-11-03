package releasepromote

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// gateLog is the end of a job log whose stable gate refused.
const gateLog = `2026-10-18T03:49:59.4137943Z v1.3.0 runs on Substrate 1.12.1
2026-10-18T03:50:00.3954515Z ERROR: the Releases table of FORK.md has no row for 1.2.0
2026-10-18T03:50:00.3976791Z ##[error].github/release-gate.custom.sh refused v1.3.0: no tag and no release are cut.
2026-10-18T03:50:00.3986906Z ##[error]Process completed with exit code 1.
2026-10-18T03:50:01.0000000Z Post job cleanup.
`

const runURL = "https://github.com/giantswarm/kserve/actions/runs/7"

// followed is a built repository whose dispatch names run 7, read as runs.
func followed(runs ...githubclient.WorkflowRun) fakeRepository {
	return fakeRepository{
		branch:   "main",
		releases: []githubclient.Release{candidate("v1.3.0-rc.2"), stable("v1.2.0")},
		status:   githubclient.CombinedStatus{State: "success", TotalCount: 1},
		run:      githubclient.DispatchedRun{ID: 7, URL: runURL},
		runReads: runs,
	}
}

func run(status, conclusion string) githubclient.WorkflowRun {
	return githubclient.WorkflowRun{ID: 7, Status: status, Conclusion: conclusion, URL: runURL}
}

func followConfig(gh GitHub) Config {
	return Config{GitHub: gh, Follow: true, Clock: agentcli.NewVirtualClock(time.Unix(0, 0))}
}

// A run that passes leaves the promotion as it was: dispatched, exit 0, the
// run's conclusion recorded.
func TestPromoteFollowsARunThatPasses(t *testing.T) {
	gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/kserve": followed(run("queued", ""), run("in_progress", ""), run("completed", "success"))}}
	result := NewResult("")
	require.NoError(t, Promote(t.Context(), followConfig(gh), []string{"giantswarm/kserve"}, &result))
	got := result.Repositories[0]
	require.Equal(t, StateDispatched, got.State)
	require.Equal(t, "dispatched zz_generated.auto_release.yaml on main to promote v1.3.0-rc.2", got.Message)
	require.Equal(t, &Run{ID: 7, URL: runURL, Status: "completed", Conclusion: "success"}, got.Run)
	require.Empty(t, got.Warnings)
	require.Empty(t, got.FailedJobs)
	require.Equal(t, 3, gh.runReads)
}

// A run whose stable gate refuses is refused at its conclusion: exit 1, the
// run's URL and the gate's annotation in the message, the gate's own output
// in failedJobs.
func TestPromoteFollowsARunTheGateRefuses(t *testing.T) {
	repository := followed(run("in_progress", ""), run("completed", "failure"))
	repository.jobs = []githubclient.WorkflowJob{
		{ID: 70, Name: "Changelog", Conclusion: "success"},
		{ID: 71, Name: "Tag", Conclusion: "failure", URL: runURL + "/job/71"},
	}
	repository.annotations = map[int64][]githubclient.Annotation{71: {
		{Level: "failure", Message: "Process completed with exit code 1."},
		{Level: "warning", Title: "Slow", Message: "the checkout took long"},
		{Level: "failure", Title: "Stable gate refused", Message: ".github/release-gate.custom.sh refused v1.3.0: no tag and no release are cut."},
	}}
	repository.logs = map[int64]string{71: gateLog}
	gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/kserve": repository}}
	result := NewResult("")
	err := Promote(t.Context(), followConfig(gh), []string{"giantswarm/kserve"}, &result)

	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.EqualError(t, err, "1 of 1 repositories not promoted: giantswarm/kserve (refused)")
	got := result.Repositories[0]
	require.Equal(t, StateRefused, got.State)
	require.Equal(t, "the run "+runURL+" promoting v1.3.0-rc.2 concluded failure: no stable release is cut; Stable gate refused: .github/release-gate.custom.sh refused v1.3.0: no tag and no release are cut.", got.Message)
	require.Len(t, got.FailedJobs, 1)
	require.Equal(t, "Tag", got.FailedJobs[0].Name)
	require.Equal(t, runURL+"/job/71", got.FailedJobs[0].URL)
	require.Contains(t, got.FailedJobs[0].LogTail, "ERROR: the Releases table of FORK.md has no row for 1.2.0")
	require.NotContains(t, got.FailedJobs[0].LogTail, "Post job cleanup")
}

// Annotations that cannot be read leave the refusal standing, saying so.
func TestPromoteRefusesWithUnreadableAnnotations(t *testing.T) {
	repository := followed(run("completed", "cancelled"))
	repository.jobs = []githubclient.WorkflowJob{{ID: 71, Name: "Tag", Conclusion: "cancelled"}}
	gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/kserve": repository}}
	result := NewResult("")
	err := Promote(t.Context(), followConfig(gh), []string{"giantswarm/kserve"}, &result)
	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.Equal(t, "the run "+runURL+" promoting v1.3.0-rc.2 concluded cancelled: no stable release is cut; reading the annotations of job Tag: 403 Resource not accessible by integration failed", result.Repositories[0].Message)
}

// A run still going at the timeout stays dispatched, exit 0, with a warning
// naming the run and the wait to follow it with.
func TestPromoteFollowTimeoutLeavesTheRunDispatched(t *testing.T) {
	gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/kserve": followed(run("in_progress", ""))}}
	config := followConfig(gh)
	config.FollowTimeout = time.Minute
	result := NewResult("")
	require.NoError(t, Promote(t.Context(), config, []string{"giantswarm/kserve"}, &result))
	got := result.Repositories[0]
	require.Equal(t, StateDispatched, got.State)
	require.Equal(t, []string{"the run " + runURL + " promoting v1.3.0-rc.2 is still in_progress after 1m0s: it has not concluded; follow the release with devctl release wait giantswarm/kserve v1.3.0"}, got.Warnings)
}

// A dispatch GitHub names no run for is dispatched with a warning; without
// Follow, nothing is read after the dispatch.
func TestPromoteWithoutARunToFollow(t *testing.T) {
	repository := followed(run("completed", "failure"))
	repository.run = githubclient.DispatchedRun{}
	gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/kserve": repository}}
	result := NewResult("")
	require.NoError(t, Promote(t.Context(), followConfig(gh), []string{"giantswarm/kserve"}, &result))
	require.Equal(t, StateDispatched, result.Repositories[0].State)
	require.Equal(t, []string{"GitHub named no run for the dispatch, so its stable gate is not followed; follow the release with devctl release wait giantswarm/kserve v1.3.0"}, result.Repositories[0].Warnings)

	gh = &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/kserve": followed(run("completed", "failure"))}}
	result = NewResult("")
	require.NoError(t, Promote(t.Context(), Config{GitHub: gh}, []string{"giantswarm/kserve"}, &result))
	require.Equal(t, StateDispatched, result.Repositories[0].State)
	require.Zero(t, gh.runReads)
}
