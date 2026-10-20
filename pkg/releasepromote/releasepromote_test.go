package releasepromote

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// dispatch is one DispatchWorkflow call.
type dispatch struct {
	repository, file, ref string
	inputs                map[string]any
}

// fakeRepository is what the fake answers for one repository.
type fakeRepository struct {
	// branch carries the workflow and the candidates; it is the default
	// branch unless defaultBranch names another.
	branch        string
	defaultBranch string
	noWorkflow    bool
	// outdated serves the workflow without the promotion step.
	outdated bool
	// noVersionInput serves a promoting workflow without the version input.
	noVersionInput bool
	// unreachable are the tags not on the default branch.
	unreachable []string
	releases    []githubclient.Release
	status      githubclient.CombinedStatus
	// statuses override status per ref.
	statuses    map[string]githubclient.CombinedStatus
	dispatchErr error
	// run is the run the dispatch names; zero names none.
	run githubclient.DispatchedRun
	// runReads answer GetWorkflowRun in turn, the last one again after.
	runReads []githubclient.WorkflowRun
	jobs     []githubclient.WorkflowJob
	// annotations and logs are per job id.
	annotations map[int64][]githubclient.Annotation
	logs        map[int64]string
}

// fakeGitHub answers per owner/repo; a repository it does not know is
// GitHub's 404.
type fakeGitHub struct {
	repositories map[string]fakeRepository
	dispatches   []dispatch
	statusRefs   []string
	compared     []string
	runReads     int
}

var errNotFound = errors.New("404 Not Found")

func (f *fakeGitHub) repository(owner, repo string) (fakeRepository, error) {
	r, ok := f.repositories[owner+"/"+repo]
	if !ok {
		return fakeRepository{}, errNotFound
	}
	return r, nil
}

func (f *fakeGitHub) DefaultBranch(_ context.Context, owner, repo string) (string, error) {
	r, err := f.repository(owner, repo)
	if r.defaultBranch != "" {
		return r.defaultBranch, err
	}
	return r.branch, err
}

const (
	newWorkflow       = "jobs:\n  release:\n    steps:\n      - name: Resolve the candidate to promote\n        if: inputs.release-type == 'stable'\n        env:\n          PROMOTE_VERSION: ${{ inputs.version }}\n"
	noVersionWorkflow = "jobs:\n  release:\n    steps:\n      - name: Resolve the candidate to promote\n        if: inputs.release-type == 'stable'\n"
	oldWorkflow       = "jobs:\n  release:\n    steps:\n      - name: Create release\n"
)

func (f *fakeGitHub) ReadFile(_ context.Context, owner, repo, path, ref string) ([]byte, bool, error) {
	r, err := f.repository(owner, repo)
	if err != nil || r.noWorkflow || path != workflowPath || ref != r.branch {
		return nil, false, err
	}
	if r.outdated {
		return []byte(oldWorkflow), true, nil
	}
	if r.noVersionInput {
		return []byte(noVersionWorkflow), true, nil
	}
	return []byte(newWorkflow), true, nil
}

func (f *fakeGitHub) Reachable(_ context.Context, owner, repo, ref, branch string) (bool, error) {
	f.compared = append(f.compared, ref)
	r, err := f.repository(owner, repo)
	return err == nil && branch == r.branch && !slices.Contains(r.unreachable, ref), err
}

func (f *fakeGitHub) ListReleases(_ context.Context, owner, repo string) ([]githubclient.Release, error) {
	r, err := f.repository(owner, repo)
	return r.releases, err
}

func (f *fakeGitHub) GetCombinedStatus(_ context.Context, owner, repo, ref string) (githubclient.CombinedStatus, error) {
	f.statusRefs = append(f.statusRefs, owner+"/"+repo+"@"+ref)
	r, err := f.repository(owner, repo)
	if status, ok := r.statuses[ref]; ok {
		return status, err
	}
	return r.status, err
}

func (f *fakeGitHub) DispatchWorkflowRun(_ context.Context, owner, repo, file, ref string, inputs map[string]any) (githubclient.DispatchedRun, error) {
	r, err := f.repository(owner, repo)
	if err != nil {
		return githubclient.DispatchedRun{}, err
	}
	if r.dispatchErr != nil {
		return githubclient.DispatchedRun{}, r.dispatchErr
	}
	f.dispatches = append(f.dispatches, dispatch{repository: owner + "/" + repo, file: file, ref: ref, inputs: inputs})
	return r.run, nil
}

