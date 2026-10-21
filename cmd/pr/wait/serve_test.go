package wait

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
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

type serverSources struct{ github prwait.GitHub }

func (s serverSources) PRMerge(context.Context, string, http.RoundTripper) (mcpserver.PRMergeSources, error) {
	panic("not a merge")
}

func (s serverSources) ReleasePromote(context.Context, string, http.RoundTripper) (*releasepromote.Sources, error) {
	panic("not a promotion")
}

func (s serverSources) PRWait(context.Context, string, http.RoundTripper) (mcpserver.PRWaitSources, error) {
	return mcpserver.PRWaitSources{GitHub: s.github}, nil
}

func (s serverSources) ReleaseWait(context.Context, string, http.RoundTripper) (*releasewait.Sources, error) {
	panic("not a release wait")
}

type person string

func (p person) GitHubToken(context.Context) (string, error) { return string(p), nil }

// TestToolDocumentEqualsCLI: for the same GitHub state, devctl serve's
// pr_wait answers the document devctl pr wait prints, apart from its
// timestamps.
func TestToolDocumentEqualsCLI(t *testing.T) {
	tests := map[string]func() *prwaittest.GitHub{
		"green": func() *prwaittest.GitHub {
			return &prwaittest.GitHub{Heads: []prwaittest.Head{
				{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "in_progress", "")}},
				{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "completed", "success")}, Statuses: []*github.RepoStatus{prwaittest.Status("ci/circleci: build", "success")}},
			}}
		},
		"red": func() *prwaittest.GitHub {
			return &prwaittest.GitHub{Heads: []prwaittest.Head{
				{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "completed", "failure")}, Statuses: []*github.RepoStatus{prwaittest.Status("ci/circleci: build", "success")}},
			}}
		},
		"required missing": func() *prwaittest.GitHub {
			return &prwaittest.GitHub{
				Required: []string{"ci/circleci: build"},
				Heads:    []prwaittest.Head{{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "completed", "success")}}},
			}
		},
		"not applicable": func() *prwaittest.GitHub {
			pr := prwaittest.OpenPullRequest(7, "abc123", time.Now().Add(-time.Hour))
			pr.State = new("closed")
			return &prwaittest.GitHub{PR: pr}
		},
	}
	for name, state := range tests {
		t.Run(name, func(t *testing.T) {
			cli, tool := cliDocument(t, state()), toolDocument(t, state())
			for _, doc := range []map[string]any{cli, tool} {
				delete(doc, "startedAt")
				delete(doc, "finishedAt")
			}
			if diff := cmp.Diff(cli, tool); diff != "" {
				t.Errorf("the tool's document differs from the CLI's (-cli +tool):\n%s", diff)
			}
		})
	}
}

// cliDocument is what devctl pr wait giantswarm/devctl 7 prints for gh.
func cliDocument(t *testing.T, gh prwait.GitHub) map[string]any {
	t.Helper()
	stdout := &bytes.Buffer{}
	clock := agentcli.NewVirtualClock(time.Now())
	r := &runner{
		gate:          func(bool) error { return nil },
		flag:          &flag{Timeout: prwait.DefaultTimeout},
		stdout:        stdout,
		stderr:        io.Discard,
		requireGitHub: loggedInFromKeychain,
		personGitHub:  loggedInFromKeychain,
		requireCircleCI: func(context.Context) (authstore.Token, error) {
			t.Fatal("CircleCI was asked for")
			return authstore.Token{}, nil
		},
		endpoints: agentcli.DefaultEndpoints,
		clock:     func() (agentcli.Clock, error) { return clock, nil },
		github: func(githubclient.Config) (prwait.GitHub, prwait.RateSource, prwait.EntryFinder, error) {
			return gh, nil, nil, nil
		},
	}
	_ = r.run(context.Background(), []string{"giantswarm/devctl", "7"})
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("the CLI printed no document: %v\n%s", err, stdout.String())
	}
	return doc
}

// toolDocument is the structured content of devctl serve's pr_wait on
// giantswarm/devctl 7 for gh.
func toolDocument(t *testing.T, gh prwait.GitHub) map[string]any {
	t.Helper()
	clock := agentcli.NewVirtualClock(time.Now())
	srv := httptest.NewServer(mcpserver.New(mcpserver.Config{Credentials: person("ghu_person"), Sources: serverSources{gh}, Clock: &clock}).Handler())
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
	request.Params.Name = mcpserver.ToolPRWait
	request.Params.Arguments = map[string]any{"repository": "giantswarm/devctl", "number": 7}
	request.Params.Meta = &mcp.Meta{ProgressToken: "wait-1"}
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
