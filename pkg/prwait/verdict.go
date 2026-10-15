package prwait

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// The check sources of the document.
const (
	SourceCheckRun         = "check_run"
	SourceStatus           = "status"
	SourceCircleCIWorkflow = "circleci_workflow"
)

// The check states the document shares between check runs and statuses.
const (
	statusCompleted = "completed"
	statusPending   = "pending"
)

// Check is one check of the head as the merge box lists it: a check run or a
// commit status (the latest of that context). Every run of a check name
// counts: the jobs of a matrix, or of several workflows calling one reusable
// workflow, report under one name, and each is listed with its own ID. Of one
// workflow run several times for the head (a title check after a retitle),
// the newest run's jobs count.
type Check struct {
	Name string `json:"name"`
	// ID is the check run's id, the job id of a GitHub Actions job; absent
	// for a status.
	ID int64 `json:"id,omitempty"`
	// Workflow is the GitHub Actions run the check run's job belongs to, when
	// the head's runs list it.
	Workflow string `json:"workflow,omitempty"`
	Source   string `json:"source"`
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

// AcceptedRed is a failed check, status or CircleCI workflow the caller
// named with --accept-red: recorded instead of making the verdict red.
type AcceptedRed struct {
	Name string `json:"name"`
	// Source is check_run, status or circleci_workflow.
	Source     string `json:"source"`
	Conclusion string `json:"conclusion"`
	// URL is the run, the status's target or the workflow.
	URL string `json:"url"`
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
	// accepted are the check names whose failure the caller accepts
	// (--accept-red); a required one is never accepted.
	accepted map[string]bool
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
	// acceptedRed are the failed checks, statuses and workflows the caller
	// named to accept: they do not make the verdict red.
	acceptedRed []AcceptedRed
	// seen are the names of every check, status and CircleCI workflow of
	// the head, the names an acceptance can match.
	seen map[string]bool
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
//  1. every check run of the head (each run of a name, of a workflow its
//     newest run) and every commit status (the latest per context) is
//     completed and not failed; a completed run that needs a human
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
	e := &evaluation{checks: []Check{}, actions: []ActionRun{}, seen: map[string]bool{}}
	required := map[string]bool{}
	for _, name := range s.required {
		required[name] = true
	}
	reported := map[string]bool{}

	checkRuns := currentCheckRuns(s.checkRuns, s.runs)
	runsOfName := map[string]int{}
	for _, c := range checkRuns {
		runsOfName[c.run.GetName()]++
	}
	for _, c := range checkRuns {
		name := c.run.GetName()
		reported[name], e.seen[name] = true, true
		check := Check{
			Name:       name,
			ID:         c.run.GetID(),
			Workflow:   c.workflow.GetName(),
			Source:     SourceCheckRun,
			Status:     c.run.GetStatus(),
			Conclusion: c.run.GetConclusion(),
			URL:        c.run.GetHTMLURL(),
			Required:   required[name],
		}
		e.checks = append(e.checks, check)
		label := check.label(runsOfName[name] > 1)
		switch {
		case check.Status != statusCompleted:
			e.unfinished = append(e.unfinished, fmt.Sprintf("check %s (%s)", label, check.Status))
		case check.Conclusion == "action_required":
			e.unfinished = append(e.unfinished, fmt.Sprintf("check %s (action_required)", label))
		case checkRunPassed(check.Conclusion):
		case s.accepts(name):
			e.accept(check.Name, check.Source, check.Conclusion, check.URL)
		default:
			e.red = append(e.red, fmt.Sprintf("check %s concluded %s", label, check.Conclusion))
			if c.run.GetApp().GetSlug() == actionsApp {
				e.failedActionsJobs = append(e.failedActionsJobs, actionsJob{id: check.ID, name: label, url: check.URL})
			}
		}
	}

	for _, status := range latestStatuses(s.statuses) {
		name := status.GetContext()
		reported[name], e.seen[name] = true, true
		check := Check{
			Name:     name,
			Source:   SourceStatus,
			Status:   statusCompleted,
			URL:      status.GetTargetURL(),
			Required: required[name],
		}
		switch state := status.GetState(); {
		case state == statusPending:
			check.Status = statusPending
			e.unfinished = append(e.unfinished, fmt.Sprintf("status %s (pending)", name))
		case state == "success":
			check.Conclusion = state
		case s.accepts(name):
			check.Conclusion = state
			e.accept(name, SourceStatus, state, check.URL)
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
		e.evaluateCircleCI(s.headSHA, s.circleci, circleStatuses(s.statuses), s.accepts)
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
		if e.checks[i].Source != e.checks[j].Source {
			return e.checks[i].Source < e.checks[j].Source
		}
		return e.checks[i].ID < e.checks[j].ID
	})
	sort.SliceStable(e.actions, func(i, j int) bool { return e.actions[i].Name < e.actions[j].Name })
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
func (e *evaluation) evaluateCircleCI(headSHA string, c *circleSnapshot, circleStatuses int, accepts func(string) bool) {
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
		e.seen[w.Name] = true
		switch {
		case circleciclient.WorkflowSucceeded(w.Status), w.Status == "not_run":
		case circleciclient.WorkflowFailed(w.Status) && accepts(w.Name):
			e.accept(w.Name, SourceCircleCIWorkflow, w.Status, url)
		case circleciclient.WorkflowFailed(w.Status):
			e.red = append(e.red, fmt.Sprintf("circleci workflow %s %s", w.Name, w.Status))
			e.failedWorkflows = append(e.failedWorkflows, failedWorkflow{id: w.ID, name: w.Name, url: url})
		default:
			e.unfinished = append(e.unfinished, fmt.Sprintf("circleci workflow %s (%s)", w.Name, w.Status))
		}
	}
}

// accepts: the caller accepts a failure of the check, status or workflow
// name, and the base does not require it.
func (s snapshot) accepts(name string) bool {
	return s.accepted[name] && !slices.Contains(s.required, name)
}

// accept records a failure the caller accepted instead of a red verdict.
func (e *evaluation) accept(name, source, conclusion, url string) {
	e.acceptedRed = append(e.acceptedRed, AcceptedRed{Name: name, Source: source, Conclusion: conclusion, URL: url})
}

// unseen are the accepted names no check, status or CircleCI workflow of
// the head carries, sorted.
func (e *evaluation) unseen(accepted map[string]bool) []string {
	var names []string
	for name := range accepted {
		if !e.seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
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

// label names the check in a verdict: its name alone while it is the only
// run of that name, else with its workflow and id, so the red one of a matrix
// is told from its green siblings.
func (c Check) label(shared bool) string {
	switch {
	case !shared:
		return c.Name
	case c.Workflow != "":
		return fmt.Sprintf("%s [%s, check run %d]", c.Name, c.Workflow, c.ID)
	default:
		return fmt.Sprintf("%s [check run %d]", c.Name, c.ID)
	}
}

// headCheckRun is a check run of the head with the GitHub Actions run its job
// belongs to; workflow is nil for another app's check run, or a job whose
// run the listing does not carry.
type headCheckRun struct {
	run      *github.CheckRun
	workflow *github.WorkflowRun
}

// checkRunGroup is the current runs of one check name in one workflow.
type checkRunGroup struct {
	workflow *github.WorkflowRun
	runs     []*github.CheckRun
}

// currentCheckRuns keeps the check runs that count, sorted by name and id.
// GitHub's filter=latest already drops the earlier attempts of a rerun job.
// Check runs are grouped by name and the workflow their job belongs to (the
// Actions run of their check suite): of a workflow run several times for the
// head, every run of the name in the newest workflow run counts, which is
// how the merge box reads a retitled pull request whose title check ran
// twice, while the jobs of a matrix, or of several workflows calling one
// reusable workflow, each count. A check run outside a listed workflow run
// keeps one run per name: the one started last, and of two started together
// the one created later.
func currentCheckRuns(runs []*github.CheckRun, workflowRuns []*github.WorkflowRun) []headCheckRun {
	bySuite := map[int64]*github.WorkflowRun{}
	for _, run := range workflowRuns {
		if id := run.GetCheckSuiteID(); id != 0 {
			bySuite[id] = run
		}
	}
	openRuns := newestOpenWorkflowRuns(workflowRuns)
	groups := map[string]*checkRunGroup{}
	for _, run := range runs {
		workflow := bySuite[run.GetCheckSuite().GetID()]
		if supersededCheckRun(run, workflow, openRuns) {
			continue
		}
		key := run.GetName() + "\x00" + workflowKey(workflow)
		group, ok := groups[key]
		switch {
		case !ok:
			groups[key] = &checkRunGroup{workflow: workflow, runs: []*github.CheckRun{run}}
		case workflow == nil:
			if newerCheckRun(run, group.runs[0]) {
				group.runs[0] = run
			}
		case workflow.GetID() == group.workflow.GetID():
			group.runs = append(group.runs, run)
		case workflow.GetID() > group.workflow.GetID():
			group.workflow, group.runs = workflow, []*github.CheckRun{run}
		}
	}
	var current []headCheckRun
	for _, group := range groups {
		for _, run := range group.runs {
			current = append(current, headCheckRun{run: run, workflow: group.workflow})
		}
	}
	sort.Slice(current, func(i, j int) bool {
		a, b := current[i].run, current[j].run
		if a.GetName() != b.GetName() {
			return a.GetName() < b.GetName()
		}
		return a.GetID() < b.GetID()
	})
	return current
}

// newestOpenWorkflowRuns maps each workflow to its newest run for the head
// while that run has not finished: the run a rerun or a reopen started, whose
// jobs may not be listed yet.
func newestOpenWorkflowRuns(runs []*github.WorkflowRun) map[string]*github.WorkflowRun {
	open := map[string]*github.WorkflowRun{}
	for key, run := range newestWorkflowRuns(runs) {
		if workflowRunOpen(run.GetStatus()) {
			open[key] = run
		}
	}
	return open
}

// supersededCheckRun: the check run belongs to an earlier run of its workflow
// than the newest one, which is still open, or to an earlier attempt of the
// run that is open again (the check run started before the attempt did). The
// newer run's jobs are what the head is judged on, and until they are listed
// the open run itself keeps the wait going: a failure of the earlier run is
// not the head's verdict.
func supersededCheckRun(run *github.CheckRun, workflow *github.WorkflowRun, open map[string]*github.WorkflowRun) bool {
	if workflow == nil {
		return false
	}
	newest, ok := open[workflowKey(workflow)]
	switch {
	case !ok:
		return false
	case newest.GetID() != workflow.GetID():
		return true
	default:
		attempt := workflow.GetRunStartedAt().Time
		return !attempt.IsZero() && run.GetStartedAt().Before(attempt)
	}
}

// workflowKey identifies the workflow a run belongs to: its workflow id, the
// name where the listing carries none; empty for no run.
func workflowKey(run *github.WorkflowRun) string {
	switch {
	case run == nil:
		return ""
	case run.GetWorkflowID() != 0:
		return fmt.Sprintf("id:%d", run.GetWorkflowID())
	default:
		return "name:" + run.GetName()
	}
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

// newestWorkflowRuns maps each workflow to its run with the highest id.
func newestWorkflowRuns(runs []*github.WorkflowRun) map[string]*github.WorkflowRun {
	newest := map[string]*github.WorkflowRun{}
	for _, run := range runs {
		key := workflowKey(run)
		current, ok := newest[key]
		if !ok || run.GetID() > current.GetID() {
			newest[key] = run
		}
	}
	return newest
}

// latestWorkflowRuns keeps one run per workflow, the one with the highest id:
// a run for the same head created later (a title check run again after the
// retitle) replaces the stale one. Two workflows that share a name are kept
// apart by their workflow id.
func latestWorkflowRuns(runs []*github.WorkflowRun) []*github.WorkflowRun {
	latest := newestWorkflowRuns(runs)
	out := make([]*github.WorkflowRun, 0, len(latest))
	for _, run := range latest {
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GetName() != out[j].GetName() {
			return out[i].GetName() < out[j].GetName()
		}
		return out[i].GetID() < out[j].GetID()
	})
	return out
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
