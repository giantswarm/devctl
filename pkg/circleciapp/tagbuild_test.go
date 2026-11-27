package circleciapp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

type imageTagCircleCI struct {
	*fakeCircleCI
	workflows   map[string]*circleciclient.WorkflowDetail
	pipelines   map[string]*circleciclient.PipelineDetail
	workflowErr error
	pipelineErr error
	failTag     string
	reads       []string
}

func (f *imageTagCircleCI) GetWorkflow(_ context.Context, id string) (*circleciclient.WorkflowDetail, error) {
	f.reads = append(f.reads, "workflow "+id)
	if f.workflowErr != nil {
		return nil, f.workflowErr
	}
	w, ok := f.workflows[id]
	if !ok {
		return nil, fmt.Errorf("unexpected workflow %s", id)
	}
	return w, nil
}

func (f *imageTagCircleCI) GetPipeline(_ context.Context, id string) (*circleciclient.PipelineDetail, error) {
	f.reads = append(f.reads, "pipeline "+id)
	if f.pipelineErr != nil {
		return nil, f.pipelineErr
	}
	p, ok := f.pipelines[id]
	if !ok {
		return nil, fmt.Errorf("unexpected pipeline %s", id)
	}
	return p, nil
}

func (f *imageTagCircleCI) RunPipeline(ctx context.Context, org, repo, definition string, ref circleciclient.Ref) (*circleciclient.Pipeline, error) {
	if f.failTag != "" && ref.Tag == f.failTag {
		return nil, fmt.Errorf("pipeline dispatch refused")
	}
	return f.fakeCircleCI.RunPipeline(ctx, org, repo, definition, ref)
}

func imageTagFixture() (*imageTagCircleCI, *fakeGitHub, Options) {
	published := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	ci := &imageTagCircleCI{
		fakeCircleCI: newFake(appDef()),
		workflows:    map[string]*circleciclient.WorkflowDetail{},
		pipelines:    map[string]*circleciclient.PipelineDetail{},
	}
	ci.triggers["app"] = []circleciclient.Trigger{{ID: "on"}}
	gh := (&fakeGitHub{
		release:  release("b/v1.0.0", published),
		releases: []*github.RepositoryRelease{release("a/v1.0.0", published), release("b/v1.0.0", published)},
		shas:     map[string]string{"a/v1.0.0": "shared", "b/v1.0.0": "shared"},
	}).built()
	return ci, gh, Options{Now: func() time.Time { return published.Add(time.Hour) }}
}

func (f *imageTagCircleCI) addBuild(gh *fakeGitHub, tag, id, state string) {
	project := circleciclient.ProjectSlug("giantswarm", "sample")
	f.workflows[id] = &circleciclient.WorkflowDetail{PipelineID: id, ProjectSlug: project}
	f.pipelines[id] = &circleciclient.PipelineDetail{
		Pipeline:    circleciclient.Pipeline{ID: id, VCS: circleciclient.PipelineVCS{Tag: tag, Revision: "shared"}},
		ProjectSlug: project,
	}
	// Every tag sees the same commit's statuses; both images post setup.
	at := gh.release.GetPublishedAt().Add(time.Minute)
	gh.statuses = append([]*github.RepoStatus{status("setup", state, id, at)}, gh.statuses...)
}

func TestSetupDoesNotReuseAnotherImageTagsBuild(t *testing.T) {
	for _, state := range []string{"success", "pending", "failed"} {
		t.Run(state, func(t *testing.T) {
			ci, gh, opts := imageTagFixture()
			ci.addBuild(gh, "a/v1.0.0", "a", state)
			report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
			require.NoError(t, err)
			require.Len(t, report.Runs, 1)
			require.Equal(t, circleciclient.Ref{Tag: "b/v1.0.0"}, report.Runs[0].Ref)
			require.Equal(t, []string{"run app on tag b/v1.0.0"}, ci.writes)
			require.Equal(t, []string{"workflow a", "pipeline a"}, ci.reads, "shared evidence is read once across releases")
		})
	}
}

func TestSetupLeavesBothBuiltImageTagsAlone(t *testing.T) {
	ci, gh, opts := imageTagFixture()
	ci.addBuild(gh, "a/v1.0.0", "a", "success")
	ci.addBuild(gh, "b/v1.0.0", "b", "success")
	report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
	require.NoError(t, err)
	require.Empty(t, report.Runs, "the older status with the same context still identifies a's build")
	require.Empty(t, ci.writes)
	require.Len(t, ci.reads, 4)
}

func TestSetupRetriesOnlyTheUndispatchedImageTag(t *testing.T) {
	ci, gh, opts := imageTagFixture()
	ci.failTag = "b/v1.0.0"
	_, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
	require.ErrorContains(t, err, "pipeline dispatch refused")
	require.Equal(t, []string{"run app on tag a/v1.0.0"}, ci.writes)

	ci.addBuild(gh, "a/v1.0.0", "a", "success")
	ci.failTag = ""
	ci.writes = nil
	report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
	require.NoError(t, err)
	require.Len(t, report.Runs, 1)
	require.Equal(t, circleciclient.Ref{Tag: "b/v1.0.0"}, report.Runs[0].Ref)
	require.Equal(t, []string{"run app on tag b/v1.0.0"}, ci.writes)
}

