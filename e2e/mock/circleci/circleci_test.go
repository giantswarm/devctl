package circleci

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

func do(t *testing.T, method, url string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(body)
}

func TestSequenceUnderTheAPIPrefix(t *testing.T) {
	s, err := Start(sequence.Routes{
		"GET /api/v2/pipeline/p1/workflow": {
			{Body: map[string]any{"items": []any{map[string]any{"name": "build", "status": "running"}}}},
			{Body: map[string]any{"items": []any{map[string]any{"name": "build", "status": "success"}}}},
		},
		"GET /api/v2/project/gh/o/r/pipeline?branch=feature": {
			{Body: map[string]any{"items": []any{map[string]any{"id": "p1", "number": 12}}}},
		},
		"POST /oauth/register": {
			{Status: http.StatusCreated, Body: map[string]any{"client_id": "device-1"}},
		},
	})
	require.NoError(t, err)
	defer s.Close()

	assert.Equal(t, s.URL+"/api/v2", s.APIURL())

	_, body := do(t, http.MethodGet, s.APIURL()+"/project/gh/o/r/pipeline?branch=feature&page-token=x")
	assert.Contains(t, body, `"number":12`)

	_, body = do(t, http.MethodGet, s.APIURL()+"/pipeline/p1/workflow")
	assert.Contains(t, body, "running")
	_, body = do(t, http.MethodGet, s.APIURL()+"/pipeline/p1/workflow")
	assert.Contains(t, body, "success")
	_, body = do(t, http.MethodGet, s.APIURL()+"/pipeline/p1/workflow")
	assert.Contains(t, body, "success", "the last response repeats")

	resp, body := do(t, http.MethodPost, s.URL+"/oauth/register")
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Contains(t, body, "device-1")

	assert.Len(t, s.Requests(), 5)
}

func TestUnscriptedRouteIsNotFound(t *testing.T) {
	s, err := Start(sequence.Routes{})
	require.NoError(t, err)
	defer s.Close()

	resp, body := do(t, http.MethodGet, s.APIURL()+"/project/gh/o/r")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.JSONEq(t, `{"message":"Not Found"}`, body)
}

func TestScriptedTruncatedResponse(t *testing.T) {
	s, err := Start(sequence.Routes{
		"GET /api/v2/pipeline/p1/workflow": {{
			Headers: map[string]string{"Content-Length": "64"},
			Body:    map[string]any{"items": []any{}},
		}},
	})
	require.NoError(t, err)
	defer s.Close()

	client := s.Client()
	client.Timeout = 5 * time.Second
	resp, err := client.Get(s.APIURL() + "/pipeline/p1/workflow")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, int64(64), resp.ContentLength)
	body, err := io.ReadAll(resp.Body)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.JSONEq(t, `{"items":[]}`, string(body))
}
