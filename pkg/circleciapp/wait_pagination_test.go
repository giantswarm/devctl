package circleciapp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

type waitPageTransport func(*http.Request) (*http.Response, error)

func (f waitPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWaitIncludesFailuresFromLaterWorkflowPages(t *testing.T) {
	ci, err := circleciclient.New(circleciclient.Config{
		Anonymous: true,
		BaseURL:   "https://circleci.invalid",
		Transport: waitPageTransport(func(req *http.Request) (*http.Response, error) {
			var body string
			switch req.URL.Path {
			case "/api/v2/pipeline/p1":
				body = `{"id":"p1","state":"created"}`
			case "/api/v2/pipeline/p1/workflow":
				if token := req.URL.Query().Get("page-token"); token == "" {
					body = `{"items":[{"id":"checks","name":"checks","status":"success"}],"next_page_token":"two"}`
				} else {
					require.Equal(t, "two", token)
					body = `{"items":[{"id":"publish","name":"publish","status":"failed"}],"next_page_token":null}`
				}
			default:
				t.Fatalf("unexpected request: %s", req.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}),
	})
	require.NoError(t, err)

	outcome, err := (Waiter{
		CircleCI: ci,
		Sleep: func(context.Context, time.Duration) error {
			t.Fatal("all workflows are terminal on the first read")
			return nil
		},
	}).Wait(context.Background(), "p1")
	require.NoError(t, err)
	require.False(t, outcome.Green, "success on page one cannot hide a failed publish on page two")
	require.Equal(t, []string{"publish (failed)"}, outcome.Failed)
}
