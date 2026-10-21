// Package serve is devctl serve: the agent-facing commands as MCP tools over
// streamable HTTP.
package serve

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
pr_wait, pr_merge, pr_merge_status and release_wait, and release_promote
with --release-promote. Each answers the command's JSON
document, devctl's envelope included, as structured content; the exit code
is a field, and the call is an MCP error only for auth_required.

A wait is one call: it blocks until its verdict and sends
notifications/progress on the caller's progressToken every
--progress-interval, which keeps the call alive through a gateway that
restarts its timeout on progress (muster). A wait without a progressToken is
refused at once, verdict progress_token_required.

pr_merge is devctl pr merge in one call: the wait for green, the merge with
the judged head as the expected head, then the release wait, with the CLI's
refusals. A pull request merged already answers its merge and its release
instead of exit 3, so a second call after a lost answer merges nothing.
pr_merge and release_promote carry no read-only annotation.

The server acts on GitHub as the person who calls it, with their token of
the GitHub App muster keeps as --grant-app, which muster attaches to every
call in X-Muster-Grant-<app> (the MCPServer's spec.auth.providerApps).
Without it a call is auth_required, structured status grant_required, which
muster answers with the App's sign-in link; a repository whose owner the App
is not installed on is not_applicable. With --oidc-issuer every request
carries the ID token muster forwards (spec.auth.forwardToken), checked
against the issuer and --oidc-audience, and each call's log line on stderr
and its metrics at --metrics-address name the caller's subject and the
repository. It reads no keychain or credential file, runs no version check
(the deployment pins the version), writes nothing to disk and ignores
DEVCTL_TIME_SCALE.`

	// shutdownTimeout bounds the drain of running calls on SIGTERM.
	shutdownTimeout = 30 * time.Second
)

type Config struct {
	Stderr io.Writer
}

type flag struct {
	Address          string
	MetricsAddress   string
	ProgressInterval time.Duration
	ReleasePromote   bool
	GrantApp         string
	GitHubApp        string
	OIDCIssuer       string
	OIDCAudiences    []string
	OIDCCAFile       string
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
	c.Flags().StringVar(&f.OIDCIssuer, "oidc-issuer", "", "The issuer of the ID token muster forwards; empty verifies no caller")
	c.Flags().StringSliceVar(&f.OIDCAudiences, "oidc-audience", nil, "An audience of the forwarded ID token this server trusts; repeatable, required with --oidc-issuer")
	c.Flags().StringVar(&f.OIDCCAFile, "oidc-ca-file", "", "PEM CA bundle the issuer's certificate is checked against, besides the system's; for an issuer with a private CA")
	c.Flags().DurationVar(&f.ProgressInterval, "progress-interval", mcpserver.DefaultProgressInterval, "How often a wait sends notifications/progress; below the registered timeout of the gateway in front")
	return c, nil
}

func run(ctx context.Context, stderr io.Writer, f *flag) error {
	if f.ProgressInterval <= 0 {
		return fmt.Errorf("--progress-interval must be positive, got %s", f.ProgressInterval)
	}
	endpoints := agentcli.EndpointsFromEnv()
	config := mcpserver.Config{
		Version:          project.Version(),
		Endpoints:        endpoints,
		ProgressInterval: f.ProgressInterval,
		ReleasePromote:   f.ReleasePromote,
		Logger:           slog.New(slog.NewJSONHandler(stderr, nil)),
	}
	if f.GrantApp != "" {
		config.Credentials = mcpserver.MusterGrant{
			App:           f.GrantApp,
			GitHubApp:     f.GitHubApp,
			Installations: &mcpserver.GitHubInstallations{BaseURL: endpoints.GitHubAPIURL},
		}
	}
	if f.OIDCIssuer != "" {
		client, err := issuerClient(f.OIDCCAFile)
		if err != nil {
			return err
		}
		verifier, err := mcpserver.NewOIDCVerifier(ctx, client, f.OIDCIssuer, f.OIDCAudiences)
		if err != nil {
			return err
		}
		config.Verifier = verifier
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

// issuerClient is the HTTP client the issuer is read with: the system's
// roots plus caFile's, or nil for the default client without one.
func issuerClient(caFile string) (*http.Client, error) {
	if caFile == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(caFile) //nolint:gosec // G304: the operator names the CA file
	if err != nil {
		return nil, err
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("--oidc-ca-file %s holds no PEM certificate", caFile)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
}
