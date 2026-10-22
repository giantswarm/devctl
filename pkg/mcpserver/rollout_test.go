package mcpserver_test

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mcpserver"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

// The people's tokens of the hub's Dex, as muster forwards them, and what
// the installation's Dex exchanges each for.
const (
	aliceHub = "hub-token-alice"
	bobHub   = "hub-token-bob"
	issuer   = "https://dex.graveler.example"
)

// jwt is an unsigned token of the installation's Dex for email.
func jwt(email string, exp time.Time) string {
	part := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return part(map[string]string{"alg": "none"}) + "." + part(map[string]any{"iss": issuer, "email": email, "aud": mcpserver.DefaultAudience, "exp": exp.Unix()}) + ".sig"
}

// dex is an installation's Dex: it exchanges a hub token it knows for the
// person's token, authenticated as its one client.
type dex struct {
	mu        sync.Mutex
	exchanges int
	forms     []map[string]string
}

func (d *dex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	d.mu.Lock()
	d.exchanges++
	form := map[string]string{}
	for k := range r.PostForm {
		form[k] = r.PostForm.Get(k)
	}
	d.forms = append(d.forms, form)
	d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if id, secret, ok := r.BasicAuth(); !ok || id != "muster-token-exchange-graveler" || secret != "client-secret" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
		return
	}
	email := map[string]string{aliceHub: "alice@example.com", bobHub: "bob@example.com"}[r.PostForm.Get("subject_token")]
	if email == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"failed to verify subject token"}`))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:gosec // G101: a test double's answer
		"access_token":      jwt(email, time.Now().Add(time.Hour)),
		"issued_token_type": "urn:ietf:params:oauth:token-type:id_token",
		"token_type":        "bearer",
		"expires_in":        3600,
	})
}

// apiServer is a management cluster whose RBAC lets alice list everything
// (there is nothing) and refuses bob.
type apiServer struct {
	mu      sync.Mutex
	callers []string
}

func (a *apiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(token, ".")
	var claims struct {
		Email string `json:"email"`
	}
	if len(parts) == 3 {
		payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
		_ = json.Unmarshal(payload, &claims)
	}
	a.mu.Lock()
	a.callers = append(a.callers, claims.Email)
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch claims.Email {
	case "alice@example.com":
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"List","metadata":{},"items":[]}`))
	case "bob@example.com":
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403,"message":"helmreleases.helm.toolkit.fluxcd.io is forbidden: User \"%s\" cannot list resource \"helmreleases\""}`, claims.Email)
	default:
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Unauthorized","code":401}`))
	}
}

// graveler is an installation the server reaches through d and a, both on
// TLS servers whose certificate the installation file trusts.
func graveler(t *testing.T, d *dex, a *apiServer) *mcpserver.DexExchange {
	t.Helper()
	dexServer := httptest.NewTLSServer(d)
	t.Cleanup(dexServer.Close)
	api := httptest.NewTLSServer(a)
	t.Cleanup(api.Close)
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	ca := write("ca.pem", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw})))
	file := map[string]any{"installations": []map[string]any{{
		"name":      "graveler",
		"apiServer": api.URL,
		"caFile":    ca,
		"dex": map[string]any{
			"tokenEndpoint":    dexServer.URL + "/token",
			"issuer":           issuer,
			"connectorId":      "giantswarm-simple-oidc",
			"clientIdFile":     write("client-id", "muster-token-exchange-graveler\n"),
			"clientSecretFile": write("client-secret", "client-secret"),
		},
	}}}
	data, _ := json.Marshal(file)
	clusters, err := mcpserver.ReadInstallations(write("installations.json", string(data)))
	if err != nil {
		t.Fatal(err)
	}
	clusters.Client = dexServer.Client()
	return clusters
}

