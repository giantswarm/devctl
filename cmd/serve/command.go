// Package serve is devctl serve: the agent-facing commands as MCP tools over
// streamable HTTP.
package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mcpserver"
	"github.com/giantswarm/devctl/v8/pkg/project"
)

const (
	name        = "serve"
	description = "Serve the agent-facing commands as MCP tools over streamable HTTP."
	long        = `Serve the agent-facing commands as MCP tools over streamable HTTP, at /mcp:
pr_wait, pr_merge, pr_merge_status, release_wait and rollout_wait, and
release_promote with --release-promote. Each answers the command's JSON
document, devctl's envelope included, as structured content; the exit code
is a field, and the call is an MCP error only for auth_required.

A wait is one call: it blocks until its verdict and sends
notifications/progress on the caller's progressToken every
--progress-interval, which keeps the call alive through a gateway that
restarts its timeout on progress (muster). A wait without a progressToken,
which a client calling through muster's call_tool or muster call may send
none of, sends no progress and answers within --no-progress-timeout: the
verdict it reached by then, timeout (exit 2) while still pending, and a
warning saying so; calling again goes on. pr_merge never merges a head it
has not judged green by then.

pr_merge is devctl pr merge in one call: the wait for green, the merge with
the judged head as the expected head, then the release wait, with the CLI's
refusals. A pull request merged already answers its merge and its release
instead of exit 3, so a second call after a lost answer merges nothing.
pr_merge and release_promote carry no read-only annotation.

The server acts on GitHub as the person who calls it, with their token of
the GitHub App muster keeps as --grant-app, which muster attaches to every
call in X-Muster-Grant-<app> (the MCPServer's spec.auth.providerApps). That
token names the caller: GitHub's user for it (GET /user, kept five minutes),
whose login and id each call's log line on stderr and its metrics at
--metrics-address name, with the repository. Without the token, or with one
GitHub refuses, every tool call is auth_required, structured status
grant_required, which muster answers with the App's sign-in link; a
repository whose owner the App is not installed on is not_applicable. It holds no CircleCI credential: the waits read CircleCI through the commit statuses it posts to GitHub, and a job the revision's CircleCI configuration runs that has posted none yet, one behind requires:, keeps a wait pending. No rerun and no CircleCI job listing is served. It reads no keychain or credential file, runs no version check
(the deployment pins the version), writes nothing to disk and ignores
DEVCTL_TIME_SCALE.

rollout_wait reads an installation's management cluster as the person too:
muster forwards the person's Dex token (forwardToken), which the server
exchanges at the installation's Dex (RFC 8693, its connector that trusts the
hub's Dex) for a token the cluster's API accepts, so the cluster answers by
the person's access. --installations names each installation's API and Dex,
and the files of the exchange client the hub's muster presents there; the
server reads no kubeconfig and holds no credential of a cluster. Without a
forwarded token the call is auth_required; an installation not in the file
is not_applicable. It never asks Flux to reconcile.`

	// shutdownTimeout bounds the drain of running calls on SIGTERM.
	shutdownTimeout = 30 * time.Second
)

type Config struct {
	Stderr io.Writer
}

type flag struct {
	Address           string
	MetricsAddress    string
	ProgressInterval  time.Duration
	NoProgressTimeout time.Duration
	ReleasePromote    bool
	GrantApp          string
	GitHubApp         string
	Installations     string
}

func New(config Config) (*cobra.Command, error) {
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	f := &flag{}
	c := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  long,
		Args:  cobra.NoArgs,
		// The deployment pins the version; a server never stops for a
		// newer release.
		Annotations: versiongate.Exempt(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return run(ctx, config.Stderr, f)
		},
	}
	c.Flags().StringVar(&f.Address, "address", ":8080", "Address to listen on")
	c.Flags().BoolVar(&f.ReleasePromote, "release-promote", false, "Also serve release_promote, which cuts stable releases")
	c.Flags().StringVar(&f.MetricsAddress, "metrics-address", "", "Address to serve /metrics on; empty serves none")
	c.Flags().StringVar(&f.GrantApp, "grant-app", "", "The provider App in muster whose grant carries the person's GitHub token (X-Muster-Grant-<app>); empty: every call is auth_required")
	c.Flags().StringVar(&f.GitHubApp, "github-app", "", "The GitHub App behind --grant-app, as refusals name it; empty is --grant-app")
	c.Flags().StringVar(&f.Installations, "installations", "", "A JSON file of the installations rollout_wait reads as the person, through token exchange at each one's Dex; empty: rollout_wait reaches none")
	c.Flags().DurationVar(&f.ProgressInterval, "progress-interval", mcpserver.DefaultProgressInterval, "How often a wait sends notifications/progress; below the registered timeout of the gateway in front")
	c.Flags().DurationVar(&f.NoProgressTimeout, "no-progress-timeout", mcpserver.DefaultNoProgressTimeout, "How long a wait called without a progressToken runs before it answers; below the registered timeout of the gateway in front")
	return c, nil
}

func run(ctx context.Context, stderr io.Writer, f *flag) error {
	if f.ProgressInterval <= 0 {
		return fmt.Errorf("--progress-interval must be positive, got %s", f.ProgressInterval)
	}
	if f.NoProgressTimeout <= 0 {
		return fmt.Errorf("--no-progress-timeout must be positive, got %s", f.NoProgressTimeout)
	}
	endpoints := agentcli.EndpointsFromEnv()
	config := mcpserver.Config{
		Version:           project.Version(),
		Endpoints:         endpoints,
		ProgressInterval:  f.ProgressInterval,
		NoProgressTimeout: f.NoProgressTimeout,
		ReleasePromote:    f.ReleasePromote,
		Logger:            slog.New(slog.NewJSONHandler(stderr, nil)),
	}
	if f.GrantApp != "" {
		config.Credentials = mcpserver.MusterGrant{
			App:           f.GrantApp,
			GitHubApp:     f.GitHubApp,
			Users:         &mcpserver.GitHubUsers{BaseURL: endpoints.GitHubAPIURL},
			Installations: &mcpserver.GitHubInstallations{BaseURL: endpoints.GitHubAPIURL},
		}
	}
	if f.Installations != "" {
		clusters, err := mcpserver.ReadInstallations(f.Installations)
		if err != nil {
			return err
		}
		config.Clusters = clusters
	}
	if f.MetricsAddress != "" {
		registry := prometheus.NewRegistry()
		metrics, err := mcpserver.NewMetrics(registry)
		if err != nil {
			return err
		}
		config.Metrics = metrics
		stopMetrics, err := serveMetrics(f.MetricsAddress, registry)
		if err != nil {
			return err
		}
		defer stopMetrics()
	}
	s := mcpserver.New(config)
	mux := http.NewServeMux()
	mux.Handle("/mcp", s.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	listener, err := net.Listen("tcp", f.Address)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintf(stderr, "devctl %s serves MCP at http://%s/mcp\n", project.Version(), listener.Addr())

	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		return err
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// serveMetrics serves registry's metrics at /metrics on address until the
// returned stop is called.
func serveMetrics(address string, registry *prometheus.Registry) (func(), error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(listener) }()
	return func() { _ = srv.Close() }, nil
}
