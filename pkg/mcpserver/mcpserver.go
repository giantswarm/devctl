// Package mcpserver is devctl serve: the agent-facing commands as MCP tools
// over streamable HTTP, acting on GitHub as the person who calls them.
//
// A tool answers with the command's JSON document, devctl's envelope
// included, as structured content: the exit code is a field, and the call is
// an MCP error only for auth_required. A wait is one call that blocks until
// its verdict and sends notifications/progress on the caller's progressToken
// meanwhile, at an interval below the server's registered timeout, so the
// gateway restarts that timeout on each.
//
// What only a laptop needs is not here: no version gate, no keychain or
// other credential store, no detached merge state on disk, no SIGHUP
// handling and no DEVCTL_TIME_SCALE.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
)

// Name is the server's name in its MCP implementation info.
const Name = "devctl"

// DefaultProgressInterval is how often a wait reports progress: below
// muster's default registered timeout of 30 s with headroom.
const DefaultProgressInterval = 10 * time.Second

// MaxTimeout bounds a wait's own timeout: below the hour muster keeps a
// progress-kept call alive at most, so the wait ends with devctl's own
// timeout verdict, not the gateway's.
const MaxTimeout = 55 * time.Minute

// Credentials answer the GitHub token of the person a call is made for.
type Credentials interface {
	// GitHubToken is the person's token for the call in ctx; an
	// [agentcli.ExitCoder] of [agentcli.ExitAuthRequired] when the server
	// holds none for them.
	GitHubToken(ctx context.Context) (string, error)
}

// NoCredentials is the server with no hand-over of a person's token: every
// call that reads GitHub is auth_required.
type NoCredentials struct{}

// GitHubToken implements [Credentials].
func (NoCredentials) GitHubToken(context.Context) (string, error) {
	return "", agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired,
		"this devctl server has no GitHub token for you: it is handed over by the platform once you signed in to its GitHub App")
}

// Config configures a Server.
type Config struct {
	// Version is the version the server reports: the binary's.
	Version string
	// Credentials answer the person's GitHub token; nil is [NoCredentials].
	Credentials Credentials
	// Sources build the clients a wait reads with; nil is [GitHubSources]
	// over Endpoints.
	Sources Sources
	// Endpoints are GitHub's and the registries'; zero is
	// [agentcli.DefaultEndpoints].
	Endpoints agentcli.Endpoints
	// Clock is the waits' clock; nil is the wall clock at scale 1, never
	// DEVCTL_TIME_SCALE.
	Clock *agentcli.Clock
	// ProgressInterval is how often a wait reports progress; zero is
	// DefaultProgressInterval.
	ProgressInterval time.Duration
	// Merges are the merges of this server pr_merge_status reads; nil is an
	// empty [mergejob.Memory]. pr_merge records its merges in them when they
	// take a Put, as a Memory does. Nothing is kept on disk.
	Merges mergejob.Jobs
	// ReleasePromote serves release_promote, the selector of the one tool
	// that cuts a stable release; off, the tool is not listed.
	ReleasePromote bool
	// Verifier checks the ID token the gateway forwards on every request
	// and names the caller; nil verifies none, and every call is
	// anonymous.
	Verifier Verifier
	// Metrics count the calls; nil counts none.
	Metrics *Metrics
	// Logger logs one line per call; nil logs nothing.
	Logger *slog.Logger
}

// recorder is a [mergejob.Jobs] pr_merge records its merges in.
type recorder interface {
	Put(mergejob.Status)
}

// Server is devctl serve's MCP server.
type Server struct {
	mcp              *server.MCPServer
	credentials      Credentials
	sources          Sources
	endpoints        agentcli.Endpoints
	clock            agentcli.Clock
	progressInterval time.Duration
	merges           mergejob.Jobs
	verifier         Verifier
	metrics          *Metrics
	logger           *slog.Logger
	// pulls serialize the merges of one pull request, by owner/repo#number.
	pulls pulls
}

