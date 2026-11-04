package approvealign

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/sirupsen/logrus"
)

type summaryTransport func(*http.Request) (*http.Response, error)

func (f summaryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCommandActionSummary(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		t.Run("dry-run="+strconv.FormatBool(dryRun), func(t *testing.T) {
			t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-offline-token")
			t.Setenv("CI", "true")
			original := http.DefaultTransport
			http.DefaultTransport = summaryTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet {
					t.Errorf("unexpected mutation: %s", req.URL.Path)
					return nil, io.ErrUnexpectedEOF
				}
				var body string
				path := req.URL.Path
				switch {
				case path == "/search/issues":
					count := 1
					if dryRun {
						count = 4
					}
					items := []*github.Issue{}
					for n := 1; n <= count; n++ {
						items = append(items, &github.Issue{Number: new(n), Title: new(alignTitle), HTMLURL: new("https://github.com/giantswarm/fixture/pull/" + strconv.Itoa(n)), User: &github.User{Login: new("fixture[bot]"), Type: new("Bot")}})
					}
					raw, err := json.Marshal(map[string]any{"total_count": count, "items": items})
					if err != nil {
						t.Fatal(err)
					}
					body = string(raw)
				case path == "/repos/giantswarm/github/contents/repositories/team-fixture.yaml":
					body = `{"type":"file","encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte("- name: fixture\n")) + `"}`
				case path == "/repos/giantswarm/fixture/rules/branches/main":
					body = `[]`
				case strings.HasPrefix(path, "/repos/giantswarm/fixture/commits/") && strings.HasSuffix(path, "/status"):
					body = `{"state":"pending","total_count":0,"statuses":[]}`
				case strings.HasPrefix(path, "/repos/giantswarm/fixture/commits/") && strings.HasSuffix(path, "/check-runs"):
					body = `{"total_count":0,"check_runs":[]}`
				case strings.HasPrefix(path, "/repos/giantswarm/fixture/pulls/"):
					parts := strings.Split(strings.TrimPrefix(path, "/repos/giantswarm/fixture/pulls/"), "/")
					n, err := strconv.Atoi(parts[0])
					if err != nil {
						t.Fatal(err)
					}
					if len(parts) > 1 && parts[1] == "reviews" {
						body = `[{"state":"APPROVED","user":{"login":"reviewer"}}]`
						if dryRun && n == 1 {
							body = `[]`
						}
					} else {
						state := "clean"
						if n == 3 {
							state = "behind"
						} else if n == 4 {
							state = "blocked"
						}
						body = `{"title":"` + alignTitle + `","head":{"sha":"checked-head-` + strconv.Itoa(n) + `"},"base":{"ref":"main"},"mergeable_state":"` + state + `"`
						if !dryRun {
							body += `,"auto_merge":{}`
						}
						body += `}`
					}
				default:
					t.Errorf("unexpected request: %s", path)
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
			command.SetArgs([]string{"--team=fixture", "--dry-run=" + strconv.FormatBool(dryRun)})
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			want := []string{"PRs queued to merge: 1\n", "PRs skipped: 0\n"}
			if dryRun {
				want = []string{"PRs that would be approved: 1\n", "PRs that would be merged: 1\n", "PR branches that would be updated: 1\n", "PRs blocked: 1\n"}
			}
			for _, line := range want {
				if !strings.Contains(output.String(), line) {
					t.Errorf("summary lacks %q; output:\n%s", line, output.String())
				}
			}
		})
	}
}
