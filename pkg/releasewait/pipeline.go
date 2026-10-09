package releasewait

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// pipelineState is where the tag's CI stands after one reading.
type pipelineState struct {
	// failed: a workflow (newest run per name) or an Actions run failed;
	// failedJobs names the jobs.
	failed     bool
	failedJobs []string
	// green: every workflow or run finished and none failed. False while
	// the pipeline does not exist yet.
	green bool
	// jobs are the names of the pipeline's jobs across its workflows, read
	// when the artifact derivation of hand-written or custom CI asks for
	// them; nil until the pipeline and the jobs of all its workflows are
	// listed.
	jobs map[string]bool
}

// The Actions conclusions that are failures.
var failedConclusions = []string{"failure", "cancelled", "timed_out", "startup_failure"}

// pipelineState reads the tag pipeline on CircleCI, or the Actions runs of
// the tag when the repository has no CircleCI, into result. The verdict is
// the workflows': their statuses say whether the tag's CI is green, failed
// or still running. The jobs of a workflow are read only where they are
// needed, with needJobs for the artifact derivation and for a failed
// workflow to name the jobs that failed: CircleCI answers 404 on the jobs
// of a workflow it lists, a finished one included, for a while after the
// workflow exists and for as long as its job data lags, and a green
// pipeline is green without them.
func (w *Waiter) pipelineState(ctx context.Context, result *Result, needJobs bool) (*pipelineState, error) {
	if w.circleci == nil {
		return w.actionsState(ctx, result)
	}
	owner, repo := w.config.Owner, w.config.Repo
	pipeline, err := w.circleci.FindPipelineByTag(ctx, owner, repo, result.Tag)
	if err != nil {
		return nil, fmt.Errorf("listing the pipelines of %s/%s: %w", owner, repo, err)
	}
	if pipeline == nil {
		w.progress.Printf("no CircleCI pipeline for %s yet", result.Tag)
		return &pipelineState{}, nil
	}
	runs, err := w.circleci.ListPipelineWorkflows(ctx, pipeline.ID)
	if err != nil {
		return nil, fmt.Errorf("reading the workflows of pipeline %d: %w", pipeline.Number, err)
	}
	newest := circleciclient.NewestWorkflows(runs)

	doc := &Pipeline{ID: pipeline.ID, Number: pipeline.Number, URL: circleciclient.PipelineURL(owner, repo, pipeline.Number), Workflows: []PipelineWorkflow{}, FailedJobs: []string{}, Unfinished: []string{}}
	state := &pipelineState{green: len(newest) > 0, jobs: map[string]bool{}}
	successes := 0
	jobsHidden := false
	for _, run := range newest {
		doc.Workflows = append(doc.Workflows, PipelineWorkflow{Name: run.Name, Status: run.Status})
		failed := circleciclient.WorkflowFailed(run.Status)
		unfinished := ""
		switch {
		case failed:
			state.failed = true
			state.green = false
		case circleciclient.WorkflowSucceeded(run.Status):
			successes++
		case run.Status == "not_run":
		default:
			state.green = false
			unfinished = fmt.Sprintf("%s (%s)", run.Name, run.Status)
		}
		w.progress.Printf("pipeline %d: workflow %s %s", pipeline.Number, run.Name, run.Status)
		if needJobs || failed {
			jobs, err := w.circleci.ListWorkflowJobs(ctx, run.ID)
			switch {
			case err == nil:
				for _, job := range jobs {
					state.jobs[job.Name] = true
					if failed && circleciclient.JobFailed(job.Status) {
						doc.FailedJobs = append(doc.FailedJobs, run.Name+"/"+job.Name)
					}
				}
				if failed && !slices.ContainsFunc(jobs, func(j circleciclient.Job) bool { return circleciclient.JobFailed(j.Status) }) {
					doc.FailedJobs = append(doc.FailedJobs, run.Name)
				}
			case circleciclient.IsNotFound(err) && failed:
				// The failed workflow is named without its jobs.
				doc.FailedJobs = append(doc.FailedJobs, run.Name)
			case circleciclient.IsNotFound(err):
				// The derivation that needs the jobs waits for the next poll.
				jobsHidden = true
				unfinished = fmt.Sprintf("%s (%s, jobs not visible yet)", run.Name, run.Status)
				w.progress.Printf("pipeline %d: workflow %s %s, jobs not visible yet", pipeline.Number, run.Name, run.Status)
			default:
				return nil, fmt.Errorf("reading the jobs of workflow %s: %w", run.Name, err)
			}
		}
		if unfinished != "" {
			doc.Unfinished = append(doc.Unfinished, unfinished)
		}
	}
	if successes == 0 {
		state.green = false
	}
	if !needJobs || jobsHidden {
		// The pipeline's jobs are not all known: a derivation that needs
		// them reads them on a later poll.
		state.jobs = nil
	}
	sort.Strings(doc.FailedJobs)
	state.failedJobs = doc.FailedJobs
	result.Pipeline = doc
	return state, nil
}

// actionsState reads the Actions runs the tag triggered: the runs on the
// tag's commit whose branch is the tag. Runs of the merge commit's push to
// main are the merge's, not the tag's, and are left out.
func (w *Waiter) actionsState(ctx context.Context, result *Result) (*pipelineState, error) {
	runs, err := w.config.GitHub.ListWorkflowRunsForSHA(ctx, w.config.Owner, w.config.Repo, result.SHA)
	if err != nil {
		return nil, fmt.Errorf("listing the Actions runs of %s: %w", short(result.SHA), err)
	}
	state := &pipelineState{green: true, jobs: map[string]bool{}}
	result.Actions = []ActionsRun{}
	for _, run := range runs {
		if run.HeadBranch != result.Tag {
			continue
		}
		result.Actions = append(result.Actions, ActionsRun{Name: run.Name, RunID: run.ID, Status: run.Status, Conclusion: run.Conclusion, URL: run.URL})
		switch {
		case run.Status != "completed":
			state.green = false
		case slices.Contains(failedConclusions, run.Conclusion):
			state.failed = true
			state.green = false
			state.failedJobs = append(state.failedJobs, run.Name)
		}
		w.progress.Printf("actions run %s: %s %s", run.Name, run.Status, run.Conclusion)
	}
	return state, nil
}
