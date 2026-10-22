package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v93/github"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mcpserver"
	"github.com/giantswarm/devctl/v8/pkg/prmerge"
	"github.com/giantswarm/devctl/v8/pkg/prmerge/prmergetest"
	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

// person hands the server one person's token.
type person string

func (p person) GitHubToken(context.Context) (string, error) { return string(p), nil }

// sources hand the server in-process doubles instead of GitHub's clients
// and record the tokens they were built with.
type sources struct {
	pr      *prwaittest.GitHub
	merge   *prmergetest.GitHub
	policy  prmerge.Policy
	release *releasewait.Sources
	promote *releasepromote.Sources

	mu     sync.Mutex
	tokens []string
}

func (s *sources) PRWait(_ context.Context, token string, _ http.RoundTripper) (mcpserver.PRWaitSources, error) {
	s.mu.Lock()
	s.tokens = append(s.tokens, token)
	s.mu.Unlock()
	return mcpserver.PRWaitSources{GitHub: s.pr}, nil
}

func (s *sources) PRMerge(_ context.Context, token string, _ http.RoundTripper) (mcpserver.PRMergeSources, error) {
	s.mu.Lock()
	s.tokens = append(s.tokens, token)
	s.mu.Unlock()
	return mcpserver.PRMergeSources{GitHub: s.merge, Policy: s.policy}, nil
}

func (s *sources) ReleasePromote(_ context.Context, token string, _ http.RoundTripper) (*releasepromote.Sources, error) {
	s.mu.Lock()
	s.tokens = append(s.tokens, token)
	s.mu.Unlock()
	return s.promote, nil
}

func (s *sources) ReleaseWait(_ context.Context, token string, _ http.RoundTripper) (*releasewait.Sources, error) {
	s.mu.Lock()
	s.tokens = append(s.tokens, token)
	s.mu.Unlock()
	return s.release, nil
}

// virtual is a clock whose sleeps return at once.
func virtual() *agentcli.Clock {
	c := agentcli.NewVirtualClock(time.Now())
	return &c
}

// connect serves config over streamable HTTP and returns an initialized
// client and every notification it receives.
func connect(t *testing.T, config mcpserver.Config) (*client.Client, *notifications) {
	t.Helper()
	srv := httptest.NewServer(mcpserver.New(config).Handler())
	t.Cleanup(srv.Close)
	c, err := client.NewStreamableHttpClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	n := &notifications{}
	c.OnNotification(n.add)
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{Params: mcp.InitializeParams{
		ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		ClientInfo:      mcp.Implementation{Name: "test", Version: "0"},
	}}); err != nil {
		t.Fatal(err)
	}
	return c, n
}

// notifications are the progress notifications a client received and when.
type notifications struct {
	mu       sync.Mutex
	progress []progress
}

type progress struct {
	at       time.Time
	token    any
	progress float64
	message  string
}

func (n *notifications) add(notification mcp.JSONRPCNotification) {
	if notification.Method != string(mcp.MethodNotificationProgress) {
		return
	}
	f := notification.Params.AdditionalFields
	p := progress{at: time.Now(), token: f["progressToken"], message: stringOf(f["message"])}
	if v, ok := f["progress"].(float64); ok {
		p.progress = v
	}
	n.mu.Lock()
	n.progress = append(n.progress, p)
	n.mu.Unlock()
}

func (n *notifications) all() []progress {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.progress)
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

// call calls tool with args, with a progressToken unless token is nil, and
// returns the result and its structured content.
func call(t *testing.T, c *client.Client, tool string, args map[string]any, token mcp.ProgressToken) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	request := mcp.CallToolRequest{}
	request.Params.Name = tool
	request.Params.Arguments = args
	if token != nil {
		request.Params.Meta = &mcp.Meta{ProgressToken: token}
	}
	result, err := c.CallTool(context.Background(), request)
	if err != nil {
		t.Fatalf("calling %s: %v", tool, err)
	}
	doc, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("%s answered no structured content: %#v", tool, result)
	}
	text := result.Content[0].(mcp.TextContent).Text
	var fromText map[string]any
	if err := json.Unmarshal([]byte(text), &fromText); err != nil {
		t.Fatalf("the text of %s is not its document: %v\n%s", tool, err, text)
	}
	return result, doc
}