func (f *fakeGitHub) GetWorkflowRun(_ context.Context, owner, repo string, id int64) (githubclient.WorkflowRun, error) {
	r, err := f.repository(owner, repo)
	if err != nil || id != r.run.ID || len(r.runReads) == 0 {
		return githubclient.WorkflowRun{}, errNotFound
	}
	f.runReads++
	return r.runReads[min(f.runReads, len(r.runReads))-1], nil
}

func (f *fakeGitHub) ListWorkflowRunJobs(_ context.Context, owner, repo string, _ int64) ([]githubclient.WorkflowJob, error) {
	r, err := f.repository(owner, repo)
	return r.jobs, err
}

func (f *fakeGitHub) CheckRunAnnotations(_ context.Context, owner, repo string, id int64) ([]githubclient.Annotation, error) {
	r, err := f.repository(owner, repo)
	if a, ok := r.annotations[id]; ok || err != nil {
		return a, err
	}
	return nil, errors.New("403 Resource not accessible by integration")
}

func (f *fakeGitHub) JobLog(_ context.Context, owner, repo string, id int64) (io.ReadCloser, error) {
	r, err := f.repository(owner, repo)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(r.logs[id])), nil
}

func stable(tag string) githubclient.Release {
	return githubclient.Release{Tag: tag, Published: true}
}

func candidate(tag string) githubclient.Release {
	return githubclient.Release{Tag: tag, Published: true, Prerelease: true}
}

func TestSelectCandidate(t *testing.T) {
	cases := []struct {
		name            string
		releases        []githubclient.Release
		stable, promote string
	}{
		{
			name:     "rc.10 follows rc.9",
			releases: []githubclient.Release{candidate("v1.3.0-rc.9"), candidate("v1.3.0-rc.10"), candidate("v1.3.0-rc.2"), stable("v1.2.0")},
			stable:   "v1.2.0", promote: "v1.3.0-rc.10",
		},
		{
			name:     "a candidate for a higher version follows the one it replaced",
			releases: []githubclient.Release{candidate("v2.0.0-rc.1"), candidate("v1.3.0-rc.4"), stable("v1.2.0")},
			stable:   "v1.2.0", promote: "v2.0.0-rc.1",
		},
		{
			name:     "a candidate older than the latest stable release is ignored",
			releases: []githubclient.Release{stable("v1.3.0"), candidate("v1.3.0-rc.2"), candidate("v1.2.0-rc.1"), stable("v1.2.0")},
			stable:   "v1.3.0",
		},
		{
			name:     "no candidate",
			releases: []githubclient.Release{stable("v1.1.0"), stable("v1.2.0")},
			stable:   "v1.2.0",
		},
		{
			name:     "no stable release yet",
			releases: []githubclient.Release{candidate("v0.1.0-rc.1"), candidate("v0.1.0-rc.2")},
			promote:  "v0.1.0-rc.2",
		},
		{
			name: "drafts, a full release below the candidate and other tags do not count",
			releases: []githubclient.Release{
				{Tag: "v1.4.0-rc.1", Prerelease: true},
				{Tag: "v1.2.1-rc.0", Published: true},
				{Tag: "v9.0.0"},
				candidate("v1.2.1-beta.1"),
				stable("1.9.0"),
				candidate("v1.2.1-rc.1"),
				stable("v1.2.0"),
			},
			stable: "v1.2.0", promote: "v1.2.1-rc.1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStable, gotCandidate, err := SelectCandidate(tc.releases, func(string) (bool, error) { return true, nil })
			require.NoError(t, err)
			require.Equal(t, tc.stable, gotStable)
			require.Equal(t, tc.promote, gotCandidate)
		})
	}
}

// The highest candidate being a full release stops the selection there, as
// in the workflow: a lower pre-release is not promoted instead.
func TestSelectCandidateRefusesAHighestCandidateThatIsNoPrerelease(t *testing.T) {
	releases := []githubclient.Release{{Tag: "v1.3.0-rc.2", Published: true}, candidate("v1.3.0-rc.1"), stable("v1.2.0")}
	stable, candidate, err := SelectCandidate(releases, func(string) (bool, error) { return true, nil })
	require.ErrorIs(t, err, ErrNotPrerelease)
	require.Equal(t, "v1.2.0", stable)
	require.Equal(t, "v1.3.0-rc.2", candidate)
}

