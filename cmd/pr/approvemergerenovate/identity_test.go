package approvemergerenovate

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

type identityTransport func(*http.Request) (*http.Response, error)

func (f identityTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// PR numbers are scoped to one repository, including in an org-wide search.
func TestCommandProcessesEqualNumbersInDifferentRepositories(t *testing.T) {
	t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-offline-token")
	var mu sync.Mutex
	seen := map[string]int{}
	transport := identityTransport(func(req *http.Request) (*http.Response, error) {
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
