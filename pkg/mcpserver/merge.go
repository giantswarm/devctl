package mcpserver

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
	"github.com/giantswarm/devctl/v8/pkg/prmerge"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// ToolPRMerge is the merge tool's name.
const ToolPRMerge = "pr_merge"

func prMergeTool(noProgress time.Duration) mcp.Tool {
	return mcp.NewTool(ToolPRMerge,
		mcp.WithDescription("Merge a pull request as you, in one call: devctl pr merge. "+
			"Waits until the head is green, merges it with the judged head as the expected head, deletes the branch, then waits until the release it triggered is pullable. "+
			"Refuses what devctl refuses: a draft, closed or conflicting pull request (exit 3), another human's (exit 5), a repository whose team-file entry says agentMerge: false (exit 5), acceptRed naming a required check or a security scan (exit 3). "+
			"Safe to call again: a pull request merged already answers its merge and its release, with no second merge. "+
			progressDescription(noProgress)+
			"Without a progressToken the wait for green and the release wait share that timeout: a head not judged green by then is not merged, and a merge whose release is not confirmed yet is release_unconfirmed (exit 9); calling again merges nothing twice. "+
			"Answers devctl's document: verdict green once merged and released (or no release follows), release_failed (exit 6) or release_unconfirmed (exit 9) after the merge, or the wait's verdict, and its exitCode."),
		mcp.WithString("repository", mcp.Required(), mcp.Description("The repository, owner/repo.")),
		mcp.WithInteger("number", mcp.Required(), mcp.Description("The pull request's number.")),
		mcp.WithString("timeout", mcp.Description("How long the wait for green (and a merge queue) may take, a Go duration; with releaseTimeout at most 55m. Default 30m.")),
		mcp.WithString("releaseTimeout", mcp.Description("How long the release wait after the merge may take before exit 9, a Go duration; default the rest of 55m after timeout, at most 30m.")),
		mcp.WithBoolean("noReleaseWait", mcp.Description("End at the merge instead of waiting for its release.")),
		mcp.WithString("method", mcp.Enum(string(githubclient.MergeSquash), string(githubclient.MergeRebase)), mcp.Description("squash or rebase; default squash, rebase on a fork line.")),
		mcp.WithBoolean("updateBranch", mcp.Description("Update a head behind its base and wait for its checks again, instead of exit 3.")),
		mcp.WithArray("acceptRed", mcp.WithStringItems(), mcp.Description(prmerge.AcceptRedHelp())),
		writes(true),
	)
}

func (s *Server) prMerge(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	method := request.GetString("method", "")
	if method == "" {
		method = string(githubclient.MergeSquash)
	}
	doc := prmerge.NewDocument(time.Now(), method)
	job, err := s.runPRMerge(ctx, request, &doc)
	text, ferr := finish(&doc, agentcli.VerdictGreen, err)
	if ferr != nil {
		return nil, ferr
	}
	if job != nil {
		job.State, job.Document = mergejob.StateFinished, text
		s.record(*job)
	}
	return resultOf(&doc, text)
}

// mergeCall is a pr_merge call's arguments, checked.
type mergeCall struct {
	owner, repo    string
	number         int
	timeout        time.Duration
	releaseTimeout time.Duration
	noReleaseWait  bool
	method         githubclient.MergeMethod
	updateBranch   bool
	acceptRed      []string
	progressToken  mcp.ProgressToken
}

func checkMerge(request mcp.CallToolRequest, doc *prmerge.Document) (mergeCall, error) {
	var c mergeCall
	var err error
	if c.owner, c.repo, err = repository(request); err != nil {
		return c, err
	}
	c.number = request.GetInt("number", 0)
	doc.Repository, doc.Number = c.owner+"/"+c.repo, c.number
	if c.number <= 0 {
		return c, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "number must be a pull request's number, got %d", c.number)
	}
	if c.timeout, err = timeoutOf(request, DefaultTimeout); err != nil {
		return c, err
	}
	c.noReleaseWait = request.GetBool("noReleaseWait", false)
	if !c.noReleaseWait {
		if c.releaseTimeout, err = releaseTimeoutOf(request, c.timeout); err != nil {
			return c, err
		}
	}
	switch m := githubclient.MergeMethod(request.GetString("method", "")); m {
	case "", githubclient.MergeSquash, githubclient.MergeRebase:
		c.method = m
	default:
		return c, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "method is squash or rebase, got %q", m)
	}
	c.updateBranch = request.GetBool("updateBranch", false)
	c.acceptRed = request.GetStringSlice("acceptRed", nil)
	c.progressToken = progressToken(request)
	return c, nil
}

// releaseTimeoutOf is the call's release timeout: what is left of
// MaxTimeout after timeout, at most DefaultTimeout, without
// one; the two together at most MaxTimeout.
func releaseTimeoutOf(request mcp.CallToolRequest, timeout time.Duration) (time.Duration, error) {
	left := MaxTimeout - timeout
	arg := request.GetString("releaseTimeout", "")
	if arg == "" {
		if left <= 0 {
			return 0, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "timeout %s leaves no time of %s for the release wait: shorten it, or pass noReleaseWait", timeout, MaxTimeout)
		}
		return min(left, DefaultTimeout), nil
	}
	release, err := time.ParseDuration(arg)
	if err != nil || release <= 0 || release > left {
		return 0, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "releaseTimeout must be a duration above 0 and, with timeout %s, at most %s, got %q", timeout, MaxTimeout, arg)
	}
	return release, nil
}

