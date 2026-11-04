package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/giantswarm/microerror"
)

// The replica drain answers call_tool itself, without its usual envelope.
const interruptedAnswer = `{"isError":true,"content":[{"type":"text","text":"interrupted: muster replica is shutting down"}],"_meta":{"giantswarm.io/interrupted":{"reason":"muster replica is shutting down","retryable":true}}}`

type musterRoundTripper func(*http.Request) (*http.Response, error)

func (f musterRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// retryMuster runs the HTTP/JSON-RPC exchange in memory, so synctest can
// measure the backoff and cancel it without network I/O or wall-clock waits.
type retryMuster struct {
	t               *testing.T
	sse             bool
	tool            string
	answers         []string
	initializations int
	callTimes       []time.Time
}

func (m *retryMuster) RoundTrip(r *http.Request) (*http.Response, error) {
	m.t.Helper()
	var msg rpcMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		m.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	if r.Header.Get("Authorization") != "Bearer test-token" {
		m.t.Errorf("missing caller's bearer token")
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: r}
	result := `{}`
	switch msg.Method {
	case "initialize":
		m.initializations++
		response.Header.Set("Mcp-Session-Id", "session-1")
		result = `{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"muster"}}`
	case "notifications/initialized":
		response.StatusCode = http.StatusAccepted
		response.Body = io.NopCloser(strings.NewReader(""))
		return response, nil
	case "tools/call":
		if r.Header.Get("Mcp-Session-Id") != "session-1" {
			m.t.Errorf("tools/call without the session")
		}
		wantParams := map[string]any{
			"name": "call_tool",
			"arguments": map[string]any{
				"name":      m.tool,
				"arguments": map[string]any{"repository": "giantswarm/my-service"},
			},
		}
		if !reflect.DeepEqual(msg.Params, wantParams) {
			m.t.Errorf("tools/call params = %#v, want %#v", msg.Params, wantParams)
		}
		m.callTimes = append(m.callTimes, time.Now())
		if len(m.callTimes) > len(m.answers) {
			m.t.Fatalf("unexpected tools/call attempt %d", len(m.callTimes))
		}
		if msg.ID == nil || *msg.ID != len(m.callTimes)+1 {
			m.t.Fatal("tools/call must have a fresh request id")
		}
		result = m.answers[len(m.callTimes)-1]
	default:
		m.t.Fatalf("unexpected method %q", msg.Method)
	}
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, *msg.ID, result)
	response.Header.Set("Content-Type", "application/json")
	if m.sse {
		response.Header.Set("Content-Type", "text/event-stream")
		body = "event: message\ndata: " + body + "\n\n"
	}
	response.Body = io.NopCloser(strings.NewReader(body))
	return response, nil
}

func TestClientCallInterrupted(t *testing.T) {
	success := envelope(false, "ok", "")
	wrapped := func(answer string) string {
		return envelope(false, answer, "")
	}
	cases := []struct {
		name        string
		answers     []string
		wantPayload string
		wantError   string
	}{
		{
			name:        "case 0: interruption then success",
			answers:     []string{interruptedAnswer, wrapped(success)},
			wantPayload: "ok",
		},
		{
			name:      "case 1: second interruption is returned unchanged",
			answers:   []string{interruptedAnswer, strings.ReplaceAll(interruptedAnswer, "shutting down", "still shutting down")},
			wantError: "interrupted: muster replica is still shutting down",
		},
		{
			name:      "case 2: retry's backend error is returned unchanged",
			answers:   []string{interruptedAnswer, wrapped(envelope(true, "repository not in the inventory", ""))},
			wantError: "repository not in the inventory",
		},
		{
			name:      "case 3: non-retryable interruption",
			answers:   []string{strings.ReplaceAll(interruptedAnswer, `"retryable":true`, `"retryable":false`)},
			wantError: "interrupted: muster replica is shutting down",
		},
		{
			name:      "case 4: interruption text without metadata",
			answers:   []string{`{"isError":true,"content":[{"type":"text","text":"interrupted: call again"}]}`},
			wantError: "interrupted: call again",
		},
		{
			name:      "case 5: malformed retryable flag",
			answers:   []string{strings.ReplaceAll(interruptedAnswer, `"retryable":true`, `"retryable":"true"`)},
			wantError: "interrupted: muster replica is shutting down",
		},
		{
			name:      "case 6: unrelated retryable metadata",
			answers:   []string{strings.ReplaceAll(interruptedAnswer, "giantswarm.io/interrupted", "other/error")},
			wantError: "interrupted: muster replica is shutting down",
		},
		{
			name:      "case 7: ordinary backend error",
			answers:   []string{wrapped(envelope(true, "permission denied", ""))},
			wantError: "permission denied",
		},
		{
			name:      "case 8: backend payload is not a muster interruption",
			answers:   []string{wrapped(envelope(true, interruptedAnswer, ""))},
			wantError: interruptedAnswer,
		},
		{
			name:        "case 9: success with interruption metadata",
			answers:     []string{strings.ReplaceAll(wrapped(success), `"isError":false`, `"isError":false,"_meta":{"giantswarm.io/interrupted":{"retryable":true}}`)},
			wantPayload: "ok",
		},
	}
	for _, sse := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("SSE=%t/%s", sse, tc.name), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					m := &retryMuster{t: t, sse: sse, tool: ToolGetInfo, answers: tc.answers}
					c := &Client{Endpoint: "https://muster.example/mcp", Token: "test-token", HTTPClient: &http.Client{Transport: m}}
					started := time.Now()
					got, err := c.Call(t.Context(), ToolGetInfo, map[string]any{"repository": "giantswarm/my-service"})
					if tc.wantError != "" {
						want := microerror.Maskf(toolError, "%s: %s", ToolGetInfo, tc.wantError)
						if !IsTool(err) || err.Error() != want.Error() || got != nil {
							t.Fatalf("got (%s, %v), want (nil, %v)", got, err, want)
						}
					} else if err != nil || string(got) != tc.wantPayload {
						t.Fatalf("got (%s, %v), want (%s, nil)", got, err, tc.wantPayload)
					}
					if m.initializations != 1 || len(m.callTimes) != len(tc.answers) {
						t.Fatalf("handshakes = %d, calls = %d, want 1 and %d", m.initializations, len(m.callTimes), len(tc.answers))
					}
					if len(m.callTimes) == 2 && m.callTimes[1].Sub(m.callTimes[0]) != time.Second {
						t.Error("retry did not wait one second after the interruption")
					}
					wantDelay := time.Duration(len(tc.answers)-1) * time.Second
					if elapsed := time.Since(started); elapsed != wantDelay {
						t.Errorf("call took %v, want %v", elapsed, wantDelay)
					}
				})
			})
		}
	}
}

