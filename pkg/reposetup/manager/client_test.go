package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// fakeMuster is the streamable HTTP surface of a muster endpoint with the
// get_repository tool: a session on initialize, then the tool's answer as
// JSON or as an SSE event.
func fakeMuster(t *testing.T, sse bool, answer string) *httptest.Server {
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
			w.Header().Set("Mcp-Session-Id", "session-1")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"muster"}}}`, *msg.ID)
		case "notifications/initialized":
			w.WriteHeader(202)
		case "tools/call":
			if r.Header.Get("Mcp-Session-Id") != "session-1" {
				t.Errorf("tools/call without the session: %q", r.Header.Get("Mcp-Session-Id"))
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
	record := `{"repository":"giantswarm/my-service","team":"team-bumblebee","setup":{"repository":"giantswarm/my-service","declared":"giantswarm/my-service","team":"team-bumblebee","mode":"check","steps":[{"step":"create","verdict":"ok"}],"converged":true}}`
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
			if got.Team != "team-bumblebee" || got.Setup == nil || !got.Setup.Converged || got.Setup.Steps[0].Step != reconcile.StepCreate {
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

	t.Run("unauthorised is unreachable", func(t *testing.T) {
		srv := fakeMuster(t, false, "")
		defer srv.Close()
		_, err := (&Client{Endpoint: srv.URL}).GetRepository(context.Background(), "giantswarm/my-service")
		if !IsUnreachable(err) {
			t.Errorf("got %v, want unreachableError", err)
		}
	})

	t.Run("no endpoint", func(t *testing.T) {
		_, err := (&Client{}).GetRepository(context.Background(), "giantswarm/my-service")
		if !IsInvalidConfig(err) {
			t.Errorf("got %v, want invalidConfigError", err)
		}
	})
}
