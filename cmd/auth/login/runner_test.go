package login

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/muster"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// fakeGitHub authorizes on the first poll.
func fakeGitHub(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dev", "user_code": "WXYZ-9876", "verification_uri": server.URL + "/login/device",
			"expires_in": 900, "interval": 1,
		})
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "ghu_secret", "token_type": "bearer", "expires_in": 28800,
			"refresh_token": "ghr_secret", "refresh_token_expires_in": 15897600,
		})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghu_secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "octocat"})
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func newRunner(t *testing.T, github *httptest.Server) (*runner, *authstore.FileStore, *bytes.Buffer, *bytes.Buffer) {
	return newRunnerWithBrowser(t, github, func(string) error { return errors.New("no display") })
}

// newRunnerWithBrowser is newRunner with the opener the login hands every
// URL for the person.
func newRunnerWithBrowser(t *testing.T, github *httptest.Server, openBrowser func(string) error) (*runner, *authstore.FileStore, *bytes.Buffer, *bytes.Buffer) {
	store := &authstore.FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
	endpoints := agentcli.DefaultEndpoints()
	if github != nil {
		endpoints.GitHubOAuthURL = github.URL
		endpoints.GitHubAPIURL = github.URL
	}
	var stdout, stderr bytes.Buffer
	r := &runner{
		gate:   func(bool) error { return nil },
		flag:   &flag{},
		stdout: &stdout,
		stderr: &stderr,
		open: func(w io.Writer) (*authstore.Auth, error) {
			return authstore.New(authstore.Config{
				Store:       store,
				Endpoints:   endpoints,
				Clock:       agentcli.NewClock(0.001, nil),
				OpenBrowser: openBrowser,
				Stderr:      w,
			})
		},
		clock:       agentcli.NewClock(0.001, nil),
		openBrowser: openBrowser,
	}
	return r, store, &stdout, &stderr
}

