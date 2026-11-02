// Package prwaittest is an in-process test double of what a pull request
// wait reads from GitHub: a [prwait.GitHub] answering from state a test
// sets, one head state per poll.
package prwaittest

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

// Head is what a pull request's head carries at one poll.
type Head struct {
	CheckRuns    []*github.CheckRun
	Statuses     []*github.RepoStatus
	WorkflowRuns []*github.WorkflowRun
}

// GitHub is a [prwait.GitHub] over the state of one pull request. Each read
// of the pull request starts a poll; the poll reads Heads at its index, the
// last one once the polls run past them.
type GitHub struct {
	// PR is the pull request; nil is an open one, its head "abc123"
	// on "feature", its base "main", last updated an hour ago.
	PR *github.PullRequest
	// Heads are the head's states, one per poll; none is a head without
	// any check.
	Heads []Head
	// Required are the contexts the base requires.
	Required []string
	// Decision is GraphQL's reviewDecision of the pull request.
	Decision string
	// Files are the repository's files at the head by path: its workflows
	// under .github/workflows.
	Files map[string]string
	// Message is the head commit's message.
	Message string

	mu    sync.Mutex
	polls int
}

var _ prwait.GitHub = &GitHub{}

// Polls is how many polls read the pull request.
func (g *GitHub) Polls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.polls
}

func (g *GitHub) head() Head {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.Heads) == 0 {
		return Head{}
	}
	i := min(max(g.polls-1, 0), len(g.Heads)-1)
	return g.Heads[i]
}

// OpenPullRequest is an open pull request of head sha, updated at updated.
func OpenPullRequest(number int, sha string, updated time.Time) *github.PullRequest {
	return &github.PullRequest{
		Number:         new(number),
		State:          new("open"),
		MergeableState: new("clean"),
		UpdatedAt:      &github.Timestamp{Time: updated},
		Head:           &github.PullRequestBranch{SHA: new(sha), Ref: new("feature")},
		Base:           &github.PullRequestBranch{Ref: new("main"), Repo: &github.Repository{}},
	}
}

func (g *GitHub) PullRequest(_ context.Context, _, _ string, number int) (*github.PullRequest, error) {
	g.mu.Lock()
	g.polls++
	g.mu.Unlock()
	if g.PR == nil {
		return OpenPullRequest(number, "abc123", time.Now().Add(-time.Hour)), nil
	}
	return g.PR, nil
}

func (g *GitHub) ReviewDecision(context.Context, string, string, int) (string, error) {
	return g.Decision, nil
}

func (g *GitHub) RequiredStatusContexts(context.Context, string, string, string) ([]string, error) {
	return append([]string{}, g.Required...), nil
}

func (g *GitHub) WorkflowRunsForSHA(context.Context, string, string, string) ([]*github.WorkflowRun, error) {
	return g.head().WorkflowRuns, nil
}

func (g *GitHub) CheckRollup(context.Context, string, string, string) ([]*github.CheckRun, []*github.RepoStatus, error) {
	h := g.head()
	return h.CheckRuns, h.Statuses, nil
}

func (g *GitHub) CheckRunsForRef(context.Context, string, string, string) ([]*github.CheckRun, error) {
	return g.head().CheckRuns, nil
}

func (g *GitHub) CommitStatuses(context.Context, string, string, string) ([]*github.RepoStatus, error) {
	return g.head().Statuses, nil
}

func (g *GitHub) FileExists(_ context.Context, _, _, file, _ string) (bool, error) {
	_, ok := g.Files[file]
	return ok, nil
}

func (g *GitHub) CommitMessage(context.Context, string, string, string) (string, error) {
	return g.Message, nil
}

func (g *GitHub) ListDirectory(_ context.Context, _, _, dir, _ string) ([]string, error) {
	names := []string{}
	for file := range g.Files {
		if path.Dir(file) == dir {
			names = append(names, path.Base(file))
		}
	}
	return names, nil
}

func (g *GitHub) InactiveWorkflowPaths(context.Context, string, string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (g *GitHub) ReadFile(_ context.Context, _, _, file, _ string) ([]byte, bool, error) {
	content, ok := g.Files[file]
	return []byte(content), ok, nil
}

func (g *GitHub) JobLog(_ context.Context, _, _ string, jobID int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(fmt.Sprintf("log of job %d\n", jobID))), nil
}

// CheckRun is a check run of name in status, with conclusion once completed.
func CheckRun(name, status, conclusion string) *github.CheckRun {
	run := &github.CheckRun{
		ID:      new(int64(len(name))),
		Name:    new(name),
		Status:  new(status),
		HTMLURL: new("https://github.com/o/r/runs/" + name),
		App:     &github.App{Slug: new("github-actions")},
	}
	if conclusion != "" {
		run.Conclusion = new(conclusion)
	}
	return run
}

// Status is a commit status of context in state.
func Status(context, state string) *github.RepoStatus {
	return &github.RepoStatus{Context: new(context), State: new(state), TargetURL: new("https://ci.example/" + context)}
}

// WorkflowRun is an Actions run of the workflow at file in status, with
// conclusion once completed, started by a pull_request event.
func WorkflowRun(name, file, status, conclusion string) *github.WorkflowRun {
	run := &github.WorkflowRun{
		ID:      new(int64(len(name))),
		Name:    new(name),
		Path:    new(file),
		Event:   new("pull_request"),
		Status:  new(status),
		HTMLURL: new("https://github.com/o/r/actions/runs/" + name),
	}
	if conclusion != "" {
		run.Conclusion = new(conclusion)
	}
	return run
}
