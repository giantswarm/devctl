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

type bodyTransport func(*http.Request) (*http.Response, error)

func (f bodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// stalledBody sends part of a 200 answer, then waits for the request context
// to end. cancel, when set, cancels the caller exactly when the body stalls.
type stalledBody struct {
	ctx    context.Context
	prefix io.Reader
	cancel context.CancelFunc
	closes int
}

func (b *stalledBody) Read(p []byte) (int, error) {
	if n, err := b.prefix.Read(p); n != 0 || err != io.EOF {
		return n, err
	}
	if b.cancel != nil {
		b.cancel()
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stalledBody) Close() error {
	b.closes++
	return nil
}

type countedBody struct {
	io.Reader
	closes int
}

func (b *countedBody) Close() error {
	b.closes++
	return nil
}

func TestRetryingInterruptedResponseBody(t *testing.T) {
	testCases := []struct {
		name           string
		cancelCaller   bool
		attemptTimeout time.Duration
		wantError      error
		wantCalls      int
	}{
		{
			name:           "case 0: attempt timeout discards the partial answer and retries",
			attemptTimeout: 20 * time.Millisecond,
			wantCalls:      2,
		},
		{
			name:           "case 1: caller cancellation stops the body read without a retry",
			cancelCaller:   true,
			attemptTimeout: time.Minute,
			wantError:      context.Canceled,
			wantCalls:      1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Bound a regression that loses the per-attempt context as well.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			first := &stalledBody{prefix: strings.NewReader("incomplete answer")}
			if tc.cancelCaller {
				first.cancel = cancel
			}
			second := &countedBody{Reader: strings.NewReader("complete answer")}
			calls := 0
			var warnings []string
			retrying := &Retrying{
				Clock:          NewVirtualClock(time.Now()),
				Attempts:       2,
				AttemptTimeout: tc.attemptTimeout,
				Warn:           func(message string) { warnings = append(warnings, message) },
				Base: bodyTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					var body io.ReadCloser = second
					if calls == 1 {
						first.ctx = req.Context()
						body = first
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Status:     "200 OK",
						Header:     make(http.Header),
						Body:       body,
					}, nil
				}),
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.invalid/read", nil)
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: retrying}
			resp, err := client.Do(req)
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("want error %v, got %v", tc.wantError, err)
			}
			if calls != tc.wantCalls || first.closes != 1 {
				t.Fatalf("want %d requests and the stalled body closed once, got %d requests and %d closes", tc.wantCalls, calls, first.closes)
			}
			if tc.wantError != nil {
				if resp != nil || len(warnings) != 0 || second.closes != 0 {
					t.Fatalf("caller cancellation returned a response or retried: response=%v warnings=%d replacement closes=%d", resp != nil, len(warnings), second.closes)
				}
				return
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil || string(body) != "complete answer" {
				t.Fatalf("want only the complete replacement body, got %q, %v", body, err)
			}
			if second.closes != 1 || len(warnings) != 1 || !strings.Contains(warnings[0], "reading the 200 OK answer: context deadline exceeded") {
				t.Fatalf("want the replacement closed once and one body-timeout warning, got %d closes and %v", second.closes, warnings)
			}
		})
	}
}
