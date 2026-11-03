package ci

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// jobURLReader is what ResolveJobURL reads of CircleCI.
type jobURLReader interface {
	GetWorkflow(ctx context.Context, id string) (*circleciclient.WorkflowDetail, error)
	ListWorkflowJobs(ctx context.Context, workflowID string) ([]circleciclient.Job, error)
}

// IsJobURL says whether raw has the shape of a CircleCI job URL
// ResolveJobURL takes, without reading anything: a usage error can be told
// before any login is needed.
func IsJobURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if t, ok := circleciclient.ParseStatusTarget(raw); ok && t.Job > 0 {
		return true
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return u.Host == "app.circleci.com" && len(parts) == 4 && parts[0] == "workflow" && parts[2] == "job"
}

// ResolveJobURL names the job a CircleCI job URL points at by its project
// and number: the job page an OAuth status links
// (https://circleci.com/gh/<owner>/<repo>/<number>) is read off the URL, the
// one a GitHub App status links (https://app.circleci.com/workflow/<workflow
// id>/job/<job id>) names no project and no number, and is looked up: the
// workflow's project, and among its jobs the one of that id. ok is false for
// a URL of neither form.
func ResolveJobURL(ctx context.Context, client jobURLReader, raw string) (owner, repo, number string, ok bool, err error) {
	u, perr := url.Parse(raw)
	if perr != nil || u.Host == "" {
		return "", "", "", false, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if t, found := circleciclient.ParseStatusTarget(raw); found && t.Job > 0 {
		return t.Org, t.Repo, strconv.FormatInt(t.Job, 10), true, nil
	}
	if u.Host != "app.circleci.com" || len(parts) != 4 || parts[0] != "workflow" || parts[2] != "job" {
		return "", "", "", false, nil
	}
	workflowID, jobID := parts[1], parts[3]
	workflow, err := client.GetWorkflow(ctx, workflowID)
	switch {
	case circleciclient.IsNotFound(err):
		return "", "", "", true, notApplicable("CircleCI has no workflow %s the login sees", workflowID)
	case err != nil:
		return "", "", "", true, err
	}
	slug := strings.Split(workflow.ProjectSlug, "/")
	if len(slug) != 3 || (slug[0] != "gh" && slug[0] != "github") {
		return "", "", "", true, notApplicable("workflow %s belongs to the project %q, not a GitHub project gh/<owner>/<repo>", workflowID, workflow.ProjectSlug)
	}
	jobs, err := client.ListWorkflowJobs(ctx, workflowID)
	if err != nil {
		return "", "", "", true, err
	}
	for _, j := range jobs {
		if j.ID != jobID {
			continue
		}
		if j.JobNumber == 0 {
			return "", "", "", true, notApplicable("job %s of workflow %s has not started: it has no number yet", jobID, workflowID)
		}
		return slug[1], slug[2], strconv.FormatInt(j.JobNumber, 10), true, nil
	}
	return "", "", "", true, notApplicable("workflow %s has no job %s", workflowID, jobID)
}