func assertVerdict(t *testing.T, doc map[string]any, verdict agentcli.Verdict, exit int) {
	t.Helper()
	if doc["verdict"] != string(verdict) || doc["exitCode"] != float64(exit) {
		t.Errorf("verdict %v, exit %v, want %s, %d; reason: %v", doc["verdict"], doc["exitCode"], verdict, exit, doc["reason"])
	}
	if doc["schemaVersion"] != float64(agentcli.SchemaVersion) {
		t.Errorf("schemaVersion = %v, want %d", doc["schemaVersion"], agentcli.SchemaVersion)
	}
}

// TestToolsListReadOnlyAnnotations: the read-only tools carry the
// read-only annotation and the write tools none, release_promote is served
// behind its selector only, and no CircleCI-only command is.
func TestToolsListReadOnlyAnnotations(t *testing.T) {
	tests := map[string]struct {
		config mcpserver.Config
		want   map[string]bool
	}{
		"default": {
			want: map[string]bool{mcpserver.ToolPRWait: true, mcpserver.ToolPRMerge: false, mcpserver.ToolPRMergeStatus: true, mcpserver.ToolReleaseWait: true, mcpserver.ToolRolloutWait: true},
		},
		"with release_promote": {
			config: mcpserver.Config{ReleasePromote: true},
			want:   map[string]bool{mcpserver.ToolPRWait: true, mcpserver.ToolPRMerge: false, mcpserver.ToolPRMergeStatus: true, mcpserver.ToolReleaseWait: true, mcpserver.ToolRolloutWait: true, mcpserver.ToolReleasePromote: false},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, _ := connect(t, tt.config)
			tools, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			readOnly := map[string]bool{}
			for _, tool := range tools.Tools {
				hint := tool.Annotations.ReadOnlyHint
				if hint == nil {
					t.Errorf("%s: no readOnlyHint", tool.Name)
				}
				readOnly[tool.Name] = hint != nil && *hint
			}
			if diff := cmp.Diff(tt.want, readOnly); diff != "" {
				t.Errorf("tools and their readOnlyHint (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPRWaitVerdicts: one call per verdict answers devctl's document with
// the verdict and exit code, as the person whose token the server holds.
func TestPRWaitVerdicts(t *testing.T) {
	updated := time.Now().Add(-time.Hour)
	workflow := ".github/workflows/ci.yaml"
	tests := []struct {
		name    string
		github  *prwaittest.GitHub
		timeout string
		verdict agentcli.Verdict
		exit    int
	}{
		{
			name: "green",
			github: &prwaittest.GitHub{Heads: []prwaittest.Head{
				{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "in_progress", "")}, Statuses: []*github.RepoStatus{prwaittest.Status("ci/circleci: build", "pending")}},
				{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "completed", "success")}, Statuses: []*github.RepoStatus{prwaittest.Status("ci/circleci: build", "success")}},
			}},
			verdict: agentcli.VerdictGreen,
		},
		{
			name: "red",
			github: &prwaittest.GitHub{Heads: []prwaittest.Head{
				{Statuses: []*github.RepoStatus{prwaittest.Status("ci/circleci: build", "failure")}, CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "completed", "success")}},
			}},
			verdict: agentcli.VerdictRed,
			exit:    agentcli.ExitRed,
		},
		{
			name: "timeout",
			github: &prwaittest.GitHub{Heads: []prwaittest.Head{
				{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "in_progress", "")}},
			}},
			timeout: "5m",
			verdict: agentcli.VerdictTimeout,
			exit:    agentcli.ExitTimeout,
		},
		{
			name: "not applicable: a draft",
			github: func() *prwaittest.GitHub {
				pr := prwaittest.OpenPullRequest(7, "abc123", updated)
				pr.Draft = new(true)
				return &prwaittest.GitHub{PR: pr}
			}(),
			verdict: agentcli.VerdictNotApplicable,
			exit:    agentcli.ExitNotApplicable,
		},
		{
			name: "required missing",
			github: &prwaittest.GitHub{
				Required: []string{"ci/circleci: build"},
				Heads:    []prwaittest.Head{{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "completed", "success")}}},
			},
			verdict: agentcli.VerdictRequiredMissing,
			exit:    agentcli.ExitRequiredMissing,
		},
		{
			name: "approval required",
			github: &prwaittest.GitHub{Heads: []prwaittest.Head{
				{WorkflowRuns: []*github.WorkflowRun{prwaittest.WorkflowRun("ci", workflow, "completed", "action_required")}},
			}},
			verdict: agentcli.VerdictApprovalRequired,
			exit:    agentcli.ExitRequiredMissing,
		},
		{
			name: "no checks",
			github: &prwaittest.GitHub{
				Files:   map[string]string{workflow: "on:\n  pull_request: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n"},
				Message: "change",
			},
			verdict: agentcli.VerdictNoChecks,
			exit:    agentcli.ExitRequiredMissing,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &sources{pr: tt.github}
			c, _ := connect(t, mcpserver.Config{Credentials: person("ghu_person"), Sources: s, Clock: virtual()})
			args := map[string]any{"repository": "giantswarm/devctl", "number": 7}
			if tt.timeout != "" {
				args["timeout"] = tt.timeout
			}
			result, doc := call(t, c, mcpserver.ToolPRWait, args, "wait-1")
			assertVerdict(t, doc, tt.verdict, tt.exit)
			if result.IsError {
				t.Errorf("%s is an MCP error", tt.verdict)
			}
			if doc["command"] != "pr wait" || doc["repository"] != "giantswarm/devctl" || doc["number"] != float64(7) {
				t.Errorf("document: %v", doc)
			}
			if !slices.Equal(s.tokens, []string{"ghu_person"}) {
				t.Errorf("the clients were built with %v, want the person's token", s.tokens)
			}
		})
	}
}

