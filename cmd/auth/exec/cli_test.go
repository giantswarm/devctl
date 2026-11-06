//go:build !windows

package exec_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	osexec "os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giantswarm/micrologger"

	"github.com/giantswarm/devctl/v8/cmd"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// TestAuthExecCLIProcess enters the real command tree in a subprocess:
// a successful Unix exec replaces it, while cmd.Execute owns failure exits.
func TestAuthExecCLIProcess(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	logger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		t.Fatal(err)
	}
	root, err := cmd.New(cmd.Config{Logger: logger, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		t.Fatal(err)
	}
	root.SetArgs(os.Args[separator+1:])
	os.Exit(cmd.Execute(root, os.Stderr))
}

func TestAuthExecCLI(t *testing.T) {
	childArgs := []string{"--help", "-h", "--log-level", "not-a-level", "--", "a b", "", "quote\"'", "$(not-a-command)"}
	var forwarded bytes.Buffer
	for _, arg := range childArgs {
		fmt.Fprintf(&forwarded, "<%s>\n", arg)
	}

	for _, tc := range []struct {
		name   string
		args   []string
		login  bool
		code   int
		stdout string
		stderr string
		help   bool
	}{
		{
			name: "arguments after delimiter",
			args: slices.Concat([]string{"--", "@fixture@"}, childArgs), login: true,
			code: 23, stdout: forwarded.String(), stderr: "fixture stderr\n",
		},
		{
			name: "arguments without delimiter",
			args: slices.Concat([]string{"@fixture@"}, childArgs), login: true,
			code: 23, stdout: forwarded.String(), stderr: "fixture stderr\n",
		},
		{
			name: "missing login ignores inherited tokens", args: []string{"--", "@fixture@"}, code: 8,
			stderr: "devctl auth exec: GitHub authentication required (no token in the keychain): run `devctl auth login --github-only`.\n",
		},
		{
			name: "missing executable before login", args: []string{"--", "no-such-audit-fixture"}, code: 7,
			stderr: "devctl auth exec: no-such-audit-fixture: not found on PATH (devctl itself skipped)\n",
		},
		{
			name: "missing command", code: 2,
			stderr: "Error: Missing <command>\nUsage: devctl auth exec -- <command> [args...] [flags]\nRun 'devctl auth exec --help' for more information.\n",
		},
		{
			name: "help without login", args: []string{"--help"}, help: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()

			home := t.TempDir()
			bin := filepath.Join(home, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			program := filepath.Join(bin, "child command")
			script := "#!/bin/sh\n[ \"${GH_TOKEN-}\" = synthetic-app-token ] || exit 91\nprintf '<%s>\\n' \"$@\"\nprintf 'fixture stderr\\n' >&2\nexit 23\n"
			if err := os.WriteFile(program, []byte(script), 0o700); err != nil { // #nosec G306 -- a test executable
				t.Fatal(err)
			}
			store := &authstore.FileStore{Path: filepath.Join(home, "keyring.json")}
			if tc.login {
				if err := store.Set(authstore.UserGitHub, authstore.Record{
					Token: "synthetic-app-token", ExpiresAt: time.Now().Add(time.Hour),
				}); err != nil {
					t.Fatal(err)
				}
			}

			args := []string{"-test.run=^TestAuthExecCLIProcess$", "--", "auth", "exec"}
			for _, arg := range tc.args {
				if arg == "@fixture@" {
					arg = program
				}
				args = append(args, arg)
			}
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			child := osexec.CommandContext(ctx, self, args...) // #nosec G204 G702 -- the test binary, with synthetic arguments
			child.Dir = home
			// Construct the whole environment: no real login, config, keychain
			// daemon, PATH entry or endpoint is available to this process.
			child.Env = []string{
				"HOME=" + home,
				"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
				"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
				"PATH=" + bin,
				"DEVCTL_KEYRING_FILE=" + store.Path,
				"DEVCTL_GITHUB_API_URL=" + server.URL,
				"DEVCTL_GITHUB_OAUTH_URL=" + server.URL,
				"DEVCTL_CIRCLECI_API_URL=" + server.URL,
				"DEVCTL_CIRCLECI_OAUTH_URL=" + server.URL,
				"DEVCTL_MUSTER_URL=" + server.URL,
				"DEVCTL_TIME_SCALE=1",
				"GH_TOKEN=synthetic-parent-gh-token",
				"GITHUB_TOKEN=synthetic-parent-github-token",
				"DEVCTL_GITHUB_TOKEN=synthetic-parent-devctl-token",
				"OPSCTL_GITHUB_TOKEN=synthetic-parent-opsctl-token",
			}
			var stdout, stderr bytes.Buffer
			child.Stdout, child.Stderr = &stdout, &stderr
			runErr := child.Run()
			if ctx.Err() != nil {
				t.Fatal("auth exec did not finish before the subprocess deadline")
			}
			code := 0
			if runErr != nil {
				var exitErr *osexec.ExitError
				if !errors.As(runErr, &exitErr) {
					t.Fatal(runErr)
				}
				code = exitErr.ExitCode()
			}
			if code != tc.code {
				t.Errorf("exit %d, want %d; stderr = %q", code, tc.code, stderr.String())
			}
			if tc.help {
				if !strings.Contains(stdout.String(), "Run a command with the GitHub token of the App login") {
					t.Errorf("help = %q", stdout.String())
				}
			} else if stdout.String() != tc.stdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tc.stdout)
			}
			if stderr.String() != tc.stderr {
				t.Errorf("stderr = %q, want %q", stderr.String(), tc.stderr)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("made %d unexpected HTTP requests, want none", n)
			}
			for _, token := range []string{"synthetic-app-token", "synthetic-parent-gh-token", "synthetic-parent-github-token", "synthetic-parent-devctl-token", "synthetic-parent-opsctl-token"} {
				if strings.Contains(stdout.String(), token) || strings.Contains(stderr.String(), token) {
					t.Error("a synthetic token reached command output")
				}
			}
		})
	}
}
