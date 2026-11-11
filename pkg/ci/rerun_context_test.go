package ci

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

type cancelWaitTransport func(*http.Request) (*http.Response, error)

func (f cancelWaitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Caller cancellation and earlier caller deadlines must remain distinguishable
// from CircleCI failing to settle a cancel within the two-minute recovery bound.
func TestAwaitCanceledPreservesCallerContext(t *testing.T) {
	for _, tc := range []struct {
		name     string
		before   bool
		deadline bool
		answer   bool
	}{
		{name: "case 0: canceled before the read", before: true},
		{name: "case 1: canceled during the read"},
		{name: "case 2: canceled before the poll sleep", answer: true},
		{name: "case 3: earlier caller deadline", deadline: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			if tc.deadline {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Unix(1, 0))
				want = context.DeadlineExceeded
			}
			defer cancel()
			if tc.before {
				cancel()
			}
			client, err := circleciclient.New(circleciclient.Config{
				Token: "token_test",
				Transport: cancelWaitTransport(func(req *http.Request) (*http.Response, error) {
					cancel()
					if !tc.answer {
						return nil, req.Context().Err()
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"id":"w1","status":"running"}`)), Request: req}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			status, err := AwaitCanceled(ctx, client, "w1", agentcli.NewVirtualClock(now))
			if status != "" || !errors.Is(err, want) || agentcli.Exit(err) == agentcli.ExitTimeout {
				t.Fatalf("status=%q err=%v exit=%d; want the caller's %v, not a cancel-settle timeout", status, err, agentcli.Exit(err), want)
			}
		})
	}
}
