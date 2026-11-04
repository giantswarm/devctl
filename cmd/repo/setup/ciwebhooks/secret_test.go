package ciwebhooks

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type emptySecretTestTransport func(*http.Request) (*http.Response, error)

func (f emptySecretTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// The runner uses http.DefaultTransport. These tests run serially and replace
// it with an in-memory transport that cannot reach a real account or network.
func newEmptySecretTestCommand(t *testing.T, transport emptySecretTestTransport) *cobra.Command {
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

func TestRunRejectsEmptyWebhookSecret(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry-run=%t", dryRun), func(t *testing.T) {
			requests := 0
			cmd := newEmptySecretTestCommand(t, func(req *http.Request) (*http.Response, error) {
				requests++
				return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
			})
			cmd.SetArgs([]string{
				"--github-token-envvar=DEVCTL_CIWEBHOOKS_TEST_TOKEN",
				"--webhook-secret=",
				fmt.Sprintf("--dry-run=%t", dryRun),
				"o/r",
			})

			err := cmd.ExecuteContext(t.Context())
			if !IsInvalidFlag(err) || !strings.Contains(err.Error(), "--webhook-secret must not be empty") {
				t.Fatalf("error = %v, want an empty-secret flag error", err)
			}
			if requests != 0 {
				t.Fatalf("sent %d requests for an empty secret, want none", requests)
			}
		})
	}
}
