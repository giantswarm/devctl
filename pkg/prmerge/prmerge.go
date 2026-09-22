// Package prmerge is the engine behind `devctl pr merge`: the refusals that
// come before any wait, the wait of pkg/prwait, then the merge.
//
// A merge is refused before the first poll for a pull request no wait can
// turn green (draft, closed, conflicting, behind a strict base; exit 3), for
// a pull request another human opened (exit 5: bots, GitHub Apps and the
// caller are fine) and in a repository whose team-file entry opts out of
// agent merges (exit 5, naming the field). Green lands through the merge
// API as a squash or a rebase with the judged head as the expected head,
// then the branch goes through the refs API. A base with a merge queue is
// enqueued instead and the pull request waited for. No protection setting,
// ruleset or enforce_admins is read to be changed, or written.
//
// The merge is made as the caller: the token is the person's (devctl holds
// no installation token), so the bypass GitHub honours is the person's, the
// owning team's as the alignment engine writes it, never the App's. A merge
// the review rule declines is exit 3 with GitHub's sentence and the
// ruleset's bypass actors, so the caller knows whose review or merge it
// takes.
package prmerge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

const (
	// mergeableReads bounds the re-reads of a pull request whose mergeable
	// state GitHub has not computed yet; mergeableInterval is between them.
	mergeableReads    = 5
	mergeableInterval = 3 * time.Second
	// updatedHeadReads bounds the re-reads after an update-branch until the
	// new head appears; updatedHeadInterval is between them.
	updatedHeadReads    = 20
	updatedHeadInterval = 3 * time.Second
)

// Config configures a Merger.
type Config struct {
	// Wait is the wait engine's configuration; its GitHub client, clock,
	// progress writer and timeout are the merge's too. Required.
	Wait prwait.Config
	// Method is squash or rebase; empty is squash.
	Method githubclient.MergeMethod
	// UpdateBranch: a head behind a strict base is updated from the base
	// and the new head waited for, instead of exit 3.
	UpdateBranch bool
	// Login is the account the token acts as, for the author check; empty
	// reads it from GET /user.
	Login string
	// Policy is the repository's opt-out; nil is [TeamFilePolicy] on the
	// GitHub client.
	Policy Policy
}

// Result is the command's part of the document: the wait's fields and the
// merge's.
type Result struct {
	prwait.Result
	// MergeCommitSHA is the commit the merge produced; empty when nothing
	// merged.
	MergeCommitSHA string `json:"mergeCommitSha"`
	// Method is squash or rebase.
	Method string `json:"method"`
	// BranchDeleted: the head branch was deleted after the merge (or was
	// gone already). False for a head in a fork, which is left alone.
	BranchDeleted bool `json:"branchDeleted"`
	// Enqueued: the base has a merge queue and the pull request went
	// through it.
	Enqueued bool `json:"enqueued"`
}

// Merger runs merges.
type Merger struct {
	github       *githubclient.Client
	waiter       *prwait.Waiter
	clock        agentcli.Clock
	progress     *agentcli.Progress
	timeout      time.Duration
	method       githubclient.MergeMethod
	updateBranch bool
	login        string
	policy       Policy
}

// New returns a Merger for config.
func New(config Config) (*Merger, error) {
	if config.Wait.GitHub == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Wait.GitHub must not be nil", config)
	}
	waiter, err := prwait.New(config.Wait)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	m := &Merger{
		github:       config.Wait.GitHub,
		waiter:       waiter,
		clock:        config.Wait.Clock,
		progress:     config.Wait.Progress,
		timeout:      config.Wait.Timeout,
		method:       config.Method,
		updateBranch: config.UpdateBranch,
		login:        config.Login,
		policy:       config.Policy,
	}
	if m.clock.Scale() == 0 {
		m.clock = agentcli.NewClock(1, nil)
	}
	if m.progress == nil {
		m.progress = agentcli.NewProgress(nil, false)
	}
	if m.timeout <= 0 {
		m.timeout = prwait.DefaultTimeout
	}
	switch m.method {
	case "":
		m.method = githubclient.MergeSquash
	case githubclient.MergeSquash, githubclient.MergeRebase:
	default:
		return nil, microerror.Maskf(invalidConfigError, "%T.Method is squash or rebase, got %q", config, config.Method)
	}
	if m.policy == nil {
		m.policy = TeamFilePolicy(m.github.GitHub())
	}
	return m, nil
}

