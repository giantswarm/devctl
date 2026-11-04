package login

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/muster"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// A later failure does not roll back a completed login. The failure document
// must describe the credentials that are actually stored, including identities
// that were present before this invocation.
func TestLoginFailureReportsStoredIdentities(t *testing.T) {
	t.Run("CircleCI fails after GitHub was stored", func(t *testing.T) {
		github := fakeGitHub(t)
		r, store, stdout, stderr := newRunner(t, github)
		endpoints := agentcli.DefaultEndpoints()
		endpoints.GitHubOAuthURL, endpoints.GitHubAPIURL = github.URL, github.URL
		// This mock has no registration route: CircleCI fails locally with 404.
		endpoints.CircleCIOAuthURL, endpoints.CircleCIAPIURL = github.URL, github.URL
		r.open = func(w io.Writer) (*authstore.Auth, error) {
			return authstore.New(authstore.Config{
				Store: store, Endpoints: endpoints, Clock: r.clock, Stderr: w,
				OpenBrowser: func(string) error { return nil },
			})
		}
		if err := store.Set(authstore.UserMuster, authstore.Record{
			Token: "existing-muster-token", Login: "existing@example.com",
			Endpoint: "https://muster.example/mcp", ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.Set(authstore.UserCircleCI, authstore.Record{
			Token: "existing-circleci-token", Login: "previous-user", ExpiresAt: time.Now().Add(5 * 24 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
		err := r.run(context.Background(), nil)
		if agentcli.Exit(err) != agentcli.ExitUsage || !strings.Contains(err.Error(), "registering devctl as a CircleCI OAuth client") {
			t.Fatalf("expected the local CircleCI failure, got %v", err)
		}
		if _, err := store.Get(authstore.UserGitHub); err != nil {
			t.Fatalf("GitHub login was not stored: %v", err)
		}
		assertFailureStatus(t, r, stdout.Bytes())
		for _, secret := range []string{"ghu_secret", "ghr_secret", "existing-muster-token", "existing-circleci-token"} {
			if strings.Contains(stdout.String()+stderr.String(), secret) {
				t.Fatal("login output contains token material")
			}
		}
	})
	t.Run("manager refuses after muster was stored", func(t *testing.T) {
		m := muster.Start(muster.Config{Tools: map[string][]muster.ToolResponse{
			manager.ToolAuthLogin: {{Error: "manager sign-in unavailable"}},
		}})
		defer m.Close()
		r, store, stdout, _ := newRunnerWithBrowser(t, nil, fetch)
		r.flag.MusterOnly, r.flag.MusterEndpoint = true, m.MCPURL()
		err := r.run(context.Background(), nil)
		if agentcli.Exit(err) != agentcli.ExitUsage || !strings.Contains(err.Error(), "manager sign-in unavailable") {
			t.Fatalf("expected the manager refusal, got %v", err)
		}
		if _, err := store.Get(authstore.UserMuster); err != nil {
			t.Fatalf("muster login was not stored: %v", err)
		}
		assertFailureStatus(t, r, stdout.Bytes())
	})
}

func assertFailureStatus(t *testing.T, r *runner, output []byte) {
	t.Helper()
	var doc document
	if err := json.Unmarshal(output, &doc); err != nil {
		t.Fatalf("expected one JSON document: %v", err)
	}
	auth, err := r.open(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want, err := auth.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.Status, want) {
		t.Errorf("failure document identities = %+v; stored identities = %+v", doc.Status, want)
	}
	if doc.ExitCode != agentcli.ExitUsage || doc.Reason == "" {
		t.Errorf("failure document lost the login error: %+v", doc.Envelope)
	}
	if !slices.Equal(doc.Warnings, want.CircleCI.Warnings) {
		t.Errorf("failure document warnings = %v, want %v", doc.Warnings, want.CircleCI.Warnings)
	}
}
