package muster

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mcpReplyNull struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

type mcpToolResultNull struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// postMetaCallNull exercises the HTTP handler with recorders, without starting
// a server or making a network request.
func postMetaCallNull(t *testing.T, s *Server, arguments string) mcpReplyNull {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"call_tool","arguments":` + arguments + `}}`
	req := httptest.NewRequest(http.MethodPost, MCPPath, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-bearer")
	w := httptest.NewRecorder()
	s.mcp(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", w.Code, w.Body.String())
	}
	var reply mcpReplyNull
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func unwrapReplyNull(t *testing.T, reply mcpReplyNull) (mcpToolResultNull, mcpToolResultNull) {
	t.Helper()
	if reply.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", reply.Error)
	}
	var outer, inner mcpToolResultNull
	if err := json.Unmarshal(reply.Result, &outer); err != nil {
		t.Fatal(err)
	}
	if len(outer.Content) != 1 || outer.Content[0].Type != "text" {
		t.Fatalf("unexpected outer content: %+v", outer.Content)
	}
	if err := json.Unmarshal([]byte(outer.Content[0].Text), &inner); err != nil {
		t.Fatal(err)
	}
	return outer, inner
}

func TestMCPCallRequiresExpectedNullArgument(t *testing.T) {
	for _, tc := range []struct {
		name      string
		arguments string
		wantError bool
	}{
		{"missing key", `{}`, true},
		{"null argument map", `null`, true},
		{"explicit null", `{"clear":null}`, false},
		{"different value", `{"clear":false}`, true},
		{"extra argument allowed", `{"clear":null,"extra":true}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{
				cfg: Config{Bearer: "test-bearer", Tools: map[string][]ToolResponse{
					"scripted": {{Args: map[string]any{"clear": nil}, Result: "scripted result"}},
				}},
				calls: map[string]int{},
			}
			reply := postMetaCallNull(t, s, `{"name":"scripted","arguments":`+tc.arguments+`}`)
			outer, inner := unwrapReplyNull(t, reply)
			if outer.IsError != tc.wantError || inner.IsError != tc.wantError {
				t.Fatalf("outer isError = %v, inner isError = %v, want %v", outer.IsError, inner.IsError, tc.wantError)
			}
			if len(inner.Content) != 1 {
				t.Fatalf("unexpected inner content: %+v", inner.Content)
			}
			if tc.wantError {
				if !strings.Contains(inner.Content[0].Text, "unexpected arguments for scripted: clear") {
					t.Fatalf("argument error = %q", inner.Content[0].Text)
				}
			} else if inner.Content[0].Text != "scripted result" {
				t.Fatalf("result = %q", inner.Content[0].Text)
			}
		})
	}
}
