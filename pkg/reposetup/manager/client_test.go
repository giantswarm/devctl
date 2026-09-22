package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// initializations counts the MCP handshakes the fakes saw.
var initializations atomic.Int32

// fakeMuster is the streamable HTTP surface of a muster endpoint with the
// get_repository tool: a session on initialize, then the tool's answer as
// JSON or as an SSE event.
func fakeMuster(t *testing.T, sse bool, answer string) *httptest.Server {
	initializations.Store(0)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(401)
			return
		}
		var msg struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		switch msg.Method {
		case "initialize":
			initializations.Add(1)
			w.Header().Set("Mcp-Session-Id", "session-1")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"muster"}}}`, *msg.ID)
		case "notifications/initialized":
			w.WriteHeader(202)
		case "tools/call":
			if r.Header.Get("Mcp-Session-Id") != "session-1" {
				t.Errorf("tools/call without the session: %q", r.Header.Get("Mcp-Session-Id"))
			}
			if msg.Params.Name == ToolAuthLogin {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"Please open https://muster.example/consent"}]}}`, *msg.ID)
				return
			}
			if msg.Params.Name != ToolGetRepository || msg.Params.Arguments["repository"] != "giantswarm/my-service" {
				t.Errorf("tools/call: %+v", msg.Params)
			}
			response := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, *msg.ID, answer)
			if sse {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", response)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, response)
		default:
			t.Errorf("unexpected method %q", msg.Method)
			w.WriteHeader(400)
		}
	}))
}

func TestClientGetRepository(t *testing.T) {
	record := `{"repository":"giantswarm/my-service","declaration":{"team":"team-bumblebee","file":"repositories/team-bumblebee.yaml"},"setup":{"checks":{"repository":"giantswarm/my-service","declared":"giantswarm/my-service","team":"team-bumblebee","mode":"check","steps":[{"step":"create","verdict":"ok"}],"converged":true}}}`
	quoted, _ := json.Marshal(record)

	cases := []struct {
		name   string
		sse    bool
		answer string
	}{
		{"structured content as JSON", false, `{"structuredContent":` + record + `,"content":[]}`},
		{"text content over SSE", true, `{"content":[{"type":"text","text":` + string(quoted) + `}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeMuster(t, tc.sse, tc.answer)
			defer srv.Close()
			c := &Client{Endpoint: srv.URL, Token: "secret"}
			got, err := c.GetRepository(context.Background(), "giantswarm/my-service")
			if err != nil {
				t.Fatal(err)
			}
			if got.Declaration == nil || got.Declaration.Team != "team-bumblebee" || got.Setup.Checks == nil || !got.Setup.Checks.Converged || got.Setup.Checks.Steps[0].Step != reconcile.StepCreate {
				t.Errorf("record: %+v", got)
			}
		})
	}

	t.Run("tool error", func(t *testing.T) {
		srv := fakeMuster(t, false, `{"isError":true,"content":[{"type":"text","text":"repository not in the inventory"}]}`)
		defer srv.Close()
		_, err := (&Client{Endpoint: srv.URL, Token: "secret"}).GetRepository(context.Background(), "giantswarm/my-service")
		if !IsTool(err) {
			t.Errorf("got %v, want toolError", err)
		}
	})

	t.Run("unauthorised is auth required", func(t *testing.T) {
		srv := fakeMuster(t, false, "")
		defer srv.Close()
		_, err := (&Client{Endpoint: srv.URL}).GetRepository(context.Background(), "giantswarm/my-service")
		if !IsAuthRequired(err) {
			t.Errorf("got %v, want authRequiredError", err)
		}
	})

	t.Run("one session for two calls", func(t *testing.T) {
		srv := fakeMuster(t, false, `{"structuredContent":`+record+`,"content":[]}`)
		defer srv.Close()
		c := &Client{Endpoint: srv.URL, Token: "secret"}
		for i := 0; i < 2; i++ {
			if _, err := c.GetRepository(context.Background(), "giantswarm/my-service"); err != nil {
				t.Fatal(err)
			}
		}
		if got := initializations.Load(); got != 1 {
			t.Errorf("initialize was called %d times, want 1", got)
		}
	})

	t.Run("no endpoint", func(t *testing.T) {
		_, err := (&Client{}).GetRepository(context.Background(), "giantswarm/my-service")
		if !IsInvalidConfig(err) {
			t.Errorf("got %v, want invalidConfigError", err)
		}
	})
}

// A tool that answers text, not JSON, is returned as that text: what
// muster's core_auth_login says.
func TestClientCallText(t *testing.T) {
	srv := fakeMuster(t, false, "")
	defer srv.Close()
	c := &Client{Endpoint: srv.URL, Token: "secret"}
	got, err := c.Call(context.Background(), ToolAuthLogin, map[string]any{"server": Server})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "Please open https://muster.example/consent" {
		t.Errorf("payload = %q", got)
	}
}

// The tool names are muster's: the server's name after the x_ prefix, the
// tool after another underscore.
func TestToolNames(t *testing.T) {
	if ToolGetRepository != "x_giantswarm-repo-manager_get_repository" || ToolSetLifecycle != "x_giantswarm-repo-manager_set_lifecycle" {
		t.Errorf("tool names: %s, %s", ToolGetRepository, ToolSetLifecycle)
	}
}
