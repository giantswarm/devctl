package jobs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/cmd/ci/jobs"
	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/ci"
	"github.com/giantswarm/devctl/v8/pkg/project"
)

const (
	pipelineID   = "11111111-2222-4333-8444-555555555555"
	loginToken   = "synthetic_stored_circleci_token"
	pipelineJSON = `{"id":"` + pipelineID + `","number":12,"state":"created","project_slug":"gh/o/r",
		"vcs":{"branch":"main","revision":"abc"}}`
)

func pipelineRoutes() sequence.Routes {
	return sequence.Routes{
		"GET /api/v2/project/gh/o/r/pipeline/12": {{Body: pipelineJSON}},
		"GET /api/v2/pipeline/" + pipelineID:     {{Body: pipelineJSON}},
		"GET /api/v2/pipeline/" + pipelineID + "/workflow": {{Body: `{"items":[
			{"id":"w1","name":"build","status":"failed","created_at":"2026-10-09T10:00:00Z"}]}`}},
		"GET /api/v2/workflow/w1/job": {{Body: `{"items":[
			{"name":"lint","type":"build","status":"failed","job_number":1,
			"started_at":"2026-10-09T10:00:10Z","stopped_at":"2026-10-09T10:04:50Z"}]}`}},
	}
}

// Restrict the real command's transport to the fixture servers, including if
// endpoint wiring regresses. None of these tests may reach a live service.
type fixtureTransport struct {
	base  http.RoundTripper
	hosts map[string]bool
}

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !f.hosts[r.URL.Host] {
		return nil, fmt.Errorf("request outside test fixtures: %s", r.URL.Host)
	}
	return f.base.RoundTrip(r)
}

