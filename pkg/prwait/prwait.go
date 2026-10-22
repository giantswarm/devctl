// Package prwait is the wait engine behind `devctl pr wait` (and the merge
// that follows it): one bounded, blocking call that returns when a pull
// request's head is green, red, or cannot become green as it is.
//
// Green is the merge box's view, not `gh pr checks`': every check run and
// commit status of the head (every run of a check name, the latest status per
// context), every CircleCI workflow of
// the head revision (the newest run per name, read from CircleCI because a job
// behind `requires:` has posted nothing yet), no GitHub Actions run of the
// head still queued, running or awaiting approval, and every required status
// context reported. A pending check of another app, or a pending status of
// another CI than CircleCI, the base does not require is not waited for, as
// the merge box does not wait for it: Result.Blocking names it, and a review
// the base requires that is missing. Draft, closed, conflicting and behind-a-strict-base end
// the wait before it starts.
package prwait

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/circleciconfig"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// DefaultTimeout bounds a wait whose caller names none.
const DefaultTimeout = 30 * time.Minute

// CircleCIConfigPath is the file whose presence at the head makes CircleCI
// part of the verdict.
const CircleCIConfigPath = ".circleci/config.yml"

// ChecksViaGraphQL is Result.ChecksVia when the head's checks were read
// through GraphQL's statusCheckRollup.
const ChecksViaGraphQL = "graphql"

// GitHub is what the wait reads from GitHub; *githubclient.Client is one.
type GitHub interface {
	JobLogs
	PullRequest(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error)
	ReviewDecision(ctx context.Context, owner, repo string, number int) (string, error)
	RequiredStatusContexts(ctx context.Context, owner, repo, branch string) ([]string, error)
	WorkflowRunsForSHA(ctx context.Context, owner, repo, sha string) ([]*github.WorkflowRun, error)
	CheckRollup(ctx context.Context, owner, repo, sha string) ([]*github.CheckRun, []*github.RepoStatus, error)
	CheckRunsForRef(ctx context.Context, owner, repo, ref string) ([]*github.CheckRun, error)
	CommitStatuses(ctx context.Context, owner, repo, ref string) ([]*github.RepoStatus, error)
	FileExists(ctx context.Context, owner, repo, path, ref string) (bool, error)
	CommitMessage(ctx context.Context, owner, repo, sha string) (string, error)
	ListDirectory(ctx context.Context, owner, repo, path, ref string) ([]string, error)
	InactiveWorkflowPaths(ctx context.Context, owner, repo string) (map[string]bool, error)
	ReadFile(ctx context.Context, owner, repo, path, ref string) ([]byte, bool, error)
}

// RateSource reports the GitHub rate limit the newest response carried.
type RateSource interface {
	Rate() githubclient.RateLimit
}

// EntryFinder finds a repository's declaration in the team files; found
// false when no team file declares it.
type EntryFinder interface {
	FindEntry(ctx context.Context, owner, repo string) (*reposetup.Fields, bool, error)
}

