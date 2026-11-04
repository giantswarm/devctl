package merge

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
)

type brokenLog struct{ partial bool }

func (w brokenLog) Write(p []byte) (int, error) {
	if w.partial {
		return len(p) / 2, io.ErrClosedPipe
	}
	return 0, io.ErrClosedPipe
}

func TestDetachedDocumentSurvivesLogWriteFailure(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "no bytes written"
		if partial {
			name = "partial write"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			job, err := mergejob.Create(root, "o", "r", 42, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := job.Save(); err != nil {
				t.Fatal(err)
			}
			stderr := &bytes.Buffer{}
			r := &runner{
				flag:   &flag{DetachedHandle: job.Handle},
				stdout: brokenLog{partial: partial}, stderr: stderr,
				jobRoot: func() (string, error) { return root, nil },
			}
			doc := r.newDocument()
			doc.Repository, doc.Number, doc.MergeCommitSHA = "o/r", 42, "m1"
			mergeErr := agentcli.NewExitError(agentcli.ExitReleaseUnconfirmed, agentcli.VerdictReleaseUnconfirmed,
				"merged as m1, and the release is not confirmed")
			if err := r.finishDetached(t.Context(), &doc, mergeErr); agentcli.Exit(err) != agentcli.ExitReleaseUnconfirmed {
				t.Errorf("log failure replaced the merge outcome: %v", err)
			}
			s, err := mergejob.Load(root, job.Handle)
			if err != nil {
				t.Fatal(err)
			}
			var saved document
			if err := json.Unmarshal(s.Document, &saved); err != nil {
				t.Fatalf("saved outcome is not a complete document: %v", err)
			}
			if saved.MergeCommitSHA != "m1" || saved.ExitCode != agentcli.ExitReleaseUnconfirmed || saved.Reason != mergeErr.Reason {
				t.Errorf("saved outcome lost the merge: %+v", saved)
			}
			status, code := statusOf(t, root, job.Handle)
			if code != agentcli.ExitReleaseUnconfirmed || status["state"] != mergejob.StateFinished {
				t.Errorf("status lost the merge outcome: exit %d, %v", code, status)
			}
			if stderr.Len() == 0 {
				t.Error("log write failure was not reported")
			}
		})
	}
}
