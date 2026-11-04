package create

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// A caller that has already canceled must not authenticate or overwrite a
// release. The fixture needs no network lookups, even on the broken path.
func TestCanceledCreatePreservesExistingRelease(t *testing.T) {
	for _, reason := range []string{"canceled", "deadline"} {
		t.Run(reason, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if reason == "deadline" {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Unix(1, 0))
			}
			cancel()
			cmd, unchanged := cancellationFixture(t)
			err := cmd.ExecuteContext(ctx)
			if !errors.Is(err, ctx.Err()) {
				t.Errorf("create returned %v, want %v", err, ctx.Err())
			}
			unchanged()
		})
	}
}

type cancellationTransport func(*http.Request) (*http.Response, error)

func (f cancellationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Cancellation while selecting the identity must reach the refresh request.
// Even a successful refresh racing with cancellation must not start generation.
func TestCreateCancellationDuringGitHubRefresh(t *testing.T) {
	for _, refreshSucceeds := range []bool{false, true} {
		name := "canceled request"
		if refreshSucceeds {
			name = "successful response races with cancellation"
		}
		t.Run(name, func(t *testing.T) {
			cmd, unchanged := cancellationFixture(t)
			t.Setenv("CI", "")
			for _, name := range authstore.GitHubEnvVars {
				t.Setenv(name, "")
			}
			t.Setenv(agentcli.EnvTimeScale, "1")
			t.Setenv(agentcli.EnvGitHubOAuthURL, "https://oauth.invalid")
			store := &authstore.FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
			t.Setenv(agentcli.EnvKeyringFile, store.Path)
			err := store.Set(authstore.UserGitHub, authstore.Record{
				Token: "synthetic-expired-token", ExpiresAt: time.Unix(1, 0),
				RefreshToken: "synthetic-refresh-token",
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			requests := 0
			http.DefaultTransport = cancellationTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.Method != http.MethodPost || req.URL.String() != "https://oauth.invalid/login/oauth/access_token" {
					t.Error("unexpected request during refresh")
					return nil, errors.New("unexpected offline request")
				}
				cancel()
				if !errors.Is(req.Context().Err(), context.Canceled) {
					t.Error("GitHub refresh discarded the caller context")
				}
				if refreshSucceeds {
					return &http.Response{
						StatusCode: http.StatusOK, Header: make(http.Header), Request: req,
						Body: io.NopCloser(strings.NewReader(`{"access_token":"synthetic-refreshed-token","expires_in":3600}`)),
					}, nil
				}
				if err := req.Context().Err(); err != nil {
					return nil, err
				}
				return nil, errors.New("refresh did not receive cancellation")
			})
			err = cmd.ExecuteContext(ctx)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("create returned %v, want context.Canceled", err)
			}
			if requests != 1 {
				t.Errorf("refresh made %d requests, want one", requests)
			}
			unchanged()
		})
	}
}

func cancellationFixture(t *testing.T) (*cobra.Command, func()) {
	t.Helper()
	t.Setenv("CI", "1")
	t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-cancellation-token")

	dir := t.TempDir()
	provider := filepath.Join(dir, "capa")
	target := filepath.Join(provider, "v30.0.1")
	files := map[string]string{
		"v30.0.0/release.yaml": "apiVersion: release.giantswarm.io/v1alpha1\nkind: Release\nmetadata:\n  name: capa-30.0.0\nspec:\n  state: active\n  components:\n  - name: kubernetes\n    version: 1.30.0\n",
		"v30.0.1/keep.txt":     "existing release content\n",
		"kustomization.yaml":   "resources:\n- v30.0.0\n",
		"releases.json":        "{\"releases\":[]}\n",
	}
	for name, content := range files {
		path := filepath.Join(provider, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--name", "v30.0.1", "--base", "v30.0.0", "--provider", "capa", "--releases", dir, "--overwrite", "--yes"})
	return cmd, func() {
		t.Helper()
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(provider, name)) // #nosec G304 -- fixed fixture names inside t.TempDir().
			if err != nil || string(got) != want {
				t.Errorf("canceled create changed %s (read error: %v)", name, err)
			}
		}
		if _, err := os.Stat(filepath.Join(target, "release.yaml")); !os.IsNotExist(err) {
			t.Errorf("canceled create wrote release.yaml (stat error: %v)", err)
		}
	}
}