// Merge refuses, waits and merges owner/repo#number. The Result is always
// returned, as far as it was filled; the error is nil on a merge, an
// *agentcli.ExitError with the code of the table otherwise, or a tooling
// failure.
func (m *Merger) Merge(ctx context.Context, owner, repo string, number int) (*Result, error) {
	result := &Result{
		Result: prwait.Result{
			Repository: owner + "/" + repo,
			Number:     number,
			Checks:     []prwait.Check{},
			Actions:    []prwait.ActionRun{},
		},
		Method: string(m.method),
	}

	pr, err := m.readMergeable(ctx, owner, repo, number)
	if err != nil {
		return result, err
	}
	result.HeadSHA, result.BaseRef = pr.GetHead().GetSHA(), pr.GetBase().GetRef()

	caller, team, err := m.refuse(ctx, owner, repo, pr)
	if err != nil {
		return result, err
	}
	m.progress.Printf("refusals: none; %s by %s", pr.GetHead().GetSHA(), pr.GetUser().GetLogin())

	if pr.GetMergeableState() == "behind" {
		// Only reached with --update-branch: refuse() said exit 3 otherwise.
		if err := m.update(ctx, owner, repo, number, pr.GetHead().GetSHA()); err != nil {
			return result, err
		}
	}

	waited, err := m.waiter.Wait(ctx, owner, repo, number)
	if waited != nil {
		result.Result = *waited
	}
	if err != nil {
		return result, err
	}

	// The pull request as it stands after the wait: the title a retitle
	// gave it, the branch to delete, the node the queue takes.
	pr, err = m.github.PullRequest(ctx, owner, repo, number)
	if err != nil {
		return result, microerror.Mask(err)
	}
	if err := prwait.NotApplicable(pr); err != nil {
		return result, err
	}

	queued, err := m.github.MergeQueueRequired(ctx, owner, repo, pr.GetBase().GetRef())
	if err != nil {
		return result, microerror.Mask(err)
	}
	if queued {
		err = m.enqueue(ctx, owner, repo, number, pr, result)
	} else {
		err = m.merge(ctx, owner, repo, number, pr, result, caller, team)
	}
	if err != nil {
		return result, err
	}

	if prwait.IsFork(pr) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("the head %s lives in the fork %s; the branch is left alone", pr.GetHead().GetRef(), pr.GetHead().GetRepo().GetFullName()))
		return result, nil
	}
	if err := m.github.DeleteBranch(ctx, owner, repo, pr.GetHead().GetRef()); err != nil {
		return result, microerror.Mask(err)
	}
	result.BranchDeleted = true
	m.progress.Printf("branch %s deleted", pr.GetHead().GetRef())
	return result, nil
}

// readMergeable reads the pull request, re-reading a mergeable state GitHub
// has not computed yet a bounded number of times: the refusals need it.
func (m *Merger) readMergeable(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error) {
	for read := 1; ; read++ {
		pr, err := m.github.PullRequest(ctx, owner, repo, number)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		if pr.GetState() != "open" || pr.GetMergeableState() != "unknown" || read == mergeableReads {
			return pr, nil
		}
		m.progress.Printf("read %d: mergeable state not computed yet; reading again in %s", read, mergeableInterval)
		if err := m.clock.Sleep(ctx, mergeableInterval); err != nil {
			return nil, microerror.Mask(err)
		}
	}
}

// refuse is every refusal that comes before the wait, in order: the pull
// request's state (3), its author (5), the repository's opt-out (5). It
// returns the caller the token acts as and the team whose file declares
// the repository (empty when none does), for the merge's reasons.
func (m *Merger) refuse(ctx context.Context, owner, repo string, pr *github.PullRequest) (caller, team string, err error) {
	if !m.updateBranch || pr.GetMergeableState() != "behind" {
		if err := prwait.NotApplicable(pr); err != nil {
			return "", "", err
		}
	}

	caller = m.login
	if caller == "" {
		caller, err = m.github.CurrentLogin(ctx)
		if err != nil {
			return "", "", microerror.Mask(err)
		}
	}
	if !authorAllowed(pr, caller) {
		return "", "", agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused,
			"the pull request was opened by %s, not by %s: devctl pr merge merges the caller's own pull requests and bots' only", pr.GetUser().GetLogin(), caller)
	}

	verdict, err := m.policy(ctx, owner, repo)
	if err != nil {
		return "", "", microerror.Mask(err)
	}
	if verdict.Refusal != "" {
		return "", "", agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused, "%s", verdict.Refusal)
	}
	return caller, verdict.Team, nil
}

