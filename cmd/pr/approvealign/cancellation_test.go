package approvealign

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

type cancellationTransport func(*http.Request) (*http.Response, error)

func (f cancellationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type cancellationKey struct{}

func TestCommandCancellationBoundary(t *testing.T) {
	for _, stage := range []string{"before start", "search", "empty search", "failed search", "pending checks", "after approval"} {
		t.Run(stage, func(t *testing.T) {
			t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-offline-token")
			t.Setenv("CI", "true")
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), cancellationKey{}, "fixture"))
			defer cancel()
			if stage == "before start" {
				cancel()
			}
			var requests, afterCancel, approvals, prReads atomic.Int32
			original := http.DefaultTransport
			http.DefaultTransport = cancellationTransport(func(req *http.Request) (*http.Response, error) {
				requests.Add(1)
				if ctx.Err() != nil {
					afterCancel.Add(1)
				}
				if req.Context().Value(cancellationKey{}) != "fixture" {
					t.Error("request lost the command context")
				}
				if err := req.Context().Err(); err != nil {
					return nil, err
				}
				code := http.StatusOK
				var body string
				switch req.Method + " " + req.URL.Path {
				case "GET /search/issues":
					if stage == "before start" || stage == "empty search" {
						body = `{"total_count":0,"items":[]}`
					} else {
						body = `{"total_count":1,"items":[{"number":1,"title":"` + alignTitle + `","html_url":"https://github.com/fixture/repo/pull/1"}]}`
					}
					if stage == "search" || stage == "empty search" || stage == "failed search" {
						cancel()
					}
					if stage == "failed search" {
						code, body = http.StatusServiceUnavailable, `{"message":"fixture unavailable"}`
					}
				case "GET /repos/fixture/repo/pulls/1":
					body = `{"title":"` + alignTitle + `","head":{"sha":"checked-head"}}`
					if prReads.Add(1) > 1 {
						// Bound the broken runner's polling to one retry.
						body = `{"merged":true}`
					}
				case "GET /repos/fixture/repo/commits/checked-head/status":
					body = `{"state":"pending","total_count":0,"statuses":[]}`
					if stage == "pending checks" {
						body = `{"state":"pending","total_count":1,"statuses":[{"context":"fixture","state":"pending"}]}`
					}
				case "GET /repos/fixture/repo/commits/checked-head/check-runs":
					body = `{"total_count":0,"check_runs":[]}`
					if stage == "pending checks" {
						cancel()
					}
				case "GET /repos/fixture/repo/pulls/1/reviews":
					body = `[]`
				case "POST /repos/fixture/repo/pulls/1/reviews":
					approvals.Add(1)
					body = `{"state":"APPROVED"}`
					if stage == "after approval" {
						cancel()
					}
				default:
					t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
					return nil, errors.New("unexpected fixture request")
				}
				return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = original })
			command, err := New(Config{Logger: logrus.New(), Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			command.SetArgs([]string{})
			start := time.Now()
			err = command.ExecuteContext(ctx)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("error = %v, want context cancellation", err)
			}
			if elapsed := time.Since(start); elapsed >= 2*time.Second {
				t.Errorf("cancellation took %s; lifecycle wait was not interrupted", elapsed)
			}
			wantApprovals := int32(0)
			if stage == "after approval" {
				wantApprovals = 1
			}
			if afterCancel.Load() != 0 || approvals.Load() != wantApprovals {
				t.Errorf("requests after cancellation = %d, approvals = %d; want 0, %d", afterCancel.Load(), approvals.Load(), wantApprovals)
			}
			if stage == "before start" && requests.Load() != 0 {
				t.Errorf("already cancelled command made %d requests", requests.Load())
			}
		})
	}
}
