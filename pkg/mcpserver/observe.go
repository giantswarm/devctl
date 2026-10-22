package mcpserver

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// Anonymous is the login of a call refused before its grant named the
// caller: one without a grant, or one refused as usage.
const Anonymous = "anonymous"

// Metrics are the server's call metrics, labelled with the tool, the
// caller's GitHub login and user id (Anonymous and empty for a call whose
// grant named none), the repository (owner/repo; empty for a tool that
// names none or an argument refused as usage) and the verdict.
type Metrics struct {
	calls    *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewMetrics registers the server's metrics with registerer.
func NewMetrics(registerer prometheus.Registerer) (*Metrics, error) {
	labels := []string{"tool", "login", "user_id", "repository", "verdict"}
	m := &Metrics{
		calls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "devctl",
			Subsystem: "mcp",
			Name:      "tool_calls_total",
			Help:      "Tool calls answered, by tool, calling GitHub login and user id, repository and verdict.",
		}, labels),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "devctl",
			Subsystem: "mcp",
			Name:      "tool_call_duration_seconds",
			Help:      "How long a tool call took, a wait included, by tool, calling GitHub login and user id, repository and verdict.",
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

// httpContext puts the person's grants and Dex token muster attached into a
// call's context.
func httpContext(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, requestKey{}, callRequest{grants: grantsOf(r), bearer: bearerOf(r)})
}

type requestKey struct{}

// callRequest is what a call's HTTP request carries for it.
type callRequest struct {
	grants map[string]string
	bearer string
}

// observe follows every tool call: it gives the call its state, answers a
// call that lacked the person's grant so muster turns it into the App's
// sign-in link, and logs and counts the call by the caller the grant names
// and the repository.
func (s *Server) observe(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repository := request.GetString("repository", "")
		r, _ := ctx.Value(requestKey{}).(callRequest)
		c := &call{grants: r.grants, bearer: r.bearer}
		if owner, _, ok := strings.Cut(repository, "/"); ok {
			c.owner = owner
		}
		started := time.Now()
		result, err := next(context.WithValue(ctx, callKey{}, c), request)
		if c.grantRequired != "" {
			grantRequired(result, c.grantRequired)
		}

		tool := request.Params.Name
		login, userID := Anonymous, ""
		if c.caller.Login != "" {
			login, userID = c.caller.Login, strconv.FormatInt(c.caller.ID, 10)
		}
		verdict, exitCode := outcomeOf(result, err)
		elapsed := time.Since(started)
		if s.metrics != nil {
			label := repositoryLabel(repository, verdict)
			s.metrics.calls.WithLabelValues(tool, login, userID, label, verdict).Inc()
			s.metrics.duration.WithLabelValues(tool, login, userID, label, verdict).Observe(elapsed.Seconds())
		}
		s.logger.LogAttrs(ctx, slog.LevelInfo, "tool call",
			slog.String("tool", tool),
			slog.String("login", login),
			slog.String("userId", userID),
			slog.String("repository", repository),
			slog.String("verdict", verdict),
			slog.Int("exitCode", exitCode),
			slog.Duration("duration", elapsed),
		)
		return result, err
	}
}

// repositoryPattern is a GitHub owner/repo.
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)

// repositoryLabel is the repository a call's metrics name: only a GitHub
// owner/repo of a call past its usage checks, so a caller's free-form
// argument never becomes a label value.
func repositoryLabel(repository, verdict string) string {
	if verdict == string(agentcli.VerdictUsage) || !repositoryPattern.MatchString(repository) {
		return ""
	}
	return repository
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