// New returns the server of config with its tools registered.
func New(config Config) *Server {
	if config.Credentials == nil {
		config.Credentials = NoCredentials{}
	}
	if config.Endpoints == (agentcli.Endpoints{}) {
		config.Endpoints = agentcli.DefaultEndpoints()
	}
	if config.Sources == nil {
		config.Sources = GitHubSources{Endpoints: config.Endpoints}
	}
	clock := agentcli.NewClock(1, time.Now)
	if config.Clock != nil {
		clock = *config.Clock
	}
	if config.ProgressInterval <= 0 {
		config.ProgressInterval = DefaultProgressInterval
	}
	if config.Merges == nil {
		config.Merges = &mergejob.Memory{}
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s := &Server{
		credentials:      config.Credentials,
		sources:          config.Sources,
		endpoints:        config.Endpoints,
		clock:            clock,
		progressInterval: config.ProgressInterval,
		merges:           config.Merges,
		verifier:         config.Verifier,
		metrics:          config.Metrics,
		logger:           config.Logger,
	}
	s.mcp = server.NewMCPServer(Name, config.Version, server.WithToolCapabilities(false), server.WithToolHandlerMiddleware(s.observe))
	s.mcp.AddTool(prWaitTool(), s.prWait)
	s.mcp.AddTool(prMergeTool(), s.prMerge)
	s.mcp.AddTool(prMergeStatusTool(), s.prMergeStatus)
	s.mcp.AddTool(releaseWaitTool(), s.releaseWait)
	if config.ReleasePromote {
		s.mcp.AddTool(releasePromoteTool(), s.releasePromote)
	}
	return s
}

// Handler is the streamable HTTP handler of the server, at every path. With
// a Verifier it refuses a request without an ID token it accepts.
func (s *Server) Handler() http.Handler {
	handler := server.NewStreamableHTTPServer(s.mcp, server.WithHTTPContextFunc(httpContext))
	if s.verifier == nil {
		return handler
	}
	return authenticate(s.verifier, handler)
}

// MCP is the server's MCP server, for in-process clients.
func (s *Server) MCP() *server.MCPServer { return s.mcp }

// answer finishes doc with err, the outcome of its command, and returns it as
// the tool's result: structured content and its JSON as text, an MCP error
// only for auth_required.
func answer(doc agentcli.Document, ok agentcli.Verdict, err error) (*mcp.CallToolResult, error) {
	text, err := finish(doc, ok, err)
	if err != nil {
		return nil, err
	}
	return resultOf(doc, text)
}

// finish finishes doc with err, the outcome of its command, and returns its
// JSON.
func finish(doc agentcli.Document, ok agentcli.Verdict, err error) ([]byte, error) {
	doc.Finish(time.Now(), ok, err)
	text, merr := json.Marshal(doc)
	if merr != nil {
		return nil, fmt.Errorf("encoding the document: %w", merr)
	}
	return text, nil
}

// resultOf is the tool result of doc, finished, and text, its JSON.
func resultOf(doc agentcli.Document, text []byte) (*mcp.CallToolResult, error) {
	// The structured content is the JSON object the text carries.
	var structured map[string]any
	if err := json.Unmarshal(text, &structured); err != nil {
		return nil, fmt.Errorf("decoding the document: %w", err)
	}
	result := mcp.NewToolResultStructured(structured, string(text))
	result.IsError = agentcli.Exit(doc.Err()) == agentcli.ExitAuthRequired
	return result, nil
}

// writes is the annotation of a tool that changes GitHub: no read-only hint,
// so a read-only preset leaves it out. idempotent says a second call with
// the same arguments changes nothing more.
func writes(idempotent bool) mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    mcp.ToBoolPtr(false),
		DestructiveHint: mcp.ToBoolPtr(false),
		IdempotentHint:  mcp.ToBoolPtr(idempotent),
		OpenWorldHint:   mcp.ToBoolPtr(true),
	})
}

// readOnly is the annotation of a tool that changes nothing: the waits and
// pr_merge_status.
func readOnly() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    mcp.ToBoolPtr(true),
		DestructiveHint: mcp.ToBoolPtr(false),
		IdempotentHint:  mcp.ToBoolPtr(true),
		OpenWorldHint:   mcp.ToBoolPtr(true),
	})
}
