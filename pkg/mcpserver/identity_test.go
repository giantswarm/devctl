package mcpserver_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
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

const (
	issuer   = "https://dex.example.com"
	audience = "muster"
	app      = "github"
)

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

// signer signs ID tokens of issuer, and its verifier accepts them.
type signer struct {
	signer   jose.Signer
	verifier *mcpserver.OIDCVerifier
}

func newSigner(t *testing.T) signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, err := mcpserver.NewStaticOIDCVerifier(issuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, []string{audience})
	if err != nil {
		t.Fatal(err)
	}
	return signer{signer: s, verifier: v}
}

func (s signer) idToken(t *testing.T, subject, email, aud string) string {
	t.Helper()
	raw, err := jwt.Signed(s.signer).Claims(jwt.Claims{
		Issuer:   issuer,
		Subject:  subject,
		Audience: jwt.Audience{aud},
		Expiry:   jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}).Claims(map[string]any{"email": email}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

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
// one every client of the call is built with, and the one the App's
// installations are read with.
func TestGrantActsAsThePerson(t *testing.T) {
	s := &sources{pr: greenHead()}
	i := &installed{owners: []string{"GiantSwarm"}}
	c, err := connectAs(t, mcpserver.Config{
		Credentials: mcpserver.MusterGrant{App: app, Installations: i},
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
	if !slices.Equal(s.tokens, []string{"ghu_person"}) || !slices.Equal(i.tokens, []string{"ghu_person"}) {
		t.Errorf("clients built with %v, installations read with %v, want the person's token of %s only", s.tokens, i.tokens, app)
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

// TestCallerRequired: with a verifier, a request without an ID token it
// accepts is refused before any tool runs.
func TestCallerRequired(t *testing.T) {
	sig := newSigner(t)
	for name, headers := range map[string]map[string]string{
		"no token":           nil,
		"another audience":   {"Authorization": "Bearer " + sig.idToken(t, "alice", "alice@example.com", "someone-else")},
		"not an ID token":    {"Authorization": "Bearer ghu_person"},
		"another key signed": {"Authorization": "Bearer " + newSigner(t).idToken(t, "alice", "alice@example.com", audience)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := connectAs(t, mcpserver.Config{Verifier: sig.verifier}, headers); err == nil {
				t.Error("the server accepted the session")
			}
		})
	}
}

// TestCallTraced: a call's log line and its metric labels carry the
// caller's subject and the repository.
func TestCallTraced(t *testing.T) {
	sig := newSigner(t)
	registry := prometheus.NewRegistry()
	metrics, err := mcpserver.NewMetrics(registry)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	c, err := connectAs(t, mcpserver.Config{
		Credentials: mcpserver.MusterGrant{App: app},
		Sources:     &sources{pr: greenHead()},
		Clock:       virtual(),
		Verifier:    sig.verifier,
		Metrics:     metrics,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	}, map[string]string{
		"Authorization":                   "Bearer " + sig.idToken(t, "Cg5hbGljZQ", "alice@example.com", audience),
		mcpserver.GrantHeaderPrefix + app: "ghu_person",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, doc := call(t, c, mcpserver.ToolPRWait, prWaitArgs("giantswarm/devctl"), "w")
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)

	var line map[string]any
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("not one JSON log line: %v\n%s", err, logs.String())
	}
	for key, want := range map[string]any{"tool": mcpserver.ToolPRWait, "subject": "Cg5hbGljZQ", "email": "alice@example.com", "repository": "giantswarm/devctl", "verdict": "green"} {
		if line[key] != want {
			t.Errorf("log %s = %v, want %v", key, line[key], want)
		}
	}
	if got := testutil.ToFloat64(metricWith(t, registry, "Cg5hbGljZQ", "giantswarm/devctl")); got != 1 {
		t.Errorf("devctl_mcp_tool_calls_total for the caller and repository = %v, want 1", got)
	}
}

// metricWith is the call counter of subject and repository for a green
// pr_wait, read back from registry.
func metricWith(t *testing.T, registry *prometheus.Registry, subject, repository string) prometheus.Collector {
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
			if labels["subject"] == subject && labels["repository"] == repository && labels["tool"] == mcpserver.ToolPRWait && labels["verdict"] == "green" {
				return prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "x", Help: "x"}, m.GetCounter().GetValue)
			}
		}
	}
	t.Fatalf("no devctl_mcp_tool_calls_total{subject=%q,repository=%q}", subject, repository)
	return nil
}

func anySlice(v any) []any {
	s, _ := v.([]any)
	return s
}
