package agentcli

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// answer is one scripted reply of the test server.
type answer func(w http.ResponseWriter)

func status(code int) answer {
	return func(w http.ResponseWriter) { http.Error(w, http.StatusText(code), code) }
}

func ok(body string) answer {
	return func(w http.ResponseWriter) { _, _ = io.WriteString(w, body) }
}

// reset closes the connection with a TCP RST before any answer: the client
// reads "connection reset by peer".
func reset(w http.ResponseWriter) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		panic(err)
	}
	_ = conn.(*net.TCPConn).SetLinger(0)
	_ = conn.Close()
}

// truncated promises a body it never sends: the client reads the headers,
// then an unexpected EOF.
func truncated(w http.ResponseWriter) {
	conn, buf, err := w.(http.Hijacker).Hijack()
	if err != nil {
		panic(err)
	}
	_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\npartial")
	_ = buf.Flush()
	_ = conn.Close()
}

// script serves the answers in order, the last repeating, and counts the
// requests.
type script struct {
	mu      sync.Mutex
	answers []answer
	calls   int
}

func (s *script) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	a := s.answers[min(s.calls, len(s.answers)-1)]
	s.calls++
	s.mu.Unlock()
	a(w)
}

func (s *script) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// fastRetrying retries at a thousandth of the production pauses.
func fastRetrying(warnings *[]string) *Retrying {
	return &Retrying{
		Clock:          NewClock(0.001, nil),
		Warn:           func(m string) { *warnings = append(*warnings, m) },
		AttemptTimeout: 2 * time.Second,
	}
}

func get(t *testing.T, ctx context.Context, client *http.Client, method, url string) (string, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	return fmt.Sprintf("%d %s", resp.StatusCode, strings.TrimSpace(string(body))), nil
}

func TestRetryingRecovers(t *testing.T) {
	cases := []struct {
		name    string
		answers []answer
		failure string
	}{
		{name: "500 once", answers: []answer{status(500), ok("green")}, failure: "500 Internal Server Error"},
		{name: "502 twice", answers: []answer{status(502), status(502), ok("green")}, failure: "502 Bad Gateway"},
		{name: "connection reset", answers: []answer{reset, ok("green")}, failure: "connection reset by peer"},
		{name: "answer cut short", answers: []answer{truncated, ok("green")}, failure: "reading the 200 OK answer: unexpected EOF"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &script{answers: tc.answers}
			server := httptest.NewServer(s)
			defer server.Close()
			var warnings []string
			client := &http.Client{Transport: fastRetrying(&warnings)}

			got, err := get(t, context.Background(), client, http.MethodGet, server.URL+"/repos/o/r/pulls/1")
			if err != nil {
				t.Fatalf("want the answer after the retries, got %v", err)
			}
			if got != "200 green" {
				t.Errorf("answer: want %q, got %q", "200 green", got)
			}
			if s.count() != len(tc.answers) {
				t.Errorf("requests: want %d, got %d", len(tc.answers), s.count())
			}
			if len(warnings) != len(tc.answers)-1 {
				t.Fatalf("warnings: want %d, got %q", len(tc.answers)-1, warnings)
			}
			for i, w := range warnings {
				for _, part := range []string{"GET " + server.URL + "/repos/o/r/pulls/1: ", tc.failure, fmt.Sprintf("(try %d of %d)", i+2, RetryAttempts)} {
					if !strings.Contains(w, part) {
						t.Errorf("warning %d %q does not name %q", i, w, part)
					}
				}
				if _, err := time.Parse(time.RFC3339, strings.Fields(w)[0]); err != nil {
					t.Errorf("warning %d %q does not start with its time: %v", i, w, err)
				}
			}
		})
	}
}