func TestPromote(t *testing.T) {
	releases := []githubclient.Release{candidate("v1.3.0-rc.2"), candidate("v1.3.0-rc.1"), stable("v1.2.0")}
	cases := []struct {
		name        string
		repository  fakeRepository
		dryRun      bool
		state       string
		statusState string
		dispatched  bool
		message     string
		warnings    []string
	}{
		{
			name:        "built candidate is dispatched",
			repository:  fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "success", TotalCount: 2}},
			state:       StateDispatched,
			statusState: "success",
			dispatched:  true,
			message:     "dispatched zz_generated.auto_release.yaml on main to promote v1.3.0-rc.2",
		},
		{
			name:        "no status at all counts as built",
			repository:  fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "pending", TotalCount: 0}},
			state:       StateDispatched,
			statusState: "pending",
			dispatched:  true,
		},
		{
			name:        "failed status is not built",
			repository:  fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "failure", TotalCount: 3}},
			state:       StateNotBuilt,
			statusState: "failure",
			message:     "the commit statuses of v1.3.0-rc.2 are failure: promote it once its pipelines passed; no other release candidate since v1.2.0 is built",
		},
		{
			name: "a failed sync-china-registry alone is built, with a warning",
			repository: fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "failure", TotalCount: 2, Statuses: []githubclient.CommitStatus{
				{Context: "ci/circleci: push-to-registries", State: "success"},
				{Context: "ci/circleci: sync-china-registry", State: "failure"},
			}}},
			state:       StateDispatched,
			statusState: "failure",
			dispatched:  true,
			message:     "dispatched zz_generated.auto_release.yaml on main to promote v1.3.0-rc.2",
			warnings:    []string{"the commit status ci/circleci: sync-china-registry of v1.3.0-rc.2 is failure; the release does not require it, so the candidate is judged without it"},
		},
		{
			name: "another failed context beside sync-china-registry is not built",
			repository: fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "failure", TotalCount: 3, Statuses: []githubclient.CommitStatus{
				{Context: "ci/circleci: push-to-registries", State: "error"},
				{Context: "ci/circleci: sync-china-registry", State: "failure"},
				{Context: "ci/circleci: go-build", State: "success"},
			}}},
			state:       StateNotBuilt,
			statusState: "failure",
			message:     "the commit statuses of v1.3.0-rc.2 are failure: promote it once its pipelines passed; no other release candidate since v1.2.0 is built",
		},
		{
			name: "a failed sync-china-registry beside a running context is not built",
			repository: fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "failure", TotalCount: 2, Statuses: []githubclient.CommitStatus{
				{Context: "ci/circleci: push-to-registries", State: "pending"},
				{Context: "ci/circleci: sync-china-registry", State: "failure"},
			}}},
			state:       StateNotBuilt,
			statusState: "failure",
			message:     "the commit statuses of v1.3.0-rc.2 are pending: promote it once its pipelines passed; no other release candidate since v1.2.0 is built",
		},
		{
			name: "a sync-china-registry context outside CircleCI still refuses",
			repository: fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "failure", TotalCount: 1, Statuses: []githubclient.CommitStatus{
				{Context: "sync-china-registry", State: "failure"},
			}}},
			state:       StateNotBuilt,
			statusState: "failure",
		},
		{
			name:        "pending status with a count is not built",
			repository:  fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "pending", TotalCount: 1}},
			state:       StateNotBuilt,
			statusState: "pending",
		},
		{
			name:        "dry run dispatches nothing",
			repository:  fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "success", TotalCount: 1}},
			dryRun:      true,
			state:       StateWouldDispatch,
			statusState: "success",
			message:     "would dispatch zz_generated.auto_release.yaml on main to promote v1.3.0-rc.2",
		},
		{
			name:       "nothing to promote",
			repository: fakeRepository{branch: "main", releases: []githubclient.Release{stable("v1.2.0"), candidate("v1.2.0-rc.3")}},
			state:      StateNothingToPromote,
			message:    "no release candidate on main since v1.2.0",
		},
		{
			name:       "workflow without the promotion step",
			repository: fakeRepository{branch: "main", outdated: true, releases: releases},
			state:      StateOutdatedWorkflow,
			message:    ".github/workflows/zz_generated.auto_release.yaml on main does not promote release candidates: its release-type stable tags the branch head; align the repository to devctl v8.102.0 or later (giantswarm/devctl#2413)",
		},
		{
			name:       "no auto-release workflow",
			repository: fakeRepository{branch: "main", noWorkflow: true, releases: releases},
			state:      StateNotAutoRelease,
		},
		{
			name:        "dispatch refused",
			repository:  fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "success", TotalCount: 1}, dispatchErr: errors.New("403 Resource not accessible by integration")},
			state:       StateFailed,
			statusState: "success",
			message:     "dispatching zz_generated.auto_release.yaml on main: 403 Resource not accessible by integration",
		},
		{
			name: "dispatch forbidden names Actions write",
			repository: fakeRepository{branch: "main", releases: releases, status: githubclient.CombinedStatus{State: "success", TotalCount: 1}, dispatchErr: &github.ErrorResponse{
				Response: &http.Response{StatusCode: http.StatusForbidden, Request: &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/dispatches"}}}, Message: "Resource not accessible by personal access token",
			}},
			state:       StateFailed,
			statusState: "success",
			message:     "dispatching zz_generated.auto_release.yaml on main: POST /dispatches: 403 Resource not accessible by personal access token []; the token needs Actions write on giantswarm/kserve",
		},
		{
			name:       "highest candidate is a full release",
			repository: fakeRepository{branch: "main", releases: []githubclient.Release{{Tag: "v1.3.0-rc.2", Published: true}, candidate("v1.3.0-rc.1"), stable("v1.2.0")}},
			state:      StateFailed,
			message:    "the highest release candidate on main, v1.3.0-rc.2, is a full GitHub release, not a pre-release: the workflow refuses to promote it",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/kserve": tc.repository}}
			result := NewResult("")
			err := Promote(t.Context(), Config{GitHub: gh, DryRun: tc.dryRun}, []string{"giantswarm/kserve"}, &result)

			require.Len(t, result.Repositories, 1)
			got := result.Repositories[0]
			require.Equal(t, tc.state, got.State, got.Message)
			require.Equal(t, tc.statusState, got.StatusState)
			if tc.message != "" {
				require.Equal(t, tc.message, got.Message)
			}
			require.Equal(t, tc.warnings, got.Warnings)
			if got.OK() {
				require.NoError(t, err)
			} else {
				require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
				require.ErrorContains(t, err, "giantswarm/kserve ("+tc.state+")")
			}
			if tc.dispatched {
				require.Equal(t, []dispatch{{repository: "giantswarm/kserve", file: Workflow, ref: "main", inputs: map[string]any{"release-type": "stable"}}}, gh.dispatches)
			} else {
				require.Empty(t, gh.dispatches)
			}
		})
	}
}

