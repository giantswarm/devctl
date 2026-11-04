package merge

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
)

func TestFailedOnDonePreservesMergedOutcome(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mergeErr  error
		cancelled bool
		code      int
		verdict   string
		reason    string
	}{
		{name: "merged and released", code: agentcli.ExitOK, verdict: "green"},
		{
			name: "merged with a cancelled release wait", cancelled: true,
			mergeErr: agentcli.NewExitError(agentcli.ExitReleaseUnconfirmed, agentcli.VerdictReleaseUnconfirmed,
				"merged as m1, and the release is not confirmed"),
			code: agentcli.ExitReleaseUnconfirmed, verdict: "release_unconfirmed",
			reason: "merged as m1, and the release is not confirmed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			job, err := mergejob.Create(root, "o", "r", 42, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := job.Save(); err != nil {
				t.Fatal(err)
			}
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			r := &runner{
				flag:   &flag{DetachedHandle: job.Handle, OnDone: "exit 23"},
				stdout: stdout, stderr: stderr,
				jobRoot: func() (string, error) { return root, nil },
			}
			doc := r.newDocument()
			doc.Repository, doc.Number, doc.MergeCommitSHA = "o/r", 42, "m1"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			if err := r.finishDetached(ctx, &doc, tc.mergeErr); agentcli.Exit(err) != tc.code {
				t.Fatalf("failed hook changed the merge's exit code: %v", err)
			}
			status, code := statusOf(t, root, job.Handle)
			if code != tc.code || status["state"] != mergejob.StateFinished || status["verdict"] != tc.verdict || status["reason"] != tc.reason {
				t.Errorf("status changed the merge outcome: exit %d, %v", code, status)
			}
			merged, _ := status["merge"].(map[string]any)
			if merged["mergeCommitSha"] != "m1" || merged["exitCode"] != float64(tc.code) || merged["verdict"] != tc.verdict || merged["reason"] != tc.reason {
				t.Errorf("saved merge outcome: %v", merged)
			}
			hook, _ := status["onDone"].(map[string]any)
			if hook["command"] != "exit 23" || hook["exitCode"] != float64(23) || hook["error"] == nil || hook["error"] == "" {
				t.Errorf("failed hook was not recorded separately: %v", hook)
			}
			if output := decode(t, stdout); output["exitCode"] != float64(tc.code) || output["mergeCommitSha"] != "m1" {
				t.Errorf("stdout lost the merged outcome: %v", output)
			}
		})
	}
}
