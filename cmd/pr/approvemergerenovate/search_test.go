package approvemergerenovate

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type searchTransport struct {
	mu         sync.Mutex
	mode       string
	pages, prs int
}

const (
	testAllPagesMode  = "all pages"
	testPageErrorMode = "page error"
)

func (f *searchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	header := make(http.Header)
	code, body := 200, `{"merged":true}`
	if req.Method != http.MethodGet {
		return nil, fmt.Errorf("unexpected mutation: %s", req.Method)
	}
	if req.URL.Path == testSearchIssuesPath {
		f.pages++
		for _, filter := range []string{"is:pr", "is:open", "archived:false", "review-requested:@me", "author:app/renovate"} {
			if !strings.Contains(req.URL.Query().Get("q"), filter) {
				return nil, fmt.Errorf("missing filter %q", filter)
			}
		}
		switch f.mode {
		case "incomplete":
			body = `{"total_count":1,"incomplete_results":true,"items":[]}`
		case "oversized":
			body = `{"total_count":1001,"items":[]}`
		default:
			if req.URL.Query().Get("page") == "2" {
				if f.mode == testPageErrorMode {
					code, body = 500, `{"message":"unavailable"}`
				} else {
					body = `{"total_count":101,"items":[{"number":101,"html_url":"https://github.com/one/api/pull/101"}]}`
				}
			} else {
				header.Set("Link", `<https://api.github.com/search/issues?page=2>; rel="next"`)
				var items []string
				for n := 1; n <= 100; n++ {
					items = append(items, fmt.Sprintf(`{"number":%d,"html_url":"https://github.com/one/api/pull/%d"}`, n, n))
				}
				body = `{"total_count":101,"items":[` + strings.Join(items, ",") + `]}`
			}
		}
	} else {
		f.prs++
	}
	return &http.Response{StatusCode: code, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestRunConsumesCompleteSearchResults(t *testing.T) {
	for _, mode := range []string{testAllPagesMode, testPageErrorMode, "incomplete", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-token")
			fake := &searchTransport{mode: mode}
			original := http.DefaultTransport
			http.DefaultTransport = fake
			t.Cleanup(func() { http.DefaultTransport = original })
			var out bytes.Buffer
			r := &runner{flag: &flag{Grouping: GroupingDependency}, logger: logrus.New(), stdout: &out, stderr: io.Discard}
			err := r.run(context.Background(), &cobra.Command{}, []string{GroupingDependency})
			wantPages, wantPRs := 1, 0
			if mode == testAllPagesMode || mode == testPageErrorMode {
				wantPages = 2
			}
			if mode == testAllPagesMode {
				wantPRs = 101
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(out.String(), "PRs skipped: 101") {
					t.Error("summary omits search matches")
				}
			} else if err == nil {
				t.Error("incomplete search must return an error before processing")
			}
			if fake.pages != wantPages || fake.prs != wantPRs {
				t.Errorf("search/PR requests = %d/%d, want %d/%d", fake.pages, fake.prs, wantPages, wantPRs)
			}
		})
	}
}
