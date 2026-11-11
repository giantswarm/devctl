package approvemergerenovate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sirupsen/logrus"
)

type cancelTransport func(*http.Request) (*http.Response, error)

func (f cancelTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

const (
	testAllowSquashMergeKey = "allow_squash_merge"
	testApprovedState       = "APPROVED"
	testCheckRunsKey        = "check_runs"
	testCleanState          = "clean"
	testContentTypeHeader   = "Content-Type"
	testFixtureArg          = "fixture"
	testFixtureTitle        = "Update fixture"
	testHTMLURLKey          = "html_url"
	testHeadKey             = "head"
	testItemsKey            = "items"
	testJSONContentType     = "application/json"
	testLoginKey            = "login"
	testMergeableStateKey   = "mergeable_state"
	testMergedKey           = "merged"
	testMergedSquashStatus  = "Merged (squash)"
	testNumberKey           = "number"
	testReviewerLogin       = "reviewer"
	testSHAKey              = "sha"
	testSearchIssuesPath    = "/search/issues"
	testStateKey            = "state"
	testSuccessState        = "success"
	testTitleKey            = "title"
	testTotalCountKey       = "total_count"
)

func TestCommandCancellation(t *testing.T) {
	// Initialize runtime signal handling outside the virtual-time bubble.
	_, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for _, tc := range []struct {
		name                         string
		before, retry, afterApproval bool
		allowedWrites                int32
	}{
		{name: "before start", before: true},
		{name: "during checks"},
		{name: "during retry delay", retry: true},
		{name: "during post approval delay", afterApproval: true, allowedWrites: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-offline-token")
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if tc.before {
					cancel()
				}
				started := time.Now()
				var requests, writes atomic.Int32
				var scheduled atomic.Bool
				scheduleCancel := func() {
					if scheduled.CompareAndSwap(false, true) {
						go func() { time.Sleep(time.Second); cancel() }()
					}
				}
				previous := http.DefaultTransport
				http.DefaultTransport = cancelTransport(func(req *http.Request) (*http.Response, error) {
					if err := req.Context().Err(); err != nil {
						return nil, err
					}
					requests.Add(1)
					if req.Method != http.MethodGet {
						writes.Add(1)
					}
					var body any
					switch req.Method + " " + req.URL.Path {
					case "GET /search/issues":
						body = map[string]any{testTotalCountKey: 1, testItemsKey: []any{map[string]any{testNumberKey: 7, testTitleKey: testFixtureTitle, testHTMLURLKey: "https://github.com/o/r/pull/7"}}}
					case "GET /repos/o/r/pulls/7":
						body = map[string]any{testHeadKey: map[string]any{testSHAKey: "abc"}, testMergeableStateKey: testCleanState}
						if tc.afterApproval {
							body.(map[string]any)["auto_merge"] = map[string]any{"enabled": true}
						}
					case "GET /repos/o/r/commits/abc/status":
						body = map[string]any{testStateKey: testSuccessState, testTotalCountKey: 0}
						if tc.retry {
							scheduleCancel()
							body = map[string]any{testStateKey: "pending", testTotalCountKey: 1}
						} else if !tc.afterApproval {
							cancel()
						}
					case "GET /repos/o/r/commits/abc/check-runs":
						body = map[string]any{testCheckRunsKey: []any{}}
					case "GET /repos/o/r/pulls/7/reviews":
						body = []any{}
					case "POST /repos/o/r/pulls/7/reviews":
						if tc.afterApproval {
							scheduleCancel()
						}
						body = map[string]any{testStateKey: testApprovedState}
					case "GET /repos/o/r":
						body = map[string]any{testAllowSquashMergeKey: true}
					case "PUT /repos/o/r/pulls/7/merge":
						body = map[string]any{testMergedKey: true}
					default:
						t.Errorf("unexpected offline request: %s %s", req.Method, req.URL)
						body = map[string]any{}
					}
					raw, err := json.Marshal(body)
					if err != nil {
						return nil, err
					}
					return &http.Response{StatusCode: 200, Header: http.Header{testContentTypeHeader: {testJSONContentType}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
				})
				defer func() { http.DefaultTransport = previous }()
				logger := logrus.New()
				logger.SetOutput(io.Discard)
				cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
				if err != nil {
					t.Fatal(err)
				}
				cmd.SilenceErrors, cmd.SilenceUsage = true, true
				cmd.SetArgs([]string{testFixtureArg})
				err = cmd.ExecuteContext(ctx)
				if !errors.Is(err, context.Canceled) {
					t.Errorf("canceled command returned %v", err)
				}
				if tc.before && requests.Load() != 0 {
					t.Errorf("canceled command made %d requests", requests.Load())
				}
				if writes.Load() != tc.allowedWrites {
					t.Errorf("command made %d writes, want only %d before cancellation", writes.Load(), tc.allowedWrites)
				}
				if (tc.retry || tc.afterApproval) && time.Since(started) > time.Second {
					t.Errorf("cancellation waited %s instead of interrupting the delay", time.Since(started))
				}
				synctest.Wait()
			})
		})
	}
}
