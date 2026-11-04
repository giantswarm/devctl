package approvealign

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

type readErrorTransport func(*http.Request) (*http.Response, error)

func (f readErrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func Test_ReadErrors_Command(t *testing.T) {
	testCases := []struct {
		name       string
		code       int
		body       string
		transport  bool
		permitted  bool
		wantDetail string
	}{
		{name: "case 0: invalid token", code: 401, body: `{"message":"Bad credentials"}`, wantDetail: "HTTP 401 Bad credentials"},
		{name: "case 1: denied read", code: 403, body: `{"message":"Resource not accessible by integration"}`, wantDetail: "HTTP 403 Resource not accessible by integration"},
		{name: "case 2: inaccessible repository", code: 404, body: `{"message":"Not Found"}`, wantDetail: "HTTP 404 Not Found"},
		{name: "case 3: service failure", code: 503, body: `{"message":"Service Unavailable"}`, wantDetail: "HTTP 503 Service Unavailable"},
		{name: "case 4: transport failure", transport: true, wantDetail: "synthetic connection failure"},
		{name: "case 5: unreadable response", code: 200, body: `{`, wantDetail: "HTTP 200 unexpected end of JSON input"},
		{name: "case 6: non-JSON service failure", code: 503, body: `<html>unavailable</html>`, wantDetail: "HTTP 503 Service Unavailable"},
		{name: "case 7: permitted reads still allow approval", permitted: true, wantDetail: "Would approve"},
	}
	for _, tc := range testCases {
		for _, endpoint := range []string{"PR", "reviews"} {
			for _, dryRun := range []bool{false, true} {
				if tc.permitted && !dryRun {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/dry-run=%t", tc.name, endpoint, dryRun), func(t *testing.T) {
					t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-offline-token")
					t.Setenv("CI", "true")
					var reads, mutations atomic.Int32
					original := http.DefaultTransport
					http.DefaultTransport = readErrorTransport(func(req *http.Request) (*http.Response, error) {
						if req.Method != http.MethodGet {
							mutations.Add(1)
							return nil, errors.New("unexpected fixture mutation")
						}
						code, body := http.StatusOK, ""
						targetRead := false
						switch {
						case req.URL.Path == "/search/issues":
							body = `{"total_count":2,"items":[{"number":1,"title":"` + alignTitle + `","html_url":"https://github.com/giantswarm/first/pull/1"},{"number":2,"title":"` + alignTitle + `","html_url":"https://github.com/giantswarm/second/pull/2"}]}`
						case strings.HasSuffix(req.URL.Path, "/reviews"):
							if endpoint != "reviews" && !tc.permitted {
								t.Error("read reviews after the PR read failed")
							}
							targetRead = endpoint == "reviews"
							body = `[]`
						case strings.Contains(req.URL.Path, "/pulls/"):
							targetRead = endpoint == "PR"
							body = `{"title":"` + alignTitle + `","head":{"sha":"offline-head"}}`
						case strings.HasSuffix(req.URL.Path, "/status"):
							body = `{"state":"pending","total_count":0,"statuses":[]}`
						case strings.HasSuffix(req.URL.Path, "/check-runs"):
							body = `{"total_count":0,"check_runs":[]}`
						default:
							t.Errorf("unexpected fixture path: %s", req.URL.Path)
							return nil, errors.New("unexpected fixture request")
						}
						if targetRead {
							reads.Add(1)
							if tc.transport {
								return nil, errors.New("synthetic connection failure")
							}
							if !tc.permitted {
								code, body = tc.code, tc.body
							}
						}
						return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
					})
					t.Cleanup(func() { http.DefaultTransport = original })
					var stdout bytes.Buffer
					cmd, err := New(Config{Logger: logrus.New(), Stdout: &stdout, Stderr: io.Discard})
					if err != nil {
						t.Fatal(err)
					}
					args := []string{}
					if dryRun {
						args = append(args, "--dry-run")
					}
					cmd.SetArgs(args)
					if err := cmd.Execute(); err != nil {
						t.Fatal(err)
					}
					output := stdout.String()
					if reads.Load() != 2 || mutations.Load() != 0 {
						t.Errorf("reads = %d, mutations = %d; want 2, 0", reads.Load(), mutations.Load())
					}
					if tc.permitted {
						if !strings.Contains(output, tc.wantDetail) || !strings.Contains(output, "PRs that would be approved: 2") || strings.Contains(output, "PRs failed:") {
							t.Errorf("permitted reads did not reach dry-run approval:\n%s", output)
						}
						return
					}
					operation := "Failed to get " + endpoint
					for _, want := range []string{operation, tc.wantDetail, "PRs failed: 2", "Token source: $DEVCTL_GITHUB_TOKEN"} {
						// Transport failures have no common HTTP status to group.
						if tc.transport && strings.HasPrefix(want, "Token source:") {
							continue
						}
						if !strings.Contains(output, want) {
							t.Errorf("output lacks %q:\n%s", want, output)
						}
					}
					if dryRun && !strings.Contains(output, "PRs that would be approved: 0") {
						t.Errorf("failed reads counted as planned approvals:\n%s", output)
					}
					if !tc.transport && strings.Contains(output, "api.github.com") {
						t.Errorf("HTTP diagnostic includes the request URL:\n%s", output)
					}
				})
			}
		}
	}
}

