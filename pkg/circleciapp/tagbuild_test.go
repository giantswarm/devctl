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
		{"OAuth status has no tag evidence", func(_ *imageTagCircleCI, gh *fakeGitHub) { gh.statuses[0].Creator.Login = new("alice") }, "cannot identify which image tag the OAuth statuses built"},
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