func TestPromoteReadsTheCandidateStatusAndDispatchesOnTheDefaultBranch(t *testing.T) {
	gh := &fakeGitHub{repositories: map[string]fakeRepository{
		"giantswarm/fork": {branch: "giantswarm-main", releases: []githubclient.Release{candidate("v0.2.0-rc.1")}},
	}}
	result := NewResult("")
	require.NoError(t, Promote(t.Context(), Config{GitHub: gh}, []string{"giantswarm/fork"}, &result))
	require.Equal(t, []string{"giantswarm/fork@v0.2.0-rc.1"}, gh.statusRefs)
	require.Equal(t, "giantswarm-main", gh.dispatches[0].ref)
	require.Equal(t, Repository{Repository: "giantswarm/fork", Candidate: "v0.2.0-rc.1", State: StateDispatched, Message: "dispatched zz_generated.auto_release.yaml on giantswarm-main to promote v0.2.0-rc.1"}, result.Repositories[0])
}

func TestPromoteOnAMaintenanceBranch(t *testing.T) {
	maintenance := fakeRepository{branch: "release-1.16", defaultBranch: "giantswarm", releases: []githubclient.Release{candidate("v1.16.5-rc.1"), stable("v1.16.4")}}

	gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/fork": maintenance}}
	result := NewResult("")
	require.NoError(t, Promote(t.Context(), Config{GitHub: gh, Branch: "release-1.16", Version: "v1.16.5-rc.1"}, []string{"giantswarm/fork"}, &result))
	require.Equal(t, []dispatch{{repository: "giantswarm/fork", file: Workflow, ref: "release-1.16", inputs: map[string]any{"release-type": "stable", "version": "v1.16.5-rc.1"}}}, gh.dispatches)
	require.Equal(t, "dispatched zz_generated.auto_release.yaml on release-1.16 to promote v1.16.5-rc.1", result.Repositories[0].Message)

	// Without --branch the default branch is read, which has neither.
	gh = &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/fork": maintenance}}
	result = NewResult("")
	require.Error(t, Promote(t.Context(), Config{GitHub: gh, Version: "v1.16.5-rc.1"}, []string{"giantswarm/fork"}, &result))
	require.Equal(t, StateNotAutoRelease, result.Repositories[0].State)
	require.Empty(t, gh.dispatches)
}

