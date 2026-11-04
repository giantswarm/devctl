package githubclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/sirupsen/logrus"
)

type webhooksTestTransport func(*http.Request) (*http.Response, error)

func (f webhooksTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSetRepositoryWebhooksPagination(t *testing.T) {
	const targetURL = "https://tekton.example.invalid/hooks"
	pageFailure := errors.New("later webhook page failed")
	cases := []struct {
		name      string
		matchPage string
		failPage  bool
		dryRun    bool
		wantPages []string
		wantWrite string
	}{
		{
			name: "update a match on a later page", matchPage: "3",
			wantPages: []string{"", "3"}, wantWrite: "PATCH /repos/o/r/hooks/77",
		},
		{
			name: "create only after the last page", wantPages: []string{"", "3"},
			wantWrite: "POST /repos/o/r/hooks",
		},
		{
			name: "later page failure prevents creation", failPage: true,
			wantPages: []string{"", "3"},
		},
		{
			name: "dry run finds a later match without writing", matchPage: "3", dryRun: true,
			wantPages: []string{"", "3"},
		},
		{
			name: "dry run exhausts pages without writing", dryRun: true,
			wantPages: []string{"", "3"},
		},
		{
			name: "stop at a first page match", matchPage: "first",
			wantPages: []string{""}, wantWrite: "PATCH /repos/o/r/hooks/77",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pages, writes []string
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			client, err := New(Config{
				Logger: logger, AccessToken: "synthetic-test-token", DryRun: tc.dryRun,
				Transport: webhooksTestTransport(func(req *http.Request) (*http.Response, error) {
					response := func(status int, body string) *http.Response {
						return &http.Response{
							Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
							StatusCode: status,
							Header:     http.Header{"Content-Type": []string{"application/json"}},
							Body:       io.NopCloser(strings.NewReader(body)),
							Request:    req,
						}
					}
					if req.Method == http.MethodGet && req.URL.Path == "/repos/o/r/hooks" {
						page := req.URL.Query().Get("page")
						pages = append(pages, page)
						if page != "" && page != "3" {
							return nil, fmt.Errorf("unexpected page %q", page)
						}
						if page == "3" && tc.failPage {
							return nil, pageFailure
						}
						hooks := []*github.Hook{}
						if page == "" {
							for i := 0; i < 50; i++ {
								hooks = append(hooks, &github.Hook{
									ID: new(int64(i + 1)),
									Config: &github.HookConfig{
										URL: new(fmt.Sprintf("https://other.example.invalid/hooks/%d", i)),
									},
								})
							}
						}
						match := &github.Hook{ID: new(int64(77)), Config: &github.HookConfig{URL: new(targetURL)}}
						if page == "" && tc.matchPage == "first" {
							hooks[0] = match
						} else if page == "3" && tc.matchPage == "3" {
							hooks = append(hooks, match)
						}
						var body bytes.Buffer
						if err := json.NewEncoder(&body).Encode(hooks); err != nil {
							return nil, err
						}
						res := response(http.StatusOK, body.String())
						if page == "" {
							// Follow the advertised page number, not a guessed page+1.
							res.Header.Set("Link", `<https://api.github.com/repos/o/r/hooks?page=3&per_page=50>; rel="next"`)
						} else if tc.matchPage == "3" {
							// Once found, no subsequent page needs to be read.
							res.Header.Set("Link", `<https://api.github.com/repos/o/r/hooks?page=4&per_page=50>; rel="next"`)
						}
						return res, nil
					}
					if req.Method == http.MethodPost || req.Method == http.MethodPatch {
						writes = append(writes, req.Method+" "+req.URL.Path)
						var hook github.Hook
						if err := json.NewDecoder(req.Body).Decode(&hook); err != nil {
							return nil, err
						}
						if hook.GetConfig().GetSecret() != "synthetic-shared-secret" {
							t.Error("write lost the requested shared secret")
						}
						return response(http.StatusOK, `{"id":77}`), nil
					}
					return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			hook := &github.Hook{
				Name: new("web"), Active: new(true), Events: []string{"issue_comment", "pull_request", "check_run"},
				Config: &github.HookConfig{
					URL: new(targetURL), ContentType: new("json"), InsecureSSL: new("0"), Secret: new("synthetic-shared-secret"),
				},
			}
			err = client.SetRepositoryWebhooks(t.Context(), testRepository(), hook)
			if tc.failPage {
				if !errors.Is(err, pageFailure) {
					t.Errorf("error = %v, want the later page failure", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(pages, tc.wantPages) {
				t.Errorf("pages = %q, want %q", pages, tc.wantPages)
			}
			var wantWrites []string
			if tc.wantWrite != "" {
				wantWrites = []string{tc.wantWrite}
			}
			if !reflect.DeepEqual(writes, wantWrites) {
				t.Errorf("writes = %q, want %q", writes, wantWrites)
			}
		})
	}
}
