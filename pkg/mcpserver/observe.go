package mcpserver

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/prometheus/client_golang/prometheus"
)

// Anonymous is the subject of a call no verified caller made.
const Anonymous = "anonymous"

// Metrics are the server's call metrics, labelled with the tool, the
// calling person's subject, the repository (owner/repo, empty for a tool
// that names none) and the verdict.
type Metrics struct {
	calls    *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewMetrics registers the server's metrics with registerer.
func NewMetrics(registerer prometheus.Registerer) (*Metrics, error) {
	labels := []string{"tool", "subject", "repository", "verdict"}
	m := &Metrics{
		calls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "devctl",
			Subsystem: "mcp",
			Name:      "tool_calls_total",
			Help:      "Tool calls answered, by tool, calling subject, repository and verdict.",
		}, labels),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "devctl",
			Subsystem: "mcp",
			Name:      "tool_call_duration_seconds",
			Help:      "How long a tool call took, a wait included, by tool, calling subject, repository and verdict.",
			Buckets:   []float64{0.1, 0.5, 1, 5, 30, 60, 300, 900, 1800, 3600},
		}, labels),
	}
	for _, c := range []prometheus.Collector{m.calls, m.duration} {
		if err := registerer.Register(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// httpContext puts the person's grants muster attached and the caller the
// server verified into a call's context.
func httpContext(ctx context.Context, r *http.Request) context.Context {
	ctx = context.WithValue(ctx, grantsKey{}, grantsOf(r))
	if caller, ok := CallerFrom(r.Context()); ok {
		ctx = context.WithValue(ctx, callerKey{}, caller)
	}
	return ctx
}

type grantsKey struct{}

// observe follows every tool call: it gives the call its state, answers a
// call that lacked the person's grant so muster turns it into the App's
// sign-in link, and logs and counts the call by its caller and repository.
func (s *Server) observe(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repository := request.GetString("repository", "")
		c := &call{}
		c.grants, _ = ctx.Value(grantsKey{}).(map[string]string)
		if owner, _, ok := strings.Cut(repository, "/"); ok {
			c.owner = owner
		}
		started := time.Now()
		result, err := next(context.WithValue(ctx, callKey{}, c), request)
		if c.grantRequired != "" {
			grantRequired(result, c.grantRequired)
		}

		tool := request.Params.Name
		subject, email := Anonymous, ""
		if caller, ok := CallerFrom(ctx); ok {
			subject, email = caller.Subject, caller.Email
		}
		verdict, exitCode := outcomeOf(result, err)
		elapsed := time.Since(started)
		if s.metrics != nil {
			s.metrics.calls.WithLabelValues(tool, subject, repository, verdict).Inc()
			s.metrics.duration.WithLabelValues(tool, subject, repository, verdict).Observe(elapsed.Seconds())
		}
		s.logger.LogAttrs(ctx, slog.LevelInfo, "tool call",
			slog.String("tool", tool),
			slog.String("subject", subject),
			slog.String("email", email),
			slog.String("repository", repository),
			slog.String("verdict", verdict),
			slog.Int("exitCode", exitCode),
			slog.Duration("duration", elapsed),
		)
		return result, err
	}
}

// grantRequired marks result, an auth_required answer, as needing the
// person's grant of app: muster answers it with the App's sign-in link.
func grantRequired(result *mcp.CallToolResult, app string) {
	if result == nil {
		return
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		return
	}
	structured["status"] = StatusGrantRequired
	structured["apps"] = []any{app}
}

// outcomeOf is the verdict and exit code a tool answered.
func outcomeOf(result *mcp.CallToolResult, err error) (string, int) {
	if err != nil {
		return "error", -1
	}
	if result == nil {
		return "", 0
	}
	structured, _ := result.StructuredContent.(map[string]any)
	verdict, _ := structured["verdict"].(string)
	exitCode, _ := structured["exitCode"].(float64)
	return verdict, int(exitCode)
}
