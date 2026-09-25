package githubclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-github/v92/github"
	"github.com/sirupsen/logrus"
)

// tokenServer accepts one token at a time, the way GitHub does once a user
// token expired or was superseded by a refresh: every other token gets 401
// Bad credentials. It records the token and body of every request.
type tokenServer struct {
	*httptest.Server
	mu     sync.Mutex
	valid  string
	tokens []string
	bodies []string
}

func newTokenServer(t *testing.T, valid string) *tokenServer {
	s := &tokenServer{valid: valid}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		s.tokens = append(s.tokens, token)
		s.bodies = append(s.bodies, string(body))
		valid := s.valid
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if token != valid {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials","status":"401"}`))
			return
		}
		_, _ = w.Write([]byte(`{"number":1,"state":"open"}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *tokenServer) expire(next string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.valid = next
}

func (s *tokenServer) seen() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tokens...), append([]string(nil), s.bodies...)
}

// renewer hands out fresh tokens and records what it was asked to replace.
type renewer struct {
	mu       sync.Mutex
	next     string
	err      error
	rejected []string
}

func (r *renewer) renew(_ context.Context, rejected string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rejected = append(r.rejected, rejected)
	return r.next, r.err
}

func newRenewingClient(t *testing.T, server *tokenServer, token string, renew func(context.Context, string) (string, error)) *github.Client {
	t.Helper()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	c, _, err := NewConditional(Config{Logger: logger, AccessToken: token, BaseURL: server.URL, Renew: renew})
	if err != nil {
		t.Fatal(err)
	}
	return c.GitHub()
}

// The token expires between two reads of a wait: the refused read is renewed
// and sent again, and the reads after it carry the renewed token without
// asking again.
func TestRenewOn401MidRun(t *testing.T) {
	server := newTokenServer(t, "ghu_first")
	r := &renewer{next: "ghu_second"}
	gh := newRenewingClient(t, server, "ghu_first", r.renew)
	ctx := context.Background()

	if _, _, err := gh.PullRequests.Get(ctx, "o", "r", 1); err != nil {
		t.Fatalf("read before the expiry: %v", err)
	}
	server.expire("ghu_second")
	if _, _, err := gh.PullRequests.Get(ctx, "o", "r", 1); err != nil {
		t.Fatalf("read across the expiry: %v", err)
	}
	if _, _, err := gh.PullRequests.Get(ctx, "o", "r", 1); err != nil {
		t.Fatalf("read after the renewal: %v", err)
	}

	tokens, _ := server.seen()
	want := []string{"ghu_first", "ghu_first", "ghu_second", "ghu_second"}
	if strings.Join(tokens, ",") != strings.Join(want, ",") {
		t.Fatalf("tokens sent = %v, want %v", tokens, want)
	}
	if strings.Join(r.rejected, ",") != "ghu_first" {
		t.Fatalf("renew asked for %v, want once for ghu_first", r.rejected)
	}
}

// A refused write is sent again with its body.
func TestRenewResendsTheBody(t *testing.T) {
	server := newTokenServer(t, "ghu_second")
	r := &renewer{next: "ghu_second"}
	gh := newRenewingClient(t, server, "ghu_first", r.renew)

	_, _, err := gh.PullRequests.Merge(context.Background(), "o", "r", 1, "", &github.PullRequestOptions{MergeMethod: "squash"})
	if err != nil {
		t.Fatalf("merge across the expiry: %v", err)
	}
	tokens, bodies := server.seen()
	if len(bodies) != 2 || bodies[0] == "" || bodies[0] != bodies[1] || tokens[1] != "ghu_second" {
		t.Fatalf("tokens %v bodies %q: want the same body sent twice, the second time with the renewed token", tokens, bodies)
	}
}

// GitHub refusing the renewed token too is the answer: one renewal, then the
// 401 goes to the caller.
func TestRenewedTokenRefusedIsTheAnswer(t *testing.T) {
	server := newTokenServer(t, "ghu_other")
	r := &renewer{next: "ghu_second"}
	gh := newRenewingClient(t, server, "ghu_first", r.renew)

	_, resp, err := gh.PullRequests.Get(context.Background(), "o", "r", 1)
	var ghErr *github.ErrorResponse
	if !errors.As(err, &ghErr) || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v, want GitHub's 401", err)
	}
	if len(r.rejected) != 1 {
		t.Fatalf("renew asked %d times, want 1", len(r.rejected))
	}
}

type renewRefused struct{}

func (renewRefused) Error() string { return "GitHub refused the refresh" }

// A renewal that fails is the request's error, as it is.
func TestFailedRenewalIsTheError(t *testing.T) {
	server := newTokenServer(t, "ghu_second")
	r := &renewer{err: renewRefused{}}
	gh := newRenewingClient(t, server, "ghu_first", r.renew)

	_, _, err := gh.PullRequests.Get(context.Background(), "o", "r", 1)
	var refused renewRefused
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want the renewal's error in the chain", err)
	}
}

// Requests refused for the same token at the same time renew it once.
func TestConcurrent401sRenewOnce(t *testing.T) {
	server := newTokenServer(t, "ghu_second")
	var calls atomic.Int32
	renew := func(context.Context, string) (string, error) {
		calls.Add(1)
		return "ghu_second", nil
	}
	gh := newRenewingClient(t, server, "ghu_first", renew)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			if _, _, err := gh.PullRequests.Get(context.Background(), "o", "r", 1); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("renew called %d times, want 1", calls.Load())
	}
}