func TestPromoteGoesOnAfterARepositoryFails(t *testing.T) {
	built := fakeRepository{branch: "main", releases: []githubclient.Release{candidate("v1.0.0-rc.1")}, status: githubclient.CombinedStatus{State: "success", TotalCount: 1}}
	gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/a": built, "giantswarm/c": built}}
	result := NewResult("")
	err := Promote(t.Context(), Config{GitHub: gh, NotFoundHint: "the App reaches giantswarm only."}, []string{"giantswarm/a", "giantswarm/missing", "giantswarm/c"}, &result)

	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.EqualError(t, err, "1 of 3 repositories not promoted: giantswarm/missing (failed)")
	states := []string{}
	for _, r := range result.Repositories {
		states = append(states, r.State)
	}
	require.Equal(t, []string{StateDispatched, StateFailed, StateDispatched}, states)
	require.Len(t, gh.dispatches, 2)
}

func TestSelectCandidateOnTheDefaultBranch(t *testing.T) {
	cases := []struct {
		name            string
		releases        []githubclient.Release
		unreachable     []string
		stable, promote string
		compared        []string
	}{
		{
			name:        "a backport-branch candidate is skipped for the next reachable one",
			releases:    []githubclient.Release{candidate("v1.2.1-rc.1"), candidate("v1.3.0-rc.2"), candidate("v1.3.0-rc.1"), stable("v1.2.0")},
			unreachable: []string{"v1.3.0-rc.2"},
			stable:      "v1.2.0", promote: "v1.3.0-rc.1",
			compared: []string{"v1.2.0", "v1.3.0-rc.2", "v1.3.0-rc.1"},
		},
		{
			name:        "no reachable candidate",
			releases:    []githubclient.Release{candidate("v1.2.1-rc.1"), stable("v1.2.0")},
			unreachable: []string{"v1.2.1-rc.1"},
			stable:      "v1.2.0",
			compared:    []string{"v1.2.0", "v1.2.1-rc.1"},
		},
		{
			name:        "a higher stable release of another branch does not hide the candidate",
			releases:    []githubclient.Release{stable("v2.1.0"), candidate("v2.0.0-rc.3"), stable("v1.9.0")},
			unreachable: []string{"v2.1.0"},
			stable:      "v1.9.0", promote: "v2.0.0-rc.3",
			compared: []string{"v2.1.0", "v1.9.0", "v2.0.0-rc.3"},
		},
		{
			name:     "candidates not above the reachable stable release are not compared",
			releases: []githubclient.Release{stable("v1.3.0"), candidate("v1.3.0-rc.1")},
			stable:   "v1.3.0",
			compared: []string{"v1.3.0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var compared []string
			reachable := func(tag string) (bool, error) {
				compared = append(compared, tag)
				return !slices.Contains(tc.unreachable, tag), nil
			}
			gotStable, gotCandidate, err := SelectCandidate(tc.releases, reachable)
			require.NoError(t, err)
			require.Equal(t, tc.stable, gotStable)
			require.Equal(t, tc.promote, gotCandidate)
			require.Equal(t, tc.compared, compared)
		})
	}
}

func TestSelectCandidateComparisonError(t *testing.T) {
	_, _, err := SelectCandidate([]githubclient.Release{candidate("v1.0.0-rc.1")}, func(string) (bool, error) { return false, errors.New("boom") })
	require.EqualError(t, err, "v1.0.0-rc.1: boom")
}

func TestPromoteComparesAgainstTheDefaultBranch(t *testing.T) {
	gh := &fakeGitHub{repositories: map[string]fakeRepository{
		"giantswarm/a": {branch: "main", releases: []githubclient.Release{candidate("v1.3.0-rc.1"), stable("v1.2.0")}, unreachable: []string{"v1.3.0-rc.1"}},
	}}
	result := NewResult("")
	require.NoError(t, Promote(t.Context(), Config{GitHub: gh}, []string{"giantswarm/a"}, &result))
	require.Equal(t, StateNothingToPromote, result.Repositories[0].State)
	require.Equal(t, []string{"v1.2.0", "v1.3.0-rc.1"}, gh.compared)
	require.Empty(t, gh.dispatches)
}

