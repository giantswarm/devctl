//go:build unix

package wait

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/interrupttest"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
)

func Test_Run_interrupted(t *testing.T) {
	for _, sig := range interrupttest.Signals {
		t.Run(sig.String(), func(t *testing.T) {
			doc := interrupttest.Run(t, agentcli.ExitTimeout, func() error {
				gh := &interruptedWaitGitHub{
					GitHub:    &prwaittest.GitHub{Heads: []prwaittest.Head{{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("go-build", "in_progress", "")}}}},
					interrupt: func(ctx context.Context) error { return interrupttest.Send(ctx, sig) },
				}
				r := &runner{
					flag: &flag{Timeout: time.Hour}, stdout: os.Stdout, stderr: os.Stderr,
					gate: func(bool) error { return nil }, requireGitHub: loggedIn, personGitHub: loggedIn,
					endpoints: agentcli.DefaultEndpoints,
					clock:     func() (agentcli.Clock, error) { return agentcli.NewClock(0.001, nil), nil },
					github: func(githubclient.Config) (prwait.GitHub, prwait.RateSource, prwait.EntryFinder, error) {
						return gh, nil, nil, nil
					},
				}
				return r.Run(&cobra.Command{}, []string{"o/r", "42"})
			})
			if doc["verdict"] != "timeout" || doc["headSha"] != "abc123" || !strings.Contains(doc["reason"].(string), "interrupted") || len(doc["checks"].([]any)) != 1 || len(doc["unfinished"].([]any)) == 0 {
				t.Fatalf("interrupted wait lost its pending head: %v", doc)
			}
		})
	}
}

type interruptedWaitGitHub struct {
	*prwaittest.GitHub
	interrupt func(context.Context) error
	reads     int
}

func (g *interruptedWaitGitHub) CheckRunsForRef(ctx context.Context, owner, repo, ref string) ([]*github.CheckRun, error) {
	g.reads++
	if g.reads == 2 {
		return nil, g.interrupt(ctx)
	}
	return g.GitHub.CheckRunsForRef(ctx, owner, repo, ref)
}