func commandEnvironment(t *testing.T, routes sequence.Routes, record *authstore.Record, outputURL ...string) (*circlemock.Server, string) {
	t.Helper()
	srv, err := circlemock.Start(routes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	hosts := map[string]bool{srv.Listener.Addr().String(): true}
	for _, raw := range outputURL {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		hosts[u.Host] = true
	}
	base := http.DefaultTransport
	http.DefaultTransport = fixtureTransport{base: base, hosts: hosts}
	t.Cleanup(func() { http.DefaultTransport = base })

	path := filepath.Join(t.TempDir(), "keyring.json")
	if record != nil {
		if err := (&authstore.FileStore{Path: path}).Set(authstore.UserCircleCI, *record); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(agentcli.EnvKeyringFile, path)
	t.Setenv("DEVCTL_UNSAFE_FORCE_VERSION", project.Version())
	t.Setenv(agentcli.EnvTimeScale, "0.001")
	t.Setenv(agentcli.EnvCircleCIAPIURL, srv.APIURL())
	t.Setenv(agentcli.EnvCircleCIOAuthURL, srv.URL)
	t.Setenv(agentcli.EnvGitHubAPIURL, srv.URL)
	t.Setenv(agentcli.EnvGitHubOAuthURL, srv.URL)
	t.Setenv(agentcli.EnvMusterURL, srv.URL)
	// This command requires the stored login even in CI and even if an
	// environment token is present. Never expose any host credential to it.
	t.Setenv(authstore.EnvCI, "true")
	t.Setenv(authstore.CircleCIEnvVar, "synthetic_ignored_environment_token")
	for _, key := range append([]string{"GH_TOKEN"}, authstore.GitHubEnvVars...) {
		t.Setenv(key, "")
	}
	return srv, path
}

type jobsDocument struct {
	agentcli.Envelope
	ci.JobsResult
}

func executeCommand(t *testing.T, ctx context.Context, code int, verdict agentcli.Verdict, args ...string) jobsDocument {
	t.Helper()
	var stdout, stderr bytes.Buffer
	c, err := jobs.New(jobs.Config{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	// The root command silences Cobra's human-readable errors and usage.
	c.SilenceErrors, c.SilenceUsage = true, true
	c.SetOut(&stdout)
	c.SetErr(&stderr)
	c.SetArgs(args)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = c.ExecuteContext(ctx)
	if agentcli.Exit(err) != code {
		t.Fatalf("exit = %d, want %d: %v", agentcli.Exit(err), code, err)
	}
	var doc jobsDocument
	// Unmarshal rejects extra documents and text before/after the JSON.
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("expected one JSON document: %v; output %q", err, stdout.String())
	}
	if doc.Command != "ci jobs" || doc.SchemaVersion != 1 || doc.ExitCode != code || doc.Verdict != verdict {
		t.Fatalf("unexpected envelope: %+v", doc.Envelope)
	}
	if doc.StartedAt.IsZero() || doc.FinishedAt.Before(doc.StartedAt) || doc.Warnings == nil || doc.Workflows == nil {
		t.Fatalf("incomplete document: %+v", doc)
	}
	if (code == 0) != (doc.Reason == "") {
		t.Errorf("exit %d has reason %q", code, doc.Reason)
	}
	if stderr.Len() != 0 || strings.Contains(stdout.String(), loginToken) {
		t.Error("unexpected stderr or a token in the document")
	}
	return doc
}

func assertRequests(t *testing.T, srv *circlemock.Server, paths ...string) {
	t.Helper()
	requests := srv.Requests()
	if len(requests) != len(paths) {
		t.Fatalf("got %d requests, want %d: %v", len(requests), len(paths), requests)
	}
	for i, req := range requests {
		wantPath := paths[i] // #nosec G602 -- the equal-length check above terminates the test before this loop on mismatch
		if req.Method != http.MethodGet || req.Path != wantPath || len(req.Query) != 0 {
			t.Errorf("request %d = %s, want GET %s", i, req, wantPath)
		}
		if req.Header.Get("Circle-Token") != loginToken {
			t.Errorf("request %d did not use the stored login", i)
		}
	}
}

func TestCommandReadsPipelineNumberAndID(t *testing.T) {
	for _, ref := range []string{"12", pipelineID} {
		t.Run(ref, func(t *testing.T) {
			srv, path := commandEnvironment(t, pipelineRoutes(), &authstore.Record{Token: loginToken})
			before, err := os.ReadFile(path) // #nosec G304 -- path is the synthetic store in this test's temporary directory
			if err != nil {
				t.Fatal(err)
			}
			doc := executeCommand(t, context.Background(), agentcli.ExitOK, agentcli.VerdictListed, "o/r", ref)
			if doc.Repository != "o/r" || doc.Pipeline == nil || doc.Pipeline.ID != pipelineID || doc.Pipeline.Number != 12 || len(doc.Warnings) != 0 {
				t.Fatalf("unexpected pipeline: %+v", doc)
			}
			if len(doc.Workflows) != 1 || doc.Workflows[0].Status != "failed" || !doc.Workflows[0].Latest || len(doc.Workflows[0].Jobs) != 1 || doc.Workflows[0].Jobs[0].Status != "failed" {
				t.Fatalf("failed workflow/job should be listed, with exit 0: %+v", doc.Workflows)
			}
			first := "/api/v2/pipeline/" + pipelineID
			if ref == "12" {
				first = "/api/v2/project/gh/o/r/pipeline/12"
			}
			assertRequests(t, srv, first, "/api/v2/pipeline/"+pipelineID+"/workflow", "/api/v2/workflow/w1/job")
			after, err := os.ReadFile(path) // #nosec G304 -- path is the synthetic store in this test's temporary directory
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("read changed the stored login: %v", err)
			}
		})
	}
}

func TestCommandPipelineNotApplicable(t *testing.T) {
	for _, tc := range []struct {
		name, ref, body, reason string
	}{
		{name: "missing number", ref: "12", reason: "no pipeline"},
		{name: "missing ID", ref: pipelineID, reason: "no pipeline"},
		{name: "another project", ref: pipelineID, body: strings.ReplaceAll(pipelineJSON, "gh/o/r", "gh/o/other"), reason: "belongs to gh/o/other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := sequence.Routes{}
			first := "/api/v2/pipeline/" + pipelineID
			if tc.ref == "12" {
				first = "/api/v2/project/gh/o/r/pipeline/12"
			}
			if tc.body != "" {
				routes["GET "+first] = []sequence.Response{{Body: tc.body}}
			}
			srv, _ := commandEnvironment(t, routes, &authstore.Record{Token: loginToken})
			doc := executeCommand(t, context.Background(), agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "o/r", tc.ref)
			if doc.Repository != "o/r" || doc.Pipeline != nil || len(doc.Workflows) != 0 || !strings.Contains(doc.Reason, tc.reason) {
				t.Fatalf("unexpected not-applicable result: %+v", doc)
			}
			assertRequests(t, srv, first)
		})
	}
}

func TestCommandWarningsPreserveReadableData(t *testing.T) {
	for _, tc := range []struct {
		name, warning string
		step, output  bool
	}{
		{name: "output readable", step: true, output: true},
		{name: "output expired", warning: "output of step", step: true},
		{name: "steps unavailable", warning: "steps of job"},
		{name: "jobs not visible", warning: "jobs of workflow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := sequence.Response{Body: `[{"time":"2026-10-09T10:01:00Z","message":"working"}]`}
			if !tc.output {
				response = sequence.Response{Status: http.StatusForbidden, Body: "expired"}
			}
			output, err := circlemock.Start(sequence.Routes{"GET /output": {response}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(output.Close)
			routes := pipelineRoutes()
			// The retry and the login's expiry notice must both reach the
			// same document as any warning from a partially readable view.
			routes["GET /api/v2/project/gh/o/r/pipeline/12"] = []sequence.Response{
				{Status: http.StatusServiceUnavailable}, {Body: pipelineJSON},
			}
			routes["GET /api/v2/pipeline/"+pipelineID+"/workflow"] = []sequence.Response{{Body: `{"items":[
				{"id":"w1","name":"build","status":"running","created_at":"2026-10-09T10:00:00Z"}]}`}}
			routes["GET /api/v2/workflow/w1/job"] = []sequence.Response{{Body: `{"items":[
				{"name":"publish","type":"build","status":"running","job_number":4,"started_at":"2026-10-09T10:00:10Z"}]}`}}
			routes["GET /api/v1.1/project/github/o/r/4"] = []sequence.Response{{Body: fmt.Sprintf(`{"steps":[
				{"name":"Publish","actions":[{"status":"running","start_time":"2026-10-09T10:00:20Z","output_url":%q}]}]}`, output.URL+"/output")}}
			if tc.name == "steps unavailable" {
				delete(routes, "GET /api/v1.1/project/github/o/r/4")
			}
			if tc.name == "jobs not visible" {
				delete(routes, "GET /api/v2/workflow/w1/job")
			}
			srv, _ := commandEnvironment(t, routes, &authstore.Record{Token: loginToken, ExpiresAt: time.Now().Add(48 * time.Hour)}, output.URL)
			doc := executeCommand(t, context.Background(), agentcli.ExitOK, agentcli.VerdictListed, "o/r", "12")
			warnings := strings.Join(doc.Warnings, "\n")
			wantWarnings := 2
			if tc.warning != "" {
				wantWarnings++
			}
			if len(doc.Warnings) != wantWarnings || !strings.Contains(warnings, "expires") || !strings.Contains(warnings, "503") || !strings.Contains(warnings, tc.warning) {
				t.Fatalf("warnings = %v", doc.Warnings)
			}
			if doc.Pipeline == nil || len(doc.Workflows) != 1 {
				t.Fatalf("readable pipeline/workflow lost: %+v", doc)
			}
			paths := []string{"/api/v2/project/gh/o/r/pipeline/12", "/api/v2/project/gh/o/r/pipeline/12", "/api/v2/pipeline/" + pipelineID + "/workflow", "/api/v2/workflow/w1/job"}
			if tc.name == "jobs not visible" {
				if len(doc.Workflows[0].Jobs) != 0 {
					t.Fatalf("jobs should be empty: %+v", doc.Workflows[0].Jobs)
				}
			} else {
				paths = append(paths, "/api/v1.1/project/github/o/r/4")
				if len(doc.Workflows[0].Jobs) != 1 {
					t.Fatalf("readable job lost: %+v", doc.Workflows[0].Jobs)
				}
				step := doc.Workflows[0].Jobs[0].Step
				if (step != nil) != tc.step {
					t.Fatalf("step = %+v, want present %v", step, tc.step)
				}
				if step != nil && (step.Name != "Publish" || (step.LastOutputAt != nil) != tc.output) {
					t.Fatalf("unexpected step: %+v", step)
				}
			}
			assertRequests(t, srv, paths...)
			requests := output.Requests()
			wantOutputReads := 0
			if tc.step {
				wantOutputReads = 1
			}
			if len(requests) != wantOutputReads {
				t.Fatalf("output reads = %d, want %d", len(requests), wantOutputReads)
			}
			for _, req := range requests {
				if req.Header.Get("Circle-Token") != "" || req.Header.Get("Authorization") != "" {
					t.Error("signed output request carried authentication")
				}
			}
		})
	}
}

func TestCommandReadFailureKeepsPipelineInDocument(t *testing.T) {
	routes := pipelineRoutes()
	routes["GET /api/v2/pipeline/"+pipelineID+"/workflow"] = []sequence.Response{{Body: "invalid JSON"}}
	srv, _ := commandEnvironment(t, routes, &authstore.Record{Token: loginToken})
	doc := executeCommand(t, context.Background(), agentcli.ExitUsage, agentcli.VerdictUsage, "o/r", "12")
	if doc.Repository != "o/r" || doc.Pipeline == nil || doc.Pipeline.ID != pipelineID || len(doc.Workflows) != 0 || !strings.Contains(doc.Reason, "invalid JSON") {
		t.Fatalf("unexpected partial failure document: %+v", doc)
	}
	assertRequests(t, srv, "/api/v2/project/gh/o/r/pipeline/12", "/api/v2/pipeline/"+pipelineID+"/workflow")
}

func TestCommandEarlyFailureDocuments(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		args         []string
		code         int
		verdict      agentcli.Verdict
	}{
		{name: "missing argument", args: []string{"o/r"}, code: 7, verdict: agentcli.VerdictUsage, reason: "usage:"},
		{name: "extra argument", args: []string{"o/r", "12", "extra"}, code: 7, verdict: agentcli.VerdictUsage, reason: "usage:"},
		{name: "invalid repository", args: []string{"o/r/extra", "12"}, code: 7, verdict: agentcli.VerdictUsage, reason: "repository"},
		{name: "empty pipeline", args: []string{"o/r", ""}, code: 7, verdict: agentcli.VerdictUsage, reason: "is empty"},
		{name: "unknown flag", args: []string{"o/r", "12", "--unknown"}, code: 7, verdict: agentcli.VerdictUsage, reason: "--help lists the flags"},
		{name: "missing login", args: []string{"o/r", "12"}, code: 8, verdict: agentcli.VerdictAuthRequired, reason: "no token"},
		{name: "expired login", args: []string{"o/r", "12"}, code: 8, verdict: agentcli.VerdictAuthRequired, reason: "token expired"},
		{name: "invalid clock", args: []string{"o/r", "12"}, code: 7, verdict: agentcli.VerdictUsage, reason: agentcli.EnvTimeScale},
		{name: "malformed record", args: []string{"o/r", "12"}, code: 7, verdict: agentcli.VerdictUsage, reason: "not devctl's"},
		{name: "canceled context", args: []string{"o/r", "12"}, code: 7, verdict: agentcli.VerdictUsage, reason: "context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := &authstore.Record{Token: loginToken}
			if tc.name == "missing login" {
				record = nil
			}
			if tc.name == "expired login" {
				record.ExpiresAt = time.Now().Add(-time.Hour)
			}
			srv, path := commandEnvironment(t, sequence.Routes{}, record)
			if tc.name == "invalid clock" {
				t.Setenv(agentcli.EnvTimeScale, "invalid")
			}
			// Wrong arguments and flags must win over a broken credential
			// store: there is no reason to open it for an invalid call.
			if tc.name == "malformed record" || tc.name == "missing argument" || tc.name == "extra argument" || tc.name == "invalid repository" || tc.name == "empty pipeline" || tc.name == "unknown flag" {
				if err := os.WriteFile(path, []byte("invalid JSON"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if tc.name == "canceled context" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			doc := executeCommand(t, ctx, tc.code, tc.verdict, tc.args...)
			if !strings.Contains(doc.Reason, tc.reason) || doc.Pipeline != nil || len(doc.Workflows) != 0 {
				t.Fatalf("unexpected failure document: %+v", doc)
			}
			assertRequests(t, srv)
		})
	}
}
