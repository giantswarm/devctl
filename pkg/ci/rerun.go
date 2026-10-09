package ci

import (
	"context"
	"fmt"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// RerunWindow is how long after a rerun of a workflow name in a pipeline the
// next rerun of that name is refused: one rerun per hour, so an agent that
// reruns on every poll cannot loop. The rule reads CircleCI, not a file: a
// rerun is a second run of the name in the pipeline, whoever started it.
const RerunWindow = time.Hour

// CancelTimeout bounds the wait for a canceled workflow to read canceled,
// which CircleCI does on its own a little after the cancel; cancelPoll is
// how often it is read meanwhile.
const (
	CancelTimeout = 2 * time.Minute
	cancelPoll    = 3 * time.Second
)

// The outcomes of `ci rerun`.
const (
	// OutcomeRerun: the rerun started.
	OutcomeRerun = "rerun"
	// OutcomeRefused: the rerun was not made; the reason says why.
	OutcomeRefused = "refused"
)

// RerunOptions are the flags of `ci rerun`.
type RerunOptions struct {
	// FromFailed reruns the failed jobs and the jobs that depend on them,
	// the passed ones kept; off, every job runs again.
	FromFailed bool
	// Cancel cancels a workflow still running before the rerun, the
	// recovery of a stuck one; off, a running workflow is refused.
	Cancel bool
}

// RerunResult is the document part of `ci rerun`.
type RerunResult struct {
	Repository string `json:"repository"`
	// Pipeline and Workflow are absent when CircleCI has no such workflow.
	Pipeline   *Pipeline `json:"pipeline,omitempty"`
	Workflow   *Run      `json:"workflow,omitempty"`
	FromFailed bool      `json:"fromFailed"`
	// Canceled says whether the workflow was canceled before the rerun.
	Canceled bool `json:"canceled"`
	// Outcome is rerun or refused; empty when the run ended before deciding.
	Outcome string `json:"outcome,omitempty"`
	// RerunID and RerunURL are the new workflow of a rerun.
	RerunID  string `json:"rerunId,omitempty"`
	RerunURL string `json:"rerunUrl,omitempty"`
	// LastRerun is the rerun of the workflow's name made within RerunWindow,
	// the one that refuses this call; absent otherwise.
	LastRerun *Run `json:"lastRerun,omitempty"`
}

// NewRerunResult is an empty result for repository.
func NewRerunResult(repository string) *RerunResult {
	return &RerunResult{Repository: repository}
}

// Rerun reruns workflowID of org/repo, recording what it did in result. The
// workflow has to belong to the repository (exit 3 otherwise, as for one
// CircleCI does not know). A rerun of the workflow's name made within
// RerunWindow is exit 5 naming it and when the hour ends. A workflow still
// running is exit 5 unless opts.Cancel cancels it first; a cancel that does
// not settle within CancelTimeout is exit 2. A rerun CircleCI refuses is exit
// 5 with its message, one it refuses with 403 exit 8 naming the login that
// grants Write access. The rerun is started, not waited for.
func Rerun(ctx context.Context, client *circleciclient.Client, org, repo, workflowID string, opts RerunOptions, clock agentcli.Clock, result *RerunResult, warn func(string)) error {
	result.FromFailed = opts.FromFailed
	detail, err := client.GetWorkflow(ctx, workflowID)
	switch {
	case circleciclient.IsNotFound(err):
		return notApplicable("CircleCI has no workflow %s the login sees", workflowID)
	case err != nil:
		return err
	}
	if !sameProject(detail.ProjectSlug, org, repo) {
		return notApplicable("workflow %s belongs to %s, not %s/%s", workflowID, detail.ProjectSlug, org, repo)
	}
	number := detail.PipelineNumber
	result.Pipeline = &Pipeline{ID: detail.PipelineID, Number: number, URL: circleciclient.PipelineURL(org, repo, number)}
	workflow := newRun(org, repo, number, detail.Workflow)
	result.Workflow = &workflow

	runs, err := client.ListPipelineWorkflows(ctx, detail.PipelineID)
	if err != nil {
		return err
	}
	newest, count := runsOfName(runs, detail.Name)
	now := clock.Now()
	if count > 1 && now.Sub(newest.CreatedAt) < RerunWindow {
		last := newRun(org, repo, number, *newest)
		result.LastRerun, result.Outcome = &last, OutcomeRefused
		return refused("workflow %s of pipeline %d was rerun %s ago as %s (%s): one rerun per hour; devctl ci jobs %s/%s %d shows where it stands, the next rerun is possible after %s",
			detail.Name, number, now.Sub(newest.CreatedAt).Round(time.Minute), newest.ID, newest.Status,
			org, repo, number, newest.CreatedAt.Add(RerunWindow).UTC().Format(time.RFC3339))
	}

	if circleciclient.WorkflowRunning(detail.Status) {
		if !opts.Cancel {
			result.Outcome = OutcomeRefused
			return refused("workflow %s (%s) is still running: devctl ci jobs %s/%s %d tells a slow job from a stuck one; --cancel cancels it before the rerun",
				detail.Name, detail.Status, org, repo, number)
		}
		if err := client.CancelWorkflow(ctx, workflowID); err != nil {
			result.Outcome = OutcomeRefused
			return writeRefused(err, "the cancel of "+detail.Name)
		}
		result.Canceled = true
		status, err := awaitCanceled(ctx, client, workflowID, clock)
		if err != nil {
			return err
		}
		result.Workflow.Status = status
	}

	id, err := client.RerunWorkflow(ctx, workflowID, opts.FromFailed)
	if err != nil {
		result.Outcome = OutcomeRefused
		return writeRefused(err, "the rerun of "+detail.Name)
	}
	result.Outcome, result.RerunID = OutcomeRerun, id
	if id != "" {
		result.RerunURL = circleciclient.WorkflowURL(org, repo, number, id)
	}
	if newest != nil && newest.ID != workflowID {
		warn(fmt.Sprintf("%s is not the newest run of %s, %s (%s) is; the rerun is of %s as asked", workflowID, detail.Name, newest.ID, newest.Status, workflowID))
	}
	return nil
}

// runsOfName is the newest run of name in runs and how many runs of the name
// there are: more than one means the name was rerun.
func runsOfName(runs []circleciclient.Workflow, name string) (newest *circleciclient.Workflow, count int) {
	for i := range runs {
		if runs[i].Name != name {
			continue
		}
		count++
		if newest == nil || runs[i].CreatedAt.After(newest.CreatedAt) {
			newest = &runs[i]
		}
	}
	return newest, count
}

// awaitCanceled reads the workflow until it no longer runs and returns its
// status; exit 2 when it still runs after CancelTimeout.
func awaitCanceled(ctx context.Context, client *circleciclient.Client, workflowID string, clock agentcli.Clock) (string, error) {
	ctx, cancel := clock.Timeout(ctx, CancelTimeout)
	defer cancel()
	status := ""
	for {
		w, err := client.GetWorkflow(ctx, workflowID)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			return "", err
		}
		status = w.Status
		if !circleciclient.WorkflowRunning(status) {
			return status, nil
		}
		if err := clock.Sleep(ctx, cancelPoll); err != nil {
			break
		}
	}
	return "", agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout,
		"workflow %s is canceled and still reads %s after %s: rerun it once it reads canceled", workflowID, status, CancelTimeout)
}

// writeRefused is the outcome of a write CircleCI refused: 403 is exit 8
// naming the login that grants Write access; any other refusal (a workflow
// CircleCI will not rerun or cancel) exit 5 with its message.
func writeRefused(err error, what string) error {
	switch {
	case circleciclient.IsForbidden(err):
		return authstore.CircleCIWriteRequired(fmt.Sprintf("CircleCI refused %s: %v", what, err))
	case circleciclient.IsAPI(err), circleciclient.IsNotFound(err):
		return refused("CircleCI refused %s: %v", what, err)
	}
	return err
}
