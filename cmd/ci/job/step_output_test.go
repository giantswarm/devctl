package job

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/internal/env"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/project"
)

// Run the public command with a synthetic login and loopback API/output. Both
// scans report success to CircleCI; only their output tells them apart.
func TestCommandStepOutput(t *testing.T) {
	testCases := []struct {
		name       string
		output     string
		status     int
		jobStatus  string
		stepOutput bool
		wantHead   string
		wantTail   string
		wantError  bool
	}{
		{
			name: "case 0: real scan in a green job",
			output: "nancy sleuth\r\nchecking dependencies\n" + strings.Repeat("progress\n", 20) +
				"Audited Dependencies: 223\nVulnerable Dependencies: 0\n",
			stepOutput: true,
			wantHead:   "nancy sleuth\nchecking dependencies",
			wantTail:   "Audited Dependencies: 223\nVulnerable Dependencies: 0",
		},
		{
			name: "case 1: soft pass in a green job",
			output: "nancy sleuth\nError: guide API request failed\n" + strings.Repeat("progress\n", 20) +
				"\x1b[31mNOT SCANNED: no Go dependency was checked for known vulnerabilities.\x1b[0m\r\nThe build is not failed for a scanner outage.\n",
			stepOutput: true,
			wantHead:   "nancy sleuth\nError: guide API request failed",
			wantTail:   "NOT SCANNED: no Go dependency was checked for known vulnerabilities.\nThe build is not failed for a scanner outage.",
		},
		{
			name:       "case 2: unreadable output preserves the steps",
			status:     http.StatusForbidden,
			stepOutput: true,
			wantError:  true,
		},
		{
			name:       "case 3: empty output",
			stepOutput: true,
		},
		{
			name:       "case 4: malformed output is unreadable",
			output:     "invalid JSON",
			status:     http.StatusOK,
			stepOutput: true,
			wantError:  true,
		},
		{
			name:   "case 5: default does not fetch successful output",
			output: "NOT SCANNED\n",
		},
		{
			name:       "case 6: failed output tail is retained",
			output:     "first\nmiddle\nfailed\n",
			jobStatus:  "failed",
			stepOutput: true,
			wantHead:   "first\nmiddle",
			wantTail:   "middle\nfailed",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			output := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads++
				if r.Method != http.MethodGet || (r.URL.Path != "/nancy" && r.URL.Path != "/after") {
					t.Errorf("unexpected output request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Circle-Token") != "" || r.Header.Get("Authorization") != "" {
					t.Error("an API credential reached signed output")
				}
				if r.URL.Path == "/after" {
					_, _ = w.Write([]byte(`[{"message":"next step\n"}]`))
					return
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.output))
					return
				}
				// Messages can split a line, including its terminal sequence.
				mid := len(tc.output) / 2
				if err := json.NewEncoder(w).Encode([]map[string]string{{"message": tc.output[:mid]}, {"message": tc.output[mid:]}}); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(output.Close)
			jobStatus := tc.jobStatus
			if jobStatus == "" {
				jobStatus = "success"
			}
			cc, err := circlemock.Start(sequence.Routes{
				"GET /api/v2/project/gh/o/r/job/7": {{Body: map[string]any{"number": 7, "name": "go-build", "status": jobStatus}}},
				"GET /api/v1.1/project/github/o/r/7": {{Body: map[string]any{"steps": []any{
					map[string]any{"name": "Check if dependencies have known security vulnerabilities", "actions": []any{
						map[string]any{"index": 0, "status": jobStatus, "failed": jobStatus == "failed", "output_url": output.URL + "/nancy"},
						map[string]any{"index": 1, "status": "not_run"},
					}},
					map[string]any{"name": "After scan", "actions": []any{map[string]any{"status": "success", "output_url": output.URL + "/after"}}},
				}}}},
			})
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
			args := []string{"o/r", "7", "--lines", "2"}
			if tc.stepOutput {
				args = append(args, "--step-output")
			}
			cmd.SetArgs(args)
			runErr := cmd.Execute()
			var doc struct {
				agentcli.Envelope
				Job struct {
					Status  string           `json:"status"`
					LogTail string           `json:"logTail"`
					Steps   []map[string]any `json:"steps"`
				} `json:"job"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatalf("one JSON document: %v", err)
			}
			if runErr != nil || doc.ExitCode != agentcli.ExitOK || doc.Verdict != agentcli.VerdictListed || doc.Command != "ci job" {
				t.Fatalf("exit %d: %+v; error=%v", agentcli.Exit(runErr), doc.Envelope, runErr)
			}
			wantLogTail := ""
			if jobStatus == "failed" {
				wantLogTail = tc.wantTail
			}
			if doc.Job.Status != jobStatus || doc.Job.LogTail != wantLogTail || len(doc.Job.Steps) != 3 {
				t.Fatalf("job: %+v", doc.Job)
			}
			want := map[string]any{"name": "Check if dependencies have known security vulnerabilities", "index": float64(0), "status": jobStatus}
			if tc.wantHead != "" {
				want["outputHead"] = tc.wantHead
			}
			if tc.wantTail != "" {
				want["outputTail"] = tc.wantTail
			}
			if tc.wantError {
				got, ok := doc.Job.Steps[0]["outputError"].(string)
				if !ok || got == "" {
					t.Errorf("missing outputError: %v", doc.Job.Steps[0])
				}
				delete(doc.Job.Steps[0], "outputError")
			}
			if diff := cmp.Diff(want, doc.Job.Steps[0]); diff != "" {
				t.Errorf("step (-want +got):\n%s", diff)
			}
			wantSkipped := map[string]any{"name": want["name"], "index": float64(1), "status": "not_run"}
			if diff := cmp.Diff(wantSkipped, doc.Job.Steps[1]); diff != "" {
				t.Errorf("parallel action without output (-want +got):\n%s", diff)
			}
			wantReads := 0
			if tc.stepOutput {
				wantReads = 2
				if jobStatus == "failed" {
					wantReads++
				}
				if doc.Job.Steps[2]["outputHead"] != "next step" || doc.Job.Steps[2]["outputTail"] != "next step" {
					t.Errorf("later step's readable output: %v", doc.Job.Steps[2])
				}
			}
			if reads != wantReads || stderr.Len() != 0 {
				t.Errorf("output reads=%d, want=%d; stderr=%q", reads, wantReads, stderr.String())
			}
			for _, req := range cc.Requests() {
				if req.Method != http.MethodGet || req.Header.Get("Circle-Token") != "token_test" ||
					(req.Path != "/api/v2/project/gh/o/r/job/7" && req.Path != "/api/v1.1/project/github/o/r/7") {
					t.Errorf("unexpected API request: %s %s", req.Method, req.Path)
				}
			}
		})
	}
}