// Config configures a Waiter.
type Config struct {
	// GitHub is required. From [githubclient.NewConditional] its polls are
	// conditional requests and Rate is the [githubclient.Conditional].
	GitHub GitHub
	// Rate is where the poll interval comes from; nil polls at the floor.
	Rate RateSource
	// CircleCI returns the client once the head is known to carry a CircleCI
	// configuration; it is the place the CircleCI token is required, so an
	// [agentcli.ExitCoder] it returns ends the wait with that code. nil
	// means CircleCI is never consulted.
	CircleCI func(ctx context.Context) (*circleciclient.Client, error)
	// CircleCIFromStatuses, without CircleCI, reads CircleCI through the
	// commit statuses it posts to GitHub alone: the jobs the head's
	// CircleCIConfigPath runs on its branch are listed from the
	// configuration, and one that has posted no status yet, a job behind
	// requires: whose required jobs still run, keeps the head pending.
	CircleCIFromStatuses bool
	// Entries is read only for a template repository whose head carries
	// CircleCIConfigPath: GitHub alone judges it while its entry declares
	// gen.ci.templateContent. nil means no repository declares it.
	Entries EntryFinder
	// Clock defaults to the wall clock at scale 1.
	Clock agentcli.Clock
	// Progress may be nil.
	Progress *agentcli.Progress
	// Timeout defaults to DefaultTimeout.
	Timeout time.Duration
	// FailedLogLines, when positive, makes a red verdict read the log of
	// each failed job once and keep that many of its last lines in
	// Result.FailedJobs. A green or pending head reads no log.
	FailedLogLines int
	// AcceptRed are check names whose failure does not make the verdict
	// red: a failed check run, status or CircleCI workflow of one of these
	// names is recorded in Result.AcceptedRed with a warning instead. A
	// name the base requires is never accepted, and a green head that
	// carries no check of a name is exit 3. Refusing a required name or a
	// security scan before the wait is the caller's (pkg/prmerge).
	AcceptRed []string
}

// Waiter runs waits.
type Waiter struct {
	github   GitHub
	rate     RateSource
	circleci func(ctx context.Context) (*circleciclient.Client, error)
	// fromStatuses is Config.CircleCIFromStatuses.
	fromStatuses bool
	entries      EntryFinder
	clock        agentcli.Clock
	progress     *agentcli.Progress
	timeout      time.Duration
	// failedLogLines is Config.FailedLogLines.
	failedLogLines int
	// acceptRed is Config.AcceptRed as a set; nil without it.
	acceptRed map[string]bool
}

// New returns a Waiter for config.
func New(config Config) (*Waiter, error) {
	if config.GitHub == nil {
		return nil, fmt.Errorf("%T.GitHub must not be empty", config)
	}
	if config.CircleCI != nil && config.CircleCIFromStatuses {
		return nil, fmt.Errorf("%T.CircleCI and CircleCIFromStatuses exclude each other", config)
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}
	if config.Progress == nil {
		config.Progress = agentcli.NewProgress(nil, false)
	}
	var acceptRed map[string]bool
	for _, name := range config.AcceptRed {
		if acceptRed == nil {
			acceptRed = map[string]bool{}
		}
		acceptRed[name] = true
	}
	return &Waiter{
		github:       config.GitHub,
		rate:         config.Rate,
		circleci:     config.CircleCI,
		fromStatuses: config.CircleCIFromStatuses,
		entries:      config.Entries,
		clock:        config.Clock,
		progress:     config.Progress,
		timeout:      config.Timeout,
		// The tail is read only after a red verdict.
		failedLogLines: config.FailedLogLines,
		acceptRed:      acceptRed,
	}, nil
}

// Result is the command's part of the document.
type Result struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	HeadSHA    string `json:"headSha"`
	BaseRef    string `json:"baseRef"`
	// Checks are the head's check runs, every run of a name that counts, and
	// its statuses, the latest per context.
	Checks []Check `json:"checks"`
	// ChecksVia is ChecksViaGraphQL when the last poll read Checks through
	// GraphQL because REST's check endpoints went unanswered; absent when REST
	// answered.
	ChecksVia string `json:"checksVia,omitempty"`
	// CircleCI is absent when the head carries no CircleCI configuration, the
	// repository is a template whose entry declares that configuration as
	// template content, or CircleCI has no project for it.
	CircleCI *CircleCI `json:"circleci,omitempty"`
	// Actions are the head's GitHub Actions runs, the latest per workflow.
	Actions []ActionRun `json:"actions"`
	// Unfinished names what the head was still waiting for when the wait
	// ended without a verdict.
	Unfinished []string `json:"unfinished,omitempty"`
	// FailedJobs are the failed jobs of a red head with the tails of their
	// logs, read with Config.FailedLogLines.
	FailedJobs []FailedJob `json:"failedJobs,omitempty"`
	// AcceptedRed are the failed checks the caller accepted with
	// Config.AcceptRed, each with its run's URL.
	AcceptedRed []AcceptedRed `json:"acceptedRed,omitempty"`
	// Blocking names what keeps GitHub's merge box from merging a green
	// head: a check or status the base does not require that is still
	// pending, which the wait does not wait for, and a review the base
	// requires that is missing or asks for changes.
	Blocking []string `json:"blocking,omitempty"`
	// Warnings are for the envelope: a head that changed under the wait, a
	// template repository, a CircleCI project that does not exist.
	Warnings []string `json:"-"`
}

