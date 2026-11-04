package approvealign

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"
)

type teamContentsTransport func(*http.Request) (*http.Response, error)

func (f teamContentsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTeamRepositoriesContents(t *testing.T) {
	file := func(content string) string {
		return `{"type":"file","encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte(content)) + `"}`
	}
	for _, tc := range []struct {
		name      string
		team      string
		body      string
		code      int
		want      map[string]bool
		wantError bool
		wantKind  bool
	}{
		{name: "directory", body: `[{"type":"file","name":"entry.yaml"}]`, wantError: true, wantKind: true},
		{name: "empty directory", body: `[]`, wantError: true, wantKind: true},
		{name: "submodule metadata", body: `{"type":"submodule","submodule_git_url":"https://example.invalid/fixture"}`, wantError: true, wantKind: true},
		{name: "unresolved symlink metadata", body: `{"type":"symlink","target":"missing"}`, wantError: true, wantKind: true},
		{name: "regular file", team: "team-fixture", body: file("- name: first\n- name: second\n- name: first\n"), want: map[string]bool{"first": true, "second": true}},
		{name: "empty team", body: file(""), want: map[string]bool{}},
		{name: "invalid declaration", body: file("not-a-list: true\n"), wantError: true},
		{name: "invalid encoding", body: `{"type":"file","encoding":"base64","content":"!"}`, wantError: true},
		{name: "missing team", code: http.StatusNotFound, body: `{"message":"fixture not found"}`, wantError: true, wantKind: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("non-file declaration caused a panic: %v", recovered)
				}
			}()
			client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: teamContentsTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.Path != "/repos/giantswarm/github/contents/repositories/team-fixture.yaml" {
					t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
				}
				code := tc.code
				if code == 0 {
					code = http.StatusOK
				}
				return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: req}, nil
			})}))
			if err != nil {
				t.Fatal(err)
			}
			team := tc.team
			if team == "" {
				team = "fixture"
			}
			got, err := teamRepositories(context.Background(), client, team)
			if (err != nil) != tc.wantError || (tc.wantKind && !IsExecutionFailed(err)) {
				t.Fatalf("error = %v, want error %v (execution failure %v)", err, tc.wantError, tc.wantKind)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("repositories = %v, want %v", got, tc.want)
			}
		})
	}
}
