package laptop

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// releaseWaitLogin isolates both logins: the App token is in a temporary
// keyring and gh is a test executable that answers only its stored login.
func releaseWaitLogin(t *testing.T) *authstore.FileStore {
	t.Helper()
	store := &authstore.FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
	if err := store.Set(authstore.UserGitHub, authstore.Record{Token: "ghu_app"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(agentcli.EnvKeyringFile, store.Path)
	dir := t.TempDir()
	gh := `#!/bin/sh
if [ "$*" != "auth token --hostname github.com" ]; then exit 1; fi
if [ -n "${GH_TOKEN-}" ] || [ -n "${GITHUB_TOKEN-}" ]; then exit 1; fi
printf 'gho_person\n'
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(gh), 0o755); err != nil { // #nosec G306 -- a test executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("GH_TOKEN", "ghu_environment")
	t.Setenv("GITHUB_TOKEN", "ghu_environment")
	return store
}

// The private repository hides its PR from the wrong identity with a 404.
// The lookup used by pr wait succeeds; release wait must use that same
// identity and read the merge commit through the same REST endpoint.
func TestReleaseWaitSourcesPrivatePullRequest(t *testing.T) {
	for _, tc := range []struct {
		owner, authorization string
	}{
		{"someone", "Bearer gho_person"},
		{"giantswarm", "Bearer ghu_app"},
	} {
		t.Run(tc.owner, func(t *testing.T) {
			releaseWaitLogin(t)
			mergedAt := time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)
			var reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/repos/"+tc.owner+"/private/pulls/42" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if r.Header.Get("Authorization") != tc.authorization {
					http.NotFound(w, r)
					return
				}
				reads.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"number":42,"state":"closed","merged":true,"merge_commit_sha":"merge-sha","merged_at":%q,"base":{"repo":{"private":true}}}`, mergedAt.Format(time.RFC3339))
			}))
			t.Cleanup(server.Close)

			token, err := authexec.RepositoryToken(t.Context(), tc.owner, authstore.RequireGitHub, authexec.PersonGitHub)
			if err != nil {
				t.Fatal(err)
			}
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			prClient, _, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: token.Value, BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			pr, err := prClient.PullRequest(t.Context(), tc.owner, "private", 42)
			if err != nil {
				t.Fatalf("pr wait lookup: %v", err)
			}
			sources, err := ReleaseWaitSources(t.Context(), tc.owner, agentcli.Endpoints{GitHubAPIURL: server.URL}, nil, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			merge, err := sources.GitHub.GetPullRequestMerge(t.Context(), tc.owner, "private", 42)
			if err != nil {
				t.Fatalf("release wait lookup after pr wait succeeded: %v", err)
			}
			if !merge.Merged || merge.Number != pr.GetNumber() || merge.State != pr.GetState() || merge.MergeCommitSHA != pr.GetMergeCommitSHA() || !merge.MergedAt.Equal(mergedAt) {
				t.Errorf("merge state differs from pr wait's lookup: %+v", merge)
			}
			if reads.Load() != 2 {
				t.Errorf("want two authorized PR lookups, got %d", reads.Load())
			}
		})
	}
}

// A refused personal login must remain that identity's failure, rather
// than consulting the App keyring and retrying with a different identity.
func TestReleaseWaitSourcesPersonalLoginRefused(t *testing.T) {
	releaseWaitLogin(t)
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"Bad credentials"}`)
	}))
	t.Cleanup(server.Close)
	sources, err := ReleaseWaitSources(t.Context(), "someone", agentcli.Endpoints{GitHubAPIURL: server.URL}, nil, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sources.GitHub.GetPullRequestMerge(t.Context(), "someone", "private", 42)
	var response *github.ErrorResponse
	if !errors.As(err, &response) || response.Response.StatusCode != http.StatusUnauthorized || reads.Load() != 1 {
		t.Fatalf("want the personal login's 401 after one read, got %v (%d reads)", err, reads.Load())
	}
}

func TestReleaseWaitSourcesAppTokenRenews(t *testing.T) {
	store := releaseWaitLogin(t)
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if reads.Add(1) == 1 {
			// Another command renewed the App token while this wait ran.
			if err := store.Set(authstore.UserGitHub, authstore.Record{Token: "ghu_renewed"}); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"Bad credentials"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer ghu_renewed" {
			t.Error("the retry must carry the renewed App token")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"number":42,"state":"closed","merged":true,"merge_commit_sha":"merge-sha"}`)
	}))
	t.Cleanup(server.Close)
	sources, err := ReleaseWaitSources(t.Context(), "giantswarm", agentcli.Endpoints{GitHubAPIURL: server.URL}, nil, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	merge, err := sources.GitHub.GetPullRequestMerge(t.Context(), "giantswarm", "private", 42)
	if err != nil || merge.MergeCommitSHA != "merge-sha" || reads.Load() != 2 {
		t.Fatalf("want the merge after one App renewal, got %+v, %v (%d reads)", merge, err, reads.Load())
	}
}