func TestTeamRepositoriesKeepsAutoReleaseEntries(t *testing.T) {
	teamFile := `- name: klaus
  gen:
    flavours: [app]
    language: go
    ci:
      generate: true
- name: legacy-operator
  gen:
    flavours: [app]
    language: go
- name: hand-written-auto
  gen:
    flavours: [cli]
    language: go
    ci:
      releaseWorkflow: auto-release
- name: opted-out
  gen:
    flavours: [app]
    language: go
    ci:
      generate: true
      releaseWorkflow: legacy
- name: no-gen
`
	gh, err := githubmock.Start(sequence.Routes{
		"GET /repos/giantswarm/github/contents/repositories/team-bumblebee.yaml": {{Body: map[string]any{
			"type": "file", "encoding": "base64", "path": "repositories/team-bumblebee.yaml", "name": "team-bumblebee.yaml", "sha": "abc",
			"content": base64.StdEncoding.EncodeToString([]byte(teamFile)),
		}}},
	})
	require.NoError(t, err)
	t.Cleanup(gh.Close)
	client := newGitHubClient(t, gh.URL)

	repositories, err := TeamRepositories(t.Context(), client, "team-bumblebee")
	require.NoError(t, err)
	require.Equal(t, []string{"giantswarm/klaus", "giantswarm/hand-written-auto"}, repositories)

	_, err = TeamRepositories(t.Context(), client, "team-unknown")
	require.Equal(t, agentcli.ExitUsage, agentcli.Exit(err))
	require.ErrorContains(t, err, "team-unknown")
}

func newGitHubClient(t *testing.T, baseURL string) *github.Client {
	t.Helper()
	client, err := github.NewClient(github.WithURLs(new(baseURL+"/"), nil))
	require.NoError(t, err)
	return client
}

// A merge stream: rc.15 is building, rc.14 is built.
var (
	streamReleases = []githubclient.Release{candidate("v1.2.3-rc.15"), candidate("v1.2.3-rc.14"), candidate("v1.2.3-rc.13"), stable("v1.2.2")}
	builtStatus    = githubclient.CombinedStatus{State: "success", TotalCount: 2}
	buildingStatus = githubclient.CombinedStatus{State: "pending", TotalCount: 2}
	streamStatuses = map[string]githubclient.CombinedStatus{"v1.2.3-rc.15": buildingStatus, "v1.2.3-rc.14": builtStatus, "v1.2.3-rc.13": builtStatus}
)

