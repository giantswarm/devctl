//go:build !windows

package authexec

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func TestProxy(t *testing.T) {
	for argv0, want := range map[string]string{
		"gh":                      "gh",
		"/home/x/agent-bin/gh":    "gh",
		"devctl":                  "",
		"/usr/local/bin/devctl":   "",
		"/home/x/agent-bin/git":   "",
		"/home/x/agent-bin/gh.sh": "",
	} {
		if got := Proxy(argv0); got != want {
			t.Errorf("Proxy(%q) = %q, want %q", argv0, got, want)
		}
	}
}

func TestEnviron(t *testing.T) {
	got := Environ([]string{"PATH=/bin", "GH_TOKEN=old", "GH_TOKENX=keep"}, "new")
	want := []string{"PATH=/bin", "GH_TOKENX=keep", "GH_TOKEN=new"}
	if !slices.Equal(got, want) {
		t.Fatalf("Environ = %v, want %v", got, want)
	}
}

// fixture is a devctl binary, a gh link to it in agentBin and a real gh in
// realBin.
func fixture(t *testing.T) (self, agentBin, realBin string) {
	t.Helper()
	dir := t.TempDir()
	self = filepath.Join(dir, "devctl")
	agentBin = filepath.Join(dir, "agent-bin")
	realBin = filepath.Join(dir, "bin")
	for _, d := range []string{agentBin, realBin} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(self, []byte("#!/bin/sh\n"), 0o700); err != nil { // nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(agentBin, "gh")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realBin, "gh"), []byte("#!/bin/sh\n"), 0o700); err != nil { // nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	return self, agentBin, realBin
}

func TestLookPath(t *testing.T) {
	self, agentBin, realBin := fixture(t)
	path := strings.Join([]string{agentBin, "", realBin}, string(os.PathListSeparator))

	got, err := LookPath("gh", path, self)
	if err != nil || got != filepath.Join(realBin, "gh") {
		t.Fatalf("LookPath(gh) = %q, %v; want the real gh", got, err)
	}
	if _, err := LookPath("gh", agentBin, self); err == nil {
		t.Fatal("LookPath found devctl itself")
	}
	if _, err := LookPath(filepath.Join(agentBin, "gh"), path, self); err == nil {
		t.Fatal("LookPath accepted a path to devctl itself")
	}
	if got, err := LookPath(filepath.Join(realBin, "gh"), "", self); err != nil || got != filepath.Join(realBin, "gh") {
		t.Fatalf("LookPath(path) = %q, %v", got, err)
	}
}

func TestRun(t *testing.T) {
	self, agentBin, realBin := fixture(t)
	path := strings.Join([]string{agentBin, realBin}, string(os.PathListSeparator))
	authErr := &authstore.AuthRequiredError{Identity: "GitHub", Cause: "no token", Hint: "devctl auth login --github-only"}

	cases := []struct {
		name     string
		program  string
		tokenErr error
		execCode int
		wantCode int
		wantExec bool
		wantErr  string
		args     []string
		// wantPerson: gh runs without the token, on the person's login.
		wantPerson bool
	}{
		{name: "runs the real gh with the token", program: "gh", execCode: 3, wantCode: 3, wantExec: true},
		{name: "no login", program: "gh", tokenErr: authErr, wantCode: agentcli.ExitAuthRequired, wantErr: "devctl auth login"},
		{name: "not found", program: "nope", wantCode: agentcli.ExitUsage, wantErr: "not found"},
		{name: "another owner's repository keeps the person's login", program: "gh", args: []string{"pr", "list", "--repo", "teemow/klaus-lab"}, wantExec: true, wantPerson: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			var gotValid time.Duration
			var execPath string
			var execArgv, execEnv []string
			c := Config{
				Token: func(_ context.Context, valid time.Duration) (authstore.Token, error) {
					gotValid = valid
					return authstore.Token{Value: "app-token"}, tc.tokenErr
				},
				Self:    self,
				Path:    path,
				Environ: []string{"PATH=" + path, "GH_TOKEN=person-token"},
				Stderr:  &stderr,
				Exec: func(p string, argv, env []string) (int, error) {
					execPath, execArgv, execEnv = p, argv, env
					return tc.execCode, nil
				},
			}
			args := tc.args
			if args == nil {
				args = []string{"api", "user"}
			}
			code := Run(context.Background(), c, tc.program, args)
			if code != tc.wantCode {
				t.Fatalf("code = %d, want %d (stderr %q)", code, tc.wantCode, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.wantErr) || strings.Contains(stderr.String(), "app-token") {
				t.Fatalf("stderr = %q", stderr.String())
			}
			if !tc.wantExec {
				if execPath != "" {
					t.Fatalf("executed %s", execPath)
				}
				return
			}
			if tc.wantPerson {
				if gotValid != 0 || slices.ContainsFunc(execEnv, func(kv string) bool { return strings.HasPrefix(kv, "GH_TOKEN=") }) {
					t.Fatalf("token handed over for another owner: env = %v", execEnv)
				}
				return
			}
			if gotValid != MinValidity {
				t.Fatalf("token valid for %s, want %s", gotValid, MinValidity)
			}
			if execPath != filepath.Join(realBin, "gh") || !slices.Equal(execArgv, []string{"gh", "api", "user"}) {
				t.Fatalf("exec %s %v", execPath, execArgv)
			}
			if !slices.Contains(execEnv, "GH_TOKEN=app-token") || slices.Contains(execEnv, "GH_TOKEN=person-token") {
				t.Fatalf("env = %v", execEnv)
			}
		})
	}
}