// TestRolloutWaitAsThePerson: two people call rollout_wait on the same
// installation; the cluster answers each by their own access, through
// their token exchanged at the installation's Dex. Alice gets the verdict,
// Bob the cluster's refusal and no data, and a call without a forwarded
// token or with one the installation's Dex refuses is auth_required.
func TestRolloutWaitAsThePerson(t *testing.T) {
	tests := []struct {
		name string
		// bearer is the token muster forwards in Authorization.
		bearer  string
		verdict agentcli.Verdict
		exit    int
		reason  string
		// caller is who the cluster saw; empty when it saw no one.
		caller string
	}{
		{
			name:    "alice reads the cluster",
			bearer:  aliceHub,
			verdict: agentcli.VerdictNotApplicable,
			exit:    agentcli.ExitNotApplicable,
			reason:  "no HelmRelease or App on graveler deploys devctl",
			caller:  "alice@example.com",
		},
		{
			name:    "bob gets the cluster's refusal",
			bearer:  bobHub,
			verdict: agentcli.VerdictUsage,
			exit:    agentcli.ExitUsage,
			reason:  `User "bob@example.com" cannot list resource "helmreleases"`,
			caller:  "bob@example.com",
		},
		{
			name:    "no forwarded token",
			verdict: agentcli.VerdictAuthRequired,
			exit:    agentcli.ExitAuthRequired,
			reason:  "muster forwards",
		},
		{
			name:    "a token the installation's Dex refuses",
			bearer:  "expired",
			verdict: agentcli.VerdictAuthRequired,
			exit:    agentcli.ExitAuthRequired,
			reason:  "the Dex of graveler refused your token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", &releasewaittest.Registry{}, "success")
			if err != nil {
				t.Fatal(err)
			}
			d, a := &dex{}, &apiServer{}
			headers := map[string]string{}
			if tt.bearer != "" {
				headers["Authorization"] = "Bearer " + tt.bearer
			}
			c, err := connectAs(t, mcpserver.Config{
				Credentials: person("ghu_person"),
				Sources:     &sources{release: release},
				Clusters:    graveler(t, d, a),
				Clock:       virtual(),
			}, headers)
			if err != nil {
				t.Fatal(err)
			}
			result, doc := call(t, c, mcpserver.ToolRolloutWait, map[string]any{"installation": "graveler", "repository": "giantswarm/devctl", "version": "v1.2.3", "timeout": "10m"}, "rollout-1")
			assertVerdict(t, doc, tt.verdict, tt.exit)
			if reason := stringOf(doc["reason"]); !strings.Contains(reason, tt.reason) {
				t.Errorf("reason %q does not say %q", reason, tt.reason)
			}
			if result.IsError != (tt.exit == agentcli.ExitAuthRequired) {
				t.Errorf("IsError %v for %s", result.IsError, tt.verdict)
			}
			if tt.caller != "" {
				if len(a.callers) == 0 || a.callers[0] != tt.caller {
					t.Errorf("the cluster saw %v, want %s", a.callers, tt.caller)
				}
				if items, _ := doc["deployments"].([]any); len(items) != 0 {
					t.Errorf("deployments %v, want none", items)
				}
				form := d.forms[0]
				for k, want := range map[string]string{ //nolint:gosec // G101: the exchange's form fields
					"grant_type":           "urn:ietf:params:oauth:grant-type:token-exchange",
					"subject_token":        tt.bearer,
					"subject_token_type":   "urn:ietf:params:oauth:token-type:id_token",
					"requested_token_type": "urn:ietf:params:oauth:token-type:id_token",
					"connector_id":         "giantswarm-simple-oidc",
					"scope":                "openid profile email groups audience:server:client_id:dex-k8s-authenticator",
				} {
					if form[k] != want {
						t.Errorf("exchange %s = %q, want %q", k, form[k], want)
					}
				}
			} else if len(a.callers) != 0 {
				t.Errorf("the cluster saw %v, want no request", a.callers)
			}
			if doc["command"] != rolloutwait.Command || doc["installation"] != "graveler" {
				t.Errorf("document: %v", doc)
			}
		})
	}
}

// TestRolloutWaitExchangesOncePerToken: the exchanged token is kept for the
// person until shortly before it expires, not exchanged per request.
func TestRolloutWaitExchangesOncePerToken(t *testing.T) {
	release, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", &releasewaittest.Registry{}, "success")
	if err != nil {
		t.Fatal(err)
	}
	d, a := &dex{}, &apiServer{}
	c, err := connectAs(t, mcpserver.Config{
		Credentials: person("ghu_person"),
		Sources:     &sources{release: release},
		Clusters:    graveler(t, d, a),
		Clock:       virtual(),
	}, map[string]string{"Authorization": "Bearer " + aliceHub})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		call(t, c, mcpserver.ToolRolloutWait, map[string]any{"installation": "graveler", "repository": "giantswarm/devctl", "version": "v1.2.3"}, "rollout-1")
	}
	if len(a.callers) < 2 {
		t.Fatalf("the cluster saw %d requests, want several", len(a.callers))
	}
	if d.exchanges != 1 {
		t.Errorf("%d exchanges for %d requests, want 1", d.exchanges, len(a.callers))
	}
}

// TestRolloutWaitInstallationNotReached: an installation the server has no
// entry for is not_applicable, naming the ones it reaches; a server without
// installations reaches none.
func TestRolloutWaitInstallationNotReached(t *testing.T) {
	for name, clusters := range map[string]mcpserver.Clusters{
		"other installation": graveler(t, &dex{}, &apiServer{}),
		"no installations":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			c, err := connectAs(t, mcpserver.Config{Credentials: person("ghu_person"), Sources: &sources{}, Clusters: clusters, Clock: virtual()},
				map[string]string{"Authorization": "Bearer " + aliceHub})
			if err != nil {
				t.Fatal(err)
			}
			_, doc := call(t, c, mcpserver.ToolRolloutWait, map[string]any{"installation": "gazelle", "repository": "giantswarm/devctl", "version": "v1.2.3"}, "rollout-1")
			assertVerdict(t, doc, agentcli.VerdictNotApplicable, agentcli.ExitNotApplicable)
			if !strings.Contains(stringOf(doc["reason"]), `no installation "gazelle"`) {
				t.Errorf("reason: %v", doc["reason"])
			}
		})
	}
}

// TestReadInstallationsRefusesBadEntries: the file is checked when the
// server starts.
func TestReadInstallationsRefusesBadEntries(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	dex := map[string]any{"tokenEndpoint": "https://dex/token", "connectorId": "c", "clientIdFile": secret, "clientSecretFile": secret} //nolint:gosec // G101: file paths, not credentials
	good := map[string]any{"name": "graveler", "apiServer": "https://api", "dex": dex}
	tests := map[string]func(map[string]any){
		"http API":            func(i map[string]any) { i["apiServer"] = "http://api" },
		"no connector":        func(i map[string]any) { delete(i["dex"].(map[string]any), "connectorId") },
		"missing secret":      func(i map[string]any) { i["dex"].(map[string]any)["clientSecretFile"] = filepath.Join(dir, "none") },
		"not an installation": func(i map[string]any) { i["name"] = "Graveler/x" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			entry := map[string]any{}
			b, _ := json.Marshal(good)
			_ = json.Unmarshal(b, &entry)
			mutate(entry)
			data, _ := json.Marshal(map[string]any{"installations": []any{entry}})
			path := filepath.Join(t.TempDir(), "installations.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := mcpserver.ReadInstallations(path); err == nil {
				t.Error("accepted")
			}
		})
	}
}