func TestPromoteVersion(t *testing.T) {
	cases := []struct {
		name       string
		repository fakeRepository
		version    string
		state      string
		candidate  string
		later      []string
		message    string
	}{
		{
			name:       "a built candidate is promoted while a later one builds, which it names as left out",
			repository: fakeRepository{branch: "main", releases: streamReleases, statuses: streamStatuses},
			version:    "v1.2.3-rc.14",
			state:      StateDispatched,
			candidate:  "v1.2.3-rc.14",
			later:      []string{"v1.2.3-rc.15"},
			message:    "dispatched zz_generated.auto_release.yaml on main to promote v1.2.3-rc.14; not in it, cut after it: v1.2.3-rc.15",
		},
		{
			name:       "the proven candidate is promoted after two merges cut newer ones",
			repository: fakeRepository{branch: "main", releases: append([]githubclient.Release{candidate("v1.2.3-rc.16")}, streamReleases...), status: builtStatus},
			version:    "v1.2.3-rc.14",
			state:      StateDispatched,
			candidate:  "v1.2.3-rc.14",
			later:      []string{"v1.2.3-rc.15", "v1.2.3-rc.16"},
			message:    "dispatched zz_generated.auto_release.yaml on main to promote v1.2.3-rc.14; not in it, cut after it: v1.2.3-rc.15, v1.2.3-rc.16",
		},
		{
			name:       "the newest candidate named leaves nothing out",
			repository: fakeRepository{branch: "main", releases: streamReleases, status: builtStatus},
			version:    "v1.2.3-rc.15",
			state:      StateDispatched,
			candidate:  "v1.2.3-rc.15",
			message:    "dispatched zz_generated.auto_release.yaml on main to promote v1.2.3-rc.15",
		},
		{
			name:       "the named candidate still building is not built and names the newest built one",
			repository: fakeRepository{branch: "main", releases: streamReleases, statuses: streamStatuses},
			version:    "v1.2.3-rc.15",
			state:      StateNotBuilt,
			candidate:  "v1.2.3-rc.15",
			message:    "the commit statuses of v1.2.3-rc.15 are pending: promote it once its pipelines passed, or promote the newest built candidate with --version v1.2.3-rc.14",
		},
		{
			name:       "a candidate without a published release",
			repository: fakeRepository{branch: "main", releases: streamReleases, statuses: streamStatuses},
			version:    "v1.2.3-rc.16",
			state:      StateFailed,
			message:    "v1.2.3-rc.16 is not a release candidate to promote on main: no published GitHub release v1.2.3-rc.16",
		},
		{
			name:       "a candidate the last stable release contains",
			repository: fakeRepository{branch: "main", releases: append([]githubclient.Release{candidate("v1.2.2-rc.1")}, streamReleases...), statuses: streamStatuses},
			version:    "v1.2.2-rc.1",
			state:      StateFailed,
			message:    "v1.2.2-rc.1 is not a release candidate to promote on main: not newer than the last stable release v1.2.2",
		},
		{
			name:       "a candidate of another branch",
			repository: fakeRepository{branch: "main", releases: streamReleases, statuses: streamStatuses, unreachable: []string{"v1.2.3-rc.14"}},
			version:    "v1.2.3-rc.14",
			state:      StateFailed,
			message:    "v1.2.3-rc.14 is not a release candidate to promote on main: not on that branch",
		},
		{
			name:       "a candidate that is a full release",
			repository: fakeRepository{branch: "main", releases: []githubclient.Release{{Tag: "v1.2.3-rc.14", Published: true}, stable("v1.2.2")}, status: builtStatus},
			version:    "v1.2.3-rc.14",
			state:      StateFailed,
			candidate:  "v1.2.3-rc.14",
			message:    "v1.2.3-rc.14 is a full GitHub release, not a pre-release: the workflow refuses to promote it",
		},
		{
			name:       "a workflow without the version input",
			repository: fakeRepository{branch: "main", releases: streamReleases, statuses: streamStatuses, noVersionInput: true},
			version:    "v1.2.3-rc.14",
			state:      StateOutdatedWorkflow,
			message:    ".github/workflows/zz_generated.auto_release.yaml on main promotes only the latest release candidate, not one named with --version: align the repository to the current devctl (giantswarm/devctl#2550)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/kserve": tc.repository}}
			result := NewResult("")
			err := Promote(t.Context(), Config{GitHub: gh, Version: tc.version}, []string{"giantswarm/kserve"}, &result)

			got := result.Repositories[0]
			require.Equal(t, tc.state, got.State, got.Message)
			require.Equal(t, tc.candidate, got.Candidate)
			require.Equal(t, tc.later, got.Later)
			require.Equal(t, tc.message, got.Message)
			if tc.state != StateDispatched {
				require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
				require.Empty(t, gh.dispatches)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []dispatch{{repository: "giantswarm/kserve", file: Workflow, ref: "main", inputs: map[string]any{"release-type": "stable", "version": tc.version}}}, gh.dispatches)
		})
	}
}

// Without --version, a latest candidate still building is refused, and the
// refusal names the newest built one, asking no status beyond it.
func TestPromoteNotBuiltNamesTheNewestBuiltCandidate(t *testing.T) {
	gh := &fakeGitHub{repositories: map[string]fakeRepository{
		"giantswarm/kserve": {branch: "main", releases: streamReleases, statuses: streamStatuses},
	}}
	result := NewResult("")
	err := Promote(t.Context(), Config{GitHub: gh}, []string{"giantswarm/kserve"}, &result)

	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.Equal(t, Repository{
		Repository:  "giantswarm/kserve",
		Stable:      "v1.2.2",
		Candidate:   "v1.2.3-rc.15",
		NewestBuilt: "v1.2.3-rc.14",
		StatusState: "pending",
		State:       StateNotBuilt,
		Message:     "the commit statuses of v1.2.3-rc.15 are pending: promote it once its pipelines passed, or promote the newest built candidate with --version v1.2.3-rc.14",
	}, result.Repositories[0])
	require.Equal(t, []string{"giantswarm/kserve@v1.2.3-rc.15", "giantswarm/kserve@v1.2.3-rc.14"}, gh.statusRefs)
	require.Empty(t, gh.dispatches)
}

