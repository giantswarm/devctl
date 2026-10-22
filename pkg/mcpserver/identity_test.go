package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mcpserver"
	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
)

const app = "github"

// installed answers the owners the App is installed on, the same for every
// token, and records the tokens it was asked with.
type installed struct {
	owners []string

	mu     sync.Mutex
	tokens []string
}

func (i *installed) Owners(_ context.Context, token string) ([]string, error) {
	i.mu.Lock()
	i.tokens = append(i.tokens, token)
	i.mu.Unlock()
	return i.owners, nil
}

// users answer the same caller for every token, or err, and record the
// tokens they were asked with.
type users struct {
	caller mcpserver.Caller
	err    error

	mu     sync.Mutex
	tokens []string
}

func (u *users) User(_ context.Context, token string) (mcpserver.Caller, error) {
	u.mu.Lock()
	u.tokens = append(u.tokens, token)
	u.mu.Unlock()
	return u.caller, u.err
}

var alice = mcpserver.Caller{Login: "alice", ID: 4711}

// connectAs serves config over streamable HTTP and returns a client whose
// every request carries headers, as muster sends them.
func connectAs(t *testing.T, config mcpserver.Config, headers map[string]string) (*client.Client, error) {
	t.Helper()
	srv := httptest.NewServer(mcpserver.New(config).Handler())
	t.Cleanup(srv.Close)
	c, err := client.NewStreamableHttpClient(srv.URL, transport.WithHTTPHeaders(headers))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		return nil, err
	}
	_, err = c.Initialize(ctx, mcp.InitializeRequest{Params: mcp.InitializeParams{
		ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		ClientInfo:      mcp.Implementation{Name: "test", Version: "0"},
	}})
	return c, err
}

func greenHead() *prwaittest.GitHub {
	return &prwaittest.GitHub{Heads: []prwaittest.Head{
		{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "completed", "success")}},
	}}
}

func prWaitArgs(repository string) map[string]any {
	return map[string]any{"repository": repository, "number": 7}
}

