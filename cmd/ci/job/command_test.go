package job

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/internal/env"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/project"
)

const (
	urlWorkflowID = "11111111-1111-4111-8111-111111111111"
	urlJobID      = "22222222-2222-4222-8222-222222222222"
	appJobURL     = "https://app.circleci.com/workflow/" + urlWorkflowID + "/job/" + urlJobID
)

// executeURLCommand exercises the public command, including its real flag,
// endpoint and authentication setup. Only a temporary synthetic login and
// loopback API are reachable; version discovery is explicitly bypassed.
func executeURLCommand(t *testing.T, routes sequence.Routes, args ...string) (document, int, *circlemock.Server) {
	t.Helper()
	cc, err := circlemock.Start(routes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cc.Close)
	store := &authstore.FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
	if err := store.Set(authstore.UserCircleCI, authstore.Record{Token: "token_test", Login: "someone"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(agentcli.EnvKeyringFile, store.Path)
	t.Setenv(agentcli.EnvCircleCIAPIURL, cc.APIURL())
	t.Setenv(agentcli.EnvCircleCIOAuthURL, cc.URL)
	t.Setenv(agentcli.EnvGitHubAPIURL, cc.URL+"/github")
	t.Setenv(agentcli.EnvGitHubOAuthURL, cc.URL+"/github-oauth")
	t.Setenv(agentcli.EnvTimeScale, "1")
	t.Setenv(env.DevctlUnsafeForceVersion.Key(), project.Version())
	var stdout, stderr bytes.Buffer
	cmd, err := New(Config{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(&stderr)
	cmd.SetErr(&stderr)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs(args)
	runErr := cmd.Execute()
	var doc document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v; stdout=%q", err, stdout.String())
	}
	if doc.ExitCode != agentcli.Exit(runErr) || doc.Command != "ci job" || doc.SchemaVersion != 1 || doc.StartedAt.IsZero() || doc.FinishedAt.Before(doc.StartedAt) || doc.Warnings == nil {
		t.Errorf("envelope: %+v; error=%v", doc.Envelope, runErr)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
	for _, req := range cc.Requests() {
		if req.Method != http.MethodGet || req.Header.Get("Circle-Token") != "token_test" {
			t.Errorf("unexpected API request method or token: %s", req.String())
		}
	}
	return doc, agentcli.Exit(runErr), cc
}

func assertURLReads(t *testing.T, cc *circlemock.Server, paths ...string) {
	t.Helper()
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		seen[path] = false
	}
	for _, req := range cc.Requests() {
		if _, ok := seen[req.Path]; !ok {
			t.Errorf("unexpected API read: %s", req.String())
			continue
		}
		seen[req.Path] = true
	}
	for path, read := range seen {
		if !read {
			t.Errorf("missing API read: %s", path)
		}
	}
}

// Both URL forms must reach the requested job, report a failed job as listed,
// apply the parsed line limit, and keep the API token off signed output URLs.
func TestCommandReadsJobURLs(t *testing.T) {
	for _, raw := range []string{"https://circleci.com/gh/o/r/7", appJobURL} {
		t.Run(raw, func(t *testing.T) {
			output := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Circle-Token") != "" || r.Header.Get("Authorization") != "" {
					t.Error("a credential reached the signed output URL")
				}
				if r.Method != http.MethodGet || r.URL.Path != "/build" {
					t.Errorf("unexpected output read: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if err := json.NewEncoder(w).Encode([]map[string]string{{"message": "\x1b[31mfirst\nsecond\nlast\x1b[0m\n"}}); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(output.Close)
			routes := sequence.Routes{
				"GET /api/v2/workflow/" + urlWorkflowID: {{Body: map[string]any{"id": urlWorkflowID, "project_slug": "gh/o/r"}}},
				"GET /api/v2/workflow/" + urlWorkflowID + "/job": {{Body: map[string]any{"items": []any{
					map[string]any{"id": "another-job", "job_number": 99},
					map[string]any{"id": urlJobID, "job_number": 7},
				}}}},
				"GET /api/v2/project/gh/o/r/job/7": {{Body: map[string]any{
					"number": 7, "name": "push-go", "status": "failed", "web_url": "https://circleci.com/gh/o/r/7",
				}}},
				"GET /api/v1.1/project/github/o/r/7": {{Body: map[string]any{"build_num": 7, "steps": []any{
					map[string]any{"name": "Checkout", "actions": []any{map[string]any{"status": "success", "output_url": output.URL + "/checkout"}}},
					map[string]any{"name": "Build", "actions": []any{map[string]any{"status": "failed", "failed": true, "output_url": output.URL + "/build"}}},
				}}}},
			}
			doc, code, cc := executeURLCommand(t, routes, raw, "--lines", "1")
			if code != agentcli.ExitOK || doc.Verdict != agentcli.VerdictListed || doc.Reason != "" || doc.JobResult == nil || doc.Repository != "o/r" {
				t.Fatalf("exit %d: %+v", code, doc)
			}
			if doc.Job == nil || doc.Job.Number != 7 || doc.Job.Name != "push-go" || doc.Job.Status != "failed" || doc.Job.LogTail != "last" || doc.Job.LogError != "" {
				t.Fatalf("job: %+v", doc.Job)
			}
			if len(doc.Job.Steps) != 2 || doc.Job.Steps[0].Status != "success" || doc.Job.Steps[1].Name != "Build" || doc.Job.Steps[1].Status != "failed" {
				t.Errorf("steps: %+v", doc.Job.Steps)
			}
			reads := []string{"/api/v2/project/gh/o/r/job/7", "/api/v1.1/project/github/o/r/7"}
			if raw == appJobURL {
				reads = append(reads, "/api/v2/workflow/"+urlWorkflowID, "/api/v2/workflow/"+urlWorkflowID+"/job")
			}
			assertURLReads(t, cc, reads...)
		})
	}
}

// An unresolved App URL is exit 3, with no job and no project job read.
func TestCommandJobURLNotApplicable(t *testing.T) {
	for _, tc := range []struct {
		name        string
		missingFlow bool
		jobs        []any
		reason      string
	}{
		{name: "missing workflow", missingFlow: true, reason: "no workflow"},
		{name: "unknown job", jobs: []any{map[string]any{"id": "another-job", "job_number": 7}}, reason: "no job"},
		{name: "job not started", jobs: []any{map[string]any{"id": urlJobID, "job_number": 0}}, reason: "no number yet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := sequence.Routes{}
			reads := []string{"/api/v2/workflow/" + urlWorkflowID}
			if !tc.missingFlow {
				routes["GET /api/v2/workflow/"+urlWorkflowID] = []sequence.Response{{Body: map[string]any{"id": urlWorkflowID, "project_slug": "gh/o/r"}}}
				routes["GET /api/v2/workflow/"+urlWorkflowID+"/job"] = []sequence.Response{{Body: map[string]any{"items": tc.jobs}}}
				reads = append(reads, "/api/v2/workflow/"+urlWorkflowID+"/job")
			}
			doc, code, cc := executeURLCommand(t, routes, appJobURL)
			if code != agentcli.ExitNotApplicable || doc.Verdict != agentcli.VerdictNotApplicable || !strings.Contains(doc.Reason, tc.reason) || doc.JobResult == nil || doc.Job != nil {
				t.Errorf("exit %d: %+v", code, doc)
			}
			assertURLReads(t, cc, reads...)
		})
	}
}