// fetch is the person at the browser: it loads the URL, and the muster
// mock's authorization endpoint redirects straight back to the loopback
// callback with the code.
func fetch(url string) error {
	go func() {
		resp, err := http.Get(url) //nolint:gosec // the test's own mock URL
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	return nil
}

// --muster-only signs in to muster, stores the record with the endpoint,
// then completes the sign-in to giantswarm-repo-manager: the consent URL is
// printed, and the manager's get_info is asked until it answers the caller.
func TestLoginMusterOnly(t *testing.T) {
	m := muster.Start(muster.Config{
		Login: "octocat@example.com",
		Tools: map[string][]muster.ToolResponse{
			manager.ToolAuthLogin: {{Result: "Please open http://127.0.0.1:1/consent to authorize the App"}},
			manager.ToolGetInfo: {
				{Error: "tool " + manager.ToolGetInfo + " requires authentication"},
				{Result: map[string]any{"caller": map[string]any{"login": "octocat", "id": 1}}},
			},
		},
	})
	defer m.Close()
	r, store, stdout, stderr := newRunnerWithBrowser(t, nil, fetch)
	r.flag.MusterOnly = true
	r.flag.MusterEndpoint = m.MCPURL()
	if err := r.run(context.Background(), nil); err != nil {
		t.Fatalf("err = %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	record, err := store.Get(authstore.UserMuster)
	if err != nil || record.Login != "octocat@example.com" || record.Endpoint != m.MCPURL() || record.RefreshToken == "" {
		t.Fatalf("stored record = %+v, %v", record, err)
	}
	if _, err := store.Get(authstore.UserGitHub); !errors.Is(err, authstore.ErrNotFound) {
		t.Fatalf("--muster-only touched GitHub: %v", err)
	}

	var doc document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("stdout: %v\n%s", err, stdout.String())
	}
	if doc.ExitCode != 0 || !doc.Muster.Present || !doc.Muster.Refreshable || doc.Muster.Endpoint != m.MCPURL() {
		t.Fatalf("document = %+v", doc)
	}
	if doc.Manager == nil || !doc.Manager.SignedIn || doc.Manager.Caller != "octocat" || doc.Manager.Server != manager.Server {
		t.Fatalf("manager sign-in = %+v", doc.Manager)
	}
	if !strings.Contains(stderr.String(), "open http://127.0.0.1:1/consent") {
		t.Fatalf("stderr lacks the consent URL:\n%s", stderr.String())
	}
}

// A muster without the manager: the login stands, the document notes it.
func TestLoginMusterOnlyWithoutManager(t *testing.T) {
	m := muster.Start(muster.Config{Tools: map[string][]muster.ToolResponse{
		manager.ToolAuthLogin: {{Error: "Server 'giantswarm-repo-manager' not found. Use list_tools to see available servers."}},
	}})
	defer m.Close()
	r, _, stdout, stderr := newRunnerWithBrowser(t, nil, fetch)
	r.flag.MusterOnly = true
	r.flag.MusterEndpoint = m.MCPURL()
	if err := r.run(context.Background(), nil); err != nil {
		t.Fatalf("err = %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	var doc document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Manager == nil || doc.Manager.SignedIn || doc.Manager.Note == "" || len(doc.Warnings) != 1 {
		t.Fatalf("document = %+v", doc)
	}
}

// The three --*-only flags exclude each other.
func TestValidateOnlyFlags(t *testing.T) {
	f := &flag{MusterOnly: true, GitHubOnly: true}
	if err := f.Validate(); err == nil {
		t.Fatal("two --*-only flags were accepted")
	}
	f = &flag{MusterOnly: true}
	if err := f.Validate(); err == nil {
		t.Fatal("--muster-only without an endpoint was accepted")
	}
}

func TestLoginGitHubOnly(t *testing.T) {
	r, store, stdout, stderr := newRunner(t, fakeGitHub(t))
	r.flag.GitHubOnly = true
	if err := r.run(context.Background(), nil); err != nil {
		t.Fatalf("err = %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	record, err := store.Get(authstore.UserGitHub)
	if err != nil || record.Token != "ghu_secret" || record.RefreshToken != "ghr_secret" || record.Login != "octocat" {
		t.Fatalf("stored record = %+v, %v", record, err)
	}
	if _, err := store.Get(authstore.UserCircleCI); !errors.Is(err, authstore.ErrNotFound) {
		t.Fatalf("--github-only touched CircleCI: %v", err)
	}

	for _, output := range []string{stdout.String(), stderr.String()} {
		if strings.Contains(output, "ghu_secret") || strings.Contains(output, "ghr_secret") {
			t.Fatalf("a token was printed:\n%s", output)
		}
	}
	if err := stderr.String(); !strings.Contains(err, "WXYZ-9876") || !strings.Contains(err, "/login/device") || !strings.Contains(err, "no browser opened") {
		t.Fatalf("stderr = %q", err)
	}

	var doc struct {
		agentcli.Envelope
		authstore.Status
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout.String())
	}
	if doc.Command != "auth login" || doc.ExitCode != 0 || doc.Verdict != agentcli.VerdictGreen {
		t.Fatalf("envelope = %+v", doc.Envelope)
	}
	if !doc.GitHub.Present || doc.GitHub.Login != "octocat" || !doc.GitHub.Refreshable || doc.GitHub.ExpiresAt == nil {
		t.Fatalf("github = %+v", doc.GitHub)
	}
	if doc.CircleCI.Present {
		t.Fatalf("circleci = %+v", doc.CircleCI)
	}
}

func TestLoginFlagsExcludeEachOther(t *testing.T) {
	r, _, stdout, _ := newRunner(t, nil)
	r.flag.GitHubOnly = true
	r.flag.CircleCIOnly = true
	err := r.run(context.Background(), nil)
	var exitErr *agentcli.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != agentcli.ExitUsage || exitErr.Verdict != agentcli.VerdictUsage {
		t.Fatalf("err = %#v", err)
	}
	var doc agentcli.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil || doc.ExitCode != agentcli.ExitUsage || !strings.Contains(doc.Reason, "--github-only") {
		t.Fatalf("document = %+v, %v", doc, err)
	}
	if doc.FinishedAt.Before(doc.StartedAt) || time.Since(doc.StartedAt) > time.Minute {
		t.Fatalf("timestamps = %s / %s", doc.StartedAt, doc.FinishedAt)
	}
}