// TestPRWaitAuthRequired: a person the server holds no token for gets
// auth_required, the one verdict that is an MCP error.
func TestPRWaitAuthRequired(t *testing.T) {
	for name, token := range map[string]mcp.ProgressToken{"with a progressToken": "wait-1", "without a progressToken": nil} {
		t.Run(name, func(t *testing.T) {
			s := &sources{pr: &prwaittest.GitHub{}}
			c, _ := connect(t, mcpserver.Config{Sources: s})
			result, doc := call(t, c, mcpserver.ToolPRWait, map[string]any{"repository": "giantswarm/devctl", "number": 7}, token)
			assertVerdict(t, doc, agentcli.VerdictAuthRequired, agentcli.ExitAuthRequired)
			if !result.IsError {
				t.Error("auth_required is not an MCP error")
			}
			if s.pr.Polls() != 0 {
				t.Errorf("GitHub was read %d times without a token", s.pr.Polls())
			}
		})
	}
}

// TestWaitWithoutProgressToken: a wait called without a progressToken
// answers within the no-progress timeout, whatever its timeout argument, with
// the verdict it reached by then, timeout while pending, a warning saying how
// to go on, and no progress notification.
func TestWaitWithoutProgressToken(t *testing.T) {
	pending := prwaittest.Head{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "in_progress", "")}}
	green := prwaittest.Head{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "completed", "success")}}
	tests := []struct {
		name      string
		tool      string
		args      map[string]any
		heads     []prwaittest.Head
		workflows []string
		verdict   agentcli.Verdict
		exit      int
	}{
		{name: "pr_wait, green", tool: mcpserver.ToolPRWait, heads: []prwaittest.Head{pending, green}, verdict: agentcli.VerdictGreen},
		{name: "pr_wait, pending", tool: mcpserver.ToolPRWait, heads: []prwaittest.Head{pending}, verdict: agentcli.VerdictTimeout, exit: agentcli.ExitTimeout},
		{name: "release_wait, available", tool: mcpserver.ToolReleaseWait, workflows: []string{"success"}, verdict: agentcli.VerdictAvailable},
		{name: "release_wait, pending", tool: mcpserver.ToolReleaseWait, workflows: []string{"running"}, verdict: agentcli.VerdictTimeout, exit: agentcli.ExitTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &sources{pr: &prwaittest.GitHub{Heads: tt.heads}}
			args := map[string]any{"repository": "giantswarm/devctl", "number": 7, "timeout": "30m"}
			if tt.tool == mcpserver.ToolReleaseWait {
				release, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", &releasewaittest.Registry{}, tt.workflows...)
				if err != nil {
					t.Fatal(err)
				}
				s.release = release
				args = map[string]any{"repository": "giantswarm/devctl", "version": "v1.2.3", "timeout": "30m"}
			}
			c, n := connect(t, mcpserver.Config{Credentials: person("ghu_person"), Sources: s, Clock: virtual(), NoProgressTimeout: 20 * time.Second})
			result, doc := call(t, c, tt.tool, args, nil)
			assertVerdict(t, doc, tt.verdict, tt.exit)
			if result.IsError {
				t.Errorf("%s is an MCP error", tt.verdict)
			}
			if tt.exit == agentcli.ExitTimeout && !strings.Contains(stringOf(doc["reason"]), "20s") {
				t.Errorf("the reason %q names no timeout of 20s", doc["reason"])
			}
			if !hasWarning(doc, "called without a progressToken") {
				t.Errorf("no warning on how to go on: %v", doc["warnings"])
			}
			if got := len(n.all()); got != 0 {
				t.Errorf("%d progress notifications without a progressToken", got)
			}
		})
	}
}

