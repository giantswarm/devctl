//go:build !windows

package watch

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/internal/hanguptest"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type hangupSequence struct {
	*sequence
	hangup func()
}

func (s hangupSequence) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	s.hangup()
	return s.sequence.Call(ctx, tool, args)
}

func Test_Run_survivesHangupAndClosedStdin(t *testing.T) {
	stdout := hanguptest.Run(t, agentcli.ExitOK, func(hangup func()) error {
		r, seq, _ := newRunner(pending, ready)
		r.flag.Output = client.OutputJSON
		r.stdout, r.stderr = os.Stdout, io.Discard
		r.open = func(context.Context) (*client.Session, error) {
			return &client.Session{Caller: hangupSequence{sequence: seq, hangup: hangup}}, nil
		}
		err := r.Run(&cobra.Command{}, []string{"new"})
		if seq.calls != 2 {
			t.Errorf("want the watch to call again after SIGHUP, got %d calls", seq.calls)
		}
		return err
	})
	var doc manager.Watch
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("stdout is not one document: %v\n%s", err, stdout)
	}
	if !doc.Ready || doc.Failure != nil || doc.Release == nil || doc.Release.Tag != "v0.1.0" {
		t.Fatalf("want the ready repository document after SIGHUP, got %+v", doc)
	}
}
