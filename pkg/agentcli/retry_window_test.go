package agentcli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type retryWindowTransport func(*http.Request) (*http.Response, error)

func (f retryWindowTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRetryingTimeoutWindow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures int
		elapsed  time.Duration
	}{
		{name: "case 0: eight timeouts then success", failures: 8, elapsed: 4*time.Minute + 10*time.Second},
		{name: "case 1: persistent timeouts", failures: 15, elapsed: 10*time.Minute + 17*time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := time.Date(2026, 10, 8, 12, 33, 0, 0, time.UTC)
			clock := NewVirtualClock(started)
			calls := 0
			retrying := &Retrying{Clock: clock, Base: retryWindowTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls <= tc.failures {
					if err := clock.Sleep(req.Context(), time.Second); err != nil {
						return nil, err
					}
					return nil, context.DeadlineExceeded
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("green")), Header: http.Header{}}, nil
			})}
			got, err := get(t, context.Background(), &http.Client{Transport: retrying}, http.MethodGet, "https://api.example/repos/o/r/commits/abc123/status")
			if elapsed := clock.Now().Sub(started); elapsed != tc.elapsed {
				t.Fatalf("retry window: got %s, want %s", elapsed, tc.elapsed)
			}
			if tc.failures == 8 {
				if err != nil || got != "200 green" || calls != 9 {
					t.Fatalf("want recovery on the ninth read, got %q, %v after %d reads", got, err, calls)
				}
				return
			}
			var exhausted *RetriesExhaustedError
			if !errors.As(err, &exhausted) || exhausted.Attempts != 15 || exhausted.Elapsed != tc.elapsed || calls != 15 {
				t.Fatalf("want a bounded 15-read failure with its elapsed time, got %v after %d reads", err, calls)
			}
			envelope := NewEnvelope("pr merge", started)
			envelope.Finish(clock.Now(), VerdictGreen, err)
			if envelope.ExitCode != ExitTimeout || envelope.Verdict != VerdictTransient || Exit(envelope.Err()) != ExitTimeout {
				t.Fatalf("want a retryable transient outcome, got %+v", envelope)
			}
			for _, part := range []string{"commits/abc123/status", "15 times", "context deadline exceeded", "transient API or network failure after 10m17s"} {
				if !strings.Contains(envelope.Reason, part) {
					t.Errorf("reason %q does not name %q", envelope.Reason, part)
				}
			}
		})
	}
}
