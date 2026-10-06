package githubclient

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// RateLimitSpent says, for an answer whose rate limit is spent
// (X-RateLimit-Remaining 0), which limit and when it resets: "the graphql
// rate limit (5000) is spent until 2026-10-06T05:40:00Z". Any other answer is
// "". GraphQL refuses a caller whose limit is spent while REST, a limit of its
// own, still answers, so an error from a GraphQL call carries this to tell the
// refusal from a failure.
func RateLimitSpent(h http.Header) string {
	if h.Get("X-RateLimit-Remaining") != "0" {
		return ""
	}
	limit := "the rate limit"
	if resource := h.Get("X-RateLimit-Resource"); resource != "" {
		limit = "the " + resource + " rate limit"
	}
	if n := h.Get("X-RateLimit-Limit"); n != "" {
		limit += " (" + n + ")"
	}
	reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return limit + " is spent, with no reset time"
	}
	return fmt.Sprintf("%s is spent until %s", limit, time.Unix(reset, 0).UTC().Format(time.RFC3339))
}

// GraphQLRefusal is the error text of a GraphQL answer that refused the call:
// the answer, followed by the spent rate limit when that is the reason.
func GraphQLRefusal(h http.Header, answer string) string {
	if spent := RateLimitSpent(h); spent != "" {
		return answer + "; " + spent
	}
	return answer
}
