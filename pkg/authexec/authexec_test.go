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

func TestLookPathPastShims(t *testing.T) {
	self, agentBin, realBin := fixture(t)
	shimBin := filepath.Join(filepath.Dir(agentBin), "shim")
	if err := os.Mkdir(shimBin, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shimBin, "gh"), []byte("#!/bin/sh\n"), 0o700); err != nil { // nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	join := func(dirs ...string) string { return strings.Join(dirs, string(os.PathListSeparator)) }
	for _, tc := range []struct {
		path, want string
	}{
		{path: join(shimBin, agentBin, realBin), want: realBin},
		{path: join(shimBin, agentBin, shimBin, agentBin, realBin), want: realBin},
		{path: join(agentBin, shimBin, realBin), want: shimBin},
		{path: join(shimBin, agentBin), want: shimBin},
		{path: join(shimBin, realBin), want: shimBin},
	} {
		if got, err := LookPath("gh", tc.path, self); err != nil || got != filepath.Join(tc.want, "gh") {
			t.Errorf("LookPath(gh, %s) = %q, %v; want %s/gh", tc.path, got, err, tc.want)
		}
	}
}

func TestChained(t *testing.T) {
	env, err := chained([]string{"PATH=/bin"}, "/a/gh", "/devctl")
	if err != nil || !slices.Contains(env, EnvChain+"=/a/gh") {
		t.Fatalf("chained = %v, %v", env, err)
	}
	full := EnvChain + "=" + strings.Repeat("/a/gh"+string(os.PathListSeparator), MaxChain-1) + "/a/gh"
	if _, err := chained([]string{full}, "/a/gh", "/devctl"); err == nil || !strings.Contains(err.Error(), "/a/gh -> /a/gh") {
		t.Fatalf("chained of a full chain = %v, want a refusal naming it", err)
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
		env      []string
		remotes  string
		// wantPerson: gh runs without the token, on the person's login.
		wantPerson bool
	}{
		{name: "runs the real gh with the token", program: "gh", execCode: 3, wantCode: 3, wantExec: true},
		{name: "no login", program: "gh", tokenErr: authErr, wantCode: agentcli.ExitAuthRequired, wantErr: "devctl auth login"},
		{name: "not found", program: "nope", wantCode: agentcli.ExitUsage, wantErr: "not found"},
		{name: "another owner's repository keeps the person's login", program: "gh", args: []string{"pr", "list", "--repo", "fixture-person/fixture-repo"}, wantExec: true, wantPerson: true},
		{name: "an upload keeps the person's login", program: "gh", args: []string{"issue", "comment", "12", "--repo", "giantswarm/devctl", "--attach", "proof.png"}, wantExec: true, wantPerson: true},
		{name: "a comment without an upload acts as the App", program: "gh", args: []string{"pr", "comment", "5", "--repo", "giantswarm/devctl", "--body", "done"}, wantExec: true},
		{name: "explicit API target beats environment", program: "gh", args: []string{"api", "repos/fixture-person/fixture-repo/pulls"}, env: []string{"GH_REPO=giantswarm/devctl"}, wantExec: true, wantPerson: true},
		{name: "explicit App API target beats environment", program: "gh", args: []string{"api", "repos/giantswarm/devctl/pulls"}, env: []string{"GH_REPO=fixture-person/fixture-repo"}, wantExec: true},
		{name: "API user remains on the App", program: "gh", args: []string{"api", "user"}, env: []string{"GH_REPO=fixture-person/fixture-repo"}, wantExec: true},
		{name: "explicit PR URL beats repo flag", program: "gh", args: []string{"pr", "view", "https://github.com/fixture-person/fixture-repo/pull/5", "--repo", "giantswarm/devctl"}, wantExec: true, wantPerson: true},
		{name: "body text does not switch the login", program: "gh", args: []string{"pr", "comment", "5", "--body", "--repo=fixture-person/x", "--repo", "giantswarm/devctl"}, wantExec: true},
		{name: "body text does not enable attachments", program: "gh", args: []string{"pr", "comment", "5", "--repo", "giantswarm/devctl", "--body", "--attach"}, wantExec: true},
		{name: "search in a personal checkout stays on the App", program: "gh", args: []string{"search", "prs", "query"}, remotes: "remote.origin.url https://github.com/fixture-person/devctl.git\n", wantExec: true},
		{name: "gh default can name a different owner from the remote", program: "gh", args: []string{"pr", "list"}, remotes: "remote.origin.url https://github.com/giantswarm/fork.git\nremote.origin.gh-resolved fixture-person/parent\n", wantExec: true, wantPerson: true},
		{name: "gh default can name the App owner from a fork", program: "gh", args: []string{"pr", "list"}, remotes: "remote.origin.url https://github.com/fixture-person/fork.git\nremote.origin.gh-resolved giantswarm/parent\n", wantExec: true},
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
				Environ: append([]string{"PATH=" + path, "GH_TOKEN=person-token", "GITHUB_TOKEN=other-token"}, tc.env...),
				Stderr:  &stderr,
				Remotes: func() string { return tc.remotes },
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
			wantArgv := append([]string{"gh"}, args...)
			if execPath != filepath.Join(realBin, "gh") || !slices.Equal(execArgv, wantArgv) {
				t.Fatalf("exec %s %v", execPath, execArgv)
			}
			if tc.wantPerson {
				if gotValid != 0 || slices.ContainsFunc(execEnv, func(kv string) bool {
					return strings.HasPrefix(kv, "GH_TOKEN=") || strings.HasPrefix(kv, "GITHUB_TOKEN=")
				}) {
					t.Fatalf("token handed over on the person's login: env = %v", execEnv)
				}
				return
			}
			if gotValid != MinValidity {
				t.Fatalf("token valid for %s, want %s", gotValid, MinValidity)
			}
			if !slices.Contains(execEnv, "GH_TOKEN=app-token") || slices.Contains(execEnv, "GH_TOKEN=person-token") {
				t.Fatalf("env = %v", execEnv)
			}
		})
	}
}
