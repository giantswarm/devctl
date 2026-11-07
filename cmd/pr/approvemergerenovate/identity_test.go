package approvemergerenovate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type identityTransport struct {
	mu    sync.Mutex
	reads map[string]int
}

func (f *identityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return nil, fmt.Errorf("unexpected mutation: %s", req.Method)
	}
	body := `{"merged":true}`
	if req.URL.Path == "/search/issues" {
		body = `{"total_count":4,"items":[
			{"number":42,"html_url":"https://github.com/one/api/pull/42"},
			{"number":42,"html_url":"https://github.com/one/web/pull/42"},
			{"number":42,"html_url":"https://github.com/two/api/pull/42"},
			{"number":42,"html_url":"https://github.com/one/api/pull/42"}]}`
	} else {
		f.mu.Lock()
		f.reads[req.URL.Path]++
		f.mu.Unlock()
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestRunKeepsRepositoryScopedPRIdentity(t *testing.T) {
	t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-token")
	fake := &identityTransport{reads: make(map[string]int)}
	original := http.DefaultTransport
	http.DefaultTransport = fake
	t.Cleanup(func() { http.DefaultTransport = original })
	var out bytes.Buffer
	r := &runner{flag: &flag{Grouping: GroupingDependency}, logger: logrus.New(), stdout: &out, stderr: io.Discard}
	if err := r.run(context.Background(), &cobra.Command{}, []string{"dependency"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/repos/one/api/pulls/42", "/repos/one/web/pulls/42", "/repos/two/api/pulls/42"} {
		if got := fake.reads[path]; got != 1 {
			t.Errorf("%s processed %d times; want once", path, got)
		}
	}
	if !strings.Contains(out.String(), "PRs skipped: 3") {
		t.Errorf("summary omitted a distinct PR: %s", &out)
	}
}

type commandIdentityTransport func(*http.Request) (*http.Response, error)

func (f commandIdentityTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// PR numbers are scoped to one repository, including in an org-wide search.
func TestCommandProcessesEqualNumbersInDifferentRepositories(t *testing.T) {
	t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-offline-token")
	var mu sync.Mutex
	seen := map[string]int{}
	transport := commandIdentityTransport(func(req *http.Request) (*http.Response, error) {
		var body any
		switch req.URL.Path {
		case "/search/issues":
			issues := []any{}
			for _, repository := range []string{"o/a", "o/b", "other/a", "o/a"} {
				issues = append(issues, map[string]any{"number": 7, "title": "Update fixture", "html_url": "https://github.com/" + repository + "/pull/7"})
			}
			body = map[string]any{"total_count": len(issues), "items": issues}
		case "/repos/o/a/pulls/7", "/repos/o/b/pulls/7", "/repos/other/a/pulls/7":
			mu.Lock()
			seen[req.URL.Path]++
			mu.Unlock()
			body = map[string]any{"merged": true}
		default:
			t.Errorf("unexpected offline request: %s %s", req.Method, req.URL)
			body = map[string]any{}
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
	})
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetArgs([]string{"fixture", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, repository := range []string{"o/a", "o/b", "other/a"} {
		if n := seen["/repos/"+repository+"/pulls/7"]; n != 1 {
			t.Errorf("%s PR 7 processed %d times, want once", repository, n)
		}
	}
}
