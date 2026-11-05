package merge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v93/github"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/mcpserver"
	"github.com/giantswarm/devctl/v8/pkg/prmerge"
	"github.com/giantswarm/devctl/v8/pkg/prmerge/prmergetest"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// serverSources hand devctl serve the merge double and its policy.
type serverSources struct {
	github *prmergetest.GitHub
	policy prmerge.Policy
}

func (s serverSources) PRWait(context.Context, string, http.RoundTripper) (mcpserver.PRWaitSources, error) {
	panic("not a wait")
}

func (s serverSources) PRMerge(context.Context, string, http.RoundTripper) (mcpserver.PRMergeSources, error) {
	return mcpserver.PRMergeSources{GitHub: s.github, Policy: s.policy}, nil
}

func (s serverSources) ReleaseWait(context.Context, string, http.RoundTripper) (*releasewait.Sources, error) {
	panic("not a release wait")
}

func (s serverSources) ReleasePromote(context.Context, string, http.RoundTripper) (*releasepromote.Sources, error) {
	panic("not a promotion")
}

type person string

func (p person) GitHubToken(context.Context) (string, error) { return string(p), nil }

// mergeState is one GitHub state of giantswarm/devctl#42 and the call made
// on it.
type mergeState struct {
	pr        func(pr *github.PullRequest)
	required  []string
	optedOut  bool
	acceptRed []string
	verdict   agentcli.Verdict
	exit      int
}

func (s mergeState) github() *prmergetest.GitHub {
	pr := prmergetest.PullRequest("giantswarm", "devctl", 42, "abc123", prmergetest.Login)
	if s.pr != nil {
		s.pr(pr)
	}
	gh := prmergetest.New(pr, prwaittest.Head{CheckRuns: []*github.CheckRun{
		prwaittest.CheckRun("test", "completed", "success"),
		prwaittest.CheckRun("lint", "completed", "success"),
	}})
	gh.Required = s.required
	gh.MergeCommitSHA = "m1"
	return gh
}

// TestToolRefusalsEqualCLI: for the same GitHub state, devctl serve's
// pr_merge refuses what devctl pr merge refuses, with the same document
// apart from its timestamps; a merge both make answers the same document
// too.
func TestToolRefusalsEqualCLI(t *testing.T) {
	tests := map[string]mergeState{
		"a draft": {
			pr:      func(pr *github.PullRequest) { pr.Draft = new(true) },
			verdict: agentcli.VerdictNotApplicable, exit: agentcli.ExitNotApplicable,
		},
		"a closed pull request": {
			pr:      func(pr *github.PullRequest) { pr.State = new("closed") },
			verdict: agentcli.VerdictNotApplicable, exit: agentcli.ExitNotApplicable,
		},
		"a conflicting pull request": {
			pr:      func(pr *github.PullRequest) { pr.MergeableState = new("dirty") },
			verdict: agentcli.VerdictNotApplicable, exit: agentcli.ExitNotApplicable,
		},
		"another human's pull request": {
			pr:      func(pr *github.PullRequest) { pr.User = &github.User{Login: new("teammate"), Type: new("User")} },
			verdict: agentcli.VerdictRefused, exit: agentcli.ExitRefused,
		},
		"a team with agentMerge: false": {
			optedOut: true,
			verdict:  agentcli.VerdictRefused, exit: agentcli.ExitRefused,
		},
		"acceptRed for a required check": {
			required: []string{"lint"}, acceptRed: []string{"lint"},
			verdict: agentcli.VerdictNotApplicable, exit: agentcli.ExitNotApplicable,
		},
		"acceptRed for a security scan": {
			acceptRed: []string{"trivy"},
			verdict:   agentcli.VerdictNotApplicable, exit: agentcli.ExitNotApplicable,
		},
		"a merge": {
			verdict: agentcli.VerdictGreen, exit: agentcli.ExitOK,
		},
	}
	for name, state := range tests {
		t.Run(name, func(t *testing.T) {
			cliGitHub, toolGitHub := state.github(), state.github()
			cli, tool := cliMergeDocument(t, cliGitHub, state), toolMergeDocument(t, toolGitHub, state)
			if cli["verdict"] != string(state.verdict) || cli["exitCode"] != float64(state.exit) {
				t.Errorf("the CLI answered %v (exit %v), want %s (exit %d): %v", cli["verdict"], cli["exitCode"], state.verdict, state.exit, cli["reason"])
			}
			for _, doc := range []map[string]any{cli, tool} {
				delete(doc, "startedAt")
				delete(doc, "finishedAt")
			}
			if diff := cmp.Diff(cli, tool); diff != "" {
				t.Errorf("the tool's document differs from the CLI's (-cli +tool):\n%s", diff)
			}
			if c, s := len(cliGitHub.Merges()), len(toolGitHub.Merges()); c != s || (state.exit == agentcli.ExitOK) != (s == 1) {
				t.Errorf("merge API requests: CLI %d, tool %d", c, s)
			}
		})
	}
}

