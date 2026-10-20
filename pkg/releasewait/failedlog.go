package releasewait

import (
	"context"
	"fmt"
	"slices"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

// failedJobs reads the log of every failed job of a failed tag pipeline: the
// jobs of its failed CircleCI workflows, the failed jobs of the tag's failed
// Actions runs, or the jobs the failed CircleCI statuses link. A log that
// cannot be read is named in its job; the verdict is the tag's CI failure
// either way.
func (w *Waiter) failedJobs(ctx context.Context, state *pipelineState) []prwait.FailedJob {
	owner, repo := w.config.Owner, w.config.Repo
	logs := prwait.LogReader{GitHub: w.config.GitHub, Lines: w.config.FailedLogLines}
	jobs := []prwait.FailedJob{}
	for _, workflow := range state.failedWorkflows {
		jobs = append(jobs, logs.CircleCIWorkflow(ctx, owner, repo, w.circleci, workflow.id, workflow.name, workflow.url)...)
	}
	for _, run := range state.failedRuns {
		jobs = append(jobs, w.actionsFailedJobs(ctx, logs, run)...)
	}
	for _, check := range state.failedChecks {
		if check.actions {
			jobs = append(jobs, logs.ActionsJob(ctx, owner, repo, check.id, check.name, check.url))
			continue
		}
		jobs = append(jobs, prwait.FailedJob{Name: check.name, Source: prwait.SourceActions, URL: check.url, LogError: "the check run is not a GitHub Actions job"})
	}
	for _, status := range state.failedStatuses {
		name := circleciclient.StatusJobName(status.Context)
		target, ok := circleciclient.ParseStatusTarget(status.TargetURL)
		if !ok || target.Job == 0 {
			jobs = append(jobs, prwait.FailedJob{Name: name, Source: prwait.SourceCircleCI, URL: status.TargetURL, LogError: "the status links no CircleCI job"})
			continue
		}
		jobs = append(jobs, logs.CircleCIJob(ctx, owner, repo, w.circleci, target.Job, name, status.TargetURL))
	}
	for _, job := range jobs {
		w.progress.Printf("failed job %s: %s", job.Name, job.URL)
	}
	return jobs
}

// actionsFailedJobs reads the failed jobs of a failed Actions run, named
// run/job; the run itself when its jobs cannot be listed.
func (w *Waiter) actionsFailedJobs(ctx context.Context, logs prwait.LogReader, run ActionsRun) []prwait.FailedJob {
	jobs, err := w.config.GitHub.ListWorkflowRunJobs(ctx, w.config.Owner, w.config.Repo, run.RunID)
	if err != nil {
		return []prwait.FailedJob{{Name: run.Name, Source: prwait.SourceActions, URL: run.URL, LogError: fmt.Sprintf("listing the jobs of run %d: %s", run.RunID, err)}}
	}
	var failed []prwait.FailedJob
	for _, job := range jobs {
		if slices.Contains(failedConclusions, job.Conclusion) {
			failed = append(failed, logs.ActionsJob(ctx, w.config.Owner, w.config.Repo, job.ID, run.Name+"/"+job.Name, job.URL))
		}
	}
	return failed
}
