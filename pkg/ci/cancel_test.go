package ci

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

type cancelFake struct {
	CancelClient
	workflows []circleciclient.Workflow
	jobs      []circleciclient.Job
	canceled  []string
	pageCalls int
	pageError bool
}

func (f *cancelFake) GetWorkflow(context.Context, string) (*circleciclient.WorkflowDetail, error) {
	return &circleciclient.WorkflowDetail{Workflow: circleciclient.Workflow{ID: "w", Status: "running"}, ProjectSlug: "gh/o/r", PipelineID: "p"}, nil
}
func (f *cancelFake) ListPipelineWorkflows(context.Context, string) ([]circleciclient.Workflow, error) {
	return f.workflows, nil
}
func (f *cancelFake) ListWorkflowJobs(context.Context, string) ([]circleciclient.Job, error) {
	return f.jobs, nil
}
func (f *cancelFake) CancelWorkflow(_ context.Context, id string) error {
	f.canceled = append(f.canceled, id)
	return nil
}
func (f *cancelFake) ListPipelines(_ context.Context, _, _, token string) (*circleciclient.PipelinePage, error) {
	f.pageCalls++
	if token != "" && f.pageError {
		return nil, errors.New("listing failed")
	}
	page := &circleciclient.PipelinePage{Items: []circleciclient.Pipeline{{ID: "p"}}}
	page.Items[0].VCS.Branch = "feature"
	page.Items[0].VCS.Revision = "old"
	if token == "" {
		page.NextPageToken = "next"
	}
	return page, nil
}

func TestCancelOneRepositoryAndDryRun(t *testing.T) {
	for _, tc := range []struct {
		repo string
		dry  bool
		want int
		fail bool
	}{
		{"r", false, 1, false}, {"r", true, 0, false}, {"other", false, 0, true},
	} {
		t.Run(tc.repo+time.Duration(tc.want).String(), func(t *testing.T) {
			f := &cancelFake{}
			result := &CancelResult{DryRun: tc.dry}
			err := CancelOne(context.Background(), f, "o", tc.repo, "w", false, result)
			require.Equal(t, tc.fail, err != nil)
			require.Len(t, f.canceled, tc.want)
		})
	}
}

func TestCancelStalePagesAndQueuedOnly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		job   circleciclient.Job
		stale bool
		want  int
	}{
		{name: "queued", job: circleciclient.Job{Status: "queued"}, stale: true, want: 2},
		{name: "started", job: circleciclient.Job{Status: "running", StartedAt: time.Now()}, stale: true},
		{name: "approval", job: circleciclient.Job{Status: "on_hold", Type: "approval"}, stale: true},
		{name: "current", job: circleciclient.Job{Status: "queued"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &cancelFake{workflows: []circleciclient.Workflow{{ID: "w", Status: "running"}}, jobs: []circleciclient.Job{tc.job}}
			result := &CancelResult{}
			err := CancelStale(context.Background(), f, "o", "r", func(context.Context, circleciclient.Pipeline) (bool, error) { return tc.stale, nil }, result)
			require.NoError(t, err)
			require.Equal(t, 2, f.pageCalls)
			require.Len(t, f.canceled, tc.want)
		})
	}
}

func TestCancelStaleReadFailureStopsWrites(t *testing.T) {
	f := &cancelFake{pageError: true}
	err := CancelStale(context.Background(), f, "o", "r", func(context.Context, circleciclient.Pipeline) (bool, error) {
		return false, errors.New("GitHub unavailable")
	}, &CancelResult{})
	require.ErrorContains(t, err, "GitHub unavailable")
	require.Empty(t, f.canceled)
	f = &cancelFake{pageError: true}
	err = CancelStale(context.Background(), f, "o", "r", func(context.Context, circleciclient.Pipeline) (bool, error) { return false, nil }, &CancelResult{})
	require.ErrorContains(t, err, "listing failed")
	require.Empty(t, f.canceled)
}
