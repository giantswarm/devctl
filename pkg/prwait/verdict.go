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
	// failedActionsJobs and failedWorkflows are the red ones whose logs
	// --failed-log reads.
	failedActionsJobs []actionsJob
	failedWorkflows   []failedWorkflow
	// unfinished names everything the head still waits for, in the words the
	// document uses at a timeout.
	unfinished []string
	// requiredMissing are the required contexts nothing has reported under.
	requiredMissing []string
	// awaitingApproval are the Actions runs that completed with
	// action_required: they start only once a repository member approves
	// them.
	awaitingApproval []ActionRun
	// settled: every check, status, Actions run and CircleCI workflow of the
	// head has finished, so nothing left is going to report under a context
	// that is still absent.
	settled bool
	// approvalOnly: nothing is red and the Actions runs awaiting approval are
	// all the head still waits for, besides absent required contexts they may
	// report. No wait changes that; a member's approval does.
	approvalOnly bool
}

func (e *evaluation) green() bool { return len(e.red) == 0 && len(e.unfinished) == 0 }

// neverReported: the head is settled and a required context is still absent.
// That verdict needs no timeout: nothing is running that could report it.
func (e *evaluation) neverReported() bool { return e.settled && len(e.requiredMissing) > 0 }

func (e *evaluation) redReason() string { return strings.Join(e.red, "; ") }

// approvalReason names the runs awaiting approval and what starts them.
func (e *evaluation) approvalReason() string {
	runs := make([]string, 0, len(e.awaitingApproval))
	for _, run := range e.awaitingApproval {
		runs = append(runs, fmt.Sprintf("%s (%s)", run.Name, run.URL))
	}
	return fmt.Sprintf("actions run(s) awaiting a repository member's approval: %s; nothing else is pending, and the runs start once a member approves them", strings.Join(runs, ", "))
}

// evaluate applies the green rules to a snapshot:
//
//  1. every check run and commit status of the head, the latest per name,
//     is completed and not failed; a completed run that needs a human
//     (action_required) still waits;
//  2. when CircleCI is consulted, the newest pipeline of the head revision
//     exists and is past its setup (a setup pipeline's continuation created,
//     its workflows listed or their jobs' statuses on GitHub), and the newest
//     run of each of its workflows is success (not_run counts as skipped);
//  3. no GitHub Actions run of the head is queued, in progress, waiting or
//     awaiting approval;
//  4. every required status context has reported.
//
// A failure anywhere is red at once; anything else still open keeps the wait
// going. A required context nothing has reported under is unfinished like the
// rest while a check, status, run, workflow or pipeline setup is still
// pending (rules 1 to 3): the one awaiting approval, still running or not
// continued yet may be what reports it. Once all
// of them have finished, the head is settled and an absent context is one
// that will never report. A head whose only pending items are Actions runs
// awaiting approval is approvalOnly: no wait starts them, a member's approval
// does.
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
			if run.GetApp().GetSlug() == actionsApp {
				e.failedActionsJobs = append(e.failedActionsJobs, actionsJob{id: run.GetID(), name: name, url: check.URL})
			}
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
			e.awaitingApproval = append(e.awaitingApproval, action)
			e.unfinished = append(e.unfinished, fmt.Sprintf("actions run %s (awaiting approval)", action.Name))
		case workflowRunOpen(action.Status):
			e.unfinished = append(e.unfinished, fmt.Sprintf("actions run %s (%s)", action.Name, action.Status))
		}
	}

	if s.circleci != nil {
		e.evaluateCircleCI(s.headSHA, s.circleci, circleStatuses(s.statuses))
	}

	// Settled is judged before the required contexts: with nothing pending,
	// an absent context has nothing left that could report it.
	e.settled = len(e.unfinished) == 0
	e.approvalOnly = len(e.red) == 0 && len(e.awaitingApproval) > 0 && len(e.unfinished) == len(e.awaitingApproval)
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

// circleStatusPrefix is the context CircleCI posts a job's status under:
// one context per job name.
const circleStatusPrefix = "ci/circleci: "

// circleStatuses counts the CircleCI jobs that have posted a status to the
// head, one context each.
func circleStatuses(statuses []*github.RepoStatus) int {
	n := 0
	for _, status := range latestStatuses(statuses) {
		if strings.HasPrefix(status.GetContext(), circleStatusPrefix) {
			n++
		}
	}
	return n
}

// evaluateCircleCI applies rule 2 to the head's pipeline. circleStatuses is
// how many CircleCI jobs have posted to GitHub: an empty listing or a setup
// workflow listed on its own is not the pipeline's last word, and GitHub
// tells whether its continuation has run.
func (e *evaluation) evaluateCircleCI(headSHA string, c *circleSnapshot, circleStatuses int) {
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
	switch {
	case circleciclient.PipelineContinuing(c.pipeline.State):
		// A finished setup workflow is not the pipeline's last: the continued
		// workflows post the build's contexts once they exist.
		e.unfinished = append(e.unfinished, fmt.Sprintf("circleci pipeline %d (%s, continuation not created yet)", c.pipeline.Number, c.pipeline.State))
	case circleStatuses > 1:
		// More than one CircleCI job has posted to the head: the build runs or
		// ran, whatever the listing reads, and the jobs' contexts are its
		// verdict (rule 1). The listing is read for what it has: a workflow
		// it lists is judged below, and one it has not listed yet, or has
		// lost (an empty listing or the setup workflow alone, for as long as
		// CircleCI's listing lags behind the workflows it created), has its
		// jobs on GitHub.
	case len(workflows) == 0:
		// Nothing of the pipeline has run yet, or the setup job alone: with
		// one CircleCI context at most, no continued job has posted, and the
		// head waits.
		e.unfinished = append(e.unfinished, fmt.Sprintf("circleci pipeline %d (no workflows yet)", c.pipeline.Number))
	case circleciclient.SetupOnly(workflows):
		// The pipeline reads created with the setup workflow alone: for a
		// while after the setup job continues it, before the build's
		// workflows exist. Every continued job posts under its own context
		// beside the setup job's, so with one CircleCI context at most
		// nothing of the build has run, and the head waits.
		e.unfinished = append(e.unfinished, fmt.Sprintf("circleci pipeline %d (setup finished, the continuation's workflows not created yet)", c.pipeline.Number))
	}
	for _, w := range workflows {
		url := fmt.Sprintf("https://app.circleci.com/pipelines/%s/%d/workflows/%s", c.project, c.pipeline.Number, w.ID)
		e.circleci.Workflows = append(e.circleci.Workflows, Workflow{Name: w.Name, Status: w.Status, URL: url})
		switch {
		case circleciclient.WorkflowSucceeded(w.Status), w.Status == "not_run":
		case circleciclient.WorkflowFailed(w.Status):
			e.red = append(e.red, fmt.Sprintf("circleci workflow %s %s", w.Name, w.Status))
			e.failedWorkflows = append(e.failedWorkflows, failedWorkflow{id: w.ID, name: w.Name, url: url})
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
