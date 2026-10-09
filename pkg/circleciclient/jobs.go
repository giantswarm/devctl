package circleciclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/giantswarm/microerror"
)

// ProjectSlug is the slug CircleCI names the project of a GitHub repository
// by, the project_slug of its pipelines and workflows.
func ProjectSlug(org, repo string) string {
	return "gh/" + org + "/" + repo
}

// PipelineDetail is one pipeline as the pipeline endpoints answer it: the
// pipeline and the project it belongs to.
type PipelineDetail struct {
	Pipeline
	ProjectSlug string `json:"project_slug"`
}

// GetPipeline returns the pipeline of id; IsNotFound when CircleCI knows
// none the token's user sees.
func (c *Client) GetPipeline(ctx context.Context, id string) (*PipelineDetail, error) {
	var p PipelineDetail
	if err := c.do(ctx, http.MethodGet, "/api/v2/pipeline/"+url.PathEscape(id), nil, &p); err != nil {
		return nil, microerror.Mask(err)
	}
	return &p, nil
}

// GetProjectPipeline returns pipeline number of org/repo, the number the
// CircleCI UI shows; IsNotFound when the project has none.
func (c *Client) GetProjectPipeline(ctx context.Context, org, repo string, number int64) (*PipelineDetail, error) {
	var p PipelineDetail
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/pipeline/%d", c.v2Project(org, repo), number), nil, &p); err != nil {
		return nil, microerror.Mask(err)
	}
	return &p, nil
}

// WorkflowDetail is one workflow as the workflow endpoint answers it: the
// workflow, its pipeline and its project.
type WorkflowDetail struct {
	Workflow
	PipelineID  string `json:"pipeline_id"`
	ProjectSlug string `json:"project_slug"`
}

// GetWorkflow returns the workflow of id; IsNotFound when CircleCI knows
// none the token's user sees.
func (c *Client) GetWorkflow(ctx context.Context, id string) (*WorkflowDetail, error) {
	var w WorkflowDetail
	if err := c.do(ctx, http.MethodGet, "/api/v2/workflow/"+url.PathEscape(id), nil, &w); err != nil {
		return nil, microerror.Mask(err)
	}
	return &w, nil
}

// CancelWorkflow cancels a running workflow. CircleCI accepts the cancel and
// finishes the workflow on its own; the workflow reads canceled a little
// later. A token without write access is refused with IsForbidden.
func (c *Client) CancelWorkflow(ctx context.Context, workflowID string) error {
	return microerror.Mask(c.do(ctx, http.MethodPost, "/api/v2/workflow/"+url.PathEscape(workflowID)+"/cancel", nil, nil))
}

// RerunWorkflow reruns a finished workflow and returns the id of the new
// workflow, a second workflow of the same name in the same pipeline: every
// job of it, or with fromFailed its failed jobs and the jobs that depend on
// them, the passed ones kept. A token without write access is refused with
// IsForbidden; a workflow CircleCI will not rerun (one still running, one
// without a failed job for fromFailed) with IsAPI carrying its message.
func (c *Client) RerunWorkflow(ctx context.Context, workflowID string, fromFailed bool) (string, error) {
	var out struct {
		WorkflowID string `json:"workflow_id"`
	}
	body := map[string]bool{"from_failed": fromFailed}
	if err := c.do(ctx, http.MethodPost, "/api/v2/workflow/"+url.PathEscape(workflowID)+"/rerun", body, &out); err != nil {
		return "", microerror.Mask(err)
	}
	return out.WorkflowID, nil
}

// Step is one step of a job as the v1.1 job detail lists it (API v2 has no
// steps): what the job is doing right now when it is running. A step that
// has not started has a zero StartedAt; one still running a zero EndedAt. A
// parallel step is listed once per action, Index telling them apart.
type Step struct {
	Name      string
	Index     int
	Status    string
	StartedAt time.Time
	EndedAt   time.Time
	// OutputURL is where the step's output is, a signed URL read without
	// the token; empty for a step without output.
	OutputURL string
}

// JobSteps returns the steps of job number of org/repo, in order.
func (c *Client) JobSteps(ctx context.Context, org, repo string, number int64) ([]Step, error) {
	var job struct {
		Steps []struct {
			Name    string `json:"name"`
			Actions []struct {
				Name      string    `json:"name"`
				Index     int       `json:"index"`
				Status    string    `json:"status"`
				StartTime time.Time `json:"start_time"`
				EndTime   time.Time `json:"end_time"`
				OutputURL string    `json:"output_url"`
			} `json:"actions"`
		} `json:"steps"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/%d", c.v1Project(org, repo), number), nil, &job); err != nil {
		return nil, microerror.Mask(err)
	}
	var steps []Step
	for _, step := range job.Steps {
		for _, action := range step.Actions {
			name := action.Name
			if name == "" {
				name = step.Name
			}
			steps = append(steps, Step{
				Name:      name,
				Index:     action.Index,
				Status:    action.Status,
				StartedAt: action.StartTime,
				EndedAt:   action.EndTime,
				OutputURL: action.OutputURL,
			})
		}
	}
	return steps, nil
}

// LastOutputAt reads a step's output at outputURL and returns the time of
// its last message: when the step last wrote anything. Zero when the step
// has written nothing yet.
func (c *Client) LastOutputAt(ctx context.Context, outputURL string) (time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, outputURL, nil)
	if err != nil {
		return time.Time{}, microerror.Mask(err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return time.Time{}, microerror.Mask(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, microerror.Maskf(apiError, "step output: HTTP %d", resp.StatusCode)
	}
	// The output is one JSON array of messages; only their times are kept,
	// so a long log costs no more memory than its longest message.
	dec := json.NewDecoder(resp.Body)
	if _, err := dec.Token(); err != nil {
		return time.Time{}, microerror.Maskf(apiError, "step output: invalid JSON answer: %v", err)
	}
	var last time.Time
	for dec.More() {
		var m struct {
			Time time.Time `json:"time"`
		}
		if err := dec.Decode(&m); err != nil {
			return time.Time{}, microerror.Maskf(apiError, "step output: invalid JSON answer: %v", err)
		}
		if m.Time.After(last) {
			last = m.Time
		}
	}
	return last, nil
}