// TestWaitKeptAliveByProgress: a wait longer than the gateway's registered
// timeout is one call; its progress notifications come on the caller's token
// at intervals below that timeout, each with a message and a progress that
// increases, and none after the verdict.
func TestWaitKeptAliveByProgress(t *testing.T) {
	const registeredTimeout = 300 * time.Millisecond
	pending := prwaittest.Head{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "in_progress", "")}}
	green := prwaittest.Head{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "completed", "success")}}
	s := &sources{pr: &prwaittest.GitHub{Heads: []prwaittest.Head{pending, pending, pending, pending, pending, green}}}
	// A poll every 15 s at scale 0.01: 150 ms of real time each.
	clock := agentcli.NewClock(0.01, nil)
	c, n := connect(t, mcpserver.Config{Credentials: person("ghu_person"), Sources: s, Clock: &clock, ProgressInterval: 50 * time.Millisecond})

	start := time.Now()
	_, doc := call(t, c, mcpserver.ToolPRWait, map[string]any{"repository": "giantswarm/devctl", "number": 7}, "wait-42")
	end := time.Now()
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)
	if took := end.Sub(start); took <= registeredTimeout {
		t.Fatalf("the wait took %s, not longer than the registered timeout %s", took, registeredTimeout)
	}

	got := n.all()
	if len(got) < 2 {
		t.Fatalf("%d progress notifications", len(got))
	}
	last := start
	for i, p := range got {
		if p.token != "wait-42" {
			t.Errorf("notification %d on token %v", i, p.token)
		}
		if p.message == "" {
			t.Errorf("notification %d has no message", i)
		}
		if i > 0 && p.progress <= got[i-1].progress {
			t.Errorf("progress %v after %v", p.progress, got[i-1].progress)
		}
		if gap := p.at.Sub(last); gap >= registeredTimeout {
			t.Errorf("notification %d came %s after the one before, not below %s", i, gap, registeredTimeout)
		}
		last = p.at
	}
	if gap := end.Sub(last); gap >= registeredTimeout {
		t.Errorf("the verdict came %s after the last notification", gap)
	}
	time.Sleep(150 * time.Millisecond)
	if after := len(n.all()); after != len(got) {
		t.Errorf("%d notifications after the verdict", after-len(got))
	}
}

