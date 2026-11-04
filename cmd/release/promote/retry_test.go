package promote

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
)

// runNetworkCommand keeps the runner's retry transport and real GitHub clients.
// Only the version gate, credentials and underlying transport are supplied.
func runNetworkCommand(t *testing.T, ctx context.Context, f *flag, endpoint string, base http.RoundTripper) (releasepromote.Document, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	r := &runner{
		flag:   f,
		stdout: &stdout,
		stderr: &stderr,
		gate:   func(bool) error { return nil },
		open: func(_ context.Context, _ agentcli.Endpoints, transport http.RoundTripper, _ func(string)) (*releasepromote.Sources, error) {
			transport.(*agentcli.Retrying).Base = base
			return releasepromote.NewSources(agentcli.Endpoints{GitHubAPIURL: endpoint}, transport, releasepromote.Credentials{GitHubToken: "ghu_test"})
		},
	}
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	err := r.Run(cmd, []string{"giantswarm/kserve"})
	var doc releasepromote.Document
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc), "stdout must be one JSON document")
	require.Empty(t, stderr.String(), "stderr stays empty without --progress")
	require.Equal(t, agentcli.Exit(err), doc.ExitCode)
	return doc, err
}

func TestRunDryRunConnectionRefused(t *testing.T) {
	// Use real time: scaling the pauses would hide the original minutes-long
	// retry budget. The caller's deadline is only a guard for a regression.
	t.Setenv(agentcli.EnvTimeScale, "1")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	var dials atomic.Int32
	dialer := &net.Dialer{}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		return dialer.DialContext(ctx, network, address)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()

	started := time.Now()
	doc, err := runNetworkCommand(t, ctx, &flag{DryRun: true}, endpoint, transport)
	elapsed := time.Since(started)
	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.Equal(t, agentcli.VerdictRed, doc.Verdict)
	require.True(t, doc.DryRun)
	require.Len(t, doc.Repositories, 1)
	require.Equal(t, releasepromote.StateFailed, doc.Repositories[0].State)
	for _, part := range []string{endpoint, "dial tcp", "connection refused"} {
		require.Contains(t, doc.Repositories[0].Message, part)
	}
	require.Less(t, elapsed, 3*time.Second, "a refused connection must fail within a few seconds")
	require.NoError(t, ctx.Err(), "the command must finish before its caller kills it")
	require.EqualValues(t, 2, dials.Load())
	require.Len(t, doc.Warnings, 1)
}

func TestRunDryRunStalledResponse(t *testing.T) {
	t.Setenv(agentcli.EnvTimeScale, "1")
	for _, body := range []bool{false, true} {
		name := "headers"
		if body {
			name = "body"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if body {
					w.Header().Set("Content-Length", "100")
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			transport := &http.Transport{}
			t.Cleanup(transport.CloseIdleConnections)
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()

			started := time.Now()
			doc, err := runNetworkCommand(t, ctx, &flag{DryRun: true}, server.URL, transport)
			elapsed := time.Since(started)
			require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
			require.Len(t, doc.Repositories, 1)
			require.Equal(t, releasepromote.StateFailed, doc.Repositories[0].State)
			require.Contains(t, doc.Repositories[0].Message, server.URL)
			require.Contains(t, doc.Repositories[0].Message, "context deadline exceeded")
			require.Less(t, elapsed, 7*time.Second, "both attempts and the backoff must be bounded")
			require.NoError(t, ctx.Err(), "the command must finish before its caller kills it")
			require.EqualValues(t, 2, requests.Load())
			require.Len(t, doc.Warnings, 1)
		})
	}
}

func TestRunRetryRecovery(t *testing.T) {
	// Dry runs still recover on the second attempt; a real promotion keeps
	// the longer retry policy needed while following its dispatched run.
	t.Setenv(agentcli.EnvTimeScale, "0.001")
	for _, dryRun := range []bool{true, false} {
		name, failures := "dry run", 1
		if !dryRun {
			name, failures = "promotion", 2
		}
		t.Run(name, func(t *testing.T) {
			routes := promoteRoutes("giantswarm/kserve")
			key := "GET /repos/giantswarm/kserve"
			responses := make([]sequence.Response, failures)
			for i := range responses {
				responses[i] = sequence.Response{Status: http.StatusServiceUnavailable}
			}
			routes[key] = append(responses, routes[key]...)
			server, err := githubmock.Start(routes)
			require.NoError(t, err)
			t.Cleanup(server.Close)
			transport := &http.Transport{}
			t.Cleanup(transport.CloseIdleConnections)

			doc, err := runNetworkCommand(t, t.Context(), &flag{DryRun: dryRun}, server.URL, transport)
			require.NoError(t, err)
			require.Len(t, doc.Warnings, failures)
			require.Len(t, doc.Repositories, 1)
			state, writes := releasepromote.StateWouldDispatch, 0
			if !dryRun {
				state, writes = releasepromote.StateDispatched, 1
			}
			require.Equal(t, state, doc.Repositories[0].State)
			require.Equal(t, writes, dispatches(server))
		})
	}
}
