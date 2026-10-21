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
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mcpserver"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

type serverSources struct{ release *releasewait.Sources }

func (s serverSources) PRMerge(context.Context, string, http.RoundTripper) (mcpserver.PRMergeSources, error) {
	panic("not a merge")
}

func (s serverSources) ReleasePromote(context.Context, string, http.RoundTripper) (*releasepromote.Sources, error) {
	panic("not a promotion")
}

func (s serverSources) PRWait(context.Context, string, http.RoundTripper) (mcpserver.PRWaitSources, error) {
	panic("not a pull request wait")
}

func (s serverSources) ReleaseWait(context.Context, string, http.RoundTripper) (*releasewait.Sources, error) {
	return s.release, nil
}

type person string

func (p person) GitHubToken(context.Context) (string, error) { return string(p), nil }

// TestToolDocumentEqualsCLI: for the same GitHub, CircleCI and registry
// state, devctl serve's release_wait answers the document devctl release
// wait prints, apart from its timestamps.
func TestToolDocumentEqualsCLI(t *testing.T) {
	for _, workflow := range []string{"success", "failed"} {
		t.Run(workflow, func(t *testing.T) {
			cli, tool := cliDocument(t, sources(t, workflow)), toolDocument(t, sources(t, workflow))
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

func sources(t *testing.T, workflow string) *releasewait.Sources {
	t.Helper()
	s, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", &releasewaittest.Registry{}, workflow)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// cliDocument is what devctl release wait giantswarm/devctl v1.2.3 prints.
func cliDocument(t *testing.T, s *releasewait.Sources) map[string]any {
	t.Helper()
	stdout := &bytes.Buffer{}
	r := &runner{
		gate:   func(bool) error { return nil },
		flag:   &flag{Timeout: releasewait.DefaultTimeout},
		stdout: stdout,
		stderr: io.Discard,
		open: func(context.Context, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
			return s, nil
		},
	}
	_ = r.run(context.Background(), []string{"giantswarm/devctl", "v1.2.3"})
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("the CLI printed no document: %v\n%s", err, stdout.String())
	}
	return doc
}

// toolDocument is the structured content of devctl serve's release_wait on
// giantswarm/devctl v1.2.3.
func toolDocument(t *testing.T, s *releasewait.Sources) map[string]any {
	t.Helper()
	clock := agentcli.NewVirtualClock(time.Now())
	srv := httptest.NewServer(mcpserver.New(mcpserver.Config{Credentials: person("ghu_person"), Sources: serverSources{s}, Clock: &clock}).Handler())
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
	request.Params.Name = mcpserver.ToolReleaseWait
	request.Params.Arguments = map[string]any{"repository": "giantswarm/devctl", "version": "v1.2.3"}
	request.Params.Meta = &mcp.Meta{ProgressToken: "release-1"}
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
