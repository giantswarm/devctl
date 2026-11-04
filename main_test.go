//go:build !windows

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// TestMainEntrypoint runs the real entrypoint in a subprocess, keeping the
// executable's invocation name while removing the testing flags. Unlike the
// authexec shim tests, it does not recreate dispatch or replace the token source.
func TestMainEntrypoint(t *testing.T) {
	if os.Getenv("DEVCTL_TEST_ENTRYPOINT") != "1" {
		return
	}
	os.Args = append([]string{os.Args[0]}, flag.Args()...)
	main()
	t.Fatal("main returned instead of exiting or replacing the process")
}

func TestMainGHProxy(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	offline := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer offline.Close()

	for _, tc := range []struct {
		name       string
		login      bool
		executable bool
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "case 0: forwards arguments, streams and the child exit code",
			login:      true,
			executable: true,
			wantCode:   23,
			wantStdout: "fixture stdout\n",
			wantStderr: "fixture stderr\n",
		},
		{
			name:       "case 1: an environment token cannot replace a missing App login",
			executable: true,
			wantCode:   agentcli.ExitAuthRequired,
			wantStderr: "devctl auth exec: GitHub authentication required (no token in the keychain): run `devctl auth login --github-only`.\n",
		},
		{
			name:       "case 2: a PATH containing only the link refuses recursion",
			login:      true,
			wantCode:   agentcli.ExitUsage,
			wantStderr: "devctl auth exec: gh: not found on PATH (devctl itself skipped)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			links, bin := filepath.Join(home, "links"), filepath.Join(home, "bin")
			for _, dir := range []string{links, bin} {
				if err := os.Mkdir(dir, 0o750); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(links, "gh")
			if err := os.Symlink(self, link); err != nil {
				t.Fatal(err)
			}
			keyring := filepath.Join(home, "keyring.json")
			if tc.login {
				if err := os.WriteFile(keyring, []byte(`{"github":{"token":"synthetic-app-token","expiresAt":"2099-01-01T00:00:00Z"}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.executable {
				if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(ghFixture), 0o700); err != nil { // #nosec G306 -- an executable test fixture
					t.Fatal(err)
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, link, "-test.run=^TestMainEntrypoint$", "--", // #nosec G204 G702 -- the test binary through its temporary gh link
				"issue", "view", "42", "--repo", "giantswarm/fixture", "--json", "title", "--jq", `.title + " with spaces"`)
			child.Dir = home
			// Construct the whole environment: no real login, keychain, git
			// configuration or network endpoint is inherited from the host.
			child.Env = []string{
				"DEVCTL_TEST_ENTRYPOINT=1",
				"HOME=" + home,
				"XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
				"XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
				"PATH=" + links + string(os.PathListSeparator) + bin,
				agentcli.EnvKeyringFile + "=" + keyring,
				agentcli.EnvGitHubAPIURL + "=" + offline.URL,
				agentcli.EnvGitHubOAuthURL + "=" + offline.URL,
				"GH_TOKEN=synthetic-inherited-token",
				"GITHUB_TOKEN=synthetic-inherited-token",
			}
			child.Stdin = strings.NewReader("fixture stdin\n")
			var stdout, stderr bytes.Buffer
			child.Stdout, child.Stderr = &stdout, &stderr
			err := child.Run()
			if ctx.Err() != nil {
				t.Fatalf("gh did not finish: %v", ctx.Err())
			}
			code := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != tc.wantCode || stdout.String() != tc.wantStdout || stderr.String() != tc.wantStderr {
				t.Errorf("gh = exit %d, stdout %q, stderr %q; want exit %d, stdout %q, stderr %q",
					code, stdout.String(), stderr.String(), tc.wantCode, tc.wantStdout, tc.wantStderr)
			}
		})
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("gh made %d HTTP requests; want no version check or token request", got)
	}
}

// The fixture checks the token without printing it, and checks argument
// boundaries before writing different output to each stream and failing.
const ghFixture = `#!/bin/sh
if [ "$GH_TOKEN" != 'synthetic-app-token' ] ||
   [ "$#" -ne 9 ] ||
   [ "$1" != 'issue' ] || [ "$2" != 'view' ] || [ "$3" != '42' ] ||
   [ "$4" != '--repo' ] || [ "$5" != 'giantswarm/fixture' ] ||
   [ "$6" != '--json' ] || [ "$7" != 'title' ] ||
   [ "$8" != '--jq' ] || [ "$9" != '.title + " with spaces"' ]; then
	printf '%s\n' 'fixture received the wrong token or arguments' >&2
	exit 91
fi
IFS= read -r input || exit 92
if [ "$input" != 'fixture stdin' ]; then exit 93; fi
printf '%s\n' 'fixture stdout'
printf '%s\n' 'fixture stderr' >&2
exit 23
`
