package approvemergerenovate

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

type lifecycleTransport func(*http.Request) (*http.Response, error)

func (f lifecycleTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func lifecycleResponse(req *http.Request, body string) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestRunHonorsCancelledCommandContext(t *testing.T) {
	t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-token")
	original := http.DefaultTransport
	var uncancelled atomic.Bool
	http.DefaultTransport = lifecycleTransport(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		uncancelled.Store(true)
		return nil, errors.New("unexpected uncancelled offline request")
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	r := &runner{flag: &flag{Grouping: GroupingDependency}, logger: logrus.New(), stdout: io.Discard, stderr: io.Discard}
	err := r.Run(cmd, []string{"dependency"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context cancellation", err)
	}
	if uncancelled.Load() {
		t.Error("command discarded its cancelled context")
	}
}

func TestProcessPRCancellationInterruptsWaits(t *testing.T) {
	for _, autoMerge := range []bool{false, true} {
		name := "pending checks"
		if autoMerge {
			name = "auto merge confirmation"
		}
		t.Run(name, func(t *testing.T) {
			reachedWait := make(chan struct{})
			var mutations atomic.Int32
			transport := lifecycleTransport(func(req *http.Request) (*http.Response, error) {
				if err := req.Context().Err(); err != nil {
					return nil, err
				}
				switch {
				case req.Method == http.MethodPost:
					mutations.Add(1)
					close(reachedWait)
					return lifecycleResponse(req, `{}`)
				case strings.HasSuffix(req.URL.Path, "/pulls/42"):
					if autoMerge {
						return lifecycleResponse(req, `{"head":{"sha":"head"},"auto_merge":{}}`)
					}
					return lifecycleResponse(req, `{"head":{"sha":"head"}}`)
				case strings.HasSuffix(req.URL.Path, "/status"):
					if autoMerge {
						return lifecycleResponse(req, `{"state":"success","total_count":1}`)
					}
					return lifecycleResponse(req, `{"state":"pending","total_count":1}`)
				case strings.HasSuffix(req.URL.Path, "/check-runs"):
					if !autoMerge {
						close(reachedWait)
					}
					return lifecycleResponse(req, `{"total_count":0,"check_runs":[]}`)
				case strings.HasSuffix(req.URL.Path, "/reviews"):
					return lifecycleResponse(req, `[]`)
				default:
					return nil, errors.New("unexpected request")
				}
			})
			client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: transport}))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				(&runner{flag: &flag{}}).processPR(ctx, client, &pr.PRStatus{Owner: "one", Repo: "api", Number: 42})
			}()
			<-reachedWait
			cancel()
			select {
			case <-done:
			case <-time.After(200 * time.Millisecond):
				t.Error("cancelled worker kept waiting")
				<-done
			}
			wantMutations := int32(0)
			if autoMerge {
				wantMutations = 1
			}
			if mutations.Load() != wantMutations {
				t.Errorf("mutation count = %d, want %d", mutations.Load(), wantMutations)
			}
		})
	}
}

func lifecycleRunner(t *testing.T, transport http.RoundTripper, watch bool) *runner {
	t.Helper()
	t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-token")
	original := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original })
	return &runner{flag: &flag{Grouping: GroupingDependency, Watch: watch}, logger: logrus.New(), stdout: io.Discard, stderr: io.Discard, pollInterval: time.Millisecond}
}

