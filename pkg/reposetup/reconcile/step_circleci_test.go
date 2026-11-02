package reconcile

import (
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"
)

// TestIsReleaseTag: the release step verifies a vX.Y.Z tag, a pre-release
// suffix allowed — what auto-release cuts and the generated pipeline's tag
// filter builds — and nothing tagged otherwise.
func TestIsReleaseTag(t *testing.T) {
	cases := map[string]bool{
		"v0.1.0":         true,
		"v10.20.30":      true,
		"v1.2.3-rc.1":    true,
		"v1.2.3-dev.abc": true,
		"0.1.0":          false, // auto-release cuts the v
		"v0.1":           false, // not a full version
		"v01.2.3":        false, // a leading zero is no semver
		"base/v0.1.0":    false, // a per-component tag
		"release-1.2.3":  false,
		"":               false,
	}
	for tag, want := range cases {
		t.Run(tag, func(t *testing.T) {
			require.Equal(t, want, isReleaseTag(tag))
		})
	}
}

// TestStatusWorkflow: the workflow of a status is the id its target URL names
// — a GitHub App status or a pipeline URL — and an OAuth status, which names
// its job alone, stands for its own workflow.
func TestStatusWorkflow(t *testing.T) {
	cases := map[string]string{
		"https://app.circleci.com/workflow/wf-1/job/job-1":                               "wf-1",
		"https://app.circleci.com/pipelines/github/giantswarm/x/7/workflows/wf-2/jobs/3": "wf-2",
		"https://circleci.com/gh/giantswarm/x/42":                                        "https://circleci.com/gh/giantswarm/x/42",
		"": "",
	}
	for target, want := range cases {
		t.Run(target, func(t *testing.T) {
			require.Equal(t, want, StatusWorkflow(target))
		})
	}
}

// TestCircleCIPosters: who posted the statuses — the GitHub App's login, and
// the other logins sorted and without repeats; a status without creator is
// no one's.
func TestCircleCIPosters(t *testing.T) {
	status := func(login string) *github.RepoStatus {
		return &github.RepoStatus{Creator: &github.User{Login: new(login)}}
	}
	app, others := CircleCIPosters(nil)
	require.False(t, app)
	require.Empty(t, others)

	app, others = CircleCIPosters([]*github.RepoStatus{status("zoe"), status(CircleCIAppLogin), status("alice"), status("zoe"), {}})
	require.True(t, app)
	require.Equal(t, []string{"alice", "zoe"}, others)
}

// TestTagBuildStatuses: of a commit's statuses those of workflows that
// started no earlier than the release (less the slack) count, the newest of
// each context. The shape of release-automation-sandbox: the release was
// published at 11:20:53, its branch build started at 11:20:41 and finished
// at 11:21:34, the tag's own build started at 11:20:55.
func TestTagBuildStatuses(t *testing.T) {
	at := func(clock string) github.Timestamp {
		ts, err := time.Parse(time.RFC3339, "2026-10-30T"+clock+"Z")
		require.NoError(t, err)
		return github.Timestamp{Time: ts}
	}
	status := func(job, state, workflow, clock string) *github.RepoStatus {
		return &github.RepoStatus{
			Context: new(CircleCIContextPrefix + job), State: new(state), CreatedAt: &github.Timestamp{Time: at(clock).Time},
			TargetURL: new("https://app.circleci.com/workflow/" + workflow + "/job/" + job),
		}
	}
	published := at("11:20:53").Time
	newestFirst := []*github.RepoStatus{
		status("go-build", "failure", "wf-branch", "11:21:34"),
		status("go-build", "success", "wf-tag", "11:21:30"),
		status("go-build", "pending", "wf-tag", "11:20:55"),
		status("go-build", "pending", "wf-branch", "11:20:41"),
	}
	got := TagBuildStatuses(newestFirst, published)
	require.Len(t, got, 1)
	require.Equal(t, "success", got[0].GetState(), "the tag's own workflow, its newest status")

	branchOnly := []*github.RepoStatus{newestFirst[0], newestFirst[3]}
	require.Empty(t, TagBuildStatuses(branchOnly, published), "a branch build alone is no tag build")
	require.Empty(t, TagBuildStatuses(nil, published))

	// The slack: a first status 10 s before the release is the tag's.
	early := []*github.RepoStatus{status("go-build", "success", "wf-early", "11:20:43")}
	require.Len(t, TagBuildStatuses(early, published), 1)

	// Dynamic config: the branch build's setup settles before the release
	// and its continuation starts after it, under a workflow id of its own.
	// It belongs to the branch setup and drops out; the tag's setup and its
	// continuation count. Without the tag pipeline the branch alone reads
	// no tag build.
	dynamic := []*github.RepoStatus{
		status("go-build", "failure", "wf-branch-build", "11:21:40"),
		status("go-build", "success", "wf-tag-build", "11:21:30"),
		status("go-build", "pending", "wf-tag-build", "11:21:05"),
		status("setup", "success", "wf-tag-setup", "11:21:04"),
		status("go-build", "pending", "wf-branch-build", "11:20:58"),
		status("setup", "pending", "wf-tag-setup", "11:20:55"),
		status("setup", "success", "wf-branch-setup", "11:20:50"),
		status("setup", "pending", "wf-branch-setup", "11:20:40"),
	}
	got = TagBuildStatuses(dynamic, published)
	require.Len(t, got, 2)
	require.Equal(t, CircleCIContextPrefix+"go-build", got[0].GetContext())
	require.Equal(t, "success", got[0].GetState(), "the tag's go-build, not the branch continuation's later failure")
	require.Equal(t, setupContext, got[1].GetContext())

	branchContinuationOnly := []*github.RepoStatus{dynamic[0], dynamic[4], dynamic[6], dynamic[7]}
	require.Empty(t, TagBuildStatuses(branchContinuationOnly, published), "a branch continuation started after the release is no tag build")
}

// TestUnpublishedJobs: publish-github-release leaves nothing unpublished.
func TestUnpublishedJobs(t *testing.T) {
	require.Equal(t, []string{"push-to-registries-release"}, UnpublishedJobs([]string{GitHubReleaseJob, "push-to-registries-release"}))
	require.Empty(t, UnpublishedJobs([]string{GitHubReleaseJob}))
}