// cliMergeDocument is what devctl pr merge giantswarm/devctl 42
// --no-release-wait prints for gh and state.
func cliMergeDocument(t *testing.T, gh *prmergetest.GitHub, state mergeState) map[string]any {
	t.Helper()
	stdout := &bytes.Buffer{}
	clock := agentcli.NewVirtualClock(time.Now())
	login := func(context.Context) (authstore.Token, error) {
		return authstore.Token{Value: "ghu_test", Login: prmergetest.Login, Source: authstore.SourceKeychain}, nil
	}
	r := &runner{
		gate:          func(bool) error { return nil },
		flag:          &flag{Timeout: prwait.DefaultTimeout, NoReleaseWait: true, AcceptRed: state.acceptRed, getenv: func(string) string { return "" }},
		stdout:        stdout,
		stderr:        io.Discard,
		requireGitHub: login,
		personGitHub:  login,
		requireCircleCI: func(context.Context) (authstore.Token, error) {
			t.Fatal("CircleCI was asked for")
			return authstore.Token{}, nil
		},
		endpoints: agentcli.DefaultEndpoints,
		clock:     func() (agentcli.Clock, error) { return clock, nil },
		github: func(githubclient.Config) (clients, error) {
			return clients{github: gh, policy: prmergetest.Policy(state.optedOut)}, nil
		},
	}
	_ = r.run(context.Background(), []string{"giantswarm/devctl", "42"})
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("the CLI printed no document: %v\n%s", err, stdout.String())
	}
	return doc
}

// toolMergeDocument is the structured content of devctl serve's pr_merge
// on giantswarm/devctl 42 with noReleaseWait for gh and state.
func toolMergeDocument(t *testing.T, gh *prmergetest.GitHub, state mergeState) map[string]any {
	t.Helper()
	clock := agentcli.NewVirtualClock(time.Now())
	srv := httptest.NewServer(mcpserver.New(mcpserver.Config{
		Credentials: person("ghu_person"),
		Sources:     serverSources{github: gh, policy: prmergetest.Policy(state.optedOut)},
		Clock:       &clock,
	}).Handler())
	t.Cleanup(srv.Close)
	c, err := client.NewStreamableHttpClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{Params: mcp.InitializeParams{ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION, ClientInfo: mcp.Implementation{Name: "test"}}}); err != nil {
		t.Fatal(err)
	}
	request := mcp.CallToolRequest{}
	request.Params.Name = mcpserver.ToolPRMerge
	args := map[string]any{"repository": "giantswarm/devctl", "number": 42, "noReleaseWait": true}
	if len(state.acceptRed) > 0 {
		args["acceptRed"] = state.acceptRed
	}
	request.Params.Arguments = args
	request.Params.Meta = &mcp.Meta{ProgressToken: "merge-1"}
	result, err := c.CallTool(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	doc, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("no structured content: %#v", result)
	}
	return doc
}
