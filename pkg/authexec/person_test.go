package authexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func TestRepositoryToken(t *testing.T) {
	app := func(context.Context) (authstore.Token, error) {
		return authstore.Token{Source: authstore.SourceKeychain}, nil
	}
	person := func(context.Context) (authstore.Token, error) { return authstore.Token{Source: SourceGHLogin}, nil }
	for owner, want := range map[string]string{"giantswarm": IdentityApp, "teemow": IdentityGH, "kagent-dev": IdentityGH} {
		token, err := RepositoryToken(context.Background(), owner, app, person)
		if err != nil {
			t.Fatal(err)
		}
		if got := Identity(token); got != want {
			t.Errorf("%s: want %s, got %s", owner, want, got)
		}
	}
}

func TestPersonGitHub(t *testing.T) {
	dir := t.TempDir()
	gh := filepath.Join(dir, "gh")
	if err := os.WriteFile(gh, []byte("#!/bin/sh\n"), 0o755); err != nil { // #nosec G306 -- a test executable
		t.Fatal(err)
	}
	// A gh link to another devctl comes first on PATH and is skipped.
	links := t.TempDir()
	devctl := filepath.Join(t.TempDir(), "devctl")
	if err := os.WriteFile(devctl, []byte("#!/bin/sh\n"), 0o755); err != nil { // #nosec G306 -- a test executable
		t.Fatal(err)
	}
	if err := os.Symlink(devctl, filepath.Join(links, "gh")); err != nil {
		t.Fatal(err)
	}
	path := links + string(os.PathListSeparator) + dir
	environ := []string{"PATH=" + path, "GH_TOKEN=app", "GITHUB_TOKEN=env", "HOME=/home/x"}

	var gotPath string
	var gotArgs, gotEnv []string
	output := func(_ context.Context, path string, args, env []string) ([]byte, error) {
		gotPath, gotArgs, gotEnv = path, args, env
		return []byte("gho_person\n"), nil
	}
	token, err := personGitHub(context.Background(), path, "/usr/bin/devctl", environ, output)
	if err != nil {
		t.Fatal(err)
	}
	if token.Value != "gho_person" || token.Source != SourceGHLogin || Identity(token) != IdentityGH {
		t.Errorf("token: %+v", token)
	}
	if gotPath != gh || strings.Join(gotArgs, " ") != "auth token --hostname github.com" {
		t.Errorf("ran %s %v", gotPath, gotArgs)
	}
	if slices.ContainsFunc(gotEnv, func(kv string) bool {
		return strings.HasPrefix(kv, "GH_TOKEN=") || strings.HasPrefix(kv, "GITHUB_TOKEN=")
	}) {
		t.Errorf("gh must answer its stored login, not a token variable: %v", gotEnv)
	}
	if !slices.Contains(gotEnv, "HOME=/home/x") {
		t.Errorf("the rest of the environment is kept: %v", gotEnv)
	}
}

func TestPersonGitHub_authRequired(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"), 0o755); err != nil { // #nosec G306 -- a test executable
		t.Fatal(err)
	}
	noLogin := func(context.Context, string, []string, []string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	}
	for name, path := range map[string]string{"no gh": t.TempDir(), "no login": dir} {
		_, err := personGitHub(context.Background(), path, "/usr/bin/devctl", nil, noLogin)
		if agentcli.Exit(err) != agentcli.ExitAuthRequired {
			t.Errorf("%s: want exit 8, got %v", name, err)
		}
		if err == nil || !strings.Contains(err.Error(), "gh auth login") || !strings.Contains(err.Error(), "outside giantswarm") {
			t.Errorf("%s: the reason names the login to run and why: %v", name, err)
		}
	}
}
