package agentcli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A rate limit beyond a virtual deadline is still refused at once; one inside
// the budget is slept through and retried, including under a nested timeout.
func TestRetryingVirtualRateLimitDeadline(t *testing.T) {
	cases := []struct {
		name    string
		timeout time.Duration
		parent  time.Duration
		refused bool
	}{
		{name: "case 0 reset inside budget", timeout: 2 * time.Minute},
		{name: "case 1 reset beyond budget", timeout: 30 * time.Second, refused: true},
		{name: "case 2 earlier parent", timeout: 2 * time.Minute, parent: 30 * time.Second, refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			clock := NewVirtualClock(start)
			ctx := context.Background()
			if tc.parent > 0 {
				var cancel context.CancelFunc
				ctx, cancel = clock.Timeout(ctx, tc.parent)
				defer cancel()
			}
			ctx, cancel := clock.Timeout(ctx, tc.timeout)
			defer cancel()
			s := &script{answers: []answer{spent(start.Add(time.Minute)), ok("green")}}
			server := httptest.NewServer(s)
			defer server.Close()
			client := &http.Client{Transport: &Retrying{Clock: clock}}

			got, err := get(t, ctx, client, http.MethodGet, server.URL)
			if tc.refused {
				var limit *RateLimitedError
				if !errors.As(err, &limit) {
					t.Fatalf("want a rate-limit deadline refusal, got %v", err)
				}
				if s.count() != 1 || clock.Now() != start || limit.Deadline != start.Add(30*time.Second) {
					t.Fatalf("refusal: %d requests, now %s, deadline %s", s.count(), clock.Now(), limit.Deadline)
				}
				return
			}
			if err != nil || got != "200 green" || s.count() != 2 {
				t.Fatalf("retry: answer %q, error %v, requests %d", got, err, s.count())
			}
			if clock.Now() != start.Add(time.Minute+time.Second) {
				t.Fatalf("the reset did not advance virtual time: %s", clock.Now())
			}
		})
	}
}

func TestVirtualClockHonorsRealParent(t *testing.T) {
	clock := NewVirtualClock(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := clock.Timeout(parent, time.Hour)
	defer cancel()
	cancelParent()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("parent cancellation: %v", ctx.Err())
	}
	parent, cancelParent = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelParent()
	ctx, cancel = clock.Timeout(parent, time.Hour)
	defer cancel()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("parent deadline: %v", ctx.Err())
	}
	if at, ok := clock.deadline(ctx); !ok || at.After(clock.Now()) {
		t.Fatalf("expired parent deadline is not visible: %s, %t", at, ok)
	}
}
