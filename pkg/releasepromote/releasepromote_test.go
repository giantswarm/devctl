package releasepromote

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"testing"

	"github.com/google/go-github/v92/github"
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
	branch      string
	noWorkflow  bool
	releases    []githubclient.Release
	status      githubclient.CombinedStatus
	dispatchErr error
}

// fakeGitHub answers per owner/repo; a repository it does not know is
// GitHub's 404.
type fakeGitHub struct {
	repositories map[string]fakeRepository
	dispatches   []dispatch
	statusRefs   []string
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
	return r.branch, err
}

func (f *fakeGitHub) HasWorkflow(_ context.Context, owner, repo, file string) (bool, error) {
	r, err := f.repository(owner, repo)
	return err == nil && file == Workflow && !r.noWorkflow, err
}

func (f *fakeGitHub) ListReleases(_ context.Context, owner, repo string) ([]githubclient.Release, error) {
	r, err := f.repository(owner, repo)
	return r.releases, err
}

func (f *fakeGitHub) GetCombinedStatus(_ context.Context, owner, repo, ref string) (githubclient.CombinedStatus, error) {
	f.statusRefs = append(f.statusRefs, owner+"/"+repo+"@"+ref)
	r, err := f.repository(owner, repo)
	return r.status, err
}

func (f *fakeGitHub) DispatchWorkflow(_ context.Context, owner, repo, file, ref string, inputs map[string]any) error {
	r, err := f.repository(owner, repo)
	if err != nil {
		return err
	}
	if r.dispatchErr != nil {
		return r.dispatchErr
	}
	f.dispatches = append(f.dispatches, dispatch{repository: owner + "/" + repo, file: file, ref: ref, inputs: inputs})
	return nil
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
			name: "drafts, non-pre-release candidates and other tags do not count",
			releases: []githubclient.Release{
				{Tag: "v1.4.0-rc.1", Prerelease: true},
				{Tag: "v1.3.0-rc.1", Published: true},
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
			gotStable, gotCandidate := SelectCandidate(tc.releases)
			require.Equal(t, tc.stable, gotStable)
			require.Equal(t, tc.promote, gotCandidate)
		})
	}
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
			message:     "the commit statuses of v1.3.0-rc.2 are failure: promote it once its pipelines passed",
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
			message:    "no release candidate since v1.2.0",
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

func TestPromoteGoesOnAfterARepositoryFails(t *testing.T) {
	built := fakeRepository{branch: "main", releases: []githubclient.Release{candidate("v1.0.0-rc.1")}, status: githubclient.CombinedStatus{State: "success", TotalCount: 1}}
	gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/a": built, "giantswarm/c": built}}
	result := NewResult("")
	err := Promote(t.Context(), Config{GitHub: gh, NotFoundHint: "the App reaches giantswarm only."}, []string{"giantswarm/a", "giantswarm/missing", "giantswarm/c"}, &result)

	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.EqualError(t, err, "1 of 3 repositories not dispatched: giantswarm/missing (failed)")
	states := []string{}
	for _, r := range result.Repositories {
		states = append(states, r.State)
	}
	require.Equal(t, []string{StateDispatched, StateFailed, StateDispatched}, states)
	require.Len(t, gh.dispatches, 2)
}

func TestPromoteAddsTheForbiddenHintToA403(t *testing.T) {
	forbidden := &github.ErrorResponse{Response: &http.Response{StatusCode: http.StatusForbidden}, Message: "Resource not accessible by integration"}
	gh := &fakeGitHub{repositories: map[string]fakeRepository{
		"giantswarm/a": {branch: "main", releases: []githubclient.Release{candidate("v1.0.0-rc.1")}, dispatchErr: forbidden},
	}}
	result := NewResult("")
	err := Promote(t.Context(), Config{GitHub: gh, ForbiddenHint: "set $GITHUB_TOKEN"}, []string{"giantswarm/a"}, &result)
	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.Equal(t, StateFailed, result.Repositories[0].State)
	require.Contains(t, result.Repositories[0].Message, "Resource not accessible by integration")
	require.Contains(t, result.Repositories[0].Message, "; set $GITHUB_TOKEN")
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