// TestLaptopOnlyBehaviourIsOff: DEVCTL_TIME_SCALE changes nothing and nothing
// is written under $XDG_STATE_HOME.
func TestLaptopOnlyBehaviourIsOff(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv(agentcli.EnvTimeScale, "0.0001")

	// A wait whose poll interval at scale 1 would take 15 s: with the time
	// scale read, the timeout of 1 s would leave a scaled 0.1 ms.
	pending := prwaittest.Head{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "in_progress", "")}}
	s := &sources{pr: &prwaittest.GitHub{Heads: []prwaittest.Head{pending}}}
	c, _ := connect(t, mcpserver.Config{Credentials: person("ghu_person"), Sources: s})
	start := time.Now()
	_, doc := call(t, c, mcpserver.ToolPRWait, map[string]any{"repository": "giantswarm/devctl", "number": 7, "timeout": "1s"}, "wait-1")
	took := time.Since(start)
	assertVerdict(t, doc, agentcli.VerdictTimeout, agentcli.ExitTimeout)
	if took < time.Second {
		t.Errorf("a 1 s wait took %s with %s set", took, agentcli.EnvTimeScale)
	}
	if polls := s.pr.Polls(); polls != 1 {
		t.Errorf("%d polls in 1 s: the poll interval was scaled", polls)
	}
	_, doc = call(t, c, mcpserver.ToolPRMergeStatus, map[string]any{}, nil)
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)

	entries, err := os.ReadDir(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("written under $XDG_STATE_HOME: %v", entries)
	}
}

var (
	image = agentcli.DefaultEndpoints().RegistryPublic + "/giantswarm/devctl:1.2.3"
	chart = agentcli.DefaultEndpoints().RegistryPublic + "/charts/giantswarm/devctl:1.2.3"
)

// TestReleaseWaitAvailableOnlyWhenPullableAndGreen: release_wait answers
// available only once the tag's image is pullable and its pipeline is green.
func TestReleaseWaitAvailableOnlyWhenPullableAndGreen(t *testing.T) {
	tests := []struct {
		name      string
		registry  *releasewaittest.Registry
		workflows []string
		verdict   agentcli.Verdict
		exit      int
		// probes is how often the image is probed at least.
		probes int
	}{
		{
			name:      "pullable and green",
			registry:  &releasewaittest.Registry{},
			workflows: []string{"success"},
			verdict:   agentcli.VerdictAvailable,
			probes:    1,
		},
		{
			name:      "green, the image pullable only after three probes",
			registry:  &releasewaittest.Registry{Missing: map[string]int{image: 3}},
			workflows: []string{"success"},
			verdict:   agentcli.VerdictAvailable,
			probes:    4,
		},
		{
			name:      "pullable, the pipeline green only later",
			registry:  &releasewaittest.Registry{},
			workflows: []string{"running", "running", "success"},
			verdict:   agentcli.VerdictAvailable,
			probes:    1,
		},
		{
			name:      "pullable, the pipeline never green",
			registry:  &releasewaittest.Registry{},
			workflows: []string{"running"},
			verdict:   agentcli.VerdictTimeout,
			exit:      agentcli.ExitTimeout,
			probes:    1,
		},
		{
			name:      "green, the chart never pullable",
			registry:  &releasewaittest.Registry{Never: []string{chart}},
			workflows: []string{"success"},
			verdict:   agentcli.VerdictTimeout,
			exit:      agentcli.ExitTimeout,
			probes:    1,
		},
		{
			name:      "the pipeline failed",
			registry:  &releasewaittest.Registry{Never: []string{image, chart}},
			workflows: []string{"failed"},
			verdict:   agentcli.VerdictCIFailed,
			exit:      agentcli.ExitRed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", tt.registry, tt.workflows...)
			if err != nil {
				t.Fatal(err)
			}
			s := &sources{release: release}
			c, _ := connect(t, mcpserver.Config{Credentials: person("ghu_person"), Sources: s, Clock: virtual()})
			result, doc := call(t, c, mcpserver.ToolReleaseWait, map[string]any{"repository": "giantswarm/devctl", "version": "v1.2.3", "timeout": "10m"}, "release-1")
			assertVerdict(t, doc, tt.verdict, tt.exit)
			if result.IsError {
				t.Errorf("%s is an MCP error", tt.verdict)
			}
			if probes := tt.registry.Probes(image); probes < tt.probes {
				t.Errorf("%s probed %d times, want at least %d; artifacts %v", image, probes, tt.probes, doc["artifacts"])
			}
			if doc["command"] != releasewait.Command || doc["tag"] != "v1.2.3" {
				t.Errorf("document: %v", doc)
			}
			if !slices.Equal(s.tokens, []string{"ghu_person"}) {
				t.Errorf("the clients were built with %v, want the person's token", s.tokens)
			}
		})
	}
}
