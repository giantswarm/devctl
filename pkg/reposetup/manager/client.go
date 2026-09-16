// Package manager is the client of giantswarm-repo-manager, the MCP server
// behind muster that keeps the repository inventory. `devctl repo status`
// asks it first -- through the person's muster endpoint, as the person --
// and falls back to the engine's own checks when it cannot be reached. The
// tool's answer carries the set-up state as the engine's structured result,
// so both paths print the same verdicts.
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
	"time"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// ToolGetRepository is the muster tool that returns one inventory record.
const ToolGetRepository = "giantswarm-repo-manager_get_repository"

// protocolVersion is the MCP revision the client speaks.
const protocolVersion = "2025-03-26"

// Record is one inventory record of giantswarm-repo-manager as `devctl repo
// status` reads it. The manager's tool schema is filled in by its own
// change; the client reads the fields below and ignores the rest, and a
// payload that is the set-up result itself is accepted as one too.
type Record struct {
	// Repository is owner/name.
	Repository string `json:"repository"`
	// Team is the slug of the team file that declares it; empty when no file
	// does.
	Team string `json:"team,omitempty"`
	// Setup is the engine's check result the inventory stores.
	Setup *reconcile.Result `json:"setup,omitempty"`
	// UpdatedAt is when the record was refreshed.
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// RepositoryGetter answers `devctl repo status` from the inventory. Client
// satisfies it; a test fakes it.
type RepositoryGetter interface {
	GetRepository(ctx context.Context, repository string) (*Record, error)
}

// Client calls giantswarm-repo-manager's tools through a muster endpoint
// over MCP's streamable HTTP transport.
type Client struct {
	// Endpoint is the muster MCP endpoint (https://muster.example/mcp).
	Endpoint string
	// Token is the person's bearer token for the endpoint; empty sends none.
	Token string
	// HTTPClient overrides the HTTP client; nil means one with a timeout.
	HTTPClient *http.Client
	// Version is reported as the client's version in the handshake.
	Version string
}

// GetRepository calls the get_repository tool for owner/name.
func (c *Client) GetRepository(ctx context.Context, repository string) (*Record, error) {
	if c.Endpoint == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Endpoint must not be empty", c)
	}

	session, err := c.initialize(ctx)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	result, err := c.call(ctx, session, 2, "tools/call", map[string]any{
		"name":      ToolGetRepository,
		"arguments": map[string]any{"repository": repository},
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var tool struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &tool); err != nil {
		return nil, microerror.Maskf(unreachableError, "%s: tool result is not an MCP result: %v", ToolGetRepository, err)
	}
	payload := tool.StructuredContent
	if len(payload) == 0 {
		for _, content := range tool.Content {
			if content.Type == "text" {
				payload = json.RawMessage(content.Text)
				break
			}
		}
	}
	if tool.IsError {
		return nil, microerror.Maskf(toolError, "%s: %s", ToolGetRepository, strings.TrimSpace(string(payload)))
	}
	if len(payload) == 0 {
		return nil, microerror.Maskf(toolError, "%s: empty result", ToolGetRepository)
	}

	var record Record
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, microerror.Maskf(toolError, "%s: result is not an inventory record: %v", ToolGetRepository, err)
	}
	if record.Setup == nil {
		// The payload may be the set-up result itself.
		var setup reconcile.Result
		if err := json.Unmarshal(payload, &setup); err == nil && len(setup.Steps) > 0 {
			record.Setup = &setup
			if record.Repository == "" {
				record.Repository = setup.Repository
			}
			if record.Team == "" {
				record.Team = setup.Team
			}
		}
	}
	if record.Repository == "" {
		record.Repository = repository
	}

	return &record, nil
}

// initialize performs the MCP handshake and returns the session id, empty
// for a stateless server.
func (c *Client) initialize(ctx context.Context) (string, error) {
	version := c.Version
	if version == "" {
		version = "dev"
	}
	_, session, err := c.post(ctx, "", request(1, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "devctl", "version": version},
	}))
	if err != nil {
		return "", microerror.Mask(err)
	}
	if _, _, err := c.post(ctx, session, request(0, "notifications/initialized", nil)); err != nil {
		return "", microerror.Mask(err)
	}
	return session, nil
}

// call sends one request and returns its result.
func (c *Client) call(ctx context.Context, session string, id int, method string, params any) (json.RawMessage, error) {
	body, _, err := c.post(ctx, session, request(id, method, params))
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

// request builds a JSON-RPC request, or a notification when id is 0.
func request(id int, method string, params any) rpcMessage {
	m := rpcMessage{JSONRPC: "2.0", Method: method, Params: params}
	if id != 0 {
		m.ID = &id
	}
	return m
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
	return &http.Client{Timeout: 30 * time.Second}
}
