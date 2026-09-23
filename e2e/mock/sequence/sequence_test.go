package sequence

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextAdvancesAndRepeatsTheLast(t *testing.T) {
	s, err := New(Routes{
		"GET /repos/o/r/pulls/1": {
			{Body: map[string]any{"state": "open"}},
			{Body: map[string]any{"state": "merged"}},
		},
	})
	require.NoError(t, err)

	states := []string{}
	for range 4 {
		resp, ok := s.Next(httptest.NewRequest(http.MethodGet, "/repos/o/r/pulls/1", nil))
		require.True(t, ok)
		states = append(states, resp.Body.(map[string]any)["state"].(string))
	}
	assert.Equal(t, []string{"open", "merged", "merged", "merged"}, states)
	assert.Len(t, s.Requests(), 4)
}

func TestNextCountsPerRoute(t *testing.T) {
	s, err := New(Routes{
		"GET /a": {{Body: "a1"}, {Body: "a2"}},
		"GET /b": {{Body: "b1"}, {Body: "b2"}},
	})
	require.NoError(t, err)

	a, _ := s.Next(httptest.NewRequest(http.MethodGet, "/a", nil))
	b, _ := s.Next(httptest.NewRequest(http.MethodGet, "/b", nil))
	a2, _ := s.Next(httptest.NewRequest(http.MethodGet, "/a", nil))
	assert.Equal(t, "a1", a.Body)
	assert.Equal(t, "b1", b.Body)
	assert.Equal(t, "a2", a2.Body)
}

func TestNextMatchesMethodAndPath(t *testing.T) {
	s, err := New(Routes{"GET /a": {{Body: "a"}}})
	require.NoError(t, err)

	_, ok := s.Next(httptest.NewRequest(http.MethodHead, "/a", nil))
	assert.False(t, ok, "HEAD is not GET")
	_, ok = s.Next(httptest.NewRequest(http.MethodGet, "/a/", nil))
	assert.False(t, ok, "the path is exact")
	_, ok = s.Next(httptest.NewRequest(http.MethodGet, "/a?anything=goes", nil))
	assert.True(t, ok, "a route without a query matches any query")
	assert.Len(t, s.Requests(), 3, "every request is recorded, matched or not")
}

func TestNextPrefersTheRouteWithTheQuery(t *testing.T) {
	s, err := New(Routes{
		"GET /runs":                    {{Body: "any"}},
		"GET /runs?head_sha=abc":       {{Body: "abc"}},
		"GET /runs?head_sha=abc&per=1": {{Body: "abc-per"}},
	})
	require.NoError(t, err)

	cases := map[string]string{
		"/runs":                             "any",
		"/runs?head_sha=def":                "any",
		"/runs?head_sha=abc":                "abc",
		"/runs?per_page=100&head_sha=abc":   "abc",
		"/runs?head_sha=abc&per=1&extra=x":  "abc-per",
		"/runs?per=1":                       "any",
		"/runs?head_sha=abc&head_sha=other": "any",
	}
	for target, want := range cases {
		resp, ok := s.Next(httptest.NewRequest(http.MethodGet, target, nil))
		require.True(t, ok, target)
		assert.Equal(t, want, resp.Body, target)
	}
}

func TestNewRefusesMalformedRoutes(t *testing.T) {
	for _, key := range []string{"GET", "GET repos", "/a", "GET  /a"} {
		_, err := New(Routes{key: {{}}})
		assert.Error(t, err, key)
	}
	_, err := New(Routes{"GET /a": nil})
	assert.Error(t, err, "no responses")
}

func TestResponseBytes(t *testing.T) {
	data, err := Response{}.Bytes()
	require.NoError(t, err)
	assert.Empty(t, data)

	data, err = Response{Body: "verbatim"}.Bytes()
	require.NoError(t, err)
	assert.Equal(t, "verbatim", string(data))

	data, err = Response{Body: map[string]any{"a": []any{1, "b"}}}.Bytes()
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":[1,"b"]}`, string(data))

	assert.Equal(t, http.StatusOK, Response{}.StatusCode())
	assert.Equal(t, http.StatusTeapot, Response{Status: 418}.StatusCode())
}

func TestWrite(t *testing.T) {
	resp := Response{Status: 201, Headers: map[string]string{"x-custom": "1"}, Body: map[string]any{"ok": true}}

	rec := httptest.NewRecorder()
	require.NoError(t, Write(rec, httptest.NewRequest(http.MethodGet, "/", nil), resp))
	assert.Equal(t, 201, rec.Code)
	assert.Equal(t, "1", rec.Header().Get("X-Custom"))
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "11", rec.Header().Get("Content-Length"))
	assert.JSONEq(t, `{"ok":true}`, rec.Body.String())

	rec = httptest.NewRecorder()
	require.NoError(t, Write(rec, httptest.NewRequest(http.MethodHead, "/", nil), resp))
	assert.Equal(t, "11", rec.Header().Get("Content-Length"), "HEAD announces the length")
	assert.Empty(t, rec.Body.String(), "HEAD carries no body")

	rec = httptest.NewRecorder()
	text := Response{Headers: map[string]string{"Content-Type": "text/plain"}, Body: "hi"}
	require.NoError(t, Write(rec, httptest.NewRequest(http.MethodGet, "/", nil), text))
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"), "the fixture's Content-Type stays")
	assert.Equal(t, "hi", rec.Body.String())
}

func TestRequestString(t *testing.T) {
	s, err := New(Routes{})
	require.NoError(t, err)
	s.Record(httptest.NewRequest(http.MethodGet, "/a?b=1&a=2", nil))
	s.Record(httptest.NewRequest(http.MethodPost, "/c", nil))
	requests := s.Requests()
	require.Len(t, requests, 2)
	assert.Equal(t, "GET /a?a=2&b=1", requests[0].String())
	assert.Equal(t, "POST /c", requests[1].String())
}

func TestWriteResetsTheConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Write(w, r, Response{Reset: true, Body: "never sent"})
	}))
	defer server.Close()

	// A fresh transport: net/http replays a GET whose reused connection
	// was reset, a fresh connection it does not.
	client := &http.Client{Transport: &http.Transport{}}
	_, err := client.Get(server.URL + "/a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset by peer")
}
