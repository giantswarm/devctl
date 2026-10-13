// Package rerun is what `devctl pr rerun` and `devctl release rerun` share:
// given the CircleCI pipeline of a pull request's head or of a tag, every
// finished workflow of it that failed is rerun from failed -- its failed jobs
// and the jobs that depend on them, the passed ones kept -- with the CircleCI
// login of `devctl auth login`. A pipeline that never got a workflow (its
// setup workflow done and the continuation never created, or pending without
// a workflow) is sent its push webhook delivery again through GitHub
// ([Redelivery]), once per head. The rerun is started, not waited for:
// `devctl pr wait` and `devctl release wait` wait for it.
package rerun

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// The commands, as their documents name them.
const (
	CommandPR      = "pr rerun"
	CommandRelease = "release rerun"
)

// The outcomes of one workflow.
const (
	// OutcomeRerun: the workflow failed and its rerun from failed started.
	OutcomeRerun = "rerun"
	// OutcomeRunning: the workflow has not finished; CircleCI reruns a
	// finished workflow only.
	OutcomeRunning = "running"
	// OutcomeNoFailedJob: the workflow was canceled (or unauthorized)
	// without a failed job to rerun.
	OutcomeNoFailedJob = "no_failed_job"
	// OutcomeNothing: the workflow did not fail.
	OutcomeNothing = "nothing_to_rerun"
	// OutcomeRefused: CircleCI refused the rerun; the reason says why.
	OutcomeRefused = "refused"
)

// Result is the commands' part of the document.
type Result struct {
	Repository string `json:"repository"`
	// Number is the pull request of `pr rerun`.
	Number int `json:"number,omitempty"`
	// Tag is the tag of `release rerun`, as CircleCI built it.
	Tag string `json:"tag,omitempty"`
	// HeadSHA is the revision the pipeline built.
	HeadSHA string `json:"headSha,omitempty"`
	// Pipeline is absent when CircleCI has no pipeline for the head or tag.
	Pipeline  *Pipeline  `json:"pipeline,omitempty"`
	Workflows []Workflow `json:"workflows"`
	// Redelivery is the push webhook delivery sent again for a pipeline
	// without a workflow; absent when the pipeline has workflows to judge.
	Redelivery *RedeliveryResult `json:"redelivery,omitempty"`
}

// Pipeline is the CircleCI pipeline whose workflows were considered.
type Pipeline struct {
	ID     string `json:"id"`
	Number int64  `json:"number"`
	URL    string `json:"url"`
}

// Workflow is the newest run of one workflow name of the pipeline and what
// the command did with it.
type Workflow struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Status string `json:"status"`
	URL    string `json:"url"`
	// FailedJobs are the failed jobs as CircleCI lists them, by name; the
	// listing can lag behind the workflow's status.
	FailedJobs []string `json:"failedJobs"`
	// Outcome is one of rerun, running, no_failed_job, nothing_to_rerun,
	// refused.
	Outcome string `json:"outcome"`
	// RerunID and RerunURL are the new workflow of a rerun.
	RerunID  string `json:"rerunId,omitempty"`
	RerunURL string `json:"rerunUrl,omitempty"`
}

// NewResult is an empty result for repository.
func NewResult(repository string) *Result {
	return &Result{Repository: repository, Workflows: []Workflow{}}
}

