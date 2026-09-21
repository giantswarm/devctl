package prwait

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// The check sources of the document.
const (
	SourceCheckRun = "check_run"
	SourceStatus   = "status"
)

// The check states the document shares between check runs and statuses.
const (
	statusCompleted = "completed"
	statusPending   = "pending"
)

// Check is one check of the head as the merge box lists it: a check run (the
// latest run of that name) or a commit status (the latest of that context).
type Check struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	// Status is queued, in_progress or completed for a check run; pending or
	// completed for a status.
	Status string `json:"status"`
	// Conclusion is the check run's conclusion or the status's state once it
	// is not pending; empty while unfinished.
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
	// Required: branch protection or a ruleset of the base names this context.
	Required bool `json:"required"`
}

// CircleCI is the newest pipeline of the head revision and its workflows,
// the newest run per workflow name.
type CircleCI struct {
	PipelineID     string     `json:"pipelineId"`
	PipelineNumber int64      `json:"pipelineNumber"`
	Workflows      []Workflow `json:"workflows"`
}

// Workflow is one CircleCI workflow of the pipeline.
type Workflow struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	URL    string `json:"url"`
}

// ActionRun is the latest GitHub Actions run of one workflow for the head.
type ActionRun struct {
	Name       string `json:"name"`
	RunID      int64  `json:"runId"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

// snapshot is what one poll read; evaluate turns it into a verdict.
type snapshot struct {
	headSHA   string
	checkRuns []*github.CheckRun
	statuses  []*github.RepoStatus
	runs      []*github.WorkflowRun
	// circleci is nil when CircleCI is not consulted for this head.
	circleci *circleSnapshot
	required []string
}

// circleSnapshot is the CircleCI side of a poll.
type circleSnapshot struct {
	// project is the slug CircleCI's UI uses, "github/<owner>/<repo>".
	project string
	// pipeline is nil while CircleCI has no pipeline for the head revision.
	pipeline  *circleciclient.Pipeline
	workflows []circleciclient.Workflow
}

// evaluation is the verdict of one snapshot with the document fields it
// fills.
type evaluation struct {
	checks   []Check
	actions  []ActionRun
	circleci *CircleCI
	// red names every check, status, run or workflow that failed.
	red []string
	// unfinished names everything the head still waits for, in the words the
	// document uses at a timeout.
	unfinished []string
	// requiredMissing are the required contexts nothing has reported under.
	requiredMissing []string
	// settled: every check, status, Actions run and CircleCI workflow of the
	// head has finished, so nothing left is going to report under a context
	// that is still absent.
	settled bool
}

func (e *evaluation) green() bool { return len(e.red) == 0 && len(e.unfinished) == 0 }

// neverReported: the head is settled and a required context is still absent.
// That verdict needs no timeout: nothing is running that could report it.
func (e *evaluation) neverReported() bool { return e.settled && len(e.requiredMissing) > 0 }

func (e *evaluation) redReason() string { return strings.Join(e.red, "; ") }

// evaluate applies the green rules to a snapshot:
//
//  1. every check run and commit status of the head, the latest per name,
//     is completed and not failed; a completed run that needs a human
//     (action_required) still waits;
//  2. when CircleCI is consulted, the newest pipeline of the head revision
//     exists, and the newest run of each of its workflows is success (not_run
//     counts as skipped);
//  3. no GitHub Actions run of the head is queued, in progress, waiting or
//     awaiting approval;
//  4. every required status context has reported.
//
// A failure anywhere is red at once; anything else still open keeps the wait
// going. A required context nothing has reported under is unfinished like the
// rest while a check, status, run or workflow is still pending (rules 1 to 3):
// the one awaiting approval or still running may be what reports it. Once all
// of them have finished, the head is settled and an absent context is one
// that will never report.
func evaluate(s snapshot) *evaluation {
	e := &evaluation{checks: []Check{}, actions: []ActionRun{}}
	required := map[string]bool{}
	for _, name := range s.required {
		required[name] = true
	}
	reported := map[string]bool{}

	for _, run := range latestCheckRuns(s.checkRuns) {
		name := run.GetName()
		reported[name] = true
		check := Check{
			Name:       name,
			Source:     SourceCheckRun,
			Status:     run.GetStatus(),
			Conclusion: run.GetConclusion(),
			URL:        run.GetHTMLURL(),
			Required:   required[name],
		}
		e.checks = append(e.checks, check)
		switch {
		case check.Status != statusCompleted:
			e.unfinished = append(e.unfinished, fmt.Sprintf("check %s (%s)", name, check.Status))
		case check.Conclusion == "action_required":
			e.unfinished = append(e.unfinished, fmt.Sprintf("check %s (action_required)", name))
		case checkRunPassed(check.Conclusion):
		default:
			e.red = append(e.red, fmt.Sprintf("check %s concluded %s", name, check.Conclusion))
		}
	}

	for _, status := range latestStatuses(s.statuses) {
		name := status.GetContext()
		reported[name] = true
		check := Check{
			Name:     name,
			Source:   SourceStatus,
			Status:   statusCompleted,
			URL:      status.GetTargetURL(),
			Required: required[name],
		}
		switch state := status.GetState(); state {
		case statusPending:
			check.Status = statusPending
			e.unfinished = append(e.unfinished, fmt.Sprintf("status %s (pending)", name))
		case "success":
			check.Conclusion = state
		default:
			check.Conclusion = state
			e.red = append(e.red, fmt.Sprintf("status %s is %s", name, state))
		}
		e.checks = append(e.checks, check)
	}

	for _, run := range latestWorkflowRuns(s.runs) {
		action := ActionRun{
			Name:       run.GetName(),
			RunID:      run.GetID(),
			Status:     run.GetStatus(),
			Conclusion: run.GetConclusion(),
			URL:        run.GetHTMLURL(),
		}
		e.actions = append(e.actions, action)
		switch {
		case action.Conclusion == "action_required":
			e.unfinished = append(e.unfinished, fmt.Sprintf("actions run %s (awaiting approval)", action.Name))
		case workflowRunOpen(action.Status):
			e.unfinished = append(e.unfinished, fmt.Sprintf("actions run %s (%s)", action.Name, action.Status))
		}
	}

	if s.circleci != nil {
		e.evaluateCircleCI(s.headSHA, s.circleci)
	}

	// Settled is judged before the required contexts: with nothing pending,
	// an absent context has nothing left that could report it.
	e.settled = len(e.unfinished) == 0
	for _, name := range s.required {
		if !reported[name] {
			e.requiredMissing = append(e.requiredMissing, name)
			e.unfinished = append(e.unfinished, fmt.Sprintf("required context %s (absent)", name))
		}
	}

	sort.Slice(e.checks, func(i, j int) bool {
		if e.checks[i].Name != e.checks[j].Name {
			return e.checks[i].Name < e.checks[j].Name
		}
		return e.checks[i].Source < e.checks[j].Source
	})
	sort.Slice(e.actions, func(i, j int) bool { return e.actions[i].Name < e.actions[j].Name })
	return e
}

func (e *evaluation) evaluateCircleCI(headSHA string, c *circleSnapshot) {
	if c.pipeline == nil {
		e.unfinished = append(e.unfinished, fmt.Sprintf("circleci pipeline for %s (absent)", headSHA))
		return
	}
	e.circleci = &CircleCI{
		PipelineID:     c.pipeline.ID,
		PipelineNumber: c.pipeline.Number,
		Workflows:      []Workflow{},
	}
	if c.pipeline.State == "errored" {
		e.red = append(e.red, fmt.Sprintf("circleci pipeline %d errored", c.pipeline.Number))
		return
	}
	workflows := latestWorkflows(c.workflows)
	if len(workflows) == 0 {
		e.unfinished = append(e.unfinished, fmt.Sprintf("circleci pipeline %d (no workflows yet)", c.pipeline.Number))
	}
	for _, w := range workflows {
		e.circleci.Workflows = append(e.circleci.Workflows, Workflow{
			Name:   w.Name,
			Status: w.Status,
			URL:    fmt.Sprintf("https://app.circleci.com/pipelines/%s/%d/workflows/%s", c.project, c.pipeline.Number, w.ID),
		})
		switch {
		case circleciclient.WorkflowSucceeded(w.Status), w.Status == "not_run":
		case circleciclient.WorkflowFailed(w.Status):
			e.red = append(e.red, fmt.Sprintf("circleci workflow %s %s", w.Name, w.Status))
		default:
			e.unfinished = append(e.unfinished, fmt.Sprintf("circleci workflow %s (%s)", w.Name, w.Status))
		}
	}
}

// checkRunPassed: the conclusion of a completed check run that does not block
// a merge.
func checkRunPassed(conclusion string) bool {
	switch conclusion {
	case "success", "neutral", "skipped":
		return true
	}
	return false
}

// workflowRunOpen: the status of a GitHub Actions run that has not finished.
func workflowRunOpen(status string) bool {
	switch status {
	case "queued", "in_progress", "waiting", statusPending, "requested":
		return true
	}
	return false
}

// latestCheckRuns keeps one run per check name: the one started last, and of
// two started together the one created later. A rerun replaces a stale run of
// the same name, which is how the merge box reads a retitled pull request
// whose title check ran twice.
func latestCheckRuns(runs []*github.CheckRun) []*github.CheckRun {
	latest := map[string]*github.CheckRun{}
	for _, run := range runs {
		name := run.GetName()
		current, ok := latest[name]
		if !ok || newerCheckRun(run, current) {
			latest[name] = run
		}
	}
	return sortedByName(latest)
}

func newerCheckRun(a, b *github.CheckRun) bool {
	at, bt := a.GetStartedAt().Time, b.GetStartedAt().Time
	if !at.Equal(bt) {
		return at.After(bt)
	}
	return a.GetID() > b.GetID()
}

// latestStatuses keeps one status per context, the one updated last; the
// combined status is already that, the fold defends against a fixture or an
// API page that is not.
func latestStatuses(statuses []*github.RepoStatus) []*github.RepoStatus {
	latest := map[string]*github.RepoStatus{}
	for _, status := range statuses {
		name := status.GetContext()
		current, ok := latest[name]
		if !ok || status.GetUpdatedAt().After(current.GetUpdatedAt().Time) {
			latest[name] = status
		}
	}
	return sortedByName(latest)
}

// latestWorkflowRuns keeps one run per workflow name, the one with the
// highest id: a run for the same head created later (a title check run again
// after the retitle) replaces the stale one.
func latestWorkflowRuns(runs []*github.WorkflowRun) []*github.WorkflowRun {
	latest := map[string]*github.WorkflowRun{}
	for _, run := range runs {
		name := run.GetName()
		current, ok := latest[name]
		if !ok || run.GetID() > current.GetID() {
			latest[name] = run
		}
	}
	return sortedByName(latest)
}

// latestWorkflows keeps one workflow per name: the one created last, and of
// two created together (or without a creation time) the one listed later. A
// rerun of a workflow is a new workflow with the same name in the same
// pipeline; the old, failed one no longer counts.
func latestWorkflows(workflows []circleciclient.Workflow) []circleciclient.Workflow {
	latest := map[string]circleciclient.Workflow{}
	for _, w := range workflows {
		current, ok := latest[w.Name]
		if !ok || !w.CreatedAt.Before(current.CreatedAt) {
			latest[w.Name] = w
		}
	}
	return sortedByName(latest)
}

// sortedByName returns the values of latest in the order of their keys.
func sortedByName[T any](latest map[string]T) []T {
	names := make([]string, 0, len(latest))
	for name := range latest {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]T, 0, len(latest))
	for _, name := range names {
		out = append(out, latest[name])
	}
	return out
}
