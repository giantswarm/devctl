package versiongate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// brokenKeychain points the keychain at a directory: any read of it fails,
// so a call that succeeds did not open it.
func brokenKeychain(t *testing.T) {
	t.Helper()
	t.Setenv(agentcli.EnvKeyringFile, t.TempDir())
}

// noSessionBus is the OS keychain of a container: the Secret Service's
// session bus is not there.
func noSessionBus(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the session bus is the keychain on Linux only")
	}
	t.Setenv(agentcli.EnvKeyringFile, "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
}

// clearGitHubEnv unsets CI and every GitHub token variable for the test: the
// CI running the tests sets some of them.
func clearGitHubEnv(t *testing.T) {
	t.Helper()
	for _, name := range append([]string{authstore.EnvCI, envGH}, authstore.GitHubEnvVars...) {
		t.Setenv(name, "")
	}
}

func TestGitHubToken(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		keychain func(t *testing.T)

		want        string
		wantErr     bool
		wantWarning string // substring; empty means no warning
	}{
		{name: "CI without a token reads anonymously and never opens the keychain",
			env: map[string]string{authstore.EnvCI: "true"}, keychain: brokenKeychain},
		{name: "without CI the keychain is opened, and its failure returned",
			keychain: brokenKeychain, wantErr: true},
		{name: "no session bus reads anonymously",
			keychain: noSessionBus},
		{name: "no App login reads anonymously",
			keychain: func(t *testing.T) { t.Setenv(agentcli.EnvKeyringFile, filepath.Join(t.TempDir(), "keyring.json")) }},
		{name: "a token in the environment overrides with the warning",
			env: map[string]string{"DEVCTL_GITHUB_TOKEN": "ghp_env"}, keychain: brokenKeychain,
			want: "ghp_env", wantWarning: "warning: the GitHub token in $DEVCTL_GITHUB_TOKEN overrides the devctl GitHub App login"},
		{name: "a token in the environment under CI, without the warning",
			env: map[string]string{"OPSCTL_GITHUB_TOKEN": "ghp_ci", authstore.EnvCI: "true"}, keychain: brokenKeychain,
			want: "ghp_ci"},
		{name: "gh's $GH_TOKEN alone under CI, the token a workflow step sets for gh",
			env: map[string]string{envGH: "ghs_job", authstore.EnvCI: "true"}, keychain: brokenKeychain,
			want: "ghs_job"},
		{name: "a devctl token variable wins over $GH_TOKEN under CI",
			env: map[string]string{authstore.GitHubEnvVars[1]: "devctl-token", envGH: "ghs_job", authstore.EnvCI: "true"}, keychain: brokenKeychain,
			want: "devctl-token"},
		{name: "without CI $GH_TOKEN is not read: the App login is",
			env:      map[string]string{envGH: "ghs_job"},
			keychain: func(t *testing.T) { t.Setenv(agentcli.EnvKeyringFile, filepath.Join(t.TempDir(), "keyring.json")) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearGitHubEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			tc.keychain(t)
			var warnings bytes.Buffer
			token := githubToken(&warnings)

			for range 2 {
				got, err := token(context.Background())
				if (err != nil) != tc.wantErr {
					t.Fatalf("err = %v, want an error: %v", err, tc.wantErr)
				}
				if got != tc.want {
					t.Errorf("token %q, want %q", got, tc.want)
				}
			}
			switch {
			case tc.wantWarning == "" && warnings.Len() != 0:
				t.Errorf("warned %q, want nothing", warnings.String())
			case tc.wantWarning != "" && (strings.Count(warnings.String(), "\n") != 1 || !strings.Contains(warnings.String(), tc.wantWarning)):
				t.Errorf("warned %q, want %q once", warnings.String(), tc.wantWarning)
			}
		})
	}
}

func TestCheckWithAFreshCacheDoesNotOpenTheKeychain(t *testing.T) {
	clearGitHubEnv(t)
	t.Setenv("DEVCTL_UNSAFE_FORCE_VERSION", "")
	brokenKeychain(t)
	// Nothing answers here: a request to GitHub fails the check.
	t.Setenv(agentcli.EnvGitHubAPIURL, "http://127.0.0.1:1")
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	dir := filepath.Join(config, "devctl")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cache := "last_update: " + time.Now().UTC().Format(time.RFC3339) + "\nlatest_version: 1.0.0\n"
	if err := os.WriteFile(filepath.Join(dir, "selfupdate.yaml"), []byte(cache), 0o600); err != nil {
		t.Fatal(err)
	}

	var warnings bytes.Buffer
	if err := check("1.0.0", false, &warnings); err != nil {
		t.Fatalf("check: %v", err)
	}
	if err := check("0.9.0", false, &warnings); err == nil || !strings.Contains(err.Error(), "version 1.0.0 of devctl is released") {
		t.Errorf("check of an older build: %v, want the cached release named", err)
	}
	if warnings.Len() != 0 {
		t.Errorf("warned %q", warnings.String())
	}
}

// TestCheckInAWorkflowStepWithGHTokenAuthenticates is a generated workflow
// step that hands devctl the job's token in $GH_TOKEN only: the version
// check's releases call carries it, where GitHub answers an anonymous one
// from a shared runner IP with the rate limit.
func TestCheckInAWorkflowStepWithGHTokenAuthenticates(t *testing.T) {
	clearGitHubEnv(t)
	t.Setenv("DEVCTL_UNSAFE_FORCE_VERSION", "")
	t.Setenv(authstore.EnvCI, "true")
	t.Setenv(envGH, "ghs_job")
	brokenKeychain(t)

	var auths []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/giantswarm/devctl/releases", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		auths = append(auths, auth)
		if auth == "" {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for 4.242.196.129."}`))
			return
		}
		asset := "devctl-" + runtime.GOOS + "-" + runtime.GOARCH
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 1, "tag_name": "v1.0.0", "name": "v1.0.0",
			"assets": []map[string]any{{"id": 2, "name": asset, "size": 3, "browser_download_url": "https://example.test/" + asset}},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv(agentcli.EnvGitHubAPIURL, srv.URL)

	var warnings bytes.Buffer
	if err := check("1.0.0", true, &warnings); err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(auths) != 1 || auths[0] != "Bearer ghs_job" {
		t.Errorf("releases requests carried Authorization %q, want one with the job's token", auths)
	}
	if warnings.Len() != 0 {
		t.Errorf("warned %q", warnings.String())
	}
}
