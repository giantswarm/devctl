package githubclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func Test_RateLimitSpent(t *testing.T) {
	tests := []struct {
		name   string
		header map[string]string
		want   string
	}{
		{
			name:   "limit left",
			header: map[string]string{"X-RateLimit-Remaining": "12", "X-RateLimit-Reset": "1791265200"},
		},
		{
			name:   "no rate limit headers",
			header: map[string]string{},
		},
		{
			name:   "graphql spent",
			header: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Resource": "graphql", "X-RateLimit-Limit": "5000", "X-RateLimit-Reset": "1791265200"},
			want:   "the graphql rate limit (5000) is spent until 2026-10-06T05:40:00Z",
		},
		{
			name:   "spent without a reset",
			header: map[string]string{"X-RateLimit-Remaining": "0"},
			want:   "the rate limit is spent, with no reset time",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.header {
				h.Set(k, v)
			}
			if got := RateLimitSpent(h); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// A GraphQL rate-limit refusal names the limit and its reset, whether GitHub
// answers it with 403 or with 200 and a RATE_LIMITED error.
func Test_EnqueuePullRequest_rateLimited(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusOK} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Resource", "graphql")
			w.Header().Set("X-RateLimit-Limit", "5000")
			w.Header().Set("X-RateLimit-Reset", "1791265200")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"errors":[{"type":"RATE_LIMITED","message":"API rate limit already exceeded for user ID 1."}]}`)
		}))
		logger := logrus.New()
		logger.SetOutput(io.Discard)
		c, _, err := NewConditional(Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
		if err != nil {
			t.Fatal(err)
		}
		err = c.EnqueuePullRequest(context.Background(), "PR_1")
		server.Close()
		if err == nil || !strings.Contains(err.Error(), "the graphql rate limit (5000) is spent until 2026-10-06T05:40:00Z") {
			t.Errorf("%d: want the spent limit and its reset, got %v", status, err)
		}
	}
}