func TestClientCallInterruptedContext(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("deadline=%t", deadline), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				wantErr := context.Canceled
				if deadline {
					cancel()
					ctx, cancel = context.WithTimeout(t.Context(), time.Second/2)
					wantErr = context.DeadlineExceeded
				}
				defer cancel()
				m := &retryMuster{t: t, tool: ToolGetInfo, answers: []string{interruptedAnswer}}
				transport := musterRoundTripper(func(r *http.Request) (*http.Response, error) {
					response, err := m.RoundTrip(r)
					if !deadline && len(m.callTimes) == 1 {
						cancel()
					}
					return response, err
				})
				c := &Client{Endpoint: "https://muster.example/mcp", Token: "test-token", HTTPClient: &http.Client{Transport: transport}}
				started := time.Now()
				got, err := c.Call(ctx, ToolGetInfo, map[string]any{"repository": "giantswarm/my-service"})
				if got != nil || microerror.Cause(err) != wantErr || len(m.callTimes) != 1 {
					t.Fatalf("got (%s, %v), calls = %d, want (nil, %v), 1 call", got, err, len(m.callTimes), wantErr)
				}
				if elapsed := time.Since(started); elapsed >= time.Second {
					t.Errorf("context did not stop the backoff: %v", elapsed)
				}
			})
		})
	}
}

// An interrupted write may already have taken effect. Preserve its error so
// the caller checks the effect before deciding whether to repeat the write.
func TestClientCallInterruptedWrites(t *testing.T) {
	tools := []string{
		ToolCreateRepository, ToolAdoptRepository, ToolUpdateRepository,
		ToolTransferRepository, ToolSetLifecycle, ToolApproveChange,
		ToolAlignRepository, ToolRefreshRepository, ToolSweepInventory,
		ToolAuthLogin, ToolPrefix + "future_tool",
	}
	for i, tool := range tools {
		t.Run(fmt.Sprintf("case %d: %s", i, tool), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m := &retryMuster{t: t, tool: tool, answers: []string{interruptedAnswer}}
				c := &Client{Endpoint: "https://muster.example/mcp", Token: "test-token", HTTPClient: &http.Client{Transport: m}}
				started := time.Now()
				got, err := c.Call(t.Context(), tool, map[string]any{"repository": "giantswarm/my-service"})
				want := microerror.Maskf(toolError, "%s: interrupted: muster replica is shutting down", tool)
				if got != nil || !IsTool(err) || err.Error() != want.Error() {
					t.Fatalf("got (%s, %v), want (nil, %v)", got, err, want)
				}
				if len(m.callTimes) != 1 || time.Since(started) != 0 {
					t.Fatal("interrupted write must return immediately without retrying")
				}
			})
		})
	}
}
