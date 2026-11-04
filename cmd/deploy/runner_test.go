package deploy

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func deployCommand(t *testing.T) *cobra.Command {
	t.Helper()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	require.NoError(t, err)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--app-name", "hello-world", "--app-version", "2.8.1"})
	return cmd
}

func clearDeployAuthEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"DEVCTL_GITHUB_TOKEN", "GITHUB_TOKEN", "OPSCTL_GITHUB_TOKEN"} {
		t.Setenv(name, "")
	}
	// Even an accidentally reached auth read must never open the OS keychain.
	t.Setenv(agentcli.EnvKeyringFile, filepath.Join(t.TempDir(), "keyring.json"))
	t.Setenv(agentcli.EnvTimeScale, "1")
}

func TestCommandCancellationBeforeAuthentication(t *testing.T) {
	clearDeployAuthEnv(t)
	t.Setenv("CI", "1")
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{
			name: "canceled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx, cancel
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			},
			want: context.DeadlineExceeded,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			defer cancel()
			require.ErrorIs(t, deployCommand(t).ExecuteContext(ctx), tc.want)
		})
	}
}

type deployTransportFunc func(*http.Request) (*http.Response, error)

func (f deployTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCommandCancellationDuringAuthentication(t *testing.T) {
	clearDeployAuthEnv(t)
	t.Setenv("CI", "")
	// The custom transport never opens a socket, including for unexpected reads.
	t.Setenv(agentcli.EnvGitHubOAuthURL, "http://127.0.0.1:1")
	store := authstore.OpenStore(agentcli.EndpointsFromEnv())
	stale := authstore.Record{
		Login: "fixture-user", Token: "synthetic-expired-access", // #nosec G101 -- synthetic offline fixture, never a real credential
		ExpiresAt:    time.Now().Add(-time.Hour),
		RefreshToken: "synthetic-refresh", RefreshExpiresAt: time.Now().Add(time.Hour), // #nosec G101 -- synthetic offline fixture
	}
	require.NoError(t, store.Set(authstore.UserGitHub, stale))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := 0
	http.DefaultTransport = deployTransportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "http://127.0.0.1:1/login/oauth/access_token", req.URL.String())
		require.Equal(t, http.MethodPost, req.Method)
		cancel()
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		return nil, io.ErrUnexpectedEOF
	})
	require.ErrorIs(t, deployCommand(t).ExecuteContext(ctx), context.Canceled)
	require.Equal(t, 1, calls)
	got, err := store.Get(authstore.UserGitHub)
	require.NoError(t, err)
	// Compare without printing even these synthetic token values on failure.
	require.True(t, got.Token == stale.Token && got.RefreshToken == stale.RefreshToken)
	require.True(t, got.ExpiresAt.Equal(stale.ExpiresAt))
}
