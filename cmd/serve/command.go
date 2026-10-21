// Package serve is devctl serve: the agent-facing commands as MCP tools over
// streamable HTTP.
package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

The server acts on GitHub as the person who calls it, with the token the
platform hands it for them; without one a call is auth_required. It holds no
CircleCI credential: the waits read CircleCI through the commit statuses it
posts to GitHub, and a job the revision's CircleCI configuration runs that
has posted none yet, one behind requires:, keeps a wait pending. No rerun
and no CircleCI job listing is served. It reads no
keychain or credential file, runs no version check (the deployment pins the
version), writes nothing to disk and ignores DEVCTL_TIME_SCALE.`

	// shutdownTimeout bounds the drain of running calls on SIGTERM.
	shutdownTimeout = 30 * time.Second
)

type Config struct {
	Stderr io.Writer
}

type flag struct {
	Address          string
	ProgressInterval time.Duration
	ReleasePromote   bool
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
	c.Flags().DurationVar(&f.ProgressInterval, "progress-interval", mcpserver.DefaultProgressInterval, "How often a wait sends notifications/progress; below the registered timeout of the gateway in front")
	return c, nil
}

func run(ctx context.Context, stderr io.Writer, f *flag) error {
	if f.ProgressInterval <= 0 {
		return fmt.Errorf("--progress-interval must be positive, got %s", f.ProgressInterval)
	}
	s := mcpserver.New(mcpserver.Config{
		Version:          project.Version(),
		Endpoints:        agentcli.EndpointsFromEnv(),
		ProgressInterval: f.ProgressInterval,
		ReleasePromote:   f.ReleasePromote,
	})
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
