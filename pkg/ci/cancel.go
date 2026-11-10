package ci

import (
	"context"
	"fmt"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

const statusQueued = "queued"

// CancelClient supplies the CircleCI reads and writes needed for cancellation.
type CancelClient interface {
	GetWorkflow(context.Context, string) (*circleciclient.WorkflowDetail, error)
	GetPipeline(context.Context, string) (*circleciclient.PipelineDetail, error)
	ListPipelineWorkflows(context.Context, string) ([]circleciclient.Workflow, error)
	ListPipelines(context.Context, string, string, string) (*circleciclient.PipelinePage, error)
	ListWorkflowJobs(context.Context, string) ([]circleciclient.Job, error)
	CancelWorkflow(context.Context, string) error
}

// CancelResult records accepted requests, rather than claiming they have settled.
type CancelResult struct {
	Repository            string           `json:"repository"`
	DryRun                bool             `json:"dryRun"`
	Workflows             []CancelWorkflow `json:"workflows"`
	UncancelablePipelines []string         `json:"uncancelablePipelines"`
}

// CancelWorkflow records one considered workflow and its outcome.
type CancelWorkflow struct {
	ID         string `json:"id"`
	PipelineID string `json:"pipelineId"`
	Status     string `json:"status"`
	Outcome    string `json:"outcome"`
}

// CancelOne cancels a workflow, or all active workflows of a pipeline.
func CancelOne(ctx context.Context, client CancelClient, owner, repo, id string, pipeline bool, result *CancelResult) error {
	if pipeline {
		p, err := client.GetPipeline(ctx, id)
		if err != nil {
			return err
		}
		if !sameProject(p.ProjectSlug, owner, repo) || p.ProjectSlug == "" {
			return notApplicable("pipeline does not belong to %s/%s", owner, repo)
		}
		return cancelPipeline(ctx, client, p.ID, false, result)
	}
	w, err := client.GetWorkflow(ctx, id)
	if err != nil {
		return err
	}
	if !sameProject(w.ProjectSlug, owner, repo) || w.ProjectSlug == "" {
		return notApplicable("workflow does not belong to %s/%s", owner, repo)
	}
	return cancelWorkflow(ctx, client, w.PipelineID, w.Workflow, false, result)
}

// CancelStale visits every pipeline page. stale must obtain current GitHub
// evidence; missing or ambiguous evidence must return false or an error.
func CancelStale(ctx context.Context, client CancelClient, owner, repo string, stale func(context.Context, circleciclient.Pipeline) (bool, error), result *CancelResult) error {
	token := ""
	seen := map[string]bool{}
	for {
		page, err := client.ListPipelines(ctx, owner, repo, token)
		if err != nil {
			return err
		}
		for _, p := range page.Items {
			if p.VCS.Tag != "" || p.VCS.Branch == "" || p.VCS.Revision == "" {
				continue
			}
			yes, err := stale(ctx, p)
			if err != nil {
				return err
			}
			if yes {
				if err := cancelPipeline(ctx, client, p.ID, true, result); err != nil {
					return err
				}
			}
		}
		if page.NextPageToken == "" {
			return nil
		}
		if seen[page.NextPageToken] {
			return fmt.Errorf("CircleCI repeated a pipeline page token")
		}
		token = page.NextPageToken
		seen[token] = true
	}
}

func cancelPipeline(ctx context.Context, client CancelClient, id string, queuedOnly bool, result *CancelResult) error {
	workflows, err := client.ListPipelineWorkflows(ctx, id)
	if err != nil {
		return err
	}
	if len(workflows) == 0 {
		result.UncancelablePipelines = append(result.UncancelablePipelines, id)
	}
	for _, w := range workflows {
		if err := cancelWorkflow(ctx, client, id, w, queuedOnly, result); err != nil {
			return err
		}
	}
	return nil
}

func cancelWorkflow(ctx context.Context, client CancelClient, pipeline string, w circleciclient.Workflow, queuedOnly bool, result *CancelResult) error {
	item := CancelWorkflow{ID: w.ID, PipelineID: pipeline, Status: w.Status, Outcome: "skipped"}
	result.Workflows = append(result.Workflows, item)
	index := len(result.Workflows) - 1
	if !circleciclient.WorkflowRunning(w.Status) && w.Status != statusQueued {
		return nil
	}
	if queuedOnly {
		jobs, err := client.ListWorkflowJobs(ctx, w.ID)
		if err != nil {
			return err
		}
		if !queuedJobs(jobs) {
			return nil
		}
	}
	if result.DryRun {
		result.Workflows[index].Outcome = "would_cancel"
		return nil
	}
	// Re-read immediately before the write, especially when a queued job may
	// have acquired an executor during the repository scan.
	current, err := client.GetWorkflow(ctx, w.ID)
	if err != nil {
		return err
	}
	if !circleciclient.WorkflowRunning(current.Status) && current.Status != statusQueued {
		return nil
	}
	if queuedOnly {
		jobs, err := client.ListWorkflowJobs(ctx, w.ID)
		if err != nil {
			return err
		}
		if !queuedJobs(jobs) {
			return nil
		}
	}
	if err := client.CancelWorkflow(ctx, w.ID); err != nil {
		return writeRefused(err, "workflow cancellation")
	}
	result.Workflows[index].Outcome = "cancel_requested"
	return nil
}

func queuedJobs(jobs []circleciclient.Job) bool {
	if len(jobs) == 0 {
		return false
	}
	for _, j := range jobs {
		if !j.StartedAt.IsZero() || j.Type == "approval" {
			return false
		}
		switch j.Status {
		case statusQueued, "not_run", "blocked", "on_hold":
		default:
			return false
		}
	}
	return true
}
