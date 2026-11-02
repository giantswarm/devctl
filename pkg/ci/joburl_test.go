package ci

import (
	"context"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

type fakeJobURLReader struct{}

func (fakeJobURLReader) GetWorkflow(_ context.Context, id string) (*circleciclient.WorkflowDetail, error) {
	return &circleciclient.WorkflowDetail{Workflow: circleciclient.Workflow{ID: id}, ProjectSlug: "gh/giantswarm/release-automation-sandbox"}, nil
}

func (fakeJobURLReader) ListWorkflowJobs(context.Context, string) ([]circleciclient.Job, error) {
	return []circleciclient.Job{
		{ID: "39750153-7998-42cf-951f-6cce6bbd9e77", Name: "build-chart", JobNumber: 411},
		{ID: "76cd04bd-82b8-4562-8054-41a2b3f93b2a", Name: "go-build", JobNumber: 410},
		{ID: "pending-approval", Name: "hold"},
	}, nil
}

// TestResolveJobURL: a GitHub App status's job URL resolves through its
// workflow to the project and the job's number; an OAuth job page is read
// off the URL; a job without a number yet, and an unknown job, are not
// applicable; anything else is no job URL.
func TestResolveJobURL(t *testing.T) {
	ctx := context.Background()
	owner, repo, number, ok, err := ResolveJobURL(ctx, fakeJobURLReader{}, "https://app.circleci.com/workflow/04a3e5ed-6254-405e-aaf7-4cefb7984eb4/job/76cd04bd-82b8-4562-8054-41a2b3f93b2a")
	if !ok || err != nil || owner != "giantswarm" || repo != "release-automation-sandbox" || number != "410" {
		t.Errorf("App URL: %s/%s %s ok=%v err=%v", owner, repo, number, ok, err)
	}
	owner, repo, number, ok, err = ResolveJobURL(ctx, fakeJobURLReader{}, "https://circleci.com/gh/giantswarm/devctl/7781")
	if !ok || err != nil || owner != "giantswarm" || repo != "devctl" || number != "7781" {
		t.Errorf("OAuth URL: %s/%s %s ok=%v err=%v", owner, repo, number, ok, err)
	}
	for _, raw := range []string{"https://app.circleci.com/workflow/w/job/pending-approval", "https://app.circleci.com/workflow/w/job/unknown"} {
		if _, _, _, ok, err := ResolveJobURL(ctx, fakeJobURLReader{}, raw); !ok || err == nil {
			t.Errorf("%s: ok=%v err=%v, want a not-applicable error", raw, ok, err)
		}
	}
	if _, _, _, ok, _ := ResolveJobURL(ctx, fakeJobURLReader{}, "giantswarm/devctl"); ok {
		t.Error("a repository is no job URL")
	}
}

// TestIsJobURL: the shapes ResolveJobURL takes, told without a read.
func TestIsJobURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://app.circleci.com/workflow/w/job/j":        true,
		"https://circleci.com/gh/giantswarm/devctl/7781":   true,
		"giantswarm/devctl":                                false,
		"https://app.circleci.com/pipelines/github/a/b/12": false,
	} {
		if got := IsJobURL(raw); got != want {
			t.Errorf("IsJobURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
