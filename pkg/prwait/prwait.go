// Package prwait is the wait engine behind `devctl pr wait` (and the merge
// that follows it): one bounded, blocking call that returns when a pull
// request's head is green, red, or cannot become green as it is.
//
// Green is the merge box's view, not `gh pr checks`': every check run and
// commit status of the head (the latest per name), every CircleCI workflow of
// the head revision (the newest run per name, read from CircleCI because a job
// behind `requires:` has posted nothing yet), no GitHub Actions run of the
// head still queued, running or awaiting approval, and every required status
// context reported. Draft, closed, conflicting and behind-a-strict-base end
// the wait before it starts.
package prwait

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// DefaultTimeout bounds a wait whose caller names none.
const DefaultTimeout = 30 * time.Minute

// CircleCIConfigPath is the file whose presence at the head makes CircleCI
// part of the verdict.
const CircleCIConfigPath = ".circleci/config.yml"

// pipelinePages bounds the search for the head revision among a branch's
// pipelines, newest first: past three pages the pipeline is not the newest
// of anything.
const pipelinePages = 3

// RateSource reports the GitHub rate limit the newest response carried.
type RateSource interface {
	Rate() githubclient.RateLimit
}

// Config configures a Waiter.
type Config struct {
	// GitHub is required. From [githubclient.NewConditional] its polls are
	// conditional requests and Rate is the [githubclient.Conditional].
	GitHub *githubclient.Client
	// Rate is where the poll interval comes from; nil polls at the floor.
	Rate RateSource
	// CircleCI returns the client once the head is known to carry a CircleCI
	// configuration; it is the place the CircleCI token is required, so an
	// [agentcli.ExitCoder] it returns ends the wait with that code. nil
	// means CircleCI is never consulted.
	CircleCI func(ctx context.Context) (*circleciclient.Client, error)
	// Clock defaults to the wall clock at scale 1.
	Clock agentcli.Clock
	// Progress may be nil.
	Progress *agentcli.Progress
	// Timeout defaults to DefaultTimeout.
	Timeout time.Duration
}

// Waiter runs waits.
type Waiter struct {
	github   *githubclient.Client
	rate     RateSource
	circleci func(ctx context.Context) (*circleciclient.Client, error)
	clock    agentcli.Clock
	progress *agentcli.Progress
	timeout  time.Duration
}

// New returns a Waiter for config.
func New(config Config) (*Waiter, error) {
	if config.GitHub == nil {
		return nil, fmt.Errorf("%T.GitHub must not be empty", config)
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}
	if config.Progress == nil {
		config.Progress = agentcli.NewProgress(nil, false)
	}
	return &Waiter{
		github:   config.GitHub,
		rate:     config.Rate,
		circleci: config.CircleCI,
		clock:    config.Clock,
		progress: config.Progress,
		timeout:  config.Timeout,
	}, nil
}

// Result is the command's part of the document.
type Result struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	HeadSHA    string `json:"headSha"`
	BaseRef    string `json:"baseRef"`
	// Checks are the head's check runs and statuses, the latest per name.
	Checks []Check `json:"checks"`
	// CircleCI is absent when the head carries no CircleCI configuration or
	// the repository has no CircleCI project.
	CircleCI *CircleCI `json:"circleci,omitempty"`
	// Actions are the head's GitHub Actions runs, the latest per workflow.
	Actions []ActionRun `json:"actions"`
	// Unfinished names what the head was still waiting for when the wait
	// ended without a verdict.
	Unfinished []string `json:"unfinished,omitempty"`
	// Warnings are for the envelope: a head that changed under the wait, a
	// CircleCI project that does not exist.
	Warnings []string `json:"-"`
}

// head is what the wait learned about the current head and base, refreshed
// when either changes.
type head struct {
	sha      string
	baseRef  string
	required []string
	// decided: whether CircleCI is consulted for this head is known.
	decided  bool
	circleci *circleciclient.Client
	branch   string
	project  string
}