func TestSetupDoesNotReuseABranchBuildForImageTags(t *testing.T) {
	ci, gh, opts := imageTagFixture()
	ci.addBuild(gh, "", "branch", "success")
	ci.pipelines["branch"].VCS.Branch = "main"
	report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
	require.NoError(t, err)
	require.Len(t, report.Runs, 2)
	require.Equal(t, circleciclient.Ref{Tag: "a/v1.0.0"}, report.Runs[0].Ref)
	require.Equal(t, circleciclient.Ref{Tag: "b/v1.0.0"}, report.Runs[1].Ref)
}

func TestSetupRefusesUncertainImageTagBuilds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*imageTagCircleCI, *fakeGitHub)
		want   string
	}{
		{"workflow read fails", func(ci *imageTagCircleCI, _ *fakeGitHub) { ci.workflowErr = fmt.Errorf("workflow unavailable") }, "workflow unavailable"},
		{"pipeline read refused", func(ci *imageTagCircleCI, _ *fakeGitHub) { ci.pipelineErr = forbidden() }, "reading pipeline a"},
		{"workflow has no pipeline", func(ci *imageTagCircleCI, _ *fakeGitHub) { ci.workflows["a"].PipelineID = "" }, "does not identify a pipeline"},
		{"workflow in another project", func(ci *imageTagCircleCI, _ *fakeGitHub) { ci.workflows["a"].ProjectSlug = "gh/other/sample" }, "does not identify a pipeline"},
		{"pipeline in another project", func(ci *imageTagCircleCI, _ *fakeGitHub) { ci.pipelines["a"].ProjectSlug = "gh/other/sample" }, "does not identify its checkout ref"},
		{"pipeline has no checkout ref", func(ci *imageTagCircleCI, _ *fakeGitHub) { ci.pipelines["a"].VCS = circleciclient.PipelineVCS{} }, "does not identify its checkout ref"},
		{"status has no workflow URL", func(_ *imageTagCircleCI, gh *fakeGitHub) { gh.statuses[0].TargetURL = nil }, "has no workflow URL"},
		{"OAuth status has no tag evidence", func(ci *imageTagCircleCI, gh *fakeGitHub) {
			gh.statuses[0].Creator.Login = new("alice")
			ci.workflowErr = notFound()
		}, "cannot identify which image tag the OAuth statuses built"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ci, gh, opts := imageTagFixture()
			ci.addBuild(gh, "a/v1.0.0", "a", "success")
			tc.change(ci, gh)
			_, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
			require.ErrorContains(t, err, tc.want)
			require.Empty(t, ci.writes, "uncertain attribution must not dispatch any release")
			if tc.name == "pipeline read refused" {
				require.True(t, circleciclient.IsForbidden(err), "keep the command's existing auth classification")
			}
		})
	}
}