// runPRMerge merges the pull request of request into doc and returns the
// merge's record once it started, for the caller to finish.
func (s *Server) runPRMerge(ctx context.Context, request mcp.CallToolRequest, doc *prmerge.Document) (*mergejob.Status, error) {
	c, err := checkMerge(request, doc)
	if err != nil {
		return nil, err
	}
	// Without a progressToken the whole call answers within the
	// no-progress timeout: the release wait gets what the wait for green
	// left of it.
	var deadline time.Time
	if c.progressToken == nil {
		deadline = s.clock.Now().Add(s.noProgressTimeout)
		c.timeout = s.bound(c.progressToken, ToolPRMerge, c.timeout, doc.Warn)
	}
	githubToken, err := s.credentials.GitHubToken(ctx)
	if err != nil {
		return nil, err
	}
	doc.Identity = "app"
	what := fmt.Sprintf("%s/%s#%d", c.owner, c.repo, c.number)

	var job *mergejob.Status
	err = s.keepAlive(ctx, c.progressToken, "the merge of "+what, func(progress *agentcli.Progress) error {
		// One merge of a pull request at a time: a second call waits for
		// the first and finds the pull request merged.
		unlock, err := s.pulls.lock(ctx, what, c.progressToken != nil, progress)
		if err != nil {
			return err
		}
		defer unlock()
		job = s.start(c.owner, c.repo, c.number)

		retrying := &agentcli.Retrying{Clock: s.clock, Progress: progress, Warn: doc.Warn}
		sources, err := s.sources.PRMerge(ctx, githubToken, retrying)
		if err != nil {
			return err
		}
		var release prmerge.ReleaseWait
		if !c.noReleaseWait {
			release = s.mergeRelease(githubToken, retrying, c.releaseTimeout, deadline, progress, doc.Warn)
		}
		merger, err := prmerge.New(prmerge.Config{
			Wait: prwait.Config{
				GitHub:  sources.GitHub,
				Rate:    sources.Rate,
				Entries: sources.Entries,
				// The server holds no CircleCI credential.
				CircleCIFromStatuses: true,
				Clock:                s.clock,
				Progress:             progress,
				Timeout:              c.timeout,
				AcceptRed:            c.acceptRed,
			},
			Method:       c.method,
			UpdateBranch: c.updateBranch,
			Policy:       sources.Policy,
			Release:      release,
			Idempotent:   true,
		})
		if err != nil {
			return err
		}
		result, err := merger.Merge(ctx, c.owner, c.repo, c.number)
		doc.Take(result)
		return err
	})
	return job, err
}

// mergeRelease is the release wait after a merge: release_wait's, on the
// merge commit, ending at deadline unless it is zero.
func (s *Server) mergeRelease(githubToken string, retrying *agentcli.Retrying, timeout time.Duration, deadline time.Time, progress *agentcli.Progress, warn func(string)) prmerge.ReleaseWait {
	return func(ctx context.Context, owner, repo string, number int, mergeCommitSHA string, result *releasewait.Result) error {
		if !deadline.IsZero() {
			left := deadline.Sub(s.clock.Now())
			if left <= 0 {
				return agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout,
					"the %s a call without a progressToken answers within ended with the merge", s.noProgressTimeout)
			}
			timeout = min(timeout, left)
		}
		sources, err := s.sources.ReleaseWait(ctx, githubToken, retrying)
		if err != nil {
			return err
		}
		waiter, err := releasewait.New(releasewait.Config{
			Owner:          owner,
			Repo:           repo,
			PR:             number,
			MergeCommitSHA: mergeCommitSHA,
			Timeout:        timeout,
			GitHub:         sources.GitHub,
			Entries:        sources.Entries,
			// The server holds no CircleCI credential.
			CircleCIFromStatuses: true,
			Registry:             sources.Registry,
			CatalogIndex:         sources.CatalogIndex,
			Endpoints:            s.endpoints,
			Clock:                s.clock,
			Rate:                 sources.Rate,
			Progress:             progress,
			Warn:                 warn,
		})
		if err != nil {
			return err
		}
		return waiter.Wait(ctx, result)
	}
}

// handles numbers the merges of this server.
var handles atomic.Int64

// start records a running merge of owner/repo#number, when the server's
// merges take records, and returns it.
func (s *Server) start(owner, repo string, number int) *mergejob.Status {
	now := time.Now().UTC()
	job := &mergejob.Status{
		Job: mergejob.Job{
			Handle:     fmt.Sprintf("%s-%s-%d-%s-%d", owner, repo, number, now.Format("20060102T150405Z"), handles.Add(1)),
			Repository: owner + "/" + repo,
			Number:     number,
			StartedAt:  now,
		},
		State: mergejob.StateRunning,
	}
	s.record(*job)
	return job
}

func (s *Server) record(job mergejob.Status) {
	if r, ok := s.merges.(recorder); ok {
		r.Put(job)
	}
}

// pulls are the pull requests a merge of this server runs on, each with the
// channel that is closed when it ends.
type pulls struct {
	mu      sync.Mutex
	running map[string]chan struct{}
}

// lock waits until no other merge of pull runs and takes it; the returned
// func gives it back. ctx ending first is its error. Without wait, another
// merge running is a timeout at once.
func (p *pulls) lock(ctx context.Context, pull string, wait bool, progress *agentcli.Progress) (func(), error) {
	for {
		p.mu.Lock()
		if p.running == nil {
			p.running = map[string]chan struct{}{}
		}
		done, busy := p.running[pull]
		if !busy {
			done = make(chan struct{})
			p.running[pull] = done
			p.mu.Unlock()
			return func() {
				p.mu.Lock()
				delete(p.running, pull)
				p.mu.Unlock()
				close(done)
			}, nil
		}
		p.mu.Unlock()
		if !wait {
			return nil, agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout, "another call merges %s; call again once it ended", pull)
		}
		progress.Printf("another call merges %s; waiting for it to end", pull)
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
