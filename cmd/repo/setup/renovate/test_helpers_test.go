package renovate

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// newTestCommand uses the same persistent flag as the setup parent. The
// transport returns only in-memory responses and never delegates a request.
// These tests must remain sequential because http.DefaultTransport is global.
func newTestCommand(t *testing.T, transport roundTripFunc) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	t.Setenv("DEVCTL_RENOVATE_TEST_TOKEN", "offline-test-token")
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	var log bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&log)
	command, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	parent := &cobra.Command{Use: "setup", SilenceErrors: true, SilenceUsage: true}
	parent.PersistentFlags().String("github-token-envvar", "DEVCTL_RENOVATE_TEST_TOKEN", "test token variable")
	parent.SetOut(io.Discard)
	parent.SetErr(io.Discard)
	parent.AddCommand(command)
	return parent, &log
}

func testResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