// Wait blocks until owner/repo#number is green (nil), red or otherwise
// decided (an [*agentcli.ExitError] with the code of the table), or the
// timeout passes (exit 2). A required context absent from a head whose
// checks, runs and workflows have all finished is exit 4 at that poll, before
// the timeout; while anything is still pending, the timeout is 2 whatever
// is absent. The Result is always returned, as far as it was filled; a
// tooling failure is any other error.
func (w *Waiter) Wait(ctx context.Context, owner, repo string, number int) (*Result, error) {
	result := &Result{
		Repository: owner + "/" + repo,
		Number:     number,
		Checks:     []Check{},
		Actions:    []ActionRun{},
	}
	ctx, cancel := w.clock.Timeout(ctx, w.timeout)
	defer cancel()

	h := &head{}
	var last *evaluation
	for poll := 1; ; poll++ {
		e, err := w.poll(ctx, owner, repo, number, h, result)
		if err != nil {
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				return result, w.timedOut(result, last)
			}
			// A rate limit resetting after the deadline is a timeout ahead
			// of it: the document names what was unfinished all the same.
			var limited *agentcli.RateLimitedError
			if errors.As(err, &limited) {
				result.Unfinished = unfinished(last)
			}
			return result, err
		}
		last = e
		switch {
		case e.green():
			w.progress.Printf("poll %d: green", poll)
			return result, nil
		case len(e.red) > 0:
			w.progress.Printf("poll %d: red: %s", poll, e.redReason())
			return result, agentcli.NewExitError(agentcli.ExitRed, agentcli.VerdictRed, "%s", e.redReason())
		case e.neverReported():
			w.progress.Printf("poll %d: required missing: %s", poll, strings.Join(e.requiredMissing, ", "))
			return result, w.neverReported(result, e)
		}
		interval := w.interval()
		w.progress.Printf("poll %d: waiting for %s; next poll in %s", poll, strings.Join(e.unfinished, ", "), interval)
		if err := w.clock.Sleep(ctx, interval); err != nil {
			return result, w.timedOut(result, last)
		}
	}
}

// timedOut is the verdict at the deadline, 2: the result names what was
// unfinished, a required context still absent among it. A head settled with
// an absent context never reaches the deadline; neverReported ends it first.
func (w *Waiter) timedOut(result *Result, last *evaluation) error {
	result.Unfinished = unfinished(last)
	if last == nil {
		return agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout, "timeout after %s before the first poll completed", w.timeout)
	}
	return agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout,
		"timeout after %s; unfinished: %s", w.timeout, strings.Join(last.unfinished, ", "))
}

// unfinished is what the last complete poll was waiting for, or that none
// completed.
func unfinished(last *evaluation) []string {
	if last == nil {
		return []string{"no complete poll before the deadline"}
	}
	return last.unfinished
}

// neverReported is the verdict of a settled head with a required context
// absent, 4, at the poll that saw it: every check, run and workflow of the
// head has finished, so no wait would make the context report.
func (w *Waiter) neverReported(result *Result, e *evaluation) error {
	result.Unfinished = e.unfinished
	return agentcli.NewExitError(agentcli.ExitRequiredMissing, agentcli.VerdictRequiredMissing,
		"required context(s) never reported: %s; every check, run and workflow of the head has finished", strings.Join(e.requiredMissing, ", "))
}

