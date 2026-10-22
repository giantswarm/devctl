package releasewait

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
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
	// failedWorkflows, failedRuns and failedStatuses are what failed, for
	// Config.FailedLogLines: the CircleCI workflows, the Actions runs, the
	// CircleCI commit statuses.
	failedWorkflows []failedWorkflow
	failedRuns      []ActionsRun
	failedChecks    []failedCheck
	failedStatuses  []githubclient.CommitStatus
}

// failedCheck is a failed check run of the tag's commit; actions says it is a
// GitHub Actions job, whose log can be read by the check run's id.
type failedCheck struct {
	id        int64
	name, url string
	actions   bool
}

// failedWorkflow is a failed workflow of the tag pipeline.
type failedWorkflow struct {
	id, name, url string
}

// statusCompleted is the status of a finished Actions run or check run.
const statusCompleted = "completed"

// The Actions conclusions that are failures.
var failedConclusions = []string{"failure", "cancelled", "timed_out", "startup_failure"}

// pipelineState reads the tag pipeline on CircleCI, or the Actions runs of
// the tag when the repository has no CircleCI, into result. The verdict is
// the workflows': their statuses say whether the tag's CI is green, failed
// or still running. The jobs of a workflow are read only where they are
// needed, with needJobs for the artifact derivation and for a failed
// workflow to name the jobs that failed, and never for the setup workflow:
// CircleCI answers 404 on the jobs of a workflow it lists, a finished one
// included, for a while after the workflow exists and for as long as its
// job data lags, and a green pipeline is green without them.
func (w *Waiter) pipelineState(ctx context.Context, result *Result, needJobs bool) (*pipelineState, error) {
	if w.statusesOnly {
		return w.circleStatusesState(ctx, result)
	}
	if w.circleci == nil {
		return w.actionsState(ctx, result)
	}
	owner, repo := w.config.Owner, w.config.Repo
	pipeline, err := w.circleci.FindPipelineByTag(ctx, owner, repo, result.Tag)
	if err != nil {
		return nil, fmt.Errorf("listing the pipelines of %s/%s: %w", owner, repo, err)
	}
	if pipeline == nil {
		var statuses []githubclient.CommitStatus
		pipeline, statuses, err = w.pipelineFromStatuses(ctx, result)
		if err != nil {
			return nil, err
		}
		if pipeline == nil && len(statuses) > 0 {
			return w.statusesState(result, statuses), nil
		}
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
		w.progress.Printf("pipeline %d: workflow %s %s", pipeline.Number, run.Name, run.Status)
		// The setup workflow's one job continues the pipeline and names no
		// artifact: it is never read, and a failed setup is named as such.
		readJobs := (needJobs || failed) && run.Tag != circleciclient.WorkflowTagSetup
		var failedJobs []string
		if failed && !readJobs {
			failedJobs = append(failedJobs, run.Name)
		}
		if readJobs {
			jobs, err := w.circleci.ListWorkflowJobs(ctx, run.ID)
			switch {
			case err == nil:
				for _, job := range jobs {
					state.jobs[job.Name] = true
				}
				if failed {
					var verdict string
					failedJobs, verdict = w.failedWorkflowJobs(run, jobs)
					switch verdict {
					case workflowAdvisoryOnly:
						failed = false
					case workflowAdvisoryRunning:
						failed = false
						unfinished = fmt.Sprintf("%s (%s)", run.Name, run.Status)
					}
				}
			case circleciclient.IsNotFound(err) && failed:
				// The failed workflow is named without its jobs.
				failedJobs = append(failedJobs, run.Name)
			case circleciclient.IsNotFound(err):
				// The derivation that needs the jobs waits for the next poll.
				jobsHidden = true
				unfinished = fmt.Sprintf("%s (%s, jobs not visible yet)", run.Name, run.Status)
				w.progress.Printf("pipeline %d: workflow %s %s, jobs not visible yet", pipeline.Number, run.Name, run.Status)
			default:
				return nil, fmt.Errorf("reading the jobs of workflow %s: %w", run.Name, err)
			}
		}
		switch {
		case failed:
			state.failed = true
			state.green = false
			state.failedWorkflows = append(state.failedWorkflows, failedWorkflow{id: run.ID, name: run.Name, url: circleciclient.WorkflowURL(owner, repo, pipeline.Number, run.ID)})
			doc.FailedJobs = append(doc.FailedJobs, failedJobs...)
		case unfinished != "":
			state.green = false
		case circleciclient.WorkflowSucceeded(run.Status), circleciclient.WorkflowFailed(run.Status):
			// A workflow failed on advisory jobs alone counts as succeeded.
			successes++
		case run.Status == "not_run":
		default:
			state.green = false
			unfinished = fmt.Sprintf("%s (%s)", run.Name, run.Status)
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

// The verdicts of a failed workflow's jobs.
const (
	// workflowFailed: a job the release requires failed, or did not run.
	workflowFailed = "failed"
	// workflowAdvisoryOnly: only advisory jobs failed and every other job
	// succeeded; the workflow counts as succeeded.
	workflowAdvisoryOnly = "advisory-only"
	// workflowAdvisoryRunning: only advisory jobs failed so far and other
	// jobs still run.
	workflowAdvisoryRunning = "advisory-running"
)

// failedWorkflowJobs judges a failed or failing workflow by its jobs: the
// failed jobs it names, and whether only advisory jobs failed ([circleciclient.AdvisoryJob]),
// whose failures are warned about instead.
func (w *Waiter) failedWorkflowJobs(run circleciclient.Workflow, jobs []circleciclient.Job) ([]string, string) {
	var failed, advisory []string
	others := statusCompleted
	for _, job := range jobs {
		switch {
		case circleciclient.JobFailed(job.Status) && circleciclient.AdvisoryJob(job.Name):
			advisory = append(advisory, run.Name+"/"+job.Name)
		case circleciclient.JobFailed(job.Status):
			failed = append(failed, run.Name+"/"+job.Name)
		case circleciclient.AdvisoryJob(job.Name), job.Status == "success":
		case run.Status != "failing":
			// A job the workflow's end left blocked or not run; a failing
			// workflow still runs its other jobs.
			others = workflowFailed
		default:
			others = "running"
		}
	}
	switch {
	case len(failed) > 0 || others == workflowFailed:
		return append(failed, advisory...), workflowFailed
	case len(advisory) == 0:
		return []string{run.Name}, workflowFailed
	}
	for _, name := range advisory {
		w.warnOnce(fmt.Sprintf("%s failed; the release does not require it, so the tag's CI is judged without it", name))
	}
	if others == "running" {
		return nil, workflowAdvisoryRunning
	}
	return nil, workflowAdvisoryOnly
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
	if err := w.checkRunsState(ctx, result, state); err != nil {
		return nil, err
	}
	result.Actions = []ActionsRun{}
	for _, run := range runs {
		if run.HeadBranch != result.Tag {
			continue
		}
		result.Actions = append(result.Actions, ActionsRun{Name: run.Name, RunID: run.ID, Status: run.Status, Conclusion: run.Conclusion, URL: run.URL})
		switch {
		case run.Status != statusCompleted:
			state.green = false
		case slices.Contains(failedConclusions, run.Conclusion):
			state.failed = true
			state.green = false
			state.failedJobs = append(state.failedJobs, run.Name)
			state.failedRuns = append(state.failedRuns, result.Actions[len(result.Actions)-1])
		}
		w.progress.Printf("actions run %s: %s %s", run.Name, run.Status, run.Conclusion)
	}
	return state, nil
}

// checkRunsState reads the check runs of the tag's commit, the CI signal of a
// repository that runs on GitHub Actions only: its jobs report no commit
// status, and the runs of a tag pushed by a workflow belong to the branch.
// With any, the model is actions and the check runs are the pipeline: the tag
// is not green until every one finished successfully, and a failed one fails
// it. A commit without check runs leaves the model as it was.
func (w *Waiter) checkRunsState(ctx context.Context, result *Result, state *pipelineState) error {
	owner, repo := w.config.Owner, w.config.Repo
	runs, err := w.config.GitHub.CheckRunsForRef(ctx, owner, repo, result.SHA)
	if githubclient.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the check runs of %s: %w", short(result.SHA), err)
	}
	if len(runs) == 0 {
		return nil
	}
	if result.CIModel == CIModelNone {
		result.CIModel = CIModelActions
	}
	doc := &Pipeline{URL: fmt.Sprintf("https://github.com/%s/%s/commit/%s/checks", owner, repo, result.SHA), Workflows: []PipelineWorkflow{}, FailedJobs: []string{}, Unfinished: []string{}}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].GetName() != runs[j].GetName() {
			return runs[i].GetName() < runs[j].GetName()
		}
		return runs[i].GetID() < runs[j].GetID()
	})
	for _, run := range runs {
		name, status, conclusion := run.GetName(), run.GetStatus(), run.GetConclusion()
		doc.Workflows = append(doc.Workflows, PipelineWorkflow{Name: name, Status: status, Conclusion: conclusion})
		switch {
		case status != statusCompleted:
			state.green = false
			doc.Unfinished = append(doc.Unfinished, fmt.Sprintf("%s (%s)", name, status))
		case slices.Contains(failedConclusions, conclusion):
			state.failed, state.green = true, false
			doc.FailedJobs = append(doc.FailedJobs, name)
			state.failedChecks = append(state.failedChecks, failedCheck{id: run.GetID(), name: name, url: run.GetHTMLURL(), actions: run.GetApp().GetSlug() == "github-actions"})
		case conclusion == "action_required":
			state.green = false
			doc.Unfinished = append(doc.Unfinished, fmt.Sprintf("%s (action_required)", name))
		}
		w.progress.Printf("check run %s: %s %s", name, status, conclusion)
	}
	sort.Strings(doc.FailedJobs)
	state.failedJobs = append(state.failedJobs, doc.FailedJobs...)
	result.Pipeline = doc
	return nil
}
