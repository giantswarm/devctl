package approvemergerenovate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

type renewTransport struct {
	store    *authstore.FileStore
	requests int
}

func (f *renewTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.requests++
	if req.URL.Path != testSearchIssuesPath || req.Method != http.MethodGet {
		return nil, fmt.Errorf("unexpected offline request")
	}
	if f.requests == 1 {
		// Another run renewed the selected App login after this command read it.
		if err := f.store.Set(authstore.UserGitHub, authstore.Record{Token: "synthetic-new", Login: "fixture-user", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			return nil, err
		}
	}
	code, body := http.StatusUnauthorized, `{"message":"expired"}`
	if req.Header.Get("Authorization") == "Bearer synthetic-new" {
		code, body = 200, `{"total_count":0,"items":[]}`
	}
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestRunRenewsOnlyStoredGitHubLogin(t *testing.T) {
	for _, override := range []bool{false, true} {
		name := "stored App login"
		if override {
			name = "explicit environment identity"
		}
		t.Run(name, func(t *testing.T) {
			for _, key := range []string{"DEVCTL_GITHUB_TOKEN", "GITHUB_TOKEN", "OPSCTL_GITHUB_TOKEN", "CI"} {
				t.Setenv(key, "")
			}
			store := &authstore.FileStore{Path: filepath.Join(t.TempDir(), "synthetic-keyring.json")}
			t.Setenv(agentcli.EnvKeyringFile, store.Path)
			if err := store.Set(authstore.UserGitHub, authstore.Record{Token: "synthetic-old", Login: "fixture-user", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			if override {
				t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-override")
			}
			fake := &renewTransport{store: store}
			original := http.DefaultTransport
			http.DefaultTransport = fake
			t.Cleanup(func() { http.DefaultTransport = original })
			r := &runner{flag: &flag{Grouping: GroupingDependency}, logger: logrus.New(), stdout: io.Discard, stderr: io.Discard}
			err := r.run(context.Background(), &cobra.Command{}, []string{GroupingDependency})
			wantRequests := 2
			if override {
				wantRequests = 1
				if err == nil {
					t.Error("a refused explicit identity must not fall back to the stored login")
				}
			} else if err != nil {
				t.Errorf("stored login did not recover after renewal: %v", err)
			}
			if fake.requests != wantRequests {
				t.Errorf("request count = %d, want %d", fake.requests, wantRequests)
			}
		})
	}
}
