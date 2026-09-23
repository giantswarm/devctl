package authstore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// countingStore is a store that counts every access.
type countingStore struct {
	Store
	calls int
}

func (s *countingStore) Get(user string) (Record, error) {
	s.calls++
	return s.Store.Get(user)
}

func (s *countingStore) Set(user string, record Record) error {
	s.calls++
	return s.Store.Set(user, record)
}

// clearGitHubEnv unsets CI and every GitHub token variable for the test:
// the CI running the tests sets some of them.
func clearGitHubEnv(t *testing.T) {
	t.Helper()
	for _, name := range append([]string{EnvCI, "MY_TOKEN"}, GitHubEnvVars...) {
		t.Setenv(name, "")
	}
}

func TestResolveGitHub(t *testing.T) {
	login := &Record{Login: "octocat", Token: "ghu_keychain", ExpiresAt: testNow.Add(time.Hour)}

	cases := []struct {
		name    string
		env     map[string]string
		envVars []string
		record  *Record

		wantValue   string
		wantSource  string
		wantWarning []string // substrings; nil means no warning
		wantErr     []string // substrings of the exit-8 sentence
		wantStore   bool     // the keychain was read
	}{
		{name: "variable set", env: map[string]string{"GITHUB_TOKEN": "ghp_env"}, record: login,
			wantValue: "ghp_env", wantSource: "$GITHUB_TOKEN",
			wantWarning: []string{"$GITHUB_TOKEN", "`devctl auth login --github-only`", "unset GITHUB_TOKEN"}},
		{name: "variables in order", env: map[string]string{"GITHUB_TOKEN": "ghp_second", "DEVCTL_GITHUB_TOKEN": "ghp_first", "OPSCTL_GITHUB_TOKEN": "ghp_third"},
			wantValue: "ghp_first", wantSource: "$DEVCTL_GITHUB_TOKEN", wantWarning: []string{"$DEVCTL_GITHUB_TOKEN"}},
		{name: "last variable", env: map[string]string{"OPSCTL_GITHUB_TOKEN": "ghp_third"},
			wantValue: "ghp_third", wantSource: "$OPSCTL_GITHUB_TOKEN", wantWarning: []string{"$OPSCTL_GITHUB_TOKEN"}},
		{name: "variable set with CI", env: map[string]string{"GITHUB_TOKEN": "ghp_env", EnvCI: "true"}, record: login,
			wantValue: "ghp_env", wantSource: "$GITHUB_TOKEN"},
		{name: "named variable only", env: map[string]string{"GITHUB_TOKEN": "ghp_ignored", "MY_TOKEN": "ghp_named"}, envVars: []string{"MY_TOKEN"},
			wantValue: "ghp_named", wantSource: "$MY_TOKEN", wantWarning: []string{"$MY_TOKEN", "unset MY_TOKEN"}},
		{name: "keychain login only", record: login,
			wantValue: "ghu_keychain", wantSource: SourceKeychain, wantStore: true},
		{name: "keychain login only, named variable unset", env: map[string]string{"GITHUB_TOKEN": "ghp_ignored"}, envVars: []string{"MY_TOKEN"}, record: login,
			wantValue: "ghu_keychain", wantSource: SourceKeychain, wantStore: true},
		{name: "neither",
			wantErr: []string{"no token in the keychain", "run `devctl auth login --github-only`."}, wantStore: true},
		{name: "neither with CI", env: map[string]string{EnvCI: "1"}, record: login,
			wantErr: []string{"$CI is set", "$DEVCTL_GITHUB_TOKEN, $GITHUB_TOKEN or $OPSCTL_GITHUB_TOKEN"}},
		{name: "neither with CI, named variable", env: map[string]string{EnvCI: "1", "GITHUB_TOKEN": "ghp_ignored"}, envVars: []string{"MY_TOKEN"},
			wantErr: []string{"there is no token in $MY_TOKEN)."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearGitHubEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			store := &countingStore{Store: &FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}}
			if tc.record != nil {
				if err := store.Store.Set(UserGitHub, *tc.record); err != nil {
					t.Fatal(err)
				}
			}
			opened := false
			open := func() (*Auth, error) {
				opened = true
				return New(Config{Store: store, Clock: agentcli.NewClock(1, func() time.Time { return testNow })})
			}

			token, err := resolveGitHub(context.Background(), open, githubVars(tc.envVars))

			if opened != tc.wantStore || (store.calls > 0) != tc.wantStore {
				t.Fatalf("store opened %v, %d call(s), want read %v", opened, store.calls, tc.wantStore)
			}
			if tc.wantErr != nil {
				var authErr *AuthRequiredError
				if !errors.As(err, &authErr) || !errors.Is(err, ErrAuthRequired) || agentcli.Exit(err) != agentcli.ExitAuthRequired {
					t.Fatalf("err = %#v, want exit 8", err)
				}
				for _, want := range tc.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q lacks %q", err.Error(), want)
					}
				}
				if token != (Token{}) {
					t.Errorf("token = %+v with the error", token)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if token.Value != tc.wantValue || token.Source != tc.wantSource {
				t.Fatalf("token = %q from %q, want %q from %q", token.Value, token.Source, tc.wantValue, tc.wantSource)
			}
			if tc.wantWarning == nil && token.Warning != "" {
				t.Fatalf("warning = %q, want none", token.Warning)
			}
			for _, want := range tc.wantWarning {
				if !strings.Contains(token.Warning, want) {
					t.Errorf("warning %q lacks %q", token.Warning, want)
				}
			}
			if strings.Contains(token.Warning, token.Value) || strings.Contains(token.Warning, "\n") {
				t.Errorf("warning is not one line without the token: %q", token.Warning)
			}
			if got := GitHubOverrideWarning(tc.envVars...); got != token.Warning {
				t.Errorf("GitHubOverrideWarning = %q, want %q", got, token.Warning)
			}
		})
	}
}

// TestResolveGitHubRefreshFailureStaysOnTheKeychain: an expired login whose
// refresh GitHub refuses is exit 8 naming the GitHub login, nothing else.
func TestResolveGitHubRefreshFailureStaysOnTheKeychain(t *testing.T) {
	clearGitHubEnv(t)
	gh := newFakeGitHub(t, "octocat", map[string]any{"error": "bad_refresh_token"})
	a, store, _ := newTestAuth(t, gh.server, nil, nil)
	if err := store.Set(UserGitHub, Record{Login: "octocat", Token: "stale", ExpiresAt: testNow.Add(-time.Minute),
		RefreshToken: "ghr_old", RefreshExpiresAt: testNow.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	_, err := resolveGitHub(context.Background(), func() (*Auth, error) { return a, nil }, GitHubEnvVars)
	if agentcli.Exit(err) != agentcli.ExitAuthRequired || !strings.Contains(err.Error(), "`devctl auth login --github-only`") {
		t.Fatalf("err = %v", err)
	}
}

// TestResolveGitHubFromTheEnvironmentStore runs the exported function over
// the store the environment selects.
func TestResolveGitHubFromTheEnvironmentStore(t *testing.T) {
	clearGitHubEnv(t)
	path := filepath.Join(t.TempDir(), "keyring.json")
	t.Setenv(agentcli.EnvKeyringFile, path)
	store := &FileStore{Path: path}
	if err := store.Set(UserGitHub, Record{Login: "octocat", Token: "ghu_keychain"}); err != nil {
		t.Fatal(err)
	}
	token, err := ResolveGitHub(context.Background())
	if err != nil || token.Value != "ghu_keychain" || token.Source != SourceKeychain || token.Login != "octocat" {
		t.Fatalf("token = %+v, err = %v", token, err)
	}
}
