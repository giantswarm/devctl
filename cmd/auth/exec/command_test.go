package exec

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// A canceled Cobra invocation must not send a refresh request. The fixture
// executable is only looked up: both the canceled request and the mock's
// refusal prevent execution, including before the fix.
func TestCommandCanceledRefresh(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want string
	}{
		{
			name: "canceled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			want: "context canceled",
		},
		{
			name: "deadline exceeded",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Minute))
			},
			want: "context deadline exceeded",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"error":"fixture_refresh_refused"}`))
			}))
			defer server.Close()

			dir := t.TempDir()
			store := &authstore.FileStore{Path: filepath.Join(dir, "keyring.json")}
			if err := store.Set(authstore.UserGitHub, authstore.Record{
				Token:            "synthetic-expired-token",
				ExpiresAt:        time.Now().Add(-time.Hour),
				RefreshToken:     "synthetic-refresh-token",
				RefreshExpiresAt: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatal(err)
			}
			t.Setenv(agentcli.EnvKeyringFile, store.Path)
			t.Setenv(agentcli.EnvGitHubOAuthURL, server.URL)
			t.Setenv(agentcli.EnvTimeScale, "1")
			t.Setenv(authexec.EnvChain, "")
			program := filepath.Join(dir, "fixture.exe")
			if err := os.WriteFile(program, []byte("fixture"), 0o700); err != nil { // #nosec G306 -- an executable fixture, never run
				t.Fatal(err)
			}

			var stderr bytes.Buffer
			command, err := New(Config{Stderr: &stderr})
			if err != nil {
				t.Fatal(err)
			}
			command.SilenceErrors, command.SilenceUsage = true, true
			command.SetArgs([]string{"--", program})
			ctx, cancel := tc.ctx()
			defer cancel()
			err = command.ExecuteContext(ctx)
			if n := hits.Load(); n != 0 {
				t.Errorf("canceled command sent %d refresh requests, want none", n)
			}
			var exitErr *agentcli.ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != agentcli.ExitUsage {
				t.Errorf("error = %v, want exit %d", err, agentcli.ExitUsage)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("diagnostic = %q, want %q", stderr.String(), tc.want)
			}
		})
	}
}
