package agentcli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// limit answers the way api.github.com does when a budget is refused: the
// status, the headers and GitHub's error body.
func limit(code int, headers map[string]string, documentationURL string) answer {
	return func(w http.ResponseWriter) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, `{"message":"API rate limit exceeded","documentation_url":"`+documentationURL+`"}`)
	}
}

// spent is the primary limit's answer, the budget resetting at reset.
func spent(reset time.Time) answer {
	return limit(http.StatusForbidden, map[string]string{
		"X-RateLimit-Limit":     "5000",
		"X-RateLimit-Remaining": "0",
		"X-RateLimit-Resource":  "core",
		"X-RateLimit-Reset":     strconv.FormatInt(reset.Unix(), 10),
	}, "https://docs.github.com/rest/overview/resources-in-the-rest-api#rate-limiting")
}

func TestRetryingWaitsForTheRateLimit(t *testing.T) {
	reset := time.Now().Add(3 * time.Second)
	cases := []struct {
		name   string
		answer answer
		// failure and pause are what the warning must name.
		failure string
		pause   string
	}{
		{
			name:    "the primary limit is spent",
			answer:  spent(reset),
			failure: "403 Forbidden: the core rate limit (5000) is spent until " + stamp(time.Unix(reset.Unix(), 0)),
		},
		{
			name:    "Retry-After",
			answer:  limit(http.StatusTooManyRequests, map[string]string{"Retry-After": "5"}, ""),
			failure: "429 Too Many Requests: rate limited until ",
			pause:   "retried in 5s ",
		},
		{
			name:    "a secondary limit without a time",
			answer:  limit(http.StatusForbidden, nil, "https://docs.github.com/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits"),
			failure: "403 Forbidden: a secondary rate limit",
			pause:   "retried in 1m0s ",
		},
		{
			name:    "a 429 without a time",
			answer:  status(http.StatusTooManyRequests),
			failure: "429 Too Many Requests",
			pause:   "retried in 2s ",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &script{answers: []answer{tc.answer, ok("green")}}
			server := httptest.NewServer(s)
			defer server.Close()
			var warnings []string
			client := &http.Client{Transport: fastRetrying(&warnings)}

			got, err := get(t, context.Background(), client, http.MethodGet, server.URL+"/repos/o/r/pulls/1")
			if err != nil {
				t.Fatalf("want the answer after the limit, got %v", err)
			}
			if got != "200 green" || s.count() != 2 {
				t.Errorf("want 200 green after 2 requests, got %q after %d", got, s.count())
			}
			if len(warnings) != 1 {
				t.Fatalf("warnings: want 1, got %q", warnings)
			}
			for _, part := range []string{"GET " + server.URL + "/repos/o/r/pulls/1: ", tc.failure, tc.pause, "(try 2 of 8)"} {
				if !strings.Contains(warnings[0], part) {
					t.Errorf("warning %q does not name %q", warnings[0], part)
				}
			}
		})
	}
}

func TestRetryingSleepsUntilTheReset(t *testing.T) {
	// At scale 1 the pause is the time to the reset and a second, not the
	// backoff's two seconds.
	s := &script{answers: []answer{spent(time.Now().Add(2 * time.Second)), ok("green")}}
	server := httptest.NewServer(s)
	defer server.Close()
	var warnings []string
	client := &http.Client{Transport: &Retrying{Warn: func(m string) { warnings = append(warnings, m) }}}

	started := time.Now()
	got, err := get(t, context.Background(), client, http.MethodGet, server.URL+"/x")
	if err != nil || got != "200 green" {
		t.Fatalf("want 200 green after the reset, got %q, %v", got, err)
	}
	if elapsed := time.Since(started); elapsed < 2*time.Second || elapsed > 5*time.Second {
		t.Errorf("want the read sent again a second after the reset, 2-4 s from now, got %s", elapsed)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings: want 1, got %q", warnings)
	}
}

func TestRetryingEndsAtALimitPastTheDeadline(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour)
	s := &script{answers: []answer{spent(reset), ok("unreached")}}
	server := httptest.NewServer(s)
	defer server.Close()
	var warnings []string
	client := &http.Client{Transport: fastRetrying(&warnings)}
	// A 30-minute wait at scale 0.001.
	ctx, cancel := context.WithTimeout(context.Background(), 1800*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := get(t, ctx, client, http.MethodGet, server.URL+"/repos/o/r/pulls/1")
	var limited *RateLimitedError
	if !errors.As(err, &limited) {
		t.Fatalf("want a RateLimitedError, got %v", err)
	}
	if code, verdict := Outcome(err); code != ExitTimeout || verdict != VerdictTimeout {
		t.Errorf("outcome: want %d %s, got %d %s", ExitTimeout, VerdictTimeout, code, verdict)
	}
	for _, part := range []string{"/repos/o/r/pulls/1", "the core rate limit (5000) is spent until " + stamp(time.Unix(reset.Unix(), 0)), "after the wait's deadline at "} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not name %q", err, part)
		}
	}
	// The deadline is named on the unscaled clock, about 30 minutes ahead.
	if ahead := limited.Deadline.Sub(started); ahead < 29*time.Minute || ahead > 31*time.Minute {
		t.Errorf("deadline: want about 30m ahead, got %s", ahead)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Errorf("want the wait ended at once, took %s", elapsed)
	}
	if s.count() != 1 || len(warnings) != 0 {
		t.Errorf("want one request and no retry, got %d and %q", s.count(), warnings)
	}
}

func TestRetryingTakesAPermissionAsTheAnswer(t *testing.T) {
	budgetLeft := map[string]string{
		"X-RateLimit-Limit":     "5000",
		"X-RateLimit-Remaining": "4211",
		"X-RateLimit-Reset":     strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10),
	}
	cases := []struct {
		name   string
		method string
		answer answer
	}{
		{name: "a 403 with budget left", method: http.MethodGet, answer: limit(http.StatusForbidden, budgetLeft, "https://docs.github.com/rest/pulls/pulls#get-a-pull-request")},
		{name: "a write refused for the limit", method: http.MethodPut, answer: spent(time.Now().Add(time.Second))},
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
			if !strings.HasPrefix(got, "403 ") || s.count() != 1 || len(warnings) != 0 {
				t.Errorf("want the 403 after one request and no warning, got %q after %d, warnings %q", got, s.count(), warnings)
			}
		})
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 23, 16, 48, 0, 0, time.UTC)
	cases := []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{value: "30", want: 30 * time.Second, ok: true},
		{value: "0", want: 0, ok: true},
		{value: "Wed, 23 Sep 2026 16:49:30 GMT", want: 90 * time.Second, ok: true},
		{value: "Wed, 23 Sep 2026 16:47:00 GMT", want: 0, ok: true},
		{value: "", ok: false},
		{value: "soon", ok: false},
		{value: "-5", ok: false},
	}
	for _, tc := range cases {
		got, ok := retryAfter(tc.value, now)
		if got != tc.want || ok != tc.ok {
			t.Errorf("retryAfter(%q): want %s %t, got %s %t", tc.value, tc.want, tc.ok, got, ok)
		}
	}
}
