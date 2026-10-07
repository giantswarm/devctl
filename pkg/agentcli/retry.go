package agentcli

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// The retry budget of one read at scale 1: RetryAttempts tries, the pause
// before the first retry RetryBackoff, doubling up to RetryBackoffCeiling,
// about three minutes of pauses in all.
const (
	RetryAttempts       = 8
	RetryBackoff        = 2 * time.Second
	RetryBackoffCeiling = 60 * time.Second
	// RetryAttemptTimeout bounds one try, the answer's body included; it is
	// a network timeout and not scaled.
	RetryAttemptTimeout = 60 * time.Second
)

// Retrying is the http.RoundTripper under the API clients of a wait: a read
// that GitHub or CircleCI did not answer -- a reset connection, an EOF, a
// timeout, a 5xx -- is sent again after a backoff instead of ending the
// wait, and a read refused for a rate limit is sent again once the limit
// resets (see [rateLimited]). Every poll reads the same state again, so one
// such failure is not an outcome; one that persists through RetryAttempts
// tries in a row is, and the error then names the request, the count and the
// last failure. The caller's context bounds the retries: the wait's own
// deadline ends them, and a rate limit that resets only after it ends the
// wait at once with a [*RateLimitedError].
//
// Only GET and HEAD are retried, and a GET's body is read within its try, so
// a connection that breaks in the middle of an answer is retried too. Other
// methods pass through untouched: a write is not repeated on a guess.
type Retrying struct {
	// Base sends the requests; nil means http.DefaultTransport.
	Base http.RoundTripper
	// Clock scales the pauses; zero means the wall clock at scale 1.
	Clock Clock
	// Progress receives one line per retry; nil is silent.
	Progress *Progress
	// Warn receives one line per retried failure, with its time, for the
	// document's warnings; nil drops them.
	Warn func(message string)
	// Attempts, Backoff, BackoffCeiling and AttemptTimeout default to the
	// Retry constants.
	Attempts       int
	Backoff        time.Duration
	BackoffCeiling time.Duration
	AttemptTimeout time.Duration

	mu sync.Mutex
}

// RoundTrip implements http.RoundTripper.
func (r *Retrying) RoundTrip(req *http.Request) (*http.Response, error) {
	base := r.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return base.RoundTrip(req)
	}
	attempts := positive(r.Attempts, RetryAttempts)
	pause := positive(r.Backoff, RetryBackoff)
	ceiling := positive(r.BackoffCeiling, RetryBackoffCeiling)
	ctx := req.Context()
	for attempt := 1; ; attempt++ {
		resp, failed, err := r.try(base, req)
		if failed == nil {
			return resp, err
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w; the last try failed: %s", ctx.Err(), failed.reason)
		}
		if deadline, ok := r.deadline(ctx); ok && failed.until.After(deadline) {
			return nil, &RateLimitedError{Limit: failed.reason, Deadline: deadline}
		}
		if attempt == attempts {
			return nil, &RetriesExhaustedError{Attempts: attempts, Last: failed.reason}
		}
		wait := pause
		if !failed.until.IsZero() {
			wait = max(pause, failed.until.Sub(r.Clock.Now()).Round(time.Second))
		}
		r.retrying(req, failed.reason, wait, attempt+1, attempts)
		if err := r.Clock.Sleep(ctx, wait); err != nil {
			return nil, fmt.Errorf("%w; the last try failed: %s", err, failed.reason)
		}
		pause = min(2*pause, ceiling)
	}
}

// failure is a try worth another: what went wrong and, for a rate limit,
// when the answer said the read may be sent again.
type failure struct {
	reason string
	// until is zero when the backoff alone decides the pause.
	until time.Time
}

// deadline is the caller's deadline on the unscaled clock, where the
// answers' reset times are.
func (r *Retrying) deadline(ctx context.Context) (time.Time, bool) {
	d, ok := ctx.Deadline()
	if !ok {
		return time.Time{}, false
	}
	return r.Clock.Now().Add(time.Duration(float64(time.Until(d)) / r.Clock.Scale())), true
}

// try sends req once. A failure worth another try is returned with no
// response; otherwise the answer or the error is the outcome.
func (r *Retrying) try(base http.RoundTripper, req *http.Request) (*http.Response, *failure, error) {
	ctx, cancel := context.WithTimeout(req.Context(), positive(r.AttemptTimeout, RetryAttemptTimeout))
	defer cancel()
	resp, err := base.RoundTrip(req.Clone(ctx))
	if err != nil {
		if req.Context().Err() != nil || !transient(err) {
			return nil, nil, err
		}
		return nil, &failure{reason: err.Error()}, nil
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		if req.Context().Err() != nil || !transient(err) {
			return nil, nil, err
		}
		return nil, &failure{reason: fmt.Sprintf("reading the %s answer: %v", resp.Status, err)}, nil
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, &failure{reason: resp.Status}, nil
	}
	if limit := rateLimited(resp, body, r.Clock.Now()); limit != nil {
		return nil, limit, nil
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp, nil, nil
}

func (r *Retrying) retrying(req *http.Request, failure string, pause time.Duration, next, attempts int) {
	message := fmt.Sprintf("%s %s %s: %s; retried in %s (try %d of %d)",
		r.Clock.Now().UTC().Format(time.RFC3339), req.Method, req.URL.Redacted(), failure, pause, next, attempts)
	r.Progress.Printf("%s", message)
	if r.Warn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Warn(message)
}

// RetriesExhaustedError is a read that failed on every try. The HTTP client
// wraps it with the method and URL.
type RetriesExhaustedError struct {
	Attempts int
	// Last is the last failure: the transport error, the 5xx status or the
	// rate limit.
	Last string
}

func (e *RetriesExhaustedError) Error() string {
	return fmt.Sprintf("failed %d times in a row: %s", e.Attempts, e.Last)
}

// transient says whether a transport error is worth another try: every
// failure of the network is, a certificate the client refuses and a host
// that does not exist are not.
func transient(err error) bool {
	var (
		certificate  *tls.CertificateVerificationError
		unknownCA    x509.UnknownAuthorityError
		hostname     x509.HostnameError
		invalidCert  x509.CertificateInvalidError
		recordHeader tls.RecordHeaderError
		dns          *net.DNSError
	)
	switch {
	case errors.As(err, &certificate), errors.As(err, &unknownCA), errors.As(err, &hostname),
		errors.As(err, &invalidCert), errors.As(err, &recordHeader):
		return false
	case errors.As(err, &dns):
		return !dns.IsNotFound
	}
	return true
}

func positive[T int | time.Duration](v, fallback T) T {
	if v > 0 {
		return v
	}
	return fallback
}
