package circleciclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type workflowPageTransport func(*http.Request) (*http.Response, error)

func (f workflowPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestListPipelineWorkflowsReadsEveryPage(t *testing.T) {
	const next = "cursor +/=&?"
	var tokens []string
	c, err := New(Config{
		Anonymous: true,
		BaseURL:   "https://circleci.invalid",
		Transport: workflowPageTransport(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodGet, req.Method)
			require.Equal(t, "/api/v2/pipeline/p1/workflow", req.URL.Path)
			token := req.URL.Query().Get("page-token")
			tokens = append(tokens, token)
			body := `{"items":[{"id":"setup","name":"setup","status":"success"}],"next_page_token":"cursor +/=&?"}`
			if token != "" {
				require.Equal(t, next, token)
				body = `{"items":[{"id":"build","name":"build","status":"failed"}],"next_page_token":null}`
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}),
	})
	require.NoError(t, err)

	workflows, err := c.ListPipelineWorkflows(context.Background(), "p1")
	require.NoError(t, err)
	require.Equal(t, []string{"", next}, tokens)
	require.Len(t, workflows, 2)
	require.Equal(t, "failed", workflows[1].Status, "a later page's failed workflow must reach the waiter")
}

func TestListPipelineWorkflowsRejectsAPartialListing(t *testing.T) {
	var requests int
	c, err := New(Config{
		Anonymous: true,
		BaseURL:   "https://circleci.invalid",
		Transport: workflowPageTransport(func(req *http.Request) (*http.Response, error) {
			requests++
			code := http.StatusOK
			body := `{"items":[{"id":"setup","name":"setup","status":"success"}],"next_page_token":"two"}`
			if req.URL.Query().Get("page-token") == "two" {
				code, body = http.StatusServiceUnavailable, `{"message":"try again"}`
			}
			return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}),
	})
	require.NoError(t, err)

	workflows, err := c.ListPipelineWorkflows(context.Background(), "p1")
	require.ErrorContains(t, err, "HTTP 503")
	require.Nil(t, workflows, "the successful first page cannot establish a green pipeline")
	require.Equal(t, 2, requests)
}