// head is what the wait learned about the current head and base, refreshed
// when either changes.
type head struct {
	sha     string
	baseRef string
	// mergeableState is GitHub's mergeable state of the pull request at the
	// last poll.
	mergeableState string
	required       []string
	// decided: whether CircleCI is consulted for this head is known.
	decided  bool
	circleci *circleciclient.Client
	branch   string
	project  string
	// circleJobs are, with CircleCIFromStatuses, the jobs the head's
	// CircleCI configuration runs on its branch, each expected to post a
	// status.
	circleJobs []string
	// workflows are the pull_request workflows an empty head expects runs
	// of, once workflowsRead.
	workflows     []string
	workflowsRead bool
}

// Wait blocks until owner/repo#number is green (nil), red or otherwise
// decided (an [*agentcli.ExitError] with the code of the table), or the
// timeout passes (exit 2). A required context absent from a head whose
// checks, runs and workflows have all finished is exit 4 at that poll, before
// the timeout, once the pull request's last update is RegistrationGrace old,
// and so is a head that waits only for Actions runs awaiting a member's
// approval; while anything else is still pending, the timeout is 2 whatever is
// absent. A failure the base does not require is red once the required
// contexts have finished. The Result is always returned, as far as it was filled; a
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
	// unanswered is the last poll's read that went unanswered, empty once a
	// poll completes again.
	var unanswered string
	for poll := 1; ; poll++ {
		e, err := w.poll(ctx, owner, repo, number, h, result)
		if err != nil {
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				return result, w.timedOut(result, last, unanswered)
			}
			// A rate limit resetting after the deadline is a timeout ahead
			// of it: the document names what was unfinished all the same.
			var limited *agentcli.RateLimitedError
			if errors.As(err, &limited) {
				result.Unfinished = unfinished(last)
			}
			// A read that failed every try, in transit or with a 5xx, says
			// nothing about the head: the wait polls again until its
			// deadline instead of ending as a tooling failure.
			var exhausted *agentcli.RetriesExhaustedError
			if !errors.As(err, &exhausted) {
				return result, err
			}
			unanswered = err.Error()
			result.Warnings = append(result.Warnings, fmt.Sprintf("poll %d went unanswered: %s", poll, unanswered))
			interval := w.interval()
			w.progress.Printf("poll %d: unanswered (%s); next poll in %s", poll, unanswered, interval)
			if err := w.clock.Sleep(ctx, interval); err != nil {
				return result, w.timedOut(result, last, unanswered)
			}
			continue
		}
		last, unanswered = e, ""
		switch {
		case e.green():
			if unseen := e.unseen(w.acceptRed); len(unseen) > 0 {
				w.progress.Printf("poll %d: green, and no check of the head is named %s", poll, strings.Join(unseen, ", "))
				return result, agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable,
					"--accept-red names %s, and no check, status or CircleCI workflow of the head %s carries that name", strings.Join(unseen, ", "), h.sha)
			}
			for _, a := range e.acceptedRed {
				result.Warnings = append(result.Warnings, fmt.Sprintf("accepted red: %s %s concluded %s (%s), named by --accept-red", a.Source, a.Name, a.Conclusion, a.URL))
			}
			w.blocking(ctx, owner, repo, number, h, e, result)
			w.progress.Printf("poll %d: green", poll)
			return result, nil
		case e.redDecided():
			w.progress.Printf("poll %d: red: %s", poll, e.redReason())
			if w.failedLogLines > 0 {
				result.FailedJobs = w.failedJobs(ctx, owner, repo, h, e)
			}
			return result, agentcli.NewExitError(agentcli.ExitRed, agentcli.VerdictRed, "%s", e.redReason())
		case e.neverReported():
			w.progress.Printf("poll %d: required missing: %s", poll, strings.Join(e.requiredMissing, ", "))
			return result, w.neverReported(result, e)
		case e.approvalOnly:
			w.progress.Printf("poll %d: waiting for approval: %s", poll, strings.Join(e.unfinished, ", "))
			result.Unfinished = e.unfinished
			return result, agentcli.NewExitError(agentcli.ExitRequiredMissing, agentcli.VerdictApprovalRequired, "%s", e.approvalReason())
		case w.noChecks(e):
			w.progress.Printf("poll %d: no checks: %s", poll, strings.Join(e.unfinished, ", "))
			result.Unfinished = e.unfinished
			return result, noChecksError(h.sha, e.expectedRuns)
		}
		interval := w.interval()
		w.progress.Printf("poll %d: waiting for %s; next poll in %s", poll, strings.Join(e.unfinished, ", "), interval)
		w.progress.Waiting(w.clock.Now(), strings.Join(e.unfinished, ", "))
		if err := w.clock.Sleep(ctx, interval); err != nil {
			return result, w.timedOut(result, last, "")
		}
	}
}

