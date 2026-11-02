// Package prmergetest is an in-process test double of what a merge reads
// from and writes to GitHub: a [prmerge.GitHub] answering from state a test
// sets, the wait's reads those of [prwaittest.GitHub]. A merge call merges
// the pull request: every read after it answers it merged.
package prmergetest

import (
	"context"
	"sync"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prmerge"
	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
)

// Login is the login the double's token acts as and its pull requests'
// author, unless a test says otherwise.
const Login = "someone"

// GitHub is a [prmerge.GitHub] over one pull request, PR of the embedded
// wait double, which a test sets ([PullRequest] is one).
type GitHub struct {
	*prwaittest.GitHub
	// Login is who the token acts as; empty is [Login].
	Login string
	// MergeCommitSHA is the commit a merge call produces.
	MergeCommitSHA string
	// Settings are the repository's merge settings; nil allows every
	// method.
	Settings *github.Repository

	mu     sync.Mutex
	merges []githubclient.MergeOptions
	merged bool
}

var _ prmerge.GitHub = &GitHub{}

// New is the double of pr, whose head carries heads at each poll.
func New(pr *github.PullRequest, heads ...prwaittest.Head) *GitHub {
	return &GitHub{GitHub: &prwaittest.GitHub{PR: pr, Heads: heads}}
}

// PullRequest is owner/repo#number, open and clean, its head sha on the
// branch feature of the same repository, opened by author an hour ago.
func PullRequest(owner, repo string, number int, sha, author string) *github.PullRequest {
	pr := prwaittest.OpenPullRequest(number, sha, time.Now().Add(-time.Hour))
	full := owner + "/" + repo
	pr.Title = new("feat: thing")
	pr.NodeID = new("PR_1")
	pr.User = &github.User{Login: new(author), Type: new("User")}
	pr.Head.Repo = &github.Repository{FullName: new(full)}
	pr.Base.Repo = &github.Repository{FullName: new(full)}
	return pr
}

// Merges are the merge calls made, in order.
func (g *GitHub) Merges() []githubclient.MergeOptions {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]githubclient.MergeOptions{}, g.merges...)
}

func (g *GitHub) login() string {
	if g.Login == "" {
		return Login
	}
	return g.Login
}

// PullRequest is the wait double's pull request, merged once a merge call
// merged it.
func (g *GitHub) PullRequest(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error) {
	pr, err := g.GitHub.PullRequest(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.merged {
		return pr, nil
	}
	merged := *pr
	merged.State, merged.Merged = new("closed"), new(true)
	merged.MergeableState = new("unknown")
	merged.MergeCommitSHA = new(g.MergeCommitSHA)
	merged.MergedBy = &github.User{Login: new(g.login())}
	return &merged, nil
}

func (g *GitHub) MergePullRequest(_ context.Context, _, _ string, _ int, opts githubclient.MergeOptions) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.merges = append(g.merges, opts)
	g.merged = true
	return g.MergeCommitSHA, nil
}

func (g *GitHub) CurrentLogin(context.Context) (string, error) { return g.login(), nil }

func (g *GitHub) Repository(context.Context, string, string) (*github.Repository, error) {
	if g.Settings != nil {
		return g.Settings, nil
	}
	return &github.Repository{AllowSquashMerge: new(true), AllowRebaseMerge: new(true), AllowMergeCommit: new(true)}, nil
}

func (g *GitHub) MergeQueueRequired(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (g *GitHub) BehindBy(context.Context, string, string, string, string) (int, error) {
	return 0, nil
}

func (g *GitHub) PullRequestReviews(context.Context, string, string, int) ([]*github.PullRequestReview, error) {
	return nil, nil
}

func (g *GitHub) PullRequestReviewComments(context.Context, string, string, int) ([]*github.PullRequestComment, error) {
	return nil, nil
}

func (g *GitHub) IssueComments(context.Context, string, string, int) ([]*github.IssueComment, error) {
	return nil, nil
}

func (g *GitHub) CommitTime(context.Context, string, string, string) (time.Time, error) {
	return time.Now().Add(-time.Hour), nil
}

func (g *GitHub) IsPullRequest(context.Context, string, string, int) (bool, error) {
	return false, nil
}

func (g *GitHub) RulesForBranch(context.Context, string, string, string) (*github.BranchRules, error) {
	return &github.BranchRules{}, nil
}

func (g *GitHub) RepositoryRuleset(context.Context, string, string, int64) (*github.RepositoryRuleset, error) {
	return &github.RepositoryRuleset{}, nil
}

func (g *GitHub) OrganizationRuleset(context.Context, string, int64) (*github.RepositoryRuleset, error) {
	return &github.RepositoryRuleset{}, nil
}

func (g *GitHub) UpdatePullRequestBranch(context.Context, string, string, int, string) error {
	return nil
}

func (g *GitHub) RebasePullRequestBranch(context.Context, string, string) error { return nil }

func (g *GitHub) EnqueuePullRequest(context.Context, string) error { return nil }

func (g *GitHub) DeleteBranch(context.Context, string, string, string) error { return nil }

func (g *GitHub) DefaultBranch(context.Context, string, string) (string, error) { return "main", nil }

func (g *GitHub) DispatchWorkflow(context.Context, string, string, string, string, map[string]any) error {
	return nil
}

// Policy is the opt-out of a repository whose team-file entry says
// agentMerge: false when optedOut, as [prmerge.TeamFilePolicy] words it.
func Policy(optedOut bool) prmerge.Policy {
	return func(_ context.Context, _, repo string) (prmerge.Verdict, error) {
		verdict := prmerge.Verdict{Team: "team-test", Entry: "the entry " + repo + " in repositories/team-test.yaml of giantswarm/github"}
		if optedOut {
			verdict.Refusal = verdict.Entry + " says " + prmerge.AgentMergeField + ": false: agents do not merge in this repository"
		}
		return verdict, nil
	}
}