func TestNewestBuilt(t *testing.T) {
	built := func(tags ...string) func(string) (bool, error) {
		return func(tag string) (bool, error) { return slices.Contains(tags, tag), nil }
	}
	cases := []struct {
		name        string
		releases    []githubclient.Release
		unreachable []string
		built       func(string) (bool, error)
		except      string
		want        string
	}{
		{
			name:     "the highest built candidate below the refused one",
			releases: streamReleases,
			built:    built("v1.2.3-rc.13", "v1.2.3-rc.14"),
			except:   "v1.2.3-rc.15",
			want:     "v1.2.3-rc.14",
		},
		{
			name:     "a built candidate above a named one",
			releases: streamReleases,
			built:    built("v1.2.3-rc.15"),
			except:   "v1.2.3-rc.14",
			want:     "v1.2.3-rc.15",
		},
		{
			name:        "candidates of another branch, full releases and those the stable release contains are skipped",
			releases:    append([]githubclient.Release{{Tag: "v1.2.3-rc.16", Published: true}, candidate("v1.2.2-rc.1")}, streamReleases...),
			unreachable: []string{"v1.2.3-rc.14"},
			built:       built("v1.2.3-rc.16", "v1.2.3-rc.14", "v1.2.2-rc.1"),
			except:      "v1.2.3-rc.15",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewestBuilt(tc.releases, func(tag string) (bool, error) { return !slices.Contains(tc.unreachable, tag), nil }, tc.built, tc.except)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	_, err := NewestBuilt(streamReleases, func(string) (bool, error) { return true, nil }, func(string) (bool, error) { return false, errors.New("boom") }, "v1.2.3-rc.15")
	require.EqualError(t, err, "v1.2.3-rc.14: boom")
}

func TestLaterCandidates(t *testing.T) {
	releases := append([]githubclient.Release{candidate("v1.3.0-rc.1"), candidate("v1.2.3-rc.16"), {Tag: "v1.2.3-rc.17"}}, streamReleases...)
	got, err := LaterCandidates(releases, func(tag string) (bool, error) { return tag != "v1.2.3-rc.16", nil }, "v1.2.3-rc.14")
	require.NoError(t, err)
	require.Equal(t, []string{"v1.2.3-rc.15", "v1.3.0-rc.1"}, got, "higher, published candidates on the default branch, lowest first")

	got, err = LaterCandidates(streamReleases, func(string) (bool, error) { return true, nil }, "v1.2.3-rc.15")
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = LaterCandidates(streamReleases, func(string) (bool, error) { return false, errors.New("boom") }, "v1.2.3-rc.13")
	require.EqualError(t, err, "v1.2.3-rc.14: boom")
}

// A failing read of the later candidates is a warning: the named candidate
// is still promoted, as the workflow tags its commit.
func TestPromoteVersionWarnsWhenTheLaterCandidatesAreUnreadable(t *testing.T) {
	gh := &laterFailingGitHub{fakeGitHub: &fakeGitHub{repositories: map[string]fakeRepository{
		"giantswarm/kserve": {branch: "main", releases: streamReleases, status: builtStatus},
	}}, failOn: "v1.2.3-rc.15"}
	result := NewResult("")
	err := Promote(t.Context(), Config{GitHub: gh, Version: "v1.2.3-rc.14"}, []string{"giantswarm/kserve"}, &result)

	require.NoError(t, err)
	got := result.Repositories[0]
	require.Equal(t, StateDispatched, got.State)
	require.Equal(t, []string{"listing the release candidates cut after v1.2.3-rc.14 failed: v1.2.3-rc.15: boom; the stable release is v1.2.3-rc.14's commit whatever came after it"}, got.Warnings)
}

// laterFailingGitHub fails the reachability of one tag.
type laterFailingGitHub struct {
	*fakeGitHub
	failOn string
}

func (f *laterFailingGitHub) Reachable(ctx context.Context, owner, repo, ref, branch string) (bool, error) {
	if ref == f.failOn {
		return false, errors.New("boom")
	}
	return f.fakeGitHub.Reachable(ctx, owner, repo, ref, branch)
}
