package merge

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

type mergeRetryTransport func(*http.Request) (*http.Response, error)

func (f mergeRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRunTransientReadWindow(t *testing.T) {
	for _, tc := range []struct {
		name        string
		path        string
		before      int
		failures    int
		wantCode    int
		wantVerdict string
	}{
		{name: "case 0: eight commit-status timeouts then merge", path: "/repos/o/r/commits/abc123/status", failures: 8, wantVerdict: "green"},
		{name: "case 1: initial PR read exhausts its window", path: "/repos/o/r/pulls/42", failures: 15, wantCode: 2, wantVerdict: "transient"},
		{name: "case 2: final PR read exhausts its window", path: "/repos/o/r/pulls/42", before: 2, failures: 15, wantCode: 2, wantVerdict: "transient"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, stdout, _, server := newRunner(t, green(), loggedIn, &flag{Timeout: 20 * time.Minute, NoReleaseWait: true})
			clock := agentcli.NewVirtualClock(time.Now())
			r.clock = func() (agentcli.Clock, error) { return clock, nil }
			reads, failures := 0, 0
			r.github = func(config githubclient.Config) (clients, error) {
				retrying := config.Transport.(*agentcli.Retrying)
				retrying.Base = mergeRetryTransport(func(req *http.Request) (*http.Response, error) {
					if req.Method == http.MethodGet && req.URL.Path == tc.path {
						reads++
						if reads > tc.before && failures < tc.failures {
							failures++
							if err := clock.Sleep(req.Context(), time.Second); err != nil {
								return nil, err
							}
							return nil, context.DeadlineExceeded
						}
					}
					return http.DefaultTransport.RoundTrip(req)
				})
				return newClients(config)
			}
			err := r.run(context.Background(), []string{"o/r", "42"})
			doc := decode(t, stdout)
			if agentcli.Exit(err) != tc.wantCode || doc["exitCode"] != float64(tc.wantCode) || doc["verdict"] != tc.wantVerdict {
				t.Fatalf("want %d/%s, got %v, document %+v", tc.wantCode, tc.wantVerdict, err, doc)
			}
			if failures != tc.failures {
				t.Fatalf("got %d injected timeouts, want %d", failures, tc.failures)
			}
			merges := 0
			for _, req := range server.Requests() {
				if req.Method == http.MethodPut && req.Path == "/repos/o/r/pulls/42/merge" {
					merges++
				}
			}
			if tc.wantCode == 0 {
				if merges != 1 || reads != 9 || doc["mergeCommitSha"] != "m1" {
					t.Fatalf("want nine status reads and one merge, got reads=%d merges=%d document=%+v", reads, merges, doc)
				}
				return
			}
			if merges != 0 || reads != tc.before+15 || doc["mergeCommitSha"] != "" {
				t.Fatalf("want no merge after 15 failed reads, got reads=%d merges=%d document=%+v", reads, merges, doc)
			}
			for _, part := range []string{tc.path, "15 times", "transient API or network failure after 10m17s"} {
				if !strings.Contains(doc["reason"].(string), part) {
					t.Errorf("reason %q does not name %q", doc["reason"], part)
				}
			}
		})
	}
}
