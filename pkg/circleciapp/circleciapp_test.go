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

// TestEnsureFollowsAMissingProject: CircleCI knows no project for a new
// repository, so Ensure follows it as the person, which creates the project,
// sets up the App pipeline and deletes the OAuth trigger the follow created,
// also without migrate.
func TestEnsureFollowsAMissingProject(t *testing.T) {
	ci := newFake()
	ci.noProject = true
	ci.followCreates = true
	result, err := Ensure(context.Background(), ci, repo, false)
	require.NoError(t, err)
	require.True(t, result.FollowedProject)
	require.True(t, result.Changed())
	require.True(t, result.CreatedDefinition)
	require.True(t, result.CreatedTrigger)
	require.Equal(t, []string{"followed"}, result.DeletedOAuthTriggers)
	require.Empty(t, result.OAuthTriggers)
	require.Equal(t, []string{
		"follow giantswarm/sample",
		"create definition sample-app repo 42 file .circleci/config.yml",
		"create trigger all-pushes on created",
		"delete trigger followed",
	}, ci.writes)

	// A second run finds the project and changes nothing.
	ci.writes = nil
	result, err = Ensure(context.Background(), ci, repo, false)
	require.NoError(t, err)
	require.False(t, result.FollowedProject)
	require.False(t, result.Changed())
	require.Empty(t, ci.writes)
}

