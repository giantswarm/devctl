package rerun

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/project"
)

// The flags authorize canceling a running workflow and choose which jobs
// run again. Exercise New and Cobra so the parser, not a test assignment
// to runner.flag, supplies that authorization to the writes.
func TestCommandRerunFlags(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		running    bool
		cancel     bool
		fromFailed bool
		code       int
	}{
		{name: "default reruns every job"},
		{name: "from failed keeps passed jobs", args: []string{"--from-failed"}, fromFailed: true},
		{name: "running requires cancel", running: true, code: agentcli.ExitRefused},
		{name: "cancel before rerun", args: []string{"--cancel"}, running: true, cancel: true},
		{name: "cancel and rerun from failed", args: []string{"--cancel", "--from-failed"}, running: true, cancel: true, fromFailed: true},
		{name: "invalid cancel never writes", args: []string{"--cancel=invalid"}, running: true, code: agentcli.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow := func(status string) map[string]any {
				return map[string]any{
					"id": "w1", "name": "build", "status": status,
					"pipeline_id": "p1", "pipeline_number": 12, "project_slug": "gh/o/r",
					"created_at": "2026-10-09T10:00:00Z",
				}
			}
			initial := workflow("failed")
			if tc.running {
				initial = workflow("running")
			}
			script, err := sequence.New(sequence.Routes{
				"GET /api/v2/workflow/w1":          {{Body: initial}, {Body: workflow("canceled")}},
				"GET /api/v2/pipeline/p1/workflow": {{Body: map[string]any{"items": []any{initial}}}},
				"POST /api/v2/workflow/w1/cancel":  {{Status: http.StatusAccepted}},
				"POST /api/v2/workflow/w1/rerun":   {{Status: http.StatusAccepted, Body: map[string]any{"workflow_id": "w2"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var rerunBodies [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Circle-Token") != "token_test" {
					t.Error("CircleCI request did not use the fixture login")
				}
				if r.Method == http.MethodPost && r.URL.Path == "/api/v2/workflow/w1/rerun" {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					mu.Lock()
					rerunBodies = append(rerunBodies, body)
					mu.Unlock()
				}
				response, ok := script.Next(r)
				if !ok {
					response = circlemock.NotFound
				}
				if err := sequence.Write(w, r, response); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(server.Close)

			// The production constructor reads only this synthetic store and
			// loopback endpoint. The version gate is explicitly bypassed.
			t.Setenv("DEVCTL_UNSAFE_FORCE_VERSION", project.Version())
			t.Setenv(agentcli.EnvTimeScale, "1")
			t.Setenv(agentcli.EnvCircleCIAPIURL, server.URL+"/api/v2")
			store := &authstore.FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
			if err := store.Set(authstore.UserCircleCI, authstore.Record{Token: "token_test", Login: "someone"}); err != nil {
				t.Fatal(err)
			}
			t.Setenv(agentcli.EnvKeyringFile, store.Path)
			var stdout, stderr bytes.Buffer
			cmd, err := New(Config{Stdout: &stdout, Stderr: &stderr})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append([]string{"o/r", "w1"}, tc.args...))
			_, err = cmd.ExecuteC()
			if code := agentcli.Exit(err); code != tc.code {
				t.Fatalf("exit %d, want %d: %v", code, tc.code, err)
			}
			var doc document
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatalf("stdout must be one JSON document: %v", err)
			}
			if doc.Command != "ci rerun" || doc.ExitCode != tc.code || stderr.Len() != 0 {
				t.Errorf("envelope=%+v stderr=%q", doc.Envelope, stderr.String())
			}

			wantRequests := []string{}
			if tc.code != agentcli.ExitUsage {
				wantRequests = append(wantRequests, "GET /api/v2/workflow/w1", "GET /api/v2/pipeline/p1/workflow")
			}
			if tc.cancel {
				wantRequests = append(wantRequests, "POST /api/v2/workflow/w1/cancel", "GET /api/v2/workflow/w1")
			}
			if tc.code == agentcli.ExitOK {
				wantRequests = append(wantRequests, "POST /api/v2/workflow/w1/rerun")
				if doc.RerunResult == nil || doc.Repository != "o/r" || doc.Outcome != "rerun" || doc.RerunID != "w2" || doc.Canceled != tc.cancel || doc.FromFailed != tc.fromFailed {
					t.Errorf("result=%+v", doc.RerunResult)
				}
			}
			gotRequests := []string{}
			for _, request := range script.Requests() {
				gotRequests = append(gotRequests, request.Method+" "+request.Path)
			}
			if !reflect.DeepEqual(gotRequests, wantRequests) {
				t.Errorf("requests=%v, want %v", gotRequests, wantRequests)
			}
			mu.Lock()
			defer mu.Unlock()
			if tc.code != agentcli.ExitOK {
				if len(rerunBodies) != 0 {
					t.Errorf("refused/invalid call posted %d reruns", len(rerunBodies))
				}
				return
			}
			if len(rerunBodies) != 1 {
				t.Fatalf("posted %d reruns, want one", len(rerunBodies))
			}
			var body struct {
				FromFailed *bool `json:"from_failed"`
			}
			if err := json.Unmarshal(rerunBodies[0], &body); err != nil {
				t.Fatal(err)
			}
			if body.FromFailed == nil || *body.FromFailed != tc.fromFailed {
				t.Errorf("rerun body=%s, want from_failed=%t", rerunBodies[0], tc.fromFailed)
			}
		})
	}
}