// TestGrantRequired: a person who has not granted the App gets auth_required
// marked grant_required for it, which muster answers with the App's
// sign-in link; no GitHub client is built.
func TestGrantRequired(t *testing.T) {
	s := &sources{pr: greenHead()}
	c, err := connectAs(t, mcpserver.Config{
		Credentials: mcpserver.MusterGrant{App: app, GitHubApp: "gazelle-agents", Installations: &installed{owners: []string{"giantswarm"}}},
		Sources:     s,
		Clock:       virtual(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, doc := call(t, c, mcpserver.ToolPRWait, prWaitArgs("giantswarm/devctl"), "w")
	assertVerdict(t, doc, agentcli.VerdictAuthRequired, agentcli.ExitAuthRequired)
	if !result.IsError {
		t.Error("an auth_required answer is not an MCP error")
	}
	if doc["status"] != mcpserver.StatusGrantRequired || !slices.Equal(anySlice(doc["apps"]), []any{app}) {
		t.Errorf("status %v, apps %v, want %s, [%s]", doc["status"], doc["apps"], mcpserver.StatusGrantRequired, app)
	}
	if reason, _ := doc["reason"].(string); !strings.Contains(reason, "gazelle-agents") || !strings.Contains(reason, "core_auth_login app=github") {
		t.Errorf("reason %q names neither the App nor how to grant it", reason)
	}
	if len(s.tokens) != 0 {
		t.Errorf("clients were built without a grant: %v", s.tokens)
	}
}

// TestGrantActsAsThePerson: the token muster attaches for the App is the
// one every client of the call is built with, the one the caller is read
// with, and the one the App's installations are read with.
func TestGrantActsAsThePerson(t *testing.T) {
	s := &sources{pr: greenHead()}
	i := &installed{owners: []string{"GiantSwarm"}}
	u := &users{caller: alice}
	c, err := connectAs(t, mcpserver.Config{
		Credentials: mcpserver.MusterGrant{App: app, Users: u, Installations: i},
		Sources:     s,
		Clock:       virtual(),
	}, map[string]string{
		mcpserver.GrantHeaderPrefix + app:     "ghu_person",
		mcpserver.GrantHeaderPrefix + "other": "ghu_other_app",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, doc := call(t, c, mcpserver.ToolPRWait, prWaitArgs("giantswarm/devctl"), "w")
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)
	if !slices.Equal(s.tokens, []string{"ghu_person"}) || !slices.Equal(u.tokens, []string{"ghu_person"}) || !slices.Equal(i.tokens, []string{"ghu_person"}) {
		t.Errorf("clients built with %v, caller read with %v, installations read with %v, want the person's token of %s only", s.tokens, u.tokens, i.tokens, app)
	}
}

// TestRepositoryOutsideTheApp: a repository whose owner the App is not
// installed on is not_applicable, naming the owner and the App.
func TestRepositoryOutsideTheApp(t *testing.T) {
	s := &sources{pr: greenHead()}
	c, err := connectAs(t, mcpserver.Config{
		Credentials: mcpserver.MusterGrant{App: app, GitHubApp: "gazelle-agents", Installations: &installed{owners: []string{"giantswarm"}}},
		Sources:     s,
		Clock:       virtual(),
	}, map[string]string{mcpserver.GrantHeaderPrefix + app: "ghu_person"})
	if err != nil {
		t.Fatal(err)
	}
	_, doc := call(t, c, mcpserver.ToolPRWait, prWaitArgs("someone-else/repo"), "w")
	assertVerdict(t, doc, agentcli.VerdictNotApplicable, agentcli.ExitNotApplicable)
	if reason, _ := doc["reason"].(string); !strings.Contains(reason, "gazelle-agents") || !strings.Contains(reason, "someone-else") {
		t.Errorf("reason %q names neither the App nor the owner", reason)
	}
	if len(s.tokens) != 0 {
		t.Errorf("clients were built for a repository outside the App: %v", s.tokens)
	}
}

// TestGrantRefusedByGitHub: a grant whose token GitHub refuses names no
// caller: the call is auth_required marked grant_required, so muster
// answers it with the App's sign-in link, and no GitHub client is built.
func TestGrantRefusedByGitHub(t *testing.T) {
	s := &sources{pr: greenHead()}
	c, err := connectAs(t, mcpserver.Config{
		Credentials: mcpserver.MusterGrant{App: app, Users: &users{err: agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired, "refused")}},
		Sources:     s,
		Clock:       virtual(),
	}, map[string]string{mcpserver.GrantHeaderPrefix + app: "ghu_revoked"})
	if err != nil {
		t.Fatal(err)
	}
	_, doc := call(t, c, mcpserver.ToolPRWait, prWaitArgs("giantswarm/devctl"), "w")
	assertVerdict(t, doc, agentcli.VerdictAuthRequired, agentcli.ExitAuthRequired)
	if doc["status"] != mcpserver.StatusGrantRequired {
		t.Errorf("status %v, want %s", doc["status"], mcpserver.StatusGrantRequired)
	}
	if len(s.tokens) != 0 {
		t.Errorf("clients were built with a refused grant: %v", s.tokens)
	}
}

// TestSessionWithoutGrant: a person who has not granted the App opens a
// session and lists the tools, so muster shows them; pr_merge_status, which
// reads no GitHub, is refused like every other tool call.
func TestSessionWithoutGrant(t *testing.T) {
	c, err := connectAs(t, mcpserver.Config{
		Credentials: mcpserver.MusterGrant{App: app, Users: &users{caller: alice}},
		Clock:       virtual(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil || len(tools.Tools) == 0 {
		t.Fatalf("listing the tools without a grant: %v, %v", tools, err)
	}
	_, doc := call(t, c, mcpserver.ToolPRMergeStatus, map[string]any{}, nil)
	assertVerdict(t, doc, agentcli.VerdictAuthRequired, agentcli.ExitAuthRequired)
	if doc["status"] != mcpserver.StatusGrantRequired {
		t.Errorf("status %v, want %s", doc["status"], mcpserver.StatusGrantRequired)
	}
}

// TestCallTraced: a call's log line and its metric labels carry the GitHub
// login and id the grant names, and the repository.
func TestCallTraced(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := mcpserver.NewMetrics(registry)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	c, err := connectAs(t, mcpserver.Config{
		Credentials: mcpserver.MusterGrant{App: app, Users: &users{caller: alice}},
		Sources:     &sources{pr: greenHead()},
		Clock:       virtual(),
		Metrics:     metrics,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	}, map[string]string{mcpserver.GrantHeaderPrefix + app: "ghu_person"})
	if err != nil {
		t.Fatal(err)
	}
	_, doc := call(t, c, mcpserver.ToolPRWait, prWaitArgs("giantswarm/devctl"), "w")
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)

	var line map[string]any
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("not one JSON log line: %v\n%s", err, logs.String())
	}
	for key, want := range map[string]any{"tool": mcpserver.ToolPRWait, "login": "alice", "userId": "4711", "repository": "giantswarm/devctl", "verdict": "green"} {
		if line[key] != want {
			t.Errorf("log %s = %v, want %v", key, line[key], want)
		}
	}
	if got := testutil.ToFloat64(metricWith(t, registry, "alice", "4711", "giantswarm/devctl")); got != 1 {
		t.Errorf("devctl_mcp_tool_calls_total for the caller and repository = %v, want 1", got)
	}
}

// TestGitHubUsers: the caller is GitHub's user for the token, read once per
// token within the TTL; a token GitHub refuses is auth_required.
func TestGitHubUsers(t *testing.T) {
	var reads atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer ghu_person" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"login":"alice","id":4711}`))
	}))
	t.Cleanup(gh.Close)
	u := &mcpserver.GitHubUsers{BaseURL: gh.URL}
	for range 2 {
		caller, err := u.User(context.Background(), "ghu_person")
		if err != nil || caller != alice {
			t.Fatalf("User = %+v, %v, want %+v", caller, err, alice)
		}
	}
	if reads.Load() != 1 {
		t.Errorf("GET /user read %d times for one token, want 1", reads.Load())
	}
	if _, err := u.User(context.Background(), "ghu_revoked"); agentcli.Exit(err) != agentcli.ExitAuthRequired {
		t.Errorf("a refused token answered %v, want auth_required", err)
	}
	if _, err := u.User(context.Background(), "ghu_revoked"); err == nil || reads.Load() != 3 {
		t.Errorf("a refusal was kept: %d reads", reads.Load())
	}
}

// metricWith is the call counter of login, userID and repository for a
// green pr_wait, read back from registry.
func metricWith(t *testing.T, registry *prometheus.Registry, login, userID, repository string) prometheus.Collector {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "devctl_mcp_tool_calls_total" {
			continue
		}
		for _, m := range family.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["login"] == login && labels["user_id"] == userID && labels["repository"] == repository && labels["tool"] == mcpserver.ToolPRWait && labels["verdict"] == "green" {
				return prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "x", Help: "x"}, m.GetCounter().GetValue)
			}
		}
	}
	t.Fatalf("no devctl_mcp_tool_calls_total{login=%q,user_id=%q,repository=%q}", login, userID, repository)
	return nil
}

func anySlice(v any) []any {
	s, _ := v.([]any)
	return s
}
