package agentcli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimitSecondaryPause is how long a read refused for a secondary rate
// limit waits when the answer names no time: GitHub's advice is at least a
// minute.
const RateLimitSecondaryPause = time.Minute

// rateLimited is the failure of an answer that refused the read for a rate
// limit, nil for any other answer. A 403 or 429 is a rate limit when it
// carries Retry-After (a secondary limit, CircleCI's 429: sent again that
// much later), X-RateLimit-Remaining 0 (the primary limit is spent: sent
// again a second after X-RateLimit-Reset), or GitHub's documentation link of
// the secondary limits (sent again after [RateLimitSecondaryPause]). A 429
// without any of them is retried on the backoff; any other 403 is an answer,
// a permission the token lacks.
func rateLimited(resp *http.Response, body []byte, now time.Time) *failure {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return nil
	}
	h := resp.Header
	if after, ok := retryAfter(h.Get("Retry-After"), now); ok {
		until := now.Add(after)
		return &failure{reason: fmt.Sprintf("%s: rate limited until %s (Retry-After %s)", resp.Status, stamp(until), h.Get("Retry-After")), until: until}
	}
	if h.Get("X-RateLimit-Remaining") == "0" {
		limit := "the rate limit"
		if resource := h.Get("X-RateLimit-Resource"); resource != "" {
			limit = "the " + resource + " rate limit"
		}
		if n := h.Get("X-RateLimit-Limit"); n != "" {
			limit += " (" + n + ")"
		}
		reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
		if err != nil {
			return &failure{reason: fmt.Sprintf("%s: %s is spent, with no reset time", resp.Status, limit)}
		}
		at := time.Unix(reset, 0)
		return &failure{reason: fmt.Sprintf("%s: %s is spent until %s", resp.Status, limit, stamp(at)), until: at.Add(time.Second)}
	}
	if secondary(body) {
		until := now.Add(RateLimitSecondaryPause)
		return &failure{reason: fmt.Sprintf("%s: a secondary rate limit, retried after %s", resp.Status, RateLimitSecondaryPause), until: until}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return &failure{reason: resp.Status}
	}
	return nil
}

// retryAfter reads Retry-After: a number of seconds or an HTTP date.
func retryAfter(value string, now time.Time) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if at, err := http.ParseTime(value); err == nil {
		return max(at.Sub(now), 0), true
	}
	return 0, false
}

// secondary says whether a GitHub error body links the secondary rate
// limits, the way go-github recognises them.
func secondary(body []byte) bool {
	var answer struct {
		DocumentationURL string `json:"documentation_url"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return false
	}
	return strings.HasSuffix(answer.DocumentationURL, "#abuse-rate-limits") ||
		strings.HasSuffix(answer.DocumentationURL, "secondary-rate-limits")
}

func stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// RateLimitedError is a read refused for a rate limit that resets only after
// the caller's deadline: sleeping to the deadline would not reach the reset,
// so the wait ends at once, a timeout (exit 2) whose reason names the reset.
// The HTTP client wraps it with the method and URL.
type RateLimitedError struct {
	// Limit is the answer, the limit it named and when it resets.
	Limit string
	// Deadline is the caller's, on the unscaled clock.
	Deadline time.Time
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("%s, after the wait's deadline at %s; run the wait again after the reset", e.Limit, stamp(e.Deadline))
}

// ExitCode implements [ExitCoder].
func (e *RateLimitedError) ExitCode() int { return ExitTimeout }

// ExitVerdict implements [ExitCoder].
func (e *RateLimitedError) ExitVerdict() Verdict { return VerdictTimeout }