// blocking fills Result.Blocking of a green head: the pending checks the
// base does not require, which the wait did not wait for, and, while GitHub
// blocks the merge, a missing review or one that requests changes. A review
// decision that cannot be read is a warning, never the verdict.
func (w *Waiter) blocking(ctx context.Context, owner, repo string, number int, h *head, e *evaluation, result *Result) {
	result.Blocking = append([]string(nil), e.notRequired...)
	if len(e.notRequired) > 0 {
		result.Warnings = append(result.Warnings, "pending: "+strings.Join(e.notRequired, ", "))
	}
	if h.mergeableState != mergeableBlocked {
		return
	}
	decision, err := w.github.ReviewDecision(ctx, owner, repo, number)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("GitHub blocks the merge, and the review decision could not be read: %v", err))
		return
	}
	review := ""
	switch decision {
	case "REVIEW_REQUIRED":
		review = "review: an approving review the base requires is missing"
	case "CHANGES_REQUESTED":
		review = "review: a reviewer requested changes"
	default:
		return
	}
	result.Blocking = append(result.Blocking, review)
	result.Warnings = append(result.Warnings, "GitHub blocks the merge: "+strings.TrimPrefix(review, "review: "))
	w.progress.Printf("blocked: %s", review)
}

// mergeableBlocked is GitHub's mergeable state of a pull request a rule of
// the base keeps from merging: a required check or a required review.
const mergeableBlocked = "blocked"

