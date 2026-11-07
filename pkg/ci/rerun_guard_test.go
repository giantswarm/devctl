package ci

import (
	"net/http"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// --cancel does not override the hourly guard. Refusing a recent rerun must
// leave it running, rather than cancel it and only then decline another rerun.
func TestRecentRerunIsNotCanceled(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
	}{
		{name: "case 0: running rerun", status: "running"},
		{name: "case 1: failing rerun", status: "failing"},
		{name: "case 2: held rerun", status: "on_hold"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, srv := newClient(t, rerunRoutes(
				run("w2", "build", "failed", "2026-10-09T16:00:00Z"),
				run("w3", "build", tc.status, "2026-10-09T17:30:00Z"),
			))
			result, _, err := rerun(t, client, "w3", RerunOptions{Cancel: true, FromFailed: true})
			if agentcli.Exit(err) != agentcli.ExitRefused || result.Outcome != OutcomeRefused || result.LastRerun == nil || result.LastRerun.ID != "w3" || result.Canceled {
				t.Fatalf("err=%v result=%+v lastRerun=%+v; want the hourly refusal without a cancel", err, result, result.LastRerun)
			}
			if result.Workflow == nil || result.Workflow.Status != tc.status {
				t.Errorf("workflow=%+v; want its original %s status", result.Workflow, tc.status)
			}
			for _, req := range srv.Requests() {
				if req.Method == http.MethodPost {
					t.Errorf("hourly refusal sent a write: %s %s", req.Method, req.Path)
				}
			}
		})
	}
}