// authorAllowed: the author is a bot or a GitHub App (type Bot or a [bot]
// login) or the caller. Another human's pull request is refused.
func authorAllowed(pr *github.PullRequest, caller string) bool {
	user := pr.GetUser()
	login := user.GetLogin()
	return user.GetType() == "Bot" || strings.HasSuffix(login, "[bot]") || strings.EqualFold(login, caller)
}

// update asks GitHub to merge the base into the head and reads the pull
// request until the new head is on it, so the wait judges that head and not
// the one behind.
func (m *Merger) update(ctx context.Context, owner, repo string, number int, headSHA string) error {
	if err := m.github.UpdatePullRequestBranch(ctx, owner, repo, number, headSHA); err != nil {
		return microerror.Mask(err)
	}
	m.progress.Printf("update-branch: requested for %s", headSHA)
	for read := 1; read <= updatedHeadReads; read++ {
		if err := m.clock.Sleep(ctx, updatedHeadInterval); err != nil {
			return microerror.Mask(err)
		}
		pr, err := m.github.PullRequest(ctx, owner, repo, number)
		if err != nil {
			return microerror.Mask(err)
		}
		if sha := pr.GetHead().GetSHA(); sha != headSHA {
			m.progress.Printf("update-branch: new head %s", sha)
			return nil
		}
	}
	return microerror.Maskf(executionError, "update-branch: GitHub did not put a new head on %s/%s#%d within %s", owner, repo, number, time.Duration(updatedHeadReads)*updatedHeadInterval)
}

// merge lands the head through the merge API with the judged head as the
// expected head. A merge GitHub declines as the pull request stands is
// exit 3 with GitHub's sentence; declined for the review rule, the reason
// goes on to name the caller, the rulesets' bypass actors and the owning
// team (explainReviewRule).
func (m *Merger) merge(ctx context.Context, owner, repo string, number int, pr *github.PullRequest, result *Result, caller, team string) error {
	opts := githubclient.MergeOptions{Method: m.method, HeadSHA: result.HeadSHA}
	if m.method == githubclient.MergeSquash {
		opts.CommitTitle = fmt.Sprintf("%s (#%d)", strings.TrimSpace(pr.GetTitle()), number)
	}
	sha, err := m.github.MergePullRequest(ctx, owner, repo, number, opts)
	if githubclient.IsMergeDeclined(err) {
		reason := err.Error()
		if declinedByReviewRule(err) {
			reason = strings.TrimSuffix(reason, ".") + ". " + m.explainReviewRule(ctx, owner, repo, pr.GetBase().GetRef(), caller, team)
		}
		return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "%s", reason)
	}
	if err != nil {
		return microerror.Mask(err)
	}
	result.MergeCommitSHA = sha
	m.progress.Printf("merged: %s of %s is %s", m.method, result.HeadSHA, sha)
	return nil
}

// enqueue puts the pull request on the base's merge queue and waits, within
// the timeout, until the queue merged it. A pull request the queue drops
// (closed, not merged) is red; the deadline is exit 2.
func (m *Merger) enqueue(ctx context.Context, owner, repo string, number int, pr *github.PullRequest, result *Result) error {
	if err := m.github.EnqueuePullRequest(ctx, pr.GetNodeID()); err != nil {
		return microerror.Mask(err)
	}
	result.Enqueued = true
	m.progress.Printf("merge queue: %s enqueued on %s", result.HeadSHA, pr.GetBase().GetRef())

	ctx, cancel := m.clock.Timeout(ctx, m.timeout)
	defer cancel()
	for poll := 1; ; poll++ {
		current, err := m.github.PullRequest(ctx, owner, repo, number)
		if err != nil {
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				return m.queueTimedOut(result)
			}
			return microerror.Mask(err)
		}
		switch {
		case current.GetMerged():
			result.MergeCommitSHA = current.GetMergeCommitSHA()
			m.progress.Printf("merge queue: poll %d: merged as %s", poll, result.MergeCommitSHA)
			return nil
		case current.GetState() != "open":
			return agentcli.NewExitError(agentcli.ExitRed, agentcli.VerdictRed, "the merge queue removed %s/%s#%d without merging it", owner, repo, number)
		}
		m.progress.Printf("merge queue: poll %d: not merged yet; next poll in %s", poll, prwait.MinInterval)
		if err := m.clock.Sleep(ctx, prwait.MinInterval); err != nil {
			return m.queueTimedOut(result)
		}
	}
}

func (m *Merger) queueTimedOut(result *Result) error {
	result.Unfinished = []string{fmt.Sprintf("merge queue: %s#%d not merged", result.Repository, result.Number)}
	return agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout, "timeout after %s in the merge queue of %s", m.timeout, result.BaseRef)
}