// timedOut is the verdict at the deadline, 2: the result names what was
// unfinished, a required context still absent among it, and the reason the
// read the last poll could not get answered. A head settled with an absent
// context never reaches the deadline; neverReported ends it first.
func (w *Waiter) timedOut(result *Result, last *evaluation, unanswered string) error {
	result.Unfinished = unfinished(last)
	suffix := ""
	if unanswered != "" {
		suffix = "; the head is unknown, not failed: the last poll went unanswered: " + unanswered
	}
	if last == nil {
		return agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout, "timeout after %s before the first poll completed%s", w.timeout, suffix)
	}
	if len(last.red) > 0 {
		suffix = "; red already, besides the required contexts it waited for: " + last.redReason() + suffix
	}
	return agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout,
		"timeout after %s; unfinished: %s%s", w.timeout, strings.Join(last.unfinished, ", "), suffix)
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
	h.mergeableState = pr.GetMergeableState()
	if h.sha != "" && h.sha != sha {
		result.Warnings = append(result.Warnings, fmt.Sprintf("the head changed from %s to %s during the wait", h.sha, sha))
	}
	if h.sha != sha {
		h.sha, h.decided, h.circleci, h.circleJobs, h.workflowsRead = sha, false, nil, nil, false
		result.HeadSHA = sha
	}
	if h.baseRef != baseRef || h.required == nil {
		h.baseRef, h.workflowsRead = baseRef, false
		result.BaseRef = baseRef
		required, err := w.github.RequiredStatusContexts(ctx, owner, repo, baseRef)
		if err != nil {
			return nil, err
		}
		h.required = required
		w.progress.Printf("base %s requires %d context(s)", baseRef, len(required))
	}
	if !h.decided {
		if err := w.decideCircleCI(ctx, owner, repo, number, pr.GetHead().GetRef(), isFork(pr), pr.GetBase().GetRepo().GetIsTemplate(), h, result); err != nil {
			return nil, err
		}
	}

	s := snapshot{headSHA: sha, required: h.required, accepted: w.acceptRed, circleJobs: h.circleJobs}
	if err := w.readChecks(ctx, owner, repo, sha, &s, result); err != nil {
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
	e.quietFor = w.clock.Now().Sub(pr.GetUpdatedAt().Time)
	if err := w.expectPullRequestRuns(ctx, owner, repo, h, e); err != nil {
		return nil, err
	}
	w.awaitRegistration(e)
	result.Checks, result.Actions, result.CircleCI, result.AcceptedRed = e.checks, e.actions, e.circleci, e.acceptedRed
	return e, nil
}

// readChecks reads the head's check runs and statuses from REST's check-runs
// and combined-status endpoints. When those fail every try while GraphQL
// answers, it reads them from the head commit's statusCheckRollup instead,
// says so in a warning and Result.ChecksVia, and keeps reading GraphQL for the
// rest of the wait, so later polls do not spend the retry budget on REST
// again; a GraphQL read that fails then goes back to REST. With both paths
// failing the read is unanswered, as a REST read alone would be.
func (w *Waiter) readChecks(ctx context.Context, owner, repo, sha string, s *snapshot, result *Result) error {
	var graphQLErr error
	if result.ChecksVia == ChecksViaGraphQL {
		if s.checkRuns, s.statuses, graphQLErr = w.github.CheckRollup(ctx, owner, repo, sha); graphQLErr == nil {
			return nil
		}
		result.ChecksVia = ""
		w.progress.Printf("reading the checks through GraphQL failed (%v): REST again", graphQLErr)
	}
	err := w.readChecksREST(ctx, owner, repo, sha, s)
	var exhausted *agentcli.RetriesExhaustedError
	if err == nil || !errors.As(err, &exhausted) {
		return err
	}
	if graphQLErr == nil {
		if s.checkRuns, s.statuses, graphQLErr = w.github.CheckRollup(ctx, owner, repo, sha); graphQLErr == nil {
			result.ChecksVia = ChecksViaGraphQL
			result.Warnings = append(result.Warnings, fmt.Sprintf("the REST check endpoints went unanswered (%v): the head's checks are read through GraphQL (statusCheckRollup)", err))
			w.progress.Printf("REST check endpoints unanswered: reading the head's checks through GraphQL (statusCheckRollup)")
			return nil
		}
	}
	return fmt.Errorf("%w; reading them through GraphQL (statusCheckRollup) failed too: %v", err, graphQLErr)
}

// readChecksREST reads the head's check runs and statuses from REST.
func (w *Waiter) readChecksREST(ctx context.Context, owner, repo, sha string, s *snapshot) error {
	var err error
	if s.checkRuns, err = w.github.CheckRunsForRef(ctx, owner, repo, sha); err != nil {
		return err
	}
	s.statuses, err = w.github.CommitStatuses(ctx, owner, repo, sha)
	return err
}

