package github

import (
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

func get(t *testing.T, url string, header map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(body)
}

func TestSequenceAndRateLimitHeaders(t *testing.T) {
	s, err := Start(sequence.Routes{
		"GET /repos/o/r/commits/abc/check-runs": {
			{Body: map[string]any{"total_count": 1, "check_runs": []any{map[string]any{"status": "in_progress"}}}},
			{Body: map[string]any{"total_count": 1, "check_runs": []any{map[string]any{"status": "completed"}}}, Headers: map[string]string{"X-RateLimit-Remaining": "3"}},
		},
	})
	require.NoError(t, err)
	defer s.Close()

	resp, body := get(t, s.URL+"/repos/o/r/commits/abc/check-runs?per_page=100", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "in_progress")
	assert.Equal(t, "4999", resp.Header.Get("X-RateLimit-Remaining"))
	assert.Equal(t, "5000", resp.Header.Get("X-RateLimit-Limit"))
	reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
	require.NoError(t, err)
	assert.Greater(t, reset, time.Now().Unix())

	resp, body = get(t, s.URL+"/repos/o/r/commits/abc/check-runs", nil)
	assert.Contains(t, body, "completed")
	assert.Equal(t, "3", resp.Header.Get("X-RateLimit-Remaining"), "the fixture's header wins")

	_, body = get(t, s.URL+"/repos/o/r/commits/abc/check-runs", nil)
	assert.Contains(t, body, "completed", "the last response repeats")
	assert.Len(t, s.Requests(), 3)
}

func TestETagAndNotModified(t *testing.T) {
	pending := map[string]any{"state": "pending"}
	s, err := Start(sequence.Routes{
		"GET /repos/o/r/commits/abc/status": {
			{Body: pending},
			{Body: pending},
			{Body: map[string]any{"state": "success"}},
		},
	})
	require.NoError(t, err)
	defer s.Close()

	first, _ := get(t, s.URL+"/repos/o/r/commits/abc/status", nil)
	etag := first.Header.Get("ETag")
	require.NotEmpty(t, etag)

	second, body := get(t, s.URL+"/repos/o/r/commits/abc/status", map[string]string{"If-None-Match": etag})
	assert.Equal(t, http.StatusNotModified, second.StatusCode, "the same body under the client's ETag is 304")
	assert.Empty(t, body)
	assert.Equal(t, etag, second.Header.Get("ETag"))
	assert.Equal(t, "4999", second.Header.Get("X-RateLimit-Remaining"), "a 304 carries the rate-limit headers")

	third, body := get(t, s.URL+"/repos/o/r/commits/abc/status", map[string]string{"If-None-Match": etag})
	assert.Equal(t, http.StatusOK, third.StatusCode, "a changed body is sent although the request was conditional")
	assert.Contains(t, body, "success")
	assert.NotEqual(t, etag, third.Header.Get("ETag"))
}

func TestFixtureETagIsKept(t *testing.T) {
	s, err := Start(sequence.Routes{
		"GET /repos/o/r/pulls/1": {{Headers: map[string]string{"ETag": `W/"fixed"`}, Body: map[string]any{"number": 1}}},
	})
	require.NoError(t, err)
	defer s.Close()

	resp, _ := get(t, s.URL+"/repos/o/r/pulls/1", nil)
	assert.Equal(t, `W/"fixed"`, resp.Header.Get("ETag"))
	resp, _ = get(t, s.URL+"/repos/o/r/pulls/1", map[string]string{"If-None-Match": `W/"fixed"`})
	assert.Equal(t, http.StatusNotModified, resp.StatusCode)
}

func TestUnscriptedRouteIsNotFound(t *testing.T) {
	s, err := Start(sequence.Routes{})
	require.NoError(t, err)
	defer s.Close()

	resp, body := get(t, s.URL+"/repos/o/r/pulls/7", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.JSONEq(t, `{"message":"Not Found","documentation_url":"https://docs.github.com/rest","status":"404"}`, body)
	assert.Empty(t, resp.Header.Get("ETag"), "an error carries no ETag")
	assert.Equal(t, "5000", resp.Header.Get("X-RateLimit-Limit"))
}

func TestErrorResponsesAreNeverConditional(t *testing.T) {
	s, err := Start(sequence.Routes{
		"GET /repos/o/r/branches/main/protection": {{Status: http.StatusNotFound, Body: map[string]any{"message": "Branch not protected"}}},
	})
	require.NoError(t, err)
	defer s.Close()

	resp, _ := get(t, s.URL+"/repos/o/r/branches/main/protection", map[string]string{"If-None-Match": ETag([]byte(`{"message":"Branch not protected"}`))})
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
