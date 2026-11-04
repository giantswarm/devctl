package approvemergerenovate

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

type searchTransport func(*http.Request) (*http.Response, error)

func (f searchTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCommandSearchPagination(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		failedPage, incomplete bool
	}{
		{name: "all pages"}, {name: "later page refused", failedPage: true}, {name: "incomplete search", incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-offline-token")
			var mu sync.Mutex
			seen := map[string]int{}
			pages := 0
			transport := searchTransport(func(req *http.Request) (*http.Response, error) {
				code := 200
				headers := http.Header{"Content-Type": {"application/json"}}
				var body any
				switch req.URL.Path {
				case "/search/issues":
					pages++
					n := 1
					if req.URL.Query().Get("page") == "2" {
						n = 2
						if tc.failedPage {
							code = 503
						}
					} else {
						headers.Set("Link", `<https://api.github.com/search/issues?page=2>; rel="next"`)
					}
					body = map[string]any{"total_count": 2, "incomplete_results": tc.incomplete, "items": []any{map[string]any{"number": n, "title": "Update fixture", "html_url": "https://github.com/o/r/pull/" + strconv.Itoa(n)}}}
				case "/repos/o/r/pulls/1", "/repos/o/r/pulls/2":
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
				return &http.Response{StatusCode: code, Header: headers, Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
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
			err = cmd.Execute()
			wantErr := tc.failedPage || tc.incomplete
			if (err != nil) != wantErr {
				t.Errorf("error=%v, want error=%v", err, wantErr)
			}
			mu.Lock()
			defer mu.Unlock()
			if wantErr {
				if len(seen) != 0 {
					t.Errorf("processed PRs from an incomplete search: %v", seen)
				}
			} else if pages != 2 || seen["/repos/o/r/pulls/1"] != 1 || seen["/repos/o/r/pulls/2"] != 1 {
				t.Errorf("search pages=%d, processed=%v; want both pages and PRs", pages, seen)
			}
		})
	}
}
