package ciwebhooks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type webhookTestTransport func(*http.Request) (*http.Response, error)

func (f webhookTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// The runner uses http.DefaultTransport. These tests run serially and replace
// it with an in-memory transport that cannot reach a real account or network.
func newWebhookTestCommand(t *testing.T, transport webhookTestTransport) *cobra.Command {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	t.Setenv("DEVCTL_CIWEBHOOKS_TEST_TOKEN", "synthetic-test-token")

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cmd, err := New(Config{Logger: logger, Stderr: io.Discard, Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd
}

func webhookTestResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestRunHonorsCancellation(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, cancelAfter := range []string{"before run", "repository read", "hooks read"} {
			t.Run(fmt.Sprintf("existing=%t/%s", existing, cancelAfter), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if cancelAfter == "before run" {
					cancel()
				}
				writes := 0
				cmd := newWebhookTestCommand(t, func(req *http.Request) (*http.Response, error) {
					if err := req.Context().Err(); err != nil {
						return nil, err
					}
					switch {
					case req.Method == http.MethodGet && req.URL.Path == "/repos/o/r":
						if cancelAfter == "repository read" {
							cancel()
						}
						return webhookTestResponse(req, http.StatusOK, `{"owner":{"login":"o"},"name":"r"}`), nil
					case req.Method == http.MethodGet && req.URL.Path == "/repos/o/r/hooks":
						if cancelAfter == "hooks read" {
							cancel()
						}
						body := `[]`
						if existing {
							body = `[{"id":7,"config":{"url":"https://github-pr-webhook.ci.giantswarm.io"}}]`
						}
						return webhookTestResponse(req, http.StatusOK, body), nil
					case req.Method == http.MethodPost || req.Method == http.MethodPatch:
						writes++
						return webhookTestResponse(req, http.StatusOK, `{"id":7}`), nil
					default:
						return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
					}
				})
				cmd.SetArgs([]string{
					"--github-token-envvar=DEVCTL_CIWEBHOOKS_TEST_TOKEN",
					"--webhook-secret=synthetic-shared-secret",
					"o/r",
				})

				err := cmd.ExecuteContext(ctx)
				if !errors.Is(err, context.Canceled) {
					t.Errorf("error = %v, want context.Canceled", err)
				}
				if writes != 0 {
					t.Errorf("sent %d webhook writes after cancellation, want none", writes)
				}
			})
		}
	}
}