// decideCircleCI settles whether CircleCI is part of this head's verdict: it
// is when the head carries CircleCIConfigPath and CircleCI has a project for
// the repository, unless the repository is a template whose team-file entry
// declares gen.ci.templateContent: that configuration is for the
// repositories created from it, and the template is never built itself. A
// template without the declaration carries its own pipeline and is judged
// like any repository, as the reconciler judges it. Whether the project has
// run a pipeline before does not decide it: the head's own may be the
// project's first, and judging the head from GitHub alone until it appeared
// would call a head green whose pipeline has not started. The token is required only past the first
// condition, so a repository without CircleCI needs GitHub alone.
func (w *Waiter) decideCircleCI(ctx context.Context, owner, repo string, number int, headRef string, fork, template bool, h *head, result *Result) error {
	h.decided = true
	if w.circleci == nil && !w.fromStatuses {
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
	if template {
		declared, err := w.templateContent(ctx, owner, repo)
		if err != nil {
			return err
		}
		if declared {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s/%s is a template repository: its %s is for the repositories created from it (gen.ci.templateContent), GitHub alone", owner, repo, CircleCIConfigPath))
			w.progress.Printf("%s/%s is a template repository: GitHub alone", owner, repo)
			return nil
		}
		w.progress.Printf("%s/%s is a template repository with its own pipeline (no gen.ci.templateContent)", owner, repo)
	}
	if w.fromStatuses {
		return w.listCircleJobs(ctx, owner, repo, circleciclient.PullRequestBranch(headRef, fork, number), h, result)
	}
	client, err := w.circleci(ctx)
	if err != nil {
		return err
	}
	if _, err := client.GetProject(ctx, owner, repo); err != nil {
		if circleciclient.IsNotFound(err) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s/%s has %s but no CircleCI project: GitHub alone", owner, repo, CircleCIConfigPath))
			w.progress.Printf("no CircleCI project for %s/%s: GitHub alone", owner, repo)
			return nil
		}
		return err
	}
	h.circleci = client
	h.project = fmt.Sprintf("github/%s/%s", owner, repo)
	h.branch = circleciclient.PullRequestBranch(headRef, fork, number)
	w.progress.Printf("CircleCI project %s, branch %s", h.project, h.branch)
	return nil
}

// listCircleJobs lists the jobs the head's CircleCI configuration runs on
// branch, the ones whose statuses the head waits for.
func (w *Waiter) listCircleJobs(ctx context.Context, owner, repo, branch string, h *head, result *Result) error {
	read := func(ctx context.Context, path string) ([]byte, bool, error) {
		return w.github.ReadFile(ctx, owner, repo, path, h.sha)
	}
	jobs, _, err := circleciconfig.List(ctx, read, circleciconfig.Run{Branch: branch})
	if err != nil {
		return fmt.Errorf("listing the CircleCI jobs of %s: %w", h.sha, err)
	}
	h.circleJobs = jobs.Names
	result.Warnings = append(result.Warnings, jobs.Warnings...)
	w.progress.Printf("CircleCI through its commit statuses: branch %s runs %s", branch, strings.Join(jobs.Names, ", "))
	return nil
}

// templateContent reports whether the repository's team-file entry declares
// its CircleCIConfigPath as template content (gen.ci.templateContent).
func (w *Waiter) templateContent(ctx context.Context, owner, repo string) (bool, error) {
	if w.entries == nil {
		return false, nil
	}
	fields, found, err := w.entries.FindEntry(ctx, owner, repo)
	if err != nil {
		return false, fmt.Errorf("reading the team-file entry of %s/%s: %w", owner, repo, err)
	}
	return found && fields.Gen != nil && fields.Gen.CI != nil && fields.Gen.CI.TemplateContent, nil
}

// readCircleCI finds the newest pipeline of the head revision on the head's
// branch and reads its workflows.
func (w *Waiter) readCircleCI(ctx context.Context, owner, repo string, h *head) (*circleSnapshot, error) {
	s := &circleSnapshot{project: h.project}
	pipeline, err := h.circleci.FindPipelineByRevision(ctx, owner, repo, h.branch, h.sha)
	if err != nil {
		return nil, err
	}
	s.pipeline = pipeline
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