// poll reads the pull request and everything of its head, and evaluates it.
func (w *Waiter) poll(ctx context.Context, owner, repo string, number int, h *head, result *Result) (*evaluation, error) {
	pr, err := w.github.PullRequest(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	if err := notApplicable(pr); err != nil {
		result.HeadSHA = pr.GetHead().GetSHA()
		result.BaseRef = pr.GetBase().GetRef()
		return nil, err
	}

	sha, baseRef := pr.GetHead().GetSHA(), pr.GetBase().GetRef()
	if h.sha != "" && h.sha != sha {
		result.Warnings = append(result.Warnings, fmt.Sprintf("the head changed from %s to %s during the wait", h.sha, sha))
	}
	if h.sha != sha {
		h.sha, h.decided, h.circleci = sha, false, nil
		result.HeadSHA = sha
	}
	if h.baseRef != baseRef || h.required == nil {
		h.baseRef = baseRef
		result.BaseRef = baseRef
		required, err := w.github.RequiredStatusContexts(ctx, owner, repo, baseRef)
		if err != nil {
			return nil, err
		}
		h.required = required
		w.progress.Printf("base %s requires %d context(s)", baseRef, len(required))
	}
	if !h.decided {
		if err := w.decideCircleCI(ctx, owner, repo, number, pr.GetHead().GetRef(), isFork(pr), h, result); err != nil {
			return nil, err
		}
	}

	s := snapshot{headSHA: sha, required: h.required}
	if s.checkRuns, err = w.github.CheckRunsForRef(ctx, owner, repo, sha); err != nil {
		return nil, err
	}
	if s.statuses, err = w.github.CommitStatuses(ctx, owner, repo, sha); err != nil {
		return nil, err
	}
	if s.runs, err = w.github.WorkflowRunsForSHA(ctx, owner, repo, sha); err != nil {
		return nil, err
	}
	if h.circleci != nil {
		if s.circleci, err = w.readCircleCI(ctx, owner, repo, h); err != nil {
			return nil, err
		}
	}

	e := evaluate(s)
	result.Checks, result.Actions, result.CircleCI = e.checks, e.actions, e.circleci
	return e, nil
}

// decideCircleCI settles whether CircleCI is part of this head's verdict: it
// is when the head carries CircleCIConfigPath and CircleCI has a project for
// the repository. The token is required only past the first condition, so a
// repository without CircleCI needs GitHub alone.
func (w *Waiter) decideCircleCI(ctx context.Context, owner, repo string, number int, headRef string, fork bool, h *head, result *Result) error {
	h.decided = true
	if w.circleci == nil {
		return nil
	}
	hasConfig, err := w.github.FileExists(ctx, owner, repo, CircleCIConfigPath, h.sha)
	if err != nil {
		return err
	}
	if !hasConfig {
		w.progress.Printf("no %s at %s: GitHub alone", CircleCIConfigPath, h.sha)
		return nil
	}
	client, err := w.circleci(ctx)
	if err != nil {
		return err
	}
	if _, err := client.GetProject(ctx, owner, repo); err != nil {
		if circleciclient.IsNotFound(err) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s/%s has %s but no CircleCI project: GitHub alone", owner, repo, CircleCIConfigPath))
			return nil
		}
		return err
	}
	h.circleci = client
	h.project = fmt.Sprintf("github/%s/%s", owner, repo)
	h.branch = headRef
	if fork {
		h.branch = fmt.Sprintf("pull/%d", number)
	}
	w.progress.Printf("CircleCI project %s, branch %s", h.project, h.branch)
	return nil
}

// readCircleCI finds the newest pipeline of the head revision on the head's
// branch and reads its workflows.
func (w *Waiter) readCircleCI(ctx context.Context, owner, repo string, h *head) (*circleSnapshot, error) {
	s := &circleSnapshot{project: h.project}
	pageToken := ""
	for page := 0; page < pipelinePages && s.pipeline == nil; page++ {
		pipelines, err := h.circleci.ListBranchPipelines(ctx, owner, repo, h.branch, pageToken)
		if err != nil {
			return nil, err
		}
		for i := range pipelines.Items {
			if pipelines.Items[i].VCS.Revision == h.sha {
				s.pipeline = &pipelines.Items[i]
				break
			}
		}
		if pipelines.NextPageToken == "" {
			break
		}
		pageToken = pipelines.NextPageToken
	}
	if s.pipeline == nil {
		return s, nil
	}
	workflows, err := h.circleci.ListPipelineWorkflows(ctx, s.pipeline.ID)
	if err != nil {
		return nil, err
	}
	s.workflows = workflows
	return s, nil
}

// interval is the time to the next poll, from the rate limit of the newest
// GitHub answer: the budget spread over the window, at least MinInterval and
// at most MaxInterval.
func (w *Waiter) interval() time.Duration {
	if w.rate == nil {
		return MinInterval
	}
	return interval(w.rate.Rate(), w.clock.Now())
}
