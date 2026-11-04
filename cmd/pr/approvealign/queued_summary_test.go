package approvealign

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

type queuedSummaryTransport func(*http.Request) (*http.Response, error)

func (f queuedSummaryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCommandQueuedSummary(t *testing.T) {
	t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-offline-token")
	t.Setenv("CI", "true")
	original := http.DefaultTransport
	http.DefaultTransport = queuedSummaryTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Errorf("already-approved PR must not cause a mutation: %s", req.URL.Path)
			return nil, io.ErrUnexpectedEOF
		}
		var body string
		switch req.URL.Path {
		case "/search/issues":
			body = `{"total_count":1,"items":[{"number":1,"title":"` + alignTitle + `","html_url":"https://github.com/giantswarm/fixture/pull/1"}]}`
		case "/repos/giantswarm/fixture/pulls/1":
			body = `{"title":"` + alignTitle + `","head":{"sha":"checked-head"},"mergeable_state":"clean","auto_merge":{}}`
		case "/repos/giantswarm/fixture/commits/checked-head/status":
			body = `{"state":"pending","total_count":0,"statuses":[]}`
		case "/repos/giantswarm/fixture/commits/checked-head/check-runs":
			body = `{"total_count":0,"check_runs":[]}`
		case "/repos/giantswarm/fixture/pulls/1/reviews":
			body = `[{"state":"APPROVED","user":{"login":"reviewer"}}]`
		default:
			t.Errorf("unexpected request: %s", req.URL.Path)
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	var output bytes.Buffer
	command, err := New(Config{Logger: logrus.New(), Stdout: &output, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	command.SetArgs([]string{})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Already approved, queued") || !strings.Contains(output.String(), "PRs queued to merge: 1\n") || !strings.Contains(output.String(), "PRs skipped: 0\n") {
		t.Errorf("already-approved auto-merge queue is miscounted: %s", output.String())
	}
}