// TestSetupAttributesOAuthImageTagBuilds: an OAuth status links its job, and
// the job's pipeline names the image tag it built: that tag reads built and
// the other runs. A job of the default branch's pipeline, as the follow that
// created the project builds, builds neither.
func TestSetupAttributesOAuthImageTagBuilds(t *testing.T) {
	for _, tc := range []struct {
		name string
		vcs  circleciclient.PipelineVCS
		want []circleciclient.Ref
	}{
		{"the job built a", circleciclient.PipelineVCS{Tag: "a/v1.0.0"}, []circleciclient.Ref{{Tag: "b/v1.0.0"}}},
		{"the job built the branch", circleciclient.PipelineVCS{Branch: "main"}, []circleciclient.Ref{{Tag: "a/v1.0.0"}, {Tag: "b/v1.0.0"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ci, gh, opts := imageTagFixture()
			ci.oauthJobs = map[int64]string{7: ""}
			ci.pipelines["oauth-7"] = &circleciclient.PipelineDetail{Pipeline: circleciclient.Pipeline{ID: "oauth-7", VCS: tc.vcs}, ProjectSlug: circleciclient.ProjectSlug("giantswarm", "sample")}
			gh.statuses = []*github.RepoStatus{oauthStatus("setup", "success", "alice", 7, gh.release.GetPublishedAt().Add(time.Minute))}
			report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
			require.NoError(t, err)
			var refs []circleciclient.Ref
			for _, r := range report.Runs {
				refs = append(refs, r.Ref)
			}
			require.Equal(t, tc.want, refs)
		})
	}
}

func TestImageTagBuildReaderSharesPipelineReads(t *testing.T) {
	ci, gh, opts := imageTagFixture()
	ci.addBuild(gh, "a/v1.0.0", "a", "success")
	project := circleciclient.ProjectSlug("giantswarm", "sample")
	ci.workflows["continued"] = &circleciclient.WorkflowDetail{PipelineID: "a", ProjectSlug: project}
	gh.statuses = append([]*github.RepoStatus{status("push-a", "success", "continued", opts.Now())}, gh.statuses...)
	report, err := Setup(context.Background(), ci, gh, "giantswarm", "sample", opts)
	require.NoError(t, err)
	require.Len(t, report.Runs, 1)
	require.Equal(t, "b/v1.0.0", report.Runs[0].Ref.Tag)
	require.Equal(t, []string{"workflow continued", "pipeline a", "workflow a"}, ci.reads)
}

// TestOAuthBuilt: an OAuth status reads the tag's build only when its job's
// pipeline checked out the tag, by workflow or by job number; a status that
// leads to no readable pipeline of the project is built but unidentified,
// unless another status identifies the tag.
func TestOAuthBuilt(t *testing.T) {
	published := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	at := published.Add(time.Minute)
	tagWF := oauthWorkflowStatus("go-build", "success", "alice", "wf-tag", at)
	branchWF := oauthWorkflowStatus("go-build", "success", "alice", "wf-branch", at)
	unseenWF := oauthWorkflowStatus("go-build", "success", "alice", "wf-unseen", at)
	tagJob := oauthStatus("go-build", "success", "alice", 7, at)
	branchJob := oauthStatus("go-build", "success", "alice", 8, at)
	otherRepoJob := oauthStatus("go-build", "success", "alice", 7, at)
	otherRepoJob.TargetURL = new("https://circleci.com/gh/giantswarm/other/7")
	oldTagJob := oauthStatus("go-build", "success", "alice", 7, published.Add(-time.Hour))
	app := status("go-build", "success", "wf-tag", at)
	for _, tc := range []struct {
		name              string
		statuses          []*github.RepoStatus
		built, identified bool
	}{
		{"none", nil, false, true},
		{"App status only", []*github.RepoStatus{app}, false, true},
		{"before the release", []*github.RepoStatus{oldTagJob}, false, true},
		{"tag by workflow", []*github.RepoStatus{tagWF}, true, true},
		{"tag by job", []*github.RepoStatus{tagJob}, true, true},
		{"branch by workflow", []*github.RepoStatus{branchWF}, false, true},
		{"branch by job", []*github.RepoStatus{branchJob}, false, true},
		{"branch, then tag", []*github.RepoStatus{branchWF, tagJob}, true, true},
		{"unseen workflow", []*github.RepoStatus{unseenWF}, true, false},
		{"unseen, then branch", []*github.RepoStatus{unseenWF, branchJob}, true, false},
		{"unseen, then tag", []*github.RepoStatus{unseenWF, tagWF}, true, true},
		{"job of another repository", []*github.RepoStatus{otherRepoJob}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ci := newFake()
			ci.oauthJobs = map[int64]string{7: "v1.0.0", 8: ""}
			ci.oauthWorkflows = map[string]string{"wf-tag": "v1.0.0", "wf-branch": ""}
			r := tagBuildReader{ci: ci, project: circleciclient.ProjectSlug("giantswarm", "sample"), workflows: map[string]string{}, pipelines: map[string]string{}}
			built, identified, err := r.oauthBuilt(context.Background(), "giantswarm", "sample", "v1.0.0", tc.statuses, published)
			require.NoError(t, err)
			require.Equal(t, tc.built, built, "built")
			require.Equal(t, tc.identified, identified, "identified")
		})
	}
}

// TestOAuthBuiltCachesOnlyIdentifiedReads: a workflow read once is not read
// again for the next release on the commit; an unseen one is not cached.
func TestOAuthBuiltCachesOnlyIdentifiedReads(t *testing.T) {
	published := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	ci := newFake()
	ci.oauthWorkflows = map[string]string{"wf-branch": ""}
	r := tagBuildReader{ci: ci, project: circleciclient.ProjectSlug("giantswarm", "sample"), workflows: map[string]string{}, pipelines: map[string]string{}}
	statuses := []*github.RepoStatus{
		oauthWorkflowStatus("setup", "failure", "alice", "wf-branch", published.Add(time.Minute)),
		oauthWorkflowStatus("setup", "failure", "alice", "wf-unseen", published.Add(time.Minute)),
	}
	for _, tag := range []string{"v1.0.0", "v1.0.1"} {
		_, _, err := r.oauthBuilt(context.Background(), "giantswarm", "sample", tag, statuses, published)
		require.NoError(t, err)
	}
	require.Equal(t, map[string]int{"wf-branch": 1, "wf-unseen": 2}, ci.workflowReads)
	require.Equal(t, map[string]string{"wf-branch": ""}, r.workflows)
	require.Equal(t, map[string]string{"oauth-wf-wf-branch": ""}, r.pipelines)
}