func Test_ReadErrors_Status(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want string
	}{
		{name: "case 0: single terminal row", err: &github.ErrorResponse{Message: "Not\naccessible\r\t\u001b[2J"}, want: "Failed to get PR: HTTP 403 Not accessible [2J"},
		{name: "case 1: primary rate limit", err: &github.RateLimitError{Message: "API rate limit exceeded"}, want: "Failed to get PR: HTTP 403 API rate limit exceeded"},
		{name: "case 2: secondary rate limit", err: &github.AbuseRateLimitError{Message: "Secondary rate limit exceeded"}, want: "Failed to get PR: HTTP 403 Secondary rate limit exceeded"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &github.Response{Response: &http.Response{StatusCode: http.StatusForbidden}}
			if got := readErrorStatus(failedPRRead, resp, tc.err); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

func Test_ReadErrors_Report(t *testing.T) {
	testCases := []struct {
		name     string
		statuses []string
		grouped  bool
	}{
		{name: "case 0: common HTTP status across endpoints", statuses: []string{"Failed to get PR: HTTP 403 Denied", "Failed to get reviews: HTTP 403 Reviews denied"}, grouped: true},
		{name: "case 1: different HTTP statuses", statuses: []string{"Failed to get PR: HTTP 403 Denied", "Failed to get reviews: HTTP 503 Service Unavailable"}},
		{name: "case 2: partial success", statuses: []string{"Failed to get PR: HTTP 403 Denied", "Would approve"}},
		{name: "case 3: transport failure", statuses: []string{"Failed to get PR: connection refused", "Failed to get PR: connection refused"}},
		{name: "case 4: ordinary success", statuses: []string{"Would approve", "Already approved"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			r := &runner{stdout: &stdout}
			statuses := []*pr.PRStatus{
				{Owner: "fixture", Repo: "first", Number: 1, URL: "https://github.com/fixture/first/pull/1", DisplayLabel: "first", Status: tc.statuses[0]},
				{Owner: "fixture", Repo: "second", Number: 2, URL: "https://github.com/fixture/second/pull/2", DisplayLabel: "second", Status: tc.statuses[1]},
			}
			r.updateStatusTable(statuses, 4, "the devctl keychain")
			output := stdout.String()
			if !strings.HasPrefix(output, "\033[4A\033[J") {
				t.Errorf("previous display was not cleared: %q", output)
			}
			if strings.Contains(output, "All 2 PRs failed GitHub reads with HTTP 403.") != tc.grouped {
				t.Errorf("grouped output = %t, want %t:\n%s", !strings.Contains(output, "Repository"), tc.grouped, output)
			}
			if strings.Contains(output, "https://github.com/fixture/") == tc.grouped {
				t.Errorf("per-PR table/group choice is incorrect:\n%s", output)
			}
			for _, status := range tc.statuses {
				if !strings.Contains(output, status) {
					t.Errorf("output lacks %q:\n%s", status, output)
				}
			}
		})
	}
	t.Run("case 5: 118 identical failures collapse to one diagnostic", func(t *testing.T) {
		var stdout bytes.Buffer
		r := &runner{stdout: &stdout}
		statuses := make([]*pr.PRStatus, 118)
		for i := range statuses {
			statuses[i] = &pr.PRStatus{Status: "Queued"}
		}
		rows := r.updateStatusTable(statuses, 0, "$DEVCTL_GITHUB_TOKEN")
		stdout.Reset()
		const failure = "Failed to get PR: HTTP 403 Resource not accessible by integration"
		for _, ps := range statuses {
			ps.UpdateStatus(failure)
		}
		r.updateStatusTable(statuses, rows, "$DEVCTL_GITHUB_TOKEN")
		output := stdout.String()
		if !strings.Contains(output, "All 118 PRs failed GitHub reads with HTTP 403.") || !strings.Contains(output, "118 PR(s): "+failure) || strings.Count(output, failure) != 1 {
			t.Fatalf("failures were not grouped:\n%s", output)
		}
		if !strings.Contains(output, "Token source: $DEVCTL_GITHUB_TOKEN") || !strings.Contains(output, "App installation access") {
			t.Fatalf("missing access diagnostic:\n%s", output)
		}
	})
}

func Test_ReadErrors_Summary(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry-run=%t", dryRun), func(t *testing.T) {
			var stdout bytes.Buffer
			r := &runner{stdout: &stdout, flag: &flag{DryRun: dryRun}}
			// API prose must not turn a failed read into an approval or merge.
			const status = "Failed to get PR: HTTP 403 Already Merged Blocked Waiting"
			r.printSummary([]*pr.PRStatus{{Status: status}, {Status: status}})
			output := stdout.String()
			for _, want := range []string{"PRs failed: 2", "PRs skipped: 0", "2 PR(s): " + status} {
				if !strings.Contains(output, want) {
					t.Errorf("summary lacks %q:\n%s", want, output)
				}
			}
			if strings.Contains(output, "PRs merged:") || strings.Contains(output, "PRs blocked:") || strings.Contains(output, "PRs still waiting:") {
				t.Errorf("API prose changed summary counts:\n%s", output)
			}
			want := "PRs approved: 0"
			if dryRun {
				want = "PRs that would be approved: 0"
			}
			if !strings.Contains(output, want) {
				t.Errorf("summary lacks %q:\n%s", want, output)
			}
		})
	}
}
