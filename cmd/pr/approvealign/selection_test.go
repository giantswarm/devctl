package approvealign

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/sirupsen/logrus"
)

type selectionTransport func(*http.Request) (*http.Response, error)

func (f selectionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCommandAlignSelection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		title      string
		current    string
		team       bool
		bot        bool
		wantReads  int
		wantChecks int
		want       string
	}{
		{name: "body-only match", title: "Document the alignment process", want: "No PRs found."},
		{name: "phrase in unrelated title", title: "Revert " + alignTitle, want: "No PRs found."},
		{name: "requested human PR retains default behavior", title: alignTitle, current: alignTitle, wantReads: 1, wantChecks: 2, want: "Would approve"},
		{name: "generated suffix", title: alignTitle + " #2", current: alignTitle + " #2", wantReads: 1, wantChecks: 2, want: "Would approve"},
		{name: "renamed after search", title: alignTitle, current: "Change deployment policy", wantReads: 1, want: "Skipped: not an Align files PR"},
		{name: "team bot", title: alignTitle, current: alignTitle, team: true, bot: true, wantReads: 1, wantChecks: 2, want: "Would approve"},
		{name: "team person", title: alignTitle, current: alignTitle, team: true, want: "No PRs found."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-offline-token")
			t.Setenv("CI", "true")
			reads, checks := 0, 0
			original := http.DefaultTransport
			http.DefaultTransport = selectionTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet {
					t.Fatalf("unexpected mutation: %s", req.URL.Path)
				}
				var body string
				switch req.URL.Path {
				case "/search/issues":
					query := req.URL.Query().Get("q")
					if !strings.Contains(query, "in:title") {
						t.Error("search does not restrict the phrase to titles")
					}
					if strings.Contains(query, "review-requested:@me") == tc.team {
						t.Errorf("unexpected review-request scope: %s", query)
					}
					u := &github.User{Login: new("fixture-user"), Type: new("User")}
					if tc.bot {
						u = &github.User{Login: new("fixture[bot]"), Type: new("Bot")}
					}
					items := []*github.Issue{{Number: new(1), Title: new(tc.title), Body: new("This change documents " + alignTitle), HTMLURL: new("https://github.com/giantswarm/fixture/pull/1"), User: u}}
					raw, err := json.Marshal(map[string]any{"total_count": 1, "items": items})
					if err != nil {
						t.Fatal(err)
					}
					body = string(raw)
				case "/repos/giantswarm/github/contents/repositories/team-fixture.yaml":
					body = `{"type":"file","encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte("- name: fixture\n")) + `"}`
				case "/repos/giantswarm/fixture/pulls/1":
					reads++
					body = `{"title":"` + tc.current + `","head":{"sha":"checked-head"}}`
				case "/repos/giantswarm/fixture/commits/checked-head/status":
					checks++
					body = `{"state":"pending","total_count":0,"statuses":[]}`
				case "/repos/giantswarm/fixture/commits/checked-head/check-runs":
					checks++
					body = `{"total_count":0,"check_runs":[]}`
				case "/repos/giantswarm/fixture/pulls/1/reviews":
					body = `[]`
				default:
					t.Fatalf("unexpected request: %s", req.URL.Path)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = original })
			var output bytes.Buffer
			command, err := New(Config{Logger: logrus.New(), Stdout: &output, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"--dry-run"}
			if tc.team {
				args = append(args, "--team=fixture")
			}
			command.SetArgs(args)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if tc.name == "renamed after search" && (!strings.Contains(output.String(), "PRs skipped: 1") || !strings.Contains(output.String(), "PRs that would be approved: 0")) {
				t.Errorf("renamed PR counted as approvable: %s", output.String())
			}
			if !strings.Contains(output.String(), tc.want) || reads != tc.wantReads || checks != tc.wantChecks {
				t.Errorf("PR reads/checks = %d/%d, want %d/%d and output containing %q; output:\n%s", reads, checks, tc.wantReads, tc.wantChecks, tc.want, output.String())
			}
		})
	}
}
