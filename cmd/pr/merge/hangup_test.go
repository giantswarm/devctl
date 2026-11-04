//go:build !windows

package merge

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/hanguptest"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prmerge/prmergetest"
	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
)

type hangupGitHub struct {
	*prmergetest.GitHub
	hangup     func()
	conclusion string
	reads      int
}

func (g *hangupGitHub) CheckRunsForRef(context.Context, string, string, string) ([]*github.CheckRun, error) {
	g.hangup()
	g.reads++
	if g.reads == 1 {
		return []*github.CheckRun{prwaittest.CheckRun("test", "in_progress", "")}, nil
	}
	return []*github.CheckRun{prwaittest.CheckRun("test", "completed", g.conclusion)}, nil
}

func Test_Run_survivesHangupAndClosedStdin(t *testing.T) {
	for _, tc := range []struct {
		name       string
		conclusion string
		exit       int
		verdict    agentcli.Verdict
		merges     int
	}{
		{name: "green", conclusion: "success", exit: agentcli.ExitOK, verdict: agentcli.VerdictGreen, merges: 1},
		{name: "red", conclusion: "failure", exit: agentcli.ExitRed, verdict: agentcli.VerdictRed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout := hanguptest.Run(t, tc.exit, func(hangup func()) error {
				gh := &hangupGitHub{
					GitHub: prmergetest.New(prmergetest.PullRequest("o", "r", 42, "abc123", "someone")),
					hangup: hangup, conclusion: tc.conclusion,
				}
				gh.MergeCommitSHA = "m1"
				clock := agentcli.NewVirtualClock(time.Now())
				r := &runner{
					gate:          func(bool) error { return nil },
					flag:          &flag{Timeout: 2 * time.Minute, NoReleaseWait: true},
					stdout:        os.Stdout,
					stderr:        io.Discard,
					requireGitHub: loggedIn,
					personGitHub:  loggedIn,
					endpoints:     agentcli.DefaultEndpoints,
					clock:         func() (agentcli.Clock, error) { return clock, nil },
					github: func(githubclient.Config) (clients, error) {
						return clients{github: gh, policy: prmergetest.Policy(false)}, nil
					},
				}
				err := r.Run(&cobra.Command{}, []string{"o/r", "42"})
				if gh.reads < 2 || len(gh.Merges()) != tc.merges {
					t.Fatalf("want polling to continue and %d mocked merges after SIGHUP, got %d check reads and %d merges", tc.merges, gh.reads, len(gh.Merges()))
				}
				return err
			})
			var doc document
			if err := json.Unmarshal(stdout, &doc); err != nil {
				t.Fatalf("stdout is not one document: %v\n%s", err, stdout)
			}
			if doc.Command != command || doc.ExitCode != tc.exit || doc.Verdict != tc.verdict || doc.BranchDeleted != (tc.merges == 1) {
				t.Fatalf("want the %s document after SIGHUP, got %+v", tc.name, doc)
			}
			if tc.merges == 1 && doc.MergeCommitSHA != "m1" || tc.merges == 0 && doc.MergeCommitSHA != "" {
				t.Fatalf("unexpected merge commit after %s: %+v", tc.name, doc)
			}
		})
	}
}