// FromFailed reruns the failed workflows of pipeline of org/repo from failed,
// recording each workflow in result as it goes. It returns nil when at least
// one rerun started (a warning names a workflow that is still running), exit
// 5 refused when nothing failed yet but a workflow is still running, and exit
// 3 not applicable when no workflow failed. A rerun CircleCI refuses with 403
// is exit 8, naming the login that grants write access. A pipeline that never
// got a workflow is handed to redelivery, which sends its push webhook
// delivery again once it is [StalledAfter] old.
func FromFailed(ctx context.Context, client *circleciclient.Client, org, repo string, pipeline *circleciclient.Pipeline, result *Result, warn func(string), redelivery Redelivery) error {
	result.HeadSHA = pipeline.VCS.Revision
	result.Pipeline = &Pipeline{ID: pipeline.ID, Number: pipeline.Number, URL: circleciclient.PipelineURL(org, repo, pipeline.Number)}

	runs, err := client.ListPipelineWorkflows(ctx, pipeline.ID)
	if err != nil {
		return err
	}
	newest := circleciclient.NewestWorkflows(runs)
	if stalled(pipeline, newest) {
		for _, run := range newest {
			result.Workflows = append(result.Workflows, Workflow{
				Name: run.Name, ID: run.ID, Status: run.Status, URL: circleciclient.WorkflowURL(org, repo, pipeline.Number, run.ID),
				FailedJobs: []string{}, Outcome: OutcomeNothing,
			})
		}
		return redelivery.redeliver(ctx, org, repo, pipeline, result, warn)
	}
	var rerun, running []string
	for _, run := range newest {
		w := Workflow{
			Name:       run.Name,
			ID:         run.ID,
			Status:     run.Status,
			URL:        circleciclient.WorkflowURL(org, repo, pipeline.Number, run.ID),
			FailedJobs: []string{},
		}
		switch {
		case stillRunning(run.Status):
			w.Outcome = OutcomeRunning
			running = append(running, fmt.Sprintf("%s (%s)", run.Name, run.Status))
		case !circleciclient.WorkflowFailed(run.Status):
			w.Outcome = OutcomeNothing
		default:
			if w.FailedJobs, err = failedJobs(ctx, client, run.ID); err != nil {
				return err
			}
			// A failed workflow is rerun whatever its jobs read: CircleCI's
			// authenticated job listing lags behind the workflow (a failed
			// job still reads blocked), and the rerun is CircleCI's to judge.
			// A canceled one without a failed job never ran what it lacks.
			if len(w.FailedJobs) == 0 && !failedRun(run.Status) {
				w.Outcome = OutcomeNoFailedJob
				break
			}
			id, err := client.RerunWorkflowFromFailed(ctx, run.ID)
			if err != nil {
				w.Outcome = OutcomeRefused
				result.Workflows = append(result.Workflows, w)
				return refused(err, run.Name)
			}
			w.Outcome, w.RerunID = OutcomeRerun, id
			if id != "" {
				w.RerunURL = circleciclient.WorkflowURL(org, repo, pipeline.Number, id)
			}
			rerun = append(rerun, run.Name)
		}
		result.Workflows = append(result.Workflows, w)
	}

	switch {
	case len(rerun) > 0:
		if len(running) > 0 {
			warn(fmt.Sprintf("still running, not rerun: %s; rerun again once it finished if it fails", strings.Join(running, ", ")))
		}
		return nil
	case len(running) > 0:
		return agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused,
			"no finished workflow failed and %s still running: CircleCI reruns a finished workflow only; wait for it and rerun if it fails", strings.Join(running, ", "))
	}
	return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable,
		"no workflow of pipeline %d has a failed job: nothing to rerun", pipeline.Number)
}

// stillRunning says whether a workflow is still moving: running, on hold, or
// failing (a job failed, others still run), which CircleCI also counts as
// failed but will not rerun yet. not_run never ran and never will.
func stillRunning(status string) bool {
	return circleciclient.WorkflowRunning(status) || !circleciclient.WorkflowFinished(status) && status != "not_run"
}

// failedRun says whether a workflow status means a job failed: failed or
// error, as against canceled or unauthorized.
func failedRun(status string) bool {
	return status == "failed" || status == "error"
}

// NoPipeline is exit 3 for a head or tag CircleCI has not built.
func NoPipeline(what string) error {
	return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "CircleCI has no pipeline for %s: nothing to rerun", what)
}

// failedJobs are the names of the jobs of workflowID that failed.
func failedJobs(ctx context.Context, client *circleciclient.Client, workflowID string) ([]string, error) {
	jobs, err := client.ListWorkflowJobs(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, job := range jobs {
		if circleciclient.JobFailed(job.Status) {
			names = append(names, job.Name)
		}
	}
	return names, nil
}

// refused turns CircleCI's 403 to a rerun into exit 8: a token from a login
// that granted Read access only cannot rerun, and the login that fixes it is
// one command away.
func refused(err error, workflow string) error {
	if !circleciclient.IsForbidden(err) {
		return err
	}
	return authstore.CircleCIWriteRequired(fmt.Sprintf("CircleCI refused the rerun of %s: %v", workflow, err))
}

// Document is the commands' JSON: the envelope and the result.
type Document struct {
	agentcli.Envelope
	*Result
}

// NewDocument starts the document of command for repository.
func NewDocument(command, repository string) Document {
	return Document{Envelope: agentcli.NewEnvelope(command, time.Now()), Result: NewResult(repository)}
}

// NewCircleCI is the CircleCI client on the keychain login require returns,
// its expiry warning passed to warn. Reads go through transport, which retries
// them; the rerun itself, a POST, is sent once.
func NewCircleCI(ctx context.Context, require func(context.Context) (authstore.Token, error), endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*circleciclient.Client, error) {
	token, err := require(ctx)
	if err != nil {
		return nil, err
	}
	warn(token.Warning)
	return circleciclient.New(circleciclient.Config{
		Token:      token.Value,
		BaseURL:    circleciclient.BaseURLFromAPIURL(endpoints.CircleCIAPIURL),
		HTTPClient: &http.Client{Transport: transport},
	})
}