func TestRetryingGivesUp(t *testing.T) {
	s := &script{answers: []answer{status(500)}}
	server := httptest.NewServer(s)
	defer server.Close()
	var warnings []string
	client := &http.Client{Transport: fastRetrying(&warnings)}

	_, err := get(t, context.Background(), client, http.MethodGet, server.URL+"/repos/o/r/pulls/1")
	want := fmt.Sprintf(`Get "%s/repos/o/r/pulls/1": failed %d times in a row: 500 Internal Server Error`, server.URL, RetryAttempts)
	if err == nil || err.Error() != want {
		t.Fatalf("error: want %q, got %v", want, err)
	}
	var exhausted *RetriesExhaustedError
	if !errors.As(err, &exhausted) || exhausted.Attempts != RetryAttempts {
		t.Errorf("want a RetriesExhaustedError of %d tries, got %#v", RetryAttempts, err)
	}
	if s.count() != RetryAttempts {
		t.Errorf("requests: want %d, got %d", RetryAttempts, s.count())
	}
	if len(warnings) != RetryAttempts-1 {
		t.Errorf("warnings: want %d, got %d", RetryAttempts-1, len(warnings))
	}
	// The pauses double from the backoff up to the ceiling.
	for i, pause := range []string{"2s", "4s", "8s", "16s", "32s", "1m0s", "1m0s"} {
		if !strings.Contains(warnings[i], "retried in "+pause+" ") {
			t.Errorf("warning %d: want a pause of %s, got %q", i, pause, warnings[i])
		}
	}
}

func TestRetryingPassesThrough(t *testing.T) {
	cases := []struct {
		name   string
		method string
		answer answer
		want   string
	}{
		{name: "a write is not repeated", method: http.MethodPut, answer: status(502), want: "502 Bad Gateway"},
		{name: "a 404 is an answer", method: http.MethodGet, answer: status(404), want: "404 Not Found"},
		{name: "a 403 is an answer", method: http.MethodGet, answer: status(403), want: "403 Forbidden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &script{answers: []answer{tc.answer, ok("unreached")}}
			server := httptest.NewServer(s)
			defer server.Close()
			var warnings []string
			client := &http.Client{Transport: fastRetrying(&warnings)}

			got, err := get(t, context.Background(), client, tc.method, server.URL+"/x")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || s.count() != 1 || len(warnings) != 0 {
				t.Errorf("want %q after one request and no warning, got %q after %d, warnings %q", tc.want, got, s.count(), warnings)
			}
		})
	}
}

func TestRetryingEndsWithTheCallersDeadline(t *testing.T) {
	s := &script{answers: []answer{status(503)}}
	server := httptest.NewServer(s)
	defer server.Close()
	var warnings []string
	// At scale 1 the first pause is two seconds, past the deadline.
	retrying := &Retrying{Warn: func(m string) { warnings = append(warnings, m) }}
	client := &http.Client{Transport: retrying}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := get(t, ctx, client, http.MethodGet, server.URL+"/x")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the deadline, got %v", err)
	}
	if !strings.Contains(err.Error(), "the last try failed: 503 Service Unavailable") {
		t.Errorf("the error does not name the last failure: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("the retry outlived the deadline: %s", elapsed)
	}
	if s.count() != 1 || len(warnings) != 1 {
		t.Errorf("want one request and one warning, got %d and %q", s.count(), warnings)
	}
}

func TestRetryingRetriesAHungTry(t *testing.T) {
	release := make(chan struct{})
	s := &script{answers: []answer{func(http.ResponseWriter) { <-release }, ok("green")}}
	server := httptest.NewServer(s)
	defer server.Close()
	// The hung handler returns before Close waits for it.
	defer close(release)
	var warnings []string
	retrying := fastRetrying(&warnings)
	retrying.AttemptTimeout = 50 * time.Millisecond
	client := &http.Client{Transport: retrying}

	got, err := get(t, context.Background(), client, http.MethodGet, server.URL+"/x")
	if err != nil || got != "200 green" {
		t.Fatalf("want the second try's answer, got %q, %v", got, err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "context deadline exceeded") {
		t.Errorf("want one warning naming the try's timeout, got %q", warnings)
	}
}

func TestTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "reset", err: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}, want: true},
		{name: "EOF", err: io.EOF, want: true},
		{name: "unexpected EOF", err: io.ErrUnexpectedEOF, want: true},
		{name: "timeout", err: context.DeadlineExceeded, want: true},
		{name: "DNS timeout", err: &net.DNSError{Err: "i/o timeout", Name: "api.github.com", IsTimeout: true}, want: true},
		{name: "unknown host", err: &net.DNSError{Err: "no such host", Name: "api.gthub.com", IsNotFound: true}, want: false},
		{name: "unknown authority", err: fmt.Errorf("tls: %w", x509.UnknownAuthorityError{}), want: false},
		{name: "wrong host in the certificate", err: x509.HostnameError{Host: "api.github.com"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := transient(tc.err); got != tc.want {
				t.Errorf("transient(%v): want %t, got %t", tc.err, tc.want, got)
			}
		})
	}
}