func TestRunWaitsForDiscoveredWorkers(t *testing.T) {
	pollStarted, initialFinished, discoveredStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseSearch, releaseWorker := make(chan struct{}), make(chan struct{})
	var searches atomic.Int32
	transport := lifecycleTransport(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/search/issues":
			if searches.Add(1) == 1 {
				return lifecycleResponse(req, `{"total_count":1,"items":[{"number":42,"html_url":"https://github.com/one/api/pull/42"}]}`)
			}
			if searches.Load() == 2 {
				close(pollStarted)
				<-releaseSearch
			}
			return lifecycleResponse(req, `{"total_count":1,"items":[{"number":43,"html_url":"https://github.com/one/api/pull/43"}]}`)
		case strings.HasSuffix(req.URL.Path, "/pulls/42"):
			<-pollStarted
			close(initialFinished)
			return lifecycleResponse(req, `{"merged":true}`)
		case strings.HasSuffix(req.URL.Path, "/pulls/43"):
			close(discoveredStarted)
			<-releaseWorker
			return lifecycleResponse(req, `{"merged":true}`)
		default:
			return nil, errors.New("unexpected request")
		}
	})
	r := lifecycleRunner(t, transport, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.run(ctx, &cobra.Command{}, []string{"dependency"}) }()
	<-initialFinished
	select {
	case err := <-done:
		t.Errorf("returned during discovery: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseSearch)
	<-discoveredStarted
	select {
	case err := <-done:
		t.Errorf("returned before discovered worker: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseWorker)
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(time.Second):
		t.Fatal("did not finish after every worker completed")
	}
}

func TestRunCancellationDrainsOwnedRequests(t *testing.T) {
	for _, polling := range []bool{false, true} {
		name := "worker"
		if polling {
			name = "discovery"
		}
		t.Run(name, func(t *testing.T) {
			started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var searches atomic.Int32
			transport := lifecycleTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/search/issues" && searches.Add(1) == 1 {
					if polling {
						return lifecycleResponse(req, `{"total_count":0,"items":[]}`)
					}
					return lifecycleResponse(req, `{"total_count":1,"items":[{"number":42,"html_url":"https://github.com/one/api/pull/42"}]}`)
				}
				close(started)
				<-req.Context().Done()
				close(cancelled)
				<-release // The transport is still cleaning up its cancelled request.
				return nil, req.Context().Err()
			})
			r := lifecycleRunner(t, transport, polling)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			done := make(chan error, 1)
			go func() { done <- r.Run(cmd, []string{"dependency"}) }()
			<-started
			cancel()
			<-cancelled
			select {
			case err := <-done:
				t.Errorf("returned with an owned request still running: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("error = %v, want cancellation", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled run did not drain")
			}
		})
	}
}

func TestWatchRunCanCancelWithoutPRs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := lifecycleRunner(t, lifecycleTransport(func(req *http.Request) (*http.Response, error) {
		cancel()
		return lifecycleResponse(req, `{"total_count":0,"items":[]}`)
	}), true)
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	if err := r.Run(cmd, []string{"dependency"}); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want cancellation", err)
	}
}

func TestProcessPRCancellationPreservesKnownMerge(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := lifecycleTransport(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/pulls/42"):
			return lifecycleResponse(req, `{"head":{"sha":"head"}}`)
		case strings.HasSuffix(req.URL.Path, "/status"):
			return lifecycleResponse(req, `{"state":"success","total_count":1}`)
		case strings.HasSuffix(req.URL.Path, "/check-runs"):
			return lifecycleResponse(req, `{"total_count":0,"check_runs":[]}`)
		case strings.HasSuffix(req.URL.Path, "/reviews"):
			if req.Method == http.MethodGet {
				return lifecycleResponse(req, `[]`)
			}
			return lifecycleResponse(req, `{}`)
		case strings.HasSuffix(req.URL.Path, "/merge"):
			cancel()
			return lifecycleResponse(req, `{"merged":true}`)
		case req.URL.Path == "/repos/one/api":
			return lifecycleResponse(req, `{"allow_squash_merge":true}`)
		default:
			return nil, errors.New("unexpected request")
		}
	})
	client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	ps := &pr.PRStatus{Owner: "one", Repo: "api", Number: 42}
	(&runner{flag: &flag{}}).processPR(ctx, client, ps)
	if got := ps.GetStatus(); got != "Merged (squash)" {
		t.Errorf("acknowledged merge was lost on cancellation: %q", got)
	}
}
