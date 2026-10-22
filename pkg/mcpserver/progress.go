package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// progressToken is the token a call asks its progress on, nil without one.
func progressToken(request mcp.CallToolRequest) mcp.ProgressToken {
	if request.Params.Meta == nil {
		return nil
	}
	return request.Params.Meta.ProgressToken
}

// bound is the timeout of a wait called with token: timeout with one; without
// one, which nothing keeps alive past the gateway's registered timeout, at
// most the server's no-progress timeout, and warn tells the caller how to go
// on.
func (s *Server) bound(token mcp.ProgressToken, tool string, timeout time.Duration, warn func(string)) time.Duration {
	if token != nil {
		return timeout
	}
	warn(fmt.Sprintf("called without a progressToken: %s answers within %s, the verdict it reached by then; "+
		"while it is pending (timeout, exit 2) call it again, or call it with a progressToken in _meta to wait for the verdict in one call",
		tool, s.noProgressTimeout))
	if timeout <= 0 || timeout > s.noProgressTimeout {
		return s.noProgressTimeout
	}
	return timeout
}

// lastLine keeps the last line a wait's progress wrote.
type lastLine struct {
	mu   sync.Mutex
	line string
}

func (l *lastLine) Write(p []byte) (int, error) {
	if line := strings.TrimSpace(string(p)); line != "" {
		l.mu.Lock()
		l.line = line
		l.mu.Unlock()
	}
	return len(p), nil
}

func (l *lastLine) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.line
}

// keepAlive runs wait and, until it returns, sends notifications/progress on
// token once at the start and then every progress interval: a progress
// that increases and a message naming what and the wait's latest line.
// Nothing is sent after wait returned, and nothing at all without a token.
func (s *Server) keepAlive(ctx context.Context, token mcp.ProgressToken, what string, wait func(progress *agentcli.Progress) error) error {
	if token == nil {
		return wait(agentcli.NewProgress(nil, false))
	}
	lines := &lastLine{}
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(s.progressInterval)
		defer ticker.Stop()
		for n := 1; ; n++ {
			message := "waiting for " + what
			if line := lines.String(); line != "" {
				message = fmt.Sprintf("%s: %s", what, line)
			}
			// A notification that does not reach the caller leaves the wait
			// to the gateway's timeout; the wait goes on.
			_ = s.mcp.SendNotificationToClient(ctx, string(mcp.MethodNotificationProgress), map[string]any{
				"progressToken": token,
				"progress":      n,
				"message":       message,
			})
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	err := wait(agentcli.NewProgress(lines, true))
	close(done)
	<-stopped
	return err
}