// TestEnsureRefusals: no project after the follow is IsProjectNotFound; a
// Read-only login is refused with CircleCI's 403 on the first write.
func TestEnsureRefusals(t *testing.T) {
	ci := &fakeCircleCI{noProject: true, triggers: map[string][]circleciclient.Trigger{}}
	_, err := Ensure(context.Background(), ci, repo, false)
	require.True(t, IsProjectNotFound(err), "%v", err)
	require.Equal(t, []string{"follow giantswarm/sample"}, ci.writes)

	ci = &fakeCircleCI{noProject: true, forbidden: true}
	_, err = Ensure(context.Background(), ci, repo, false)
	require.True(t, circleciclient.IsForbidden(err), "%v", err)

	ci = newFake()
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

// TestWaitReturnsASetupApprovalHold: setup approvals settle the wait even
// before continuation, without another read or the setup-only grace period.
func TestWaitReturnsASetupApprovalHold(t *testing.T) {
	for _, state := range []string{"setup", "created"} {
		t.Run(state, func(t *testing.T) {
			ci := newFake()
			ci.reads = []pipelineRead{{state: state, workflows: []circleciclient.Workflow{
				{Name: "prepare", Status: "on_hold", Tag: circleciclient.WorkflowTagSetup},
			}}}
			sleeps := 0
			outcome, err := (Waiter{CircleCI: ci, Sleep: func(context.Context, time.Duration) error {
				sleeps++
				return context.DeadlineExceeded
			}}).Wait(context.Background(), "pl1")
			require.NoError(t, err)
			require.True(t, outcome.Green)
			require.Empty(t, outcome.Failed)
			require.Equal(t, []string{"prepare"}, outcome.OnHold)
			require.Equal(t, 1, ci.read)
			require.Zero(t, sleeps)
		})
	}
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

// TestSetupNeverRebuildsAnOAuthBuiltTag: a tag whose OAuth status links a
// job of the tag's own pipeline reads built, so the App pipeline does not
// publish it again; so does one whose OAuth job CircleCI does not show, as
// nothing tells it was not built.
func TestSetupNeverRebuildsAnOAuthBuiltTag(t *testing.T) {
	published := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	opts := Options{Now: func() time.Time { return published.Add(time.Hour) }}
	branchJob := oauthWorkflowStatus("go-build", "success", "alice", "wf-branch", published.Add(2*time.Minute))
	for name, tc := range map[string]struct {
		jobs     map[int64]string
		statuses []*github.RepoStatus
	}{
		"the tag's job": {jobs: map[int64]string{7: "v1.0.0"}, statuses: []*github.RepoStatus{oauthStatus("go-build", "success", "alice", 7, published.Add(time.Minute))}},
		"an unseen job": {jobs: map[int64]string{}, statuses: []*github.RepoStatus{oauthStatus("go-build", "success", "alice", 7, published.Add(time.Minute))}},
		"no job URL": {statuses: []*github.RepoStatus{func() *github.RepoStatus {
			st := oauthStatus("go-build", "success", "alice", 7, published.Add(time.Minute))
			st.TargetURL = nil
			return st
		}()}},
		"a later branch job": {jobs: map[int64]string{7: "v1.0.0"}, statuses: []*github.RepoStatus{branchJob, oauthStatus("go-build", "success", "alice", 7, published.Add(time.Minute))}},
	} {
		t.Run(name, func(t *testing.T) {
			gh := (&fakeGitHub{release: release("v1.0.0", published), statuses: tc.statuses}).built()
			ci := newFake(appDef())
			ci.triggers["app"] = []circleciclient.Trigger{{ID: "on"}}
			ci.oauthJobs = tc.jobs
			ci.oauthWorkflows = map[string]string{"wf-branch": ""}
			report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
			require.NoError(t, err)
			require.Empty(t, report.Runs)
		})
	}
}

// TestSetupBuildsATagAfterFollowingTheProject: the follow builds the default
// branch on the OAuth pipeline, and a first release shares that commit, so
// its OAuth statuses land on the tag after the release. Their job ran in the
// branch's pipeline, not the tag's: the tag runs, in the run that followed
// and in a rerun after that run failed before running it.
func TestSetupBuildsATagAfterFollowingTheProject(t *testing.T) {
	published := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for name, st := range map[string]*github.RepoStatus{
		// The form a follow posts, seen live: the job by workflow.
		"workflow URL": oauthWorkflowStatus("setup", "failure", "alice", "wf1", published.Add(time.Minute)),
		"job URL":      oauthStatus("setup", "failure", "alice", 1, published.Add(time.Minute)),
	} {
		t.Run(name, func(t *testing.T) {
			gh := &fakeGitHub{
				release:  release("v0.1.0", published),
				statuses: []*github.RepoStatus{st},
				shas:     map[string]string{"main": "scaffold", "v0.1.0": "scaffold"},
			}
			ci := newFake()
			ci.noProject = true
			ci.followCreates = true
			ci.oauthJobs = map[int64]string{1: ""}
			ci.oauthWorkflows = map[string]string{"wf1": ""}
			opts := Options{Now: func() time.Time { return published.Add(time.Hour) }}
			report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
			require.NoError(t, err)
			require.True(t, report.Result.FollowedProject)
			require.Len(t, report.Runs, 1)
			require.Equal(t, circleciclient.Ref{Tag: "v0.1.0"}, report.Runs[0].Ref)

			// The rerun finds the project the follow created.
			report, err = Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
			require.NoError(t, err)
			require.False(t, report.Result.FollowedProject)
			require.NotEmpty(t, report.Runs)
			require.Equal(t, circleciclient.Ref{Tag: "v0.1.0"}, report.Runs[0].Ref)
		})
	}
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

// TestWaitRetriesFailedReads: a CircleCI hiccup is read again; three
// failures in a row end the wait.
func TestWaitRetriesFailedReads(t *testing.T) {
	ci := newFake()
	ci.failReads = 2
	outcome, err := Waiter{CircleCI: ci, Sleep: noSleep}.Wait(context.Background(), "pl1")
	require.NoError(t, err)
	require.True(t, outcome.Green)

	ci = newFake()
	ci.failReads = readFailures
	_, err = Waiter{CircleCI: ci, Sleep: noSleep}.Wait(context.Background(), "pl1")
	require.Error(t, err)
}

// following is a fake CircleCI that knows no project, whose follow creates
// it and starts the OAuth build of main that build reads.
func following(build ...pipelineRead) *fakeCircleCI {
	ci := newFake()
	ci.noProject = true
	ci.followCreates = true
	ci.followBuild = build
	return ci
}

// running is a listing of the follow's build in state with workflows.
func running(state string, workflows ...circleciclient.Workflow) pipelineRead {
	return pipelineRead{state: state, workflows: workflows}
}

var (
	setupDone    = circleciclient.Workflow{ID: "wf-setup", Name: "setup", Tag: circleciclient.WorkflowTagSetup, Status: "success"}
	setupRunning = circleciclient.Workflow{ID: "wf-setup", Name: "setup", Tag: circleciclient.WorkflowTagSetup, Status: "running"}
	goBuild      = circleciclient.Workflow{ID: "wf-go-build", Name: "go-build", Status: "running"}
	goBuildDone  = circleciclient.Workflow{ID: "wf-go-build", Name: "go-build", Status: "failed"}
	// setupCancelled is the setup workflow once its cancel took.
	setupCancelled = circleciclient.Workflow{ID: "wf-setup", Name: "setup", Tag: circleciclient.WorkflowTagSetup, Status: "canceled"}
	lint           = circleciclient.Workflow{ID: "wf-lint", Name: "lint", Status: "running"}
)

// noWait reads the follow's build again without waiting.
var noWait = Options{Waiter: Waiter{Sleep: noSleep}}

// TestSetupCancelsTheFollowsBuildBeforeTheBranchRun: the follow that created
// the project builds the default branch at once on the OAuth pipeline, as
// the person. Its running workflows are cancelled right before the App
// pipeline runs the branch, so the App's statuses are the newest of every
// context on the branch's head; a finished workflow is left alone.
func TestSetupCancelsTheFollowsBuildBeforeTheBranchRun(t *testing.T) {
	ci := following(running("created", setupDone, goBuild))
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
	require.NoError(t, err)
	require.Equal(t, []string{"go-build"}, report.CancelledFollowWorkflows)
	require.Empty(t, report.FollowBuildUncancelled)
	require.Equal(t, []string{
		"follow giantswarm/sample",
		"create definition sample-app repo 42 file .circleci/config.yml",
		"create trigger all-pushes on created",
		"delete trigger followed",
		"cancel workflow wf-go-build",
		"run created on branch main",
	}, ci.writes)
}

// TestSetupWaitsForTheFollowsBuildToSettle: CircleCI may list the follow's
// build a moment after the follow, and its setup workflow creates the
// workflows that carry the build only when it finishes. The build is read
// again until it is listed and continued, and then cancelled; a running
// setup workflow is cancelled, so nothing continues it.
func TestSetupWaitsForTheFollowsBuildToSettle(t *testing.T) {
	for name, tc := range map[string]struct {
		build     []pipelineRead
		refused   map[string]int
		cancelled []string
	}{
		"not listed yet": {
			build:     []pipelineRead{running("setup-pending"), running("created", setupDone, goBuild)},
			cancelled: []string{"go-build"},
		},
		"continuing": {
			build:     []pipelineRead{running("setup", setupDone), running("created", setupDone, goBuild, lint)},
			cancelled: []string{"go-build", "lint"},
		},
		"setup running": {
			build:     []pipelineRead{running("setup", setupRunning)},
			cancelled: []string{"setup"},
		},
		"listing lags the continuation": {
			build:     []pipelineRead{running("created", setupDone), running("created", setupDone, goBuild)},
			cancelled: []string{"go-build"},
		},
		"continued as setup was cancelled": {
			build:     []pipelineRead{running("setup", setupRunning), running("created", setupCancelled, goBuild)},
			cancelled: []string{"setup", "go-build"},
		},
		"refused once": {
			build:     []pipelineRead{running("created", setupDone, goBuild)},
			refused:   map[string]int{"wf-go-build": 1},
			cancelled: []string{"go-build"},
		},
		"finished before the cancel": {
			build:   []pipelineRead{running("created", setupDone, goBuild), running("created", setupDone, goBuildDone)},
			refused: map[string]int{"wf-go-build": followBuildReads},
		},
		"errored": {
			build: []pipelineRead{running("errored")},
		},
		"nothing to build": {
			build: []pipelineRead{running("created")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			ci := following(tc.build...)
			ci.refuseCancel = tc.refused
			report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
			require.NoError(t, err)
			require.Equal(t, tc.cancelled, report.CancelledFollowWorkflows)
			require.Empty(t, report.FollowBuildUncancelled)
			require.Less(t, ci.branchListings, followBuildReads, "settled before the reads ran out")
		})
	}
}

// TestSetupReportsAnUncancelledFollowBuild: a cancel CircleCI refuses, or a
// follow's build CircleCI never lists, is noted in the report; the other
// workflows are still cancelled, and the branch still runs on the App
// pipeline.
func TestSetupReportsAnUncancelledFollowBuild(t *testing.T) {
	for name, tc := range map[string]struct {
		ci        *fakeCircleCI
		cancelled []string
		reason    string
	}{
		"refused": {
			ci: func() *fakeCircleCI {
				ci := following(running("created", setupDone, goBuild, lint))
				ci.forbidCancel = map[string]bool{"wf-go-build": true}
				return ci
			}(),
			cancelled: []string{"lint"},
			reason:    "cancelling workflow go-build: ",
		},
		"never listed": {
			ci: func() *fakeCircleCI {
				ci := following()
				ci.followUnlisted = true
				return ci
			}(),
			reason: "CircleCI listed no build of main",
		},
		"never continued": {
			ci:     following(running("setup", setupDone)),
			reason: "the build of main had not continued",
		},
		"workflows unread": {
			ci: func() *fakeCircleCI {
				ci := following(running("created", setupDone, goBuild))
				ci.refuseWorkflows = true
				return ci
			}(),
			reason: "listing the workflows of pipeline 1: ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			report, err := Setup(t.Context(), tc.ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
			require.NoError(t, err)
			require.Equal(t, tc.cancelled, report.CancelledFollowWorkflows)
			require.Contains(t, report.FollowBuildUncancelled, tc.reason)
			require.Len(t, report.Runs, 1)
			require.Equal(t, circleciclient.Ref{Branch: "main"}, report.Runs[0].Ref)
		})
	}
}

// TestSetupStopsReadingTheFollowsBuildWithTheContext: a wait the context
// ends stops the reads; that is noted, and the branch still runs.
func TestSetupStopsReadingTheFollowsBuildWithTheContext(t *testing.T) {
	ci := following(running("setup", setupDone))
	opts := Options{Waiter: Waiter{Sleep: func(context.Context, time.Duration) error { return context.Canceled }}}
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", opts)
	require.NoError(t, err)
	require.Equal(t, 1, ci.branchListings)
	require.Contains(t, report.FollowBuildUncancelled, context.Canceled.Error())
	require.Len(t, report.Runs, 1)
}

// TestSetupCancelsTheFollowsBuildWhateverItsTrigger: CircleCI may type the
// follow's build as run through the API; on a project seconds old, every
// build of the branch is the follow's.
func TestSetupCancelsTheFollowsBuildWhateverItsTrigger(t *testing.T) {
	ci := newFake()
	ci.noProject = true
	ci.followCreates = true
	ci.followUnlisted = true
	ci.branchPipelines = []circleciclient.Pipeline{{ID: "api", Number: 1, State: "created", Trigger: circleciclient.PipelineTrigger{Type: "api"}}}
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
	require.NoError(t, err)
	require.Equal(t, []string{"build"}, report.CancelledFollowWorkflows)
	require.Contains(t, ci.writes, "cancel workflow wf-api")
}

// TestSetupGivesUpARefusedCancel: CircleCI refusing the person the cancel
// (403) is no hiccup; it is noted at once, not tried for the rest of the
// reads.
func TestSetupGivesUpARefusedCancel(t *testing.T) {
	ci := following(running("created", setupDone, goBuild))
	ci.forbidCancel = map[string]bool{"wf-go-build": true}
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
	require.NoError(t, err)
	require.Contains(t, report.FollowBuildUncancelled, "cancelling workflow go-build: ")
	require.Equal(t, 1, ci.branchListings)
	require.Equal(t, 1, countOf(ci.writes, "cancel workflow wf-go-build"))
}

// TestSetupForgetsTheRefusalOfAWorkflowGone: a workflow whose cancel failed
// and that the build no longer lists does not hold the build unsettled.
func TestSetupForgetsTheRefusalOfAWorkflowGone(t *testing.T) {
	lintDone := circleciclient.Workflow{ID: "wf-lint", Name: "lint", Status: "success"}
	ci := following(running("created", setupDone, goBuild, lintDone), running("created", setupDone, lintDone))
	ci.refuseCancel = map[string]int{"wf-go-build": followBuildReads}
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
	require.NoError(t, err)
	require.Empty(t, report.FollowBuildUncancelled)
}

// TestSetupLeavesTheBuildsOfAnExistingProject: without a follow the
// branch's builds are not even read.
func TestSetupLeavesTheBuildsOfAnExistingProject(t *testing.T) {
	ci := newFake(oauthDef())
	ci.followed = true
	ci.followBuild = []pipelineRead{running("created", goBuild)}
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
	require.NoError(t, err)
	require.Empty(t, report.CancelledFollowWorkflows)
	require.Empty(t, report.FollowBuildUncancelled)
	require.Zero(t, ci.branchListings)
}

func countOf(writes []string, w string) int {
	n := 0
	for _, x := range writes {
		if x == w {
			n++
		}
	}
	return n
}

// TestSetupStopsReadingARefusedListing: CircleCI refusing the person the
// build's workflows (403) is noted at once, not read again.
func TestSetupStopsReadingARefusedListing(t *testing.T) {
	ci := following(running("created", setupDone, goBuild))
	ci.refuseWorkflows = true
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
	require.NoError(t, err)
	require.Contains(t, report.FollowBuildUncancelled, "listing the workflows of pipeline 1: ")
	require.Equal(t, 1, ci.branchListings)
}

// TestSetupNotesTheWaitEndingAfterACancel: a wait that ends after a read
// that cancelled the build says so, after what it cancelled.
func TestSetupNotesTheWaitEndingAfterACancel(t *testing.T) {
	ci := following(running("created", setupDone, goBuild))
	opts := Options{Waiter: Waiter{Sleep: func(context.Context, time.Duration) error { return context.Canceled }}}
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", opts)
	require.NoError(t, err)
	require.Equal(t, []string{"go-build"}, report.CancelledFollowWorkflows)
	require.Equal(t, "the wait for further reads of the build of main ended: context canceled", report.FollowBuildUncancelled)
}

// TestSetupNamesACancelledWorkflowOnce: workflows of one name in two
// pipelines are named once.
func TestSetupNamesACancelledWorkflowOnce(t *testing.T) {
	ci := following(running("created", setupDone, circleciclient.Workflow{ID: "wf-build", Name: "build", Status: "running"}))
	ci.branchPipelines = []circleciclient.Pipeline{{ID: "push", Number: 2, State: "created"}}
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
	require.NoError(t, err)
	require.Equal(t, []string{"build"}, report.CancelledFollowWorkflows)
	require.Contains(t, ci.writes, "cancel workflow wf-push")
}

// TestSetupReadsAgainAfterAListingNotFound: right after the follow, CircleCI
// may answer the branch's pipelines with 404 a moment; that is read again.
func TestSetupReadsAgainAfterAListingNotFound(t *testing.T) {
	ci := following(running("created", setupDone, goBuild))
	ci.branchNotFound = 1
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
	require.NoError(t, err)
	require.Equal(t, []string{"go-build"}, report.CancelledFollowWorkflows)
	require.Empty(t, report.FollowBuildUncancelled)
}

// TestSetupReadsOnPastOneRefusedPipeline: CircleCI refusing the workflows
// of one pipeline while another is read does not end the reads.
func TestSetupReadsOnPastOneRefusedPipeline(t *testing.T) {
	ci := following(running("created", setupDone, goBuild))
	ci.refuseWorkflows = true
	ci.branchPipelines = []circleciclient.Pipeline{{ID: "push", Number: 2, State: "created"}}
	report, err := Setup(t.Context(), ci, &fakeGitHub{}, "giantswarm", "sample", noWait)
	require.NoError(t, err)
	require.Equal(t, []string{"build"}, report.CancelledFollowWorkflows)
	require.Contains(t, report.FollowBuildUncancelled, "listing the workflows of pipeline 1: ")
	require.Equal(t, followBuildReads, ci.branchListings)
}
