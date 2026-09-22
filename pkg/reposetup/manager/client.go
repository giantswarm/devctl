// Package manager is the client of giantswarm-repo-manager, the MCP server
// behind muster that keeps the repository inventory and lands the team-file
// changes as the person. The repo commands reach it through the person's
// muster endpoint with the muster token of the keychain; every tool of the
// manager is one call here, and the tool's answer is printed as it came.
package manager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/giantswarm/microerror"
)

// Server is the manager's name in muster, and ToolPrefix what muster puts in
// front of every tool of an external server.
const (
	Server     = "giantswarm-repo-manager"
	ToolPrefix = "x_" + Server + "_"
)

// The manager's tools, as muster exposes them.
const (
	ToolGetInfo            = ToolPrefix + "get_info"
	ToolListRepositories   = ToolPrefix + "list_repositories"
	ToolGetRepository      = ToolPrefix + "get_repository"
	ToolRefreshRepository  = ToolPrefix + "refresh_repository"
	ToolSweepInventory     = ToolPrefix + "sweep_inventory"
	ToolValidateRepository = ToolPrefix + "validate_repository"
	ToolCreateRepository   = ToolPrefix + "create_repository"
	ToolWatchRepository    = ToolPrefix + "watch_repository"
	ToolAdoptRepository    = ToolPrefix + "adopt_repository"
	ToolUpdateRepository   = ToolPrefix + "update_repository"
	ToolTransferRepository = ToolPrefix + "transfer_repository"
	ToolSetLifecycle       = ToolPrefix + "set_lifecycle"
	ToolApproveChange      = ToolPrefix + "approve_change"
	ToolAlignRepository    = ToolPrefix + "align_repository"

	// ToolAuthLogin is muster's own: the sign-in to one of its servers,
	// answering the URL the person opens once.
	ToolAuthLogin = "core_auth_login"
)

// protocolVersion is the MCP revision the client speaks.
const protocolVersion = "2025-03-26"

// Caller calls the manager's tools. Client satisfies it; a test fakes it.
type Caller interface {
	Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error)
}

// Client calls giantswarm-repo-manager's tools through a muster endpoint
// over MCP's streamable HTTP transport. One Client is one MCP session:
// initialized on the first call, kept for the next.
type Client struct {
	// Endpoint is the muster MCP endpoint (https://muster.example/mcp).
	Endpoint string
	// Token is the person's bearer token for the endpoint; empty sends none.
	Token string
	// HTTPClient overrides the HTTP client; nil means one without a timeout
	// of its own, bounded by the context of every call.
	HTTPClient *http.Client
	// Version is reported as the client's version in the handshake.
	Version string

	mu      sync.Mutex
	session string
	ready   bool
	nextID  int
}

// GetRepository calls the get_repository tool for owner/name.
func (c *Client) GetRepository(ctx context.Context, repository string) (*Record, error) {
	payload, err := c.Call(ctx, ToolGetRepository, map[string]any{"repository": repository})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	var record Record
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, microerror.Maskf(toolError, "%s: result is not an inventory record: %v", ToolGetRepository, err)
	}
	if record.Repository == "" {
		record.Repository = repository
	}
	return &record, nil
}

// Call calls tool with args and returns the result's payload: its
// structured content when the tool has one, else its first text content
// as raw bytes (JSON when the text is JSON, the text otherwise). A tool
// that answers an error is [IsTool] with the text; an endpoint that refuses
// the bearer is [IsAuthRequired]; one that does not answer is
// [IsUnreachable].
func (c *Client) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	if c.Endpoint == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Endpoint must not be empty", c)
	}
	if args == nil {
		args = map[string]any{}
	}

	session, err := c.initialize(ctx)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	result, err := c.call(ctx, session, "tools/call", map[string]any{
		"name":      tool,
		"arguments": args,
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var res struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return nil, microerror.Maskf(unreachableError, "%s: tool result is not an MCP result: %v", tool, err)
	}
	payload := res.StructuredContent
	if len(payload) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		payload = nil
		for _, content := range res.Content {
			if content.Type == "text" {
				payload = json.RawMessage(content.Text)
				break
			}
		}
	}
	if res.IsError {
		return nil, microerror.Maskf(toolError, "%s: %s", tool, strings.TrimSpace(string(payload)))
	}
	if len(payload) == 0 {
		return nil, microerror.Maskf(toolError, "%s: empty result", tool)
	}
	return payload, nil
}

// initialize performs the MCP handshake once and returns the session id,
// empty for a stateless server.
func (c *Client) initialize(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ready {
		return c.session, nil
	}
	version := c.Version
	if version == "" {
		version = "dev"
	}
	_, session, err := c.post(ctx, "", c.request("initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "devctl", "version": version},
	}))
	if err != nil {
		return "", microerror.Mask(err)
	}
	if _, _, err := c.post(ctx, session, notification("notifications/initialized")); err != nil {
		return "", microerror.Mask(err)
	}
	c.session, c.ready = session, true
	return session, nil
}

// call sends one request and returns its result.
func (c *Client) call(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	msg := c.request(method, params)
	c.mu.Unlock()
	body, _, err := c.post(ctx, session, msg)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return body, nil
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// request builds a JSON-RPC request with the next id. The caller holds c.mu.
func (c *Client) request(method string, params any) rpcMessage {
	c.nextID++
	id := c.nextID
	return rpcMessage{JSONRPC: "2.0", ID: &id, Method: method, Params: params}
}

// notification builds a JSON-RPC notification.
func notification(method string) rpcMessage {
	return rpcMessage{JSONRPC: "2.0", Method: method}
}

// post sends one message and returns the matching response's result (nil
// for a notification) and the session id the server assigned or kept.
func (c *Client) post(ctx context.Context, session string, msg rpcMessage) (json.RawMessage, string, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, "", microerror.Mask(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, "", microerror.Mask(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, "", microerror.Maskf(unreachableError, "%s: %v", c.Endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, "", microerror.Maskf(authRequiredError, "%s refused the token: HTTP %d", c.Endpoint, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", microerror.Maskf(unreachableError, "%s: %s %s: HTTP %d", c.Endpoint, msg.Method, resp.Request.URL.Path, resp.StatusCode)
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		session = sid
	}
	if msg.ID == nil {
		return nil, session, nil
	}

	response, err := readResponse(resp, *msg.ID)
	if err != nil {
		return nil, "", microerror.Maskf(unreachableError, "%s: %s: %v", c.Endpoint, msg.Method, err)
	}
	if response.Error != nil {
		return nil, "", microerror.Maskf(toolError, "%s: %s (code %d)", msg.Method, response.Error.Message, response.Error.Code)
	}
	return response.Result, session, nil
}

// readResponse reads the JSON-RPC response with the given id from a JSON
// body or an SSE stream.
func readResponse(resp *http.Response, id int) (*rpcMessage, error) {
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		var m rpcMessage
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
			return nil, fmt.Errorf("decoding the response: %w", err)
		}
		return &m, nil
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var data strings.Builder
	for {
		more := scanner.Scan()
		line := scanner.Text()
		if more && line != "" {
			if strings.HasPrefix(line, "data:") {
				data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
			continue
		}
		if data.Len() > 0 {
			var m rpcMessage
			if err := json.Unmarshal([]byte(data.String()), &m); err == nil && m.ID != nil && *m.ID == id {
				return &m, nil
			}
			data.Reset()
		}
		if !more {
			break
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return nil, err
	}
	return nil, fmt.Errorf("no response with id %d in the event stream", id)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}
