package wait

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/giantswarm/devctl/v8/internal/laptop"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

type circleTransport func(*http.Request) (*http.Response, error)

func (f circleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the CLI, laptop credential gate and shared source constructor
// together. The only credential store is a temporary synthetic file and all
// service reads stay in process, including the actual CircleCI HTTP client.
func TestRunCircleCILoginRenewal(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		renew      bool
		persistent bool
	}{
		{name: "renewed login", status: 401, renew: true},
		{name: "transient 401", status: 401},
		{name: "transient 5xx", status: 503},
		{name: "persistent 401", status: 401, renew: true, persistent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &authstore.FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
			t.Setenv(agentcli.EnvKeyringFile, store.Path)
			t.Setenv(agentcli.EnvTimeScale, "1")
			synctest.Test(t, func(t *testing.T) {
				for user, token := range map[string]string{authstore.UserGitHub: "fixture-github", authstore.UserCircleCI: "fixture-old"} {
					if err := store.Set(user, authstore.Record{Token: token, ExpiresAt: time.Now().Add(24 * time.Hour)}); err != nil {
						t.Fatal(err)
					}
				}
				workflows := 0
				transport := circleTransport(func(req *http.Request) (*http.Response, error) {
					wantToken := "fixture-old"
					if tc.renew && workflows >= 2 {
						wantToken = "fixture-new"
					}
					if req.Header.Get("Circle-Token") != wantToken || req.Header.Get("Authorization") != "" {
						t.Fatal("CircleCI received the wrong identity")
					}
					status, body := 200, ""
					switch req.URL.Path {
					case "/api/v2/project/gh/giantswarm/devctl/pipeline":
						body = `{"items":[{"id":"p1","number":12,"vcs":{"tag":"v1.2.3","revision":"` + releasewaittest.ReleaseSHA + `"}}]}`
					case "/api/v2/pipeline/p1/workflow":
						workflows++
						state := "success"
						if workflows == 1 {
							state = "running"
							if tc.renew {
								if err := store.Set(authstore.UserCircleCI, authstore.Record{Token: "fixture-new", ExpiresAt: time.Now().Add(24 * time.Hour)}); err != nil {
									t.Fatal(err)
								}
							}
						}
						body = `{"items":[{"id":"w1","name":"build","status":"` + state + `","pipeline_number":12}]}`
						if workflows == 2 || tc.persistent && workflows > 2 {
							status, body = tc.status, `{"message":"Invalid token provided"}`
						}
					case "/api/v2/workflow/w1/job":
						body = `{"items":[{"name":"push-to-registries-release","type":"build","status":"success"},{"name":"push-chart-release","type":"build","status":"success"}]}`
					default:
						t.Fatalf("unexpected CircleCI request: %s", req.URL.Path)
					}
					return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
				})
				var stdout bytes.Buffer
				r := &runner{
					gate: func(bool) error { return nil }, flag: &flag{Timeout: 2 * time.Minute}, stdout: &stdout, stderr: io.Discard,
					open: func(ctx context.Context, endpoints agentcli.Endpoints, retry http.RoundTripper, warn func(string)) (*releasewait.Sources, error) {
						retry.(*agentcli.Retrying).Base = transport
						actual, err := laptop.ReleaseWaitSources(ctx, endpoints, retry, warn)
						if err != nil {
							return nil, err
						}
						fixture := sources(t, "success")
						fixture.CircleCI = actual.CircleCI
						return fixture, nil
					},
				}
				err := r.run(t.Context(), []string{"giantswarm/devctl", "v1.2.3"})
				var doc releasewait.Document
				if decodeErr := json.Unmarshal(stdout.Bytes(), &doc); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				wantCode, wantVerdict := 0, agentcli.VerdictAvailable
				if tc.persistent {
					wantCode, wantVerdict = agentcli.ExitRemoteError, agentcli.VerdictRemoteError
				}
				if agentcli.Exit(err) != wantCode || doc.ExitCode != wantCode || doc.Verdict != wantVerdict {
					t.Fatalf("exit=%d document=%+v error=%v", agentcli.Exit(err), doc, err)
				}
				if workflows != 3 {
					t.Fatalf("workflow reads=%d, want pending, refused, retry", workflows)
				}
				if strings.Contains(stdout.String(), "fixture-") {
					t.Fatal("document exposed a fixture credential")
				}
			})
		})
	}
}
