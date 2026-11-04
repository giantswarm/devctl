package merge

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

type timedReleaseStatuses struct {
	*releasewaittest.GitHub
	clock agentcli.Clock
	reads []time.Time
}

func (g *timedReleaseStatuses) ListCommitStatuses(ctx context.Context, owner, repo, sha string) ([]githubclient.CommitStatus, error) {
	g.reads = append(g.reads, g.clock.Now())
	return g.GitHub.ListCommitStatuses(ctx, owner, repo, sha)
}

const circleConfig = `version: 2.1
workflows:
  ci:
    jobs:
      - build:
          filters:
            tags: {only: /^v.*/}
`

// Both the PR and its release carry CircleCI configuration. The published
// release is still pending until its tag job posts a successful status.
func Test_run_circleCIFromStatuses(t *testing.T) {
	for _, tc := range []struct {
		state     string
		failedLog bool
		exit      int
		verdict   string
		merges    int
	}{
		{state: "success", verdict: "green", merges: 1},
		{state: "failure", exit: agentcli.ExitRed, verdict: "red"},
		{state: "success", failedLog: true, exit: agentcli.ExitAuthRequired, verdict: "auth_required"},
	} {
		name := tc.state
		if tc.failedLog {
			name += " with failed logs"
		}
		t.Run(name, func(t *testing.T) {
			routes := green()
			routes["GET /repos/o/r/contents/.circleci/config.yml?ref=abc123"] = []sequence.Response{{Body: map[string]any{
				"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(circleConfig)),
			}}}
			routes["GET /repos/o/r/commits/abc123/status"] = []sequence.Response{{Body: map[string]any{"statuses": []any{
				map[string]any{"context": "ci/circleci: build", "state": tc.state, "target_url": "https://circleci.com/gh/o/r/1"},
			}}}}
			r, stdout, stderr, server := newRunner(t, routes, loggedIn, nil)
			cmd := &cobra.Command{}
			r.flag.Init(cmd)
			if err := cmd.ParseFlags([]string{"--circleci-from-statuses", "--progress"}); err != nil {
				t.Fatal(err)
			}
			if tc.failedLog {
				if err := cmd.ParseFlags([]string{"--failed-log"}); err != nil {
					t.Fatal(err)
				}
			} else {
				r.requireCircleCI = nil
			}
			clock := agentcli.NewVirtualClock(time.Now())
			r.clock = func() (agentcli.Clock, error) { return clock, nil }
			var releaseGitHub *timedReleaseStatuses
			r.github = func(config githubclient.Config) (clients, error) {
				c, err := newClients(config)
				releaseGitHub = &timedReleaseStatuses{clock: clock, GitHub: &releasewaittest.GitHub{
					Tags: map[string]string{"v1.2.3": "m1"},
					Files: map[string]string{
						".circleci/config.yml":                             circleConfig,
						".github/workflows/zz_generated.auto_release.yaml": "on: push\n",
					},
					Releases: []githubclient.Release{{Tag: "v1.2.3", Published: true}},
					StatusReads: [][]githubclient.CommitStatus{
						{},
						{{Context: "ci/circleci: build", State: "success", TargetURL: "https://circleci.com/gh/o/r/2"}},
					},
				}}
				c.release = releaseGitHub
				return c, err
			}

			err := r.run(t.Context(), []string{"o/r", "42"})
			if agentcli.Exit(err) != tc.exit {
				t.Fatalf("want exit %d, got %v\n%s", tc.exit, err, stdout)
			}
			doc := decode(t, stdout)
			if doc["verdict"] != tc.verdict {
				t.Fatalf("document: %v", doc)
			}
			if tc.failedLog && !strings.Contains(doc["reason"].(string), "devctl auth login --circleci-only") {
				t.Errorf("CircleCI log access should name the login: %v", doc)
			}
			merges := 0
			for _, req := range server.Requests() {
				if req.Method == "PUT" && req.Path == "/repos/o/r/pulls/42/merge" {
					merges++
				}
			}
			if merges != tc.merges {
				t.Errorf("want %d merge calls, got %d", tc.merges, merges)
			}
			if tc.merges > 0 {
				release, _ := doc["release"].(map[string]any)
				if release["verdict"] != "available" || release["tag"] != "v1.2.3" {
					t.Errorf("release should wait for its tag job: %v\n%s", release, stderr)
				}
				if reads := releaseGitHub.reads; len(reads) < 2 || reads[1].Sub(reads[0]) < releasewait.IntervalFloor {
					t.Errorf("release did not wait between the missing and successful tag status: %v", reads)
				}
			}
		})
	}
}

func TestCircleCIFromStatusesReachesDetachedMerge(t *testing.T) {
	f := &flag{}
	cmd := &cobra.Command{}
	f.Init(cmd)
	if err := cmd.ParseFlags([]string{"--circleci-from-statuses", "--detach"}); err != nil {
		t.Fatal(err)
	}
	args := f.childArgs("o/r", 42, "handle")
	if !slices.Contains(args, "--circleci-from-statuses") {
		t.Errorf("detached merge lost status mode: %v", args)
	}
}
