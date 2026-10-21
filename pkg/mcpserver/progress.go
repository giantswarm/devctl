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

// errNoProgressToken refuses a wait called without a progressToken: its
// progress is what keeps its one call alive, so it is never started.
func errNoProgressToken(tool string) error {
	return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictProgressTokenRequired,
		"%s blocks until its verdict and is kept alive by notifications/progress: call it with a progressToken in _meta", tool)
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
// Nothing is sent after wait returned.
func (s *Server) keepAlive(ctx context.Context, token mcp.ProgressToken, what string, wait func(progress *agentcli.Progress) error) error {
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
