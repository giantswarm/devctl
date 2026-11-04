package merge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
)

type failedDocumentWriter struct{}

func (failedDocumentWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic output failure")
}

func TestFinishDetachedPersistence(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		brokenOutput, brokenStorage bool
		mergeCode                   int
	}{
		{name: "stdout fails", brokenOutput: true},
		{name: "storage fails", brokenStorage: true},
		{name: "merge refusal survives storage failure", brokenStorage: true, mergeCode: agentcli.ExitRefused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			job, err := mergejob.Create(root, "o", "r", 7, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := job.Save(); err != nil {
				t.Fatal(err)
			}
			callback := filepath.Join(t.TempDir(), "callback")
			var stdout io.Writer = &bytes.Buffer{}
			if tc.brokenOutput {
				stdout = failedDocumentWriter{}
			}
			r := &runner{stdout: stdout, stderr: io.Discard, flag: &flag{DetachedHandle: job.Handle, OnDone: `echo done > "` + callback + `"`}, jobRoot: func() (string, error) {
				if tc.brokenStorage {
					return filepath.Join(root, "absent"), nil
				}
				return root, nil
			}}
			doc := r.newDocument()
			doc.Repository, doc.Number = "o/r", 7
			var mergeErr error
			if tc.mergeCode != 0 {
				mergeErr = agentcli.NewExitError(tc.mergeCode, agentcli.VerdictRefused, "synthetic refusal")
			}
			err = r.finishDetached(context.Background(), &doc, mergeErr)
			if tc.brokenStorage {
				if err == nil {
					t.Error("successful merge reported success although its outcome was not persisted")
				}
				if tc.mergeCode != 0 && agentcli.Exit(err) != tc.mergeCode {
					t.Errorf("lost merge exit %d: %v", tc.mergeCode, err)
				}
				if _, err := os.Stat(callback); !os.IsNotExist(err) {
					t.Errorf("callback ran without a persisted document: %v", err)
				}
			} else {
				raw, readErr := os.ReadFile(job.DocumentPath())
				if readErr != nil {
					t.Fatal(readErr)
				}
				var saved map[string]any
				if err := json.Unmarshal(raw, &saved); err != nil {
					t.Fatalf("detached outcome lost when stdout failed: %q (%v)", raw, err)
				}
				if saved["repository"] != "o/r" || saved["exitCode"] != float64(0) {
					t.Errorf("persisted outcome: %v", saved)
				}
				if _, err := os.Stat(callback); err != nil {
					t.Errorf("callback did not run after durable completion: %v", err)
				}
				if err == nil {
					t.Error("stdout failure not returned")
				}
			}
		})
	}
}
