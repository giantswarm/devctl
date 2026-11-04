package muster

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mcpReplyParameters struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

type mcpToolResultParameters struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// postMetaCallParameters exercises the HTTP handler with recorders, without starting
// a server or making a network request.
func postMetaCallParameters(t *testing.T, s *Server, arguments string) mcpReplyParameters {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"call_tool","arguments":` + arguments + `}}`
	req := httptest.NewRequest(http.MethodPost, MCPPath, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-bearer")
	w := httptest.NewRecorder()
	s.mcp(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", w.Code, w.Body.String())
	}
	var reply mcpReplyParameters
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func unwrapReplyParameters(t *testing.T, reply mcpReplyParameters) (mcpToolResultParameters, mcpToolResultParameters) {
	t.Helper()
	if reply.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", reply.Error)
	}
	var outer, inner mcpToolResultParameters
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

func TestMCPCallRejectsMalformedInnerArgumentsWithoutConsumingAnswer(t *testing.T) {
	for _, tc := range []struct {
		name      string
		arguments string
	}{
		{"string arguments", `{"name":"scripted","arguments":"wrong"}`},
		{"array arguments", `{"name":"scripted","arguments":[]}`},
		{"boolean arguments", `{"name":"scripted","arguments":false}`},
		{"number arguments", `{"name":"scripted","arguments":1}`},
		{"non-string name", `{"name":1,"arguments":{}}`},
		{"missing name", `{"arguments":{}}`},
		{"empty name", `{"name":"","arguments":{}}`},
		{"null envelope", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{
				cfg: Config{Bearer: "test-bearer", Tools: map[string][]ToolResponse{
					"scripted": {{Result: "first answer"}, {Result: "second answer"}},
				}},
				calls: map[string]int{},
			}
			reply := postMetaCallParameters(t, s, tc.arguments)
			if reply.Error == nil || reply.Error.Code != -32602 {
				t.Fatalf("JSON-RPC error = %+v, result = %s; want invalid params", reply.Error, reply.Result)
			}
			_, inner := unwrapReplyParameters(t, postMetaCallParameters(t, s, `{"name":"scripted","arguments":{}}`))
			if inner.IsError || len(inner.Content) != 1 || inner.Content[0].Text != "first answer" {
				t.Fatalf("first valid call = %+v; malformed call consumed the script", inner)
			}
		})
	}
}
