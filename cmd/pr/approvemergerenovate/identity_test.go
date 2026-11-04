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
