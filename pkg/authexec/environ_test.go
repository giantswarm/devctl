package authexec

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func TestEnvironmentVariableCase(t *testing.T) {
	env := []string{"gh_token=old-app", "Github_Token=old-other", "GH_TOKENX=keep", "GH_TOKEN=old"}
	want := []string{"gh_token=old-app", "Github_Token=old-other", "GH_TOKENX=keep"}
	if runtime.GOOS == "windows" {
		want = []string{"GH_TOKENX=keep"}
	}
	if got := withoutGHTokens(env); !slices.Equal(got, want) {
		t.Fatalf("person's environment = %v, want %v", got, want)
	}
	wantApp := []string{"gh_token=old-app", "Github_Token=old-other", "GH_TOKENX=keep", "GH_TOKEN=new-app"}
	if runtime.GOOS == "windows" {
		wantApp = []string{"Github_Token=old-other", "GH_TOKENX=keep", "GH_TOKEN=new-app"}
	}
	if got := Environ(env, "new-app"); !slices.Equal(got, wantApp) {
		t.Fatalf("App environment = %v, want %v", got, wantApp)
	}
	wantOwner := "giantswarm"
	if runtime.GOOS == "windows" {
		wantOwner = "outside"
	}
	if got := GHOwner([]string{"pr", "list"}, []string{"gh_repo=outside/repo"}, func() string {
		return "remote.origin.url https://github.com/giantswarm/devctl.git\n"
	}); got != wantOwner {
		t.Fatalf("owner = %q, want %q", got, wantOwner)
	}
	chain := strings.Repeat("previous"+string(os.PathListSeparator), MaxChain-1) + "previous"
	_, err := chained([]string{"devctl_auth_exec_chain=" + chain}, "next", "self")
	if (err != nil) != (runtime.GOOS == "windows") {
		t.Fatalf("mixed-case full chain: %v", err)
	}
}

func TestPersonLoginEnvironmentCase(t *testing.T) {
	bin := t.TempDir()
	name := "gh"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	gh := filepath.Join(bin, name)
	if err := os.WriteFile(gh, []byte("fixture"), 0o700); err != nil { // nolint:gosec // executable lookup fixture, never executed
		t.Fatal(err)
	}
	env := []string{"gh_token=old-app", "Github_Token=old-other", "GH_TOKENX=keep"}
	want := env
	if runtime.GOOS == "windows" {
		want = []string{"GH_TOKENX=keep"}
	}
	_, err := personGitHub(context.Background(), bin, filepath.Join(bin, "devctl"), env,
		func(_ context.Context, _ string, _ []string, got []string) ([]byte, error) {
			if !slices.Equal(got, want) {
				t.Fatalf("gh auth token environment = %v, want %v", got, want)
			}
			return []byte("person-token"), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	c := Config{
		Self: filepath.Join(bin, "devctl"), Path: bin, Environ: env, Stderr: &stderr,
		Token: func(context.Context, time.Duration) (authstore.Token, error) {
			t.Fatal("person-login fallback requested the App login")
			return authstore.Token{}, nil
		},
		Exec: func(_ string, _ []string, got []string) (int, error) {
			if !slices.Equal(got[:len(got)-1], want) {
				t.Fatalf("person-login gh environment = %v, want %v plus the chain", got, want)
			}
			return agentcli.ExitOK, nil
		},
	}
	for _, args := range [][]string{
		{"pr", "list", "--repo", "outside/repo"},
		{"issue", "comment", "1", "--repo", "giantswarm/devctl", "--attach", "fixture.png"},
	} {
		if code := Run(context.Background(), c, name, args); code != agentcli.ExitOK {
			t.Fatalf("Run(%v) = %d, stderr %q", args, code, stderr.String())
		}
	}
}
