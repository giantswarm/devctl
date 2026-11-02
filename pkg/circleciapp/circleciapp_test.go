package circleciapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

func clientError(code int) error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"message":"Permission denied."}`))
	}))
	defer srv.Close()
	c, err := circleciclient.New(circleciclient.Config{Token: "t", BaseURL: srv.URL})
	if err != nil {
		panic(err)
	}
	_, err = c.GetProject(context.Background(), "giantswarm", "sample")
	return err
}

var repo = Repository{Owner: "giantswarm", Name: "sample", ID: 42}

// TestEnsureCreatesTheAppPipelineOnce: a project with the implicit OAuth
// definition alone gets <repo>-app with an all-pushes trigger, naming the
// repository by GitHub id; a second Ensure finds both and writes nothing.
func TestEnsureCreatesTheAppPipelineOnce(t *testing.T) {
	ci := newFake(oauthDef())
	result, err := Ensure(context.Background(), ci, repo, false)
	require.NoError(t, err)
	require.True(t, result.CreatedDefinition)
	require.True(t, result.CreatedTrigger)
	require.Equal(t, []string{
		"create definition sample-app repo 42 file .circleci/config.yml",
		"create trigger all-pushes on created",
	}, ci.writes)

	ci.writes = nil
	again, err := Ensure(context.Background(), ci, repo, false)
	require.NoError(t, err)
	require.False(t, again.Changed())
	require.Equal(t, "created", again.Definition.ID)
	require.Empty(t, ci.writes)
}

// TestEnsureReportsAndMigratesTheOAuthTrigger: an OAuth trigger left in
// place is reported; with migrate it is deleted, and the disabled App
// trigger counts as none.
func TestEnsureReportsAndMigratesTheOAuthTrigger(t *testing.T) {
	ci := newFake(oauthDef(), appDef())
	ci.triggers["oauth"] = []circleciclient.Trigger{{ID: "implicit"}}
	ci.triggers["app"] = []circleciclient.Trigger{{ID: "off", Disabled: true}}

	result, err := Ensure(context.Background(), ci, repo, false)
	require.NoError(t, err)
	require.False(t, result.CreatedDefinition)
	require.True(t, result.CreatedTrigger)
	require.Equal(t, []string{"implicit"}, result.OAuthTriggers)

	ci.writes = nil
	result, err = Ensure(context.Background(), ci, repo, true)
	require.NoError(t, err)
	require.Equal(t, []string{"implicit"}, result.DeletedOAuthTriggers)
	require.Empty(t, result.OAuthTriggers)
	require.Equal(t, []string{"delete trigger implicit"}, ci.writes)
}

// TestEnsureRefusals: no project is IsProjectNotFound; a Read-only login is
// refused with CircleCI's 403 on the first write.
func TestEnsureRefusals(t *testing.T) {
	_, err := Ensure(context.Background(), &fakeCircleCI{noProject: true}, repo, false)
	require.True(t, IsProjectNotFound(err), "%v", err)

	ci := newFake()
	ci.forbidden = true
	_, err = Ensure(context.Background(), ci, repo, false)
	require.True(t, circleciclient.IsForbidden(err), "%v", err)
}

// TestSetupRunsWhatWasNotBuilt: a new set-up runs the default branch and
// the latest release, whose commit carries only the branch build's statuses
// (its workflow started before the release); a set-up in place with a built
// release runs nothing.
func TestSetupRunsWhatWasNotBuilt(t *testing.T) {
	published := time.Date(2026, 11, 2, 11, 20, 53, 0, time.UTC)
	now := func() time.Time { return published.Add(time.Hour) }
	gh := &fakeGitHub{
		release: release("v0.1.0", published),
		statuses: []*github.RepoStatus{
			status("go-build", "success", "branch-build", published.Add(40*time.Second)),
			status("go-build", "pending", "branch-build", published.Add(-12*time.Second)),
		},
	}
	ci := newFake(oauthDef())
	report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", Options{Now: now, Wait: true, Waiter: Waiter{CircleCI: ci, Sleep: noSleep}})
	require.NoError(t, err)
	require.Len(t, report.Runs, 2)
	require.True(t, report.Green())
	require.Equal(t, "run created on tag v0.1.0", ci.writes[2], "the tag runs first")
	require.Equal(t, "run created on branch main", ci.writes[3])

	// The tag's own build, started after the release, counts as built, and
	// the branch carries the App pipeline's status: nothing runs.
	gh.built()
	gh.statuses = append([]*github.RepoStatus{status("go-build", "success", "tag-build", published.Add(70*time.Second)), status("setup", "pending", "tag-setup", published.Add(2*time.Second))}, gh.statuses...)
	ci.writes = nil
	report, err = Setup(context.Background(), ci, gh, "giantswarm", "sample", Options{Now: now})
	require.NoError(t, err)
	require.Empty(t, report.Runs)
	require.Empty(t, ci.writes)
}

// TestSetupLeavesAFreshTagToItsTrigger: a release published a minute ago on
// a pipeline whose trigger existed already is the trigger's to build; the
// same release with a trigger created now never reached a pipeline.
func TestSetupLeavesAFreshTagToItsTrigger(t *testing.T) {
	published := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return published.Add(time.Minute) }
	gh := (&fakeGitHub{release: release("v1.2.0", published)}).built()

	ci := newFake(appDef())
	ci.triggers["app"] = []circleciclient.Trigger{{ID: "on"}}
	report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", Options{Now: now})
	require.NoError(t, err)
	require.Empty(t, report.Runs)

	ci = newFake(appDef())
	report, err = Setup(context.Background(), ci, gh, "giantswarm", "sample", Options{Now: now})
	require.NoError(t, err)
	require.Len(t, report.Runs, 2)
	require.Equal(t, circleciclient.Ref{Tag: "v1.2.0"}, report.Runs[0].Ref)
}

// TestWaitFollowsTheSetupContinuation: the setup workflow's success alone
// does not settle a continuing pipeline; the build's workflows do, and a
// failed one is named.
func TestWaitFollowsTheSetupContinuation(t *testing.T) {
	setup := circleciclient.Workflow{Name: "setup", Status: "success", Tag: circleciclient.WorkflowTagSetup}
	ci := newFake()
	ci.reads = []pipelineRead{
		{state: "setup", workflows: []circleciclient.Workflow{{Name: "setup", Status: "running", Tag: circleciclient.WorkflowTagSetup}}},
		{state: "pending", workflows: []circleciclient.Workflow{setup}},
		{state: "created", workflows: []circleciclient.Workflow{setup, {Name: "build", Status: "running"}}},
		{state: "created", workflows: []circleciclient.Workflow{setup, {Name: "build", Status: "failed"}}},
	}
	outcome, err := Waiter{CircleCI: ci, Sleep: noSleep}.Wait(context.Background(), "pl1")
	require.NoError(t, err)
	require.False(t, outcome.Green)
	require.Equal(t, []string{"build (failed)"}, outcome.Failed)
	require.Equal(t, 4, ci.read)
}

// TestWaitTakesASetupOnlyRun: a continuation that creates no workflow (a
// branch every job's filter ignores) settles green after a few reads.
func TestWaitTakesASetupOnlyRun(t *testing.T) {
	ci := newFake()
	ci.reads = []pipelineRead{{state: "created", workflows: []circleciclient.Workflow{{Name: "setup", Status: "success", Tag: circleciclient.WorkflowTagSetup}}}}
	outcome, err := Waiter{CircleCI: ci, Sleep: noSleep}.Wait(context.Background(), "pl1")
	require.NoError(t, err)
	require.True(t, outcome.Green)
	require.Equal(t, setupOnlyReads, ci.read)
}

// TestSetupRefusesARepositoryWithoutConfig: no .circleci/config.yml on the
// default branch is IsNoConfig, before any write.
func TestSetupRefusesARepositoryWithoutConfig(t *testing.T) {
	ci := newFake(oauthDef())
	_, err := Setup(context.Background(), ci, &fakeGitHub{noConfig: true}, "giantswarm", "sample", Options{})
	require.True(t, IsNoConfig(err), "%v", err)
	require.Empty(t, ci.writes)
}

// TestEnsureSwitchesSetupWorkflowsOn: a project with setup workflows off has
// them switched on (the generated dynamic config needs them); a refused read
// is noted and does not fail the set-up.
func TestEnsureSwitchesSetupWorkflowsOn(t *testing.T) {
	ci := newFake(appDef())
	ci.triggers["app"] = []circleciclient.Trigger{{ID: "on"}}
	ci.setupWorkflows = false
	result, err := Ensure(context.Background(), ci, repo, false)
	require.NoError(t, err)
	require.True(t, result.EnabledSetupWorkflows)
	require.True(t, result.Changed())
	require.Equal(t, []string{"enable setup workflows"}, ci.writes)

	ci = newFake(appDef())
	ci.triggers["app"] = []circleciclient.Trigger{{ID: "on"}}
	ci.settingsRefused = true
	result, err = Ensure(context.Background(), ci, repo, false)
	require.NoError(t, err)
	require.NotEmpty(t, result.SetupWorkflowsUnread)
	require.False(t, result.Changed())
}

// TestWaitEndsOnHoldAndOnEmptyPipelines: a workflow waiting for an approval
// and one not run end the wait; a settled pipeline without any workflow ends
// it green after a few reads.
func TestWaitEndsOnHoldAndOnEmptyPipelines(t *testing.T) {
	ci := newFake()
	ci.reads = []pipelineRead{{state: "created", workflows: []circleciclient.Workflow{
		{Name: "build", Status: "success"}, {Name: "deploy", Status: "on_hold"}, {Name: "nightly", Status: "not_run"},
	}}}
	outcome, err := Waiter{CircleCI: ci, Sleep: noSleep}.Wait(context.Background(), "pl1")
	require.NoError(t, err)
	require.True(t, outcome.Green)
	require.Equal(t, []string{"deploy"}, outcome.OnHold)

	ci = newFake()
	ci.reads = []pipelineRead{{state: "created"}}
	outcome, err = Waiter{CircleCI: ci, Sleep: noSleep}.Wait(context.Background(), "pl1")
	require.NoError(t, err)
	require.True(t, outcome.Green)
	require.Equal(t, setupOnlyReads, ci.read)
}

// TestEnsurePrefersTheTriggeredAppDefinition: a dormant App definition
// listed before the triggered one gets no trigger; the triggered one is the
// pipeline, and nothing is written.
func TestEnsurePrefersTheTriggeredAppDefinition(t *testing.T) {
	dormant := circleciclient.AppDefinition("sample-old", "", "42")
	dormant.ID = "dormant"
	ci := newFake(oauthDef(), dormant, appDef())
	ci.triggers["app"] = []circleciclient.Trigger{{ID: "on"}}
	result, err := Ensure(context.Background(), ci, repo, false)
	require.NoError(t, err)
	require.Equal(t, "app", result.Definition.ID)
	require.False(t, result.Changed())
	require.Empty(t, ci.writes)
}

// TestEnsureTriggersTheCheckoutRepository: the trigger watches the
// repository the definition checks out, not the one its configuration comes
// from.
func TestEnsureTriggersTheCheckoutRepository(t *testing.T) {
	d := circleciclient.AppDefinition("sample-app", "", "42")
	d.ID = "app"
	d.ConfigSource.Repo.ExternalID = "7" // a shared configuration repository
	ci := newFake(d)
	_, err := Ensure(context.Background(), ci, repo, false)
	require.NoError(t, err)
	require.Equal(t, "42", ci.triggers["app"][0].EventSource.Repo.ExternalID)
}

// TestSetupRunsFlowReleasesOnly: a latest release outside the vX.Y.Z flow is
// not run; a multi-image repository runs the newest unbuilt release of every
// image line.
func TestSetupRunsFlowReleasesOnly(t *testing.T) {
	published := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return published.Add(time.Hour) }
	ci := newFake(appDef())
	ci.triggers["app"] = []circleciclient.Trigger{{ID: "on"}}
	report, err := Setup(context.Background(), ci, (&fakeGitHub{release: release("base/0.1", published)}).built(), "giantswarm", "sample", Options{Now: now})
	require.NoError(t, err)
	require.Empty(t, report.Runs)

	gh := (&fakeGitHub{
		release:  release("b/v0.2.0", published),
		releases: []*github.RepositoryRelease{release("b/v0.2.0", published), release("a/v0.1.1", published), release("a/v0.1.0", published.Add(-time.Hour))},
	}).built()
	report, err = Setup(context.Background(), ci, gh, "giantswarm", "sample", Options{Now: now})
	require.NoError(t, err)
	require.Len(t, report.Runs, 2)
	require.Equal(t, circleciclient.Ref{Tag: "a/v0.1.1"}, report.Runs[0].Ref)
	require.Equal(t, circleciclient.Ref{Tag: "b/v0.2.0"}, report.Runs[1].Ref)
}

// TestSetupRunsAnUnbuiltBranch: a set-up in place whose default branch
// carries no App status (a pipeline made by hand, a run that failed after the
// set-up) runs the branch, so "nothing to do" never leaves it unbuilt.
func TestSetupRunsAnUnbuiltBranch(t *testing.T) {
	ci := newFake(appDef())
	ci.triggers["app"] = []circleciclient.Trigger{{ID: "on"}}
	report, err := Setup(context.Background(), ci, &fakeGitHub{}, "giantswarm", "sample", Options{})
	require.NoError(t, err)
	require.Len(t, report.Runs, 1)
	require.Equal(t, circleciclient.Ref{Branch: "main"}, report.Runs[0].Ref)
}

// TestSetupNeverRebuildsAnOAuthBuiltTag: a tag an OAuth follow built after
// the release reads built, so the App pipeline does not publish it again.
func TestSetupNeverRebuildsAnOAuthBuiltTag(t *testing.T) {
	published := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	oauth := status("go-build", "success", "job", published.Add(time.Minute))
	oauth.Creator = &github.User{Login: new("alice")}
	gh := (&fakeGitHub{release: release("v1.0.0", published), statuses: []*github.RepoStatus{oauth}}).built()
	ci := newFake(appDef())
	ci.triggers["app"] = []circleciclient.Trigger{{ID: "on"}}
	report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", Options{Now: func() time.Time { return published.Add(time.Hour) }})
	require.NoError(t, err)
	require.Empty(t, report.Runs)
}

// TestSetupSkipsTheBranchOnTheTagsCommit: a new repository's first tag is
// its branch's head, the scaffold commit; the tag run builds it, and no
// branch run posts the same contexts after it.
func TestSetupSkipsTheBranchOnTheTagsCommit(t *testing.T) {
	published := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	gh := &fakeGitHub{release: release("v0.1.0", published), shas: map[string]string{"main": "scaffold", "v0.1.0": "scaffold"}}
	ci := newFake(oauthDef())
	report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", Options{Now: func() time.Time { return published.Add(time.Hour) }})
	require.NoError(t, err)
	require.Len(t, report.Runs, 1)
	require.Equal(t, circleciclient.Ref{Tag: "v0.1.0"}, report.Runs[0].Ref)
}
