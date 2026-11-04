//go:build unix

package merge

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
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prmerge/prmergetest"
	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

func Test_Run_interrupted(t *testing.T) {
	for _, phase := range []string{"github_login", "before_merge", "branch_cleanup", "release_wait"} {
		afterMerge := phase == "branch_cleanup" || phase == "release_wait"
		code := agentcli.ExitTimeout
		if afterMerge {
			code = agentcli.ExitReleaseUnconfirmed
		}
		t.Run(phase, func(t *testing.T) {
			for _, sig := range interrupttest.Signals {
				t.Run(sig.String(), func(t *testing.T) {
					doc := interrupttest.Run(t, code, func() error {
						sources := interrupttest.ReleaseSources(t, nil)
						status, conclusion := "in_progress", ""
						if afterMerge {
							status, conclusion = "completed", "success"
						}
						gh := &interruptedMergeGitHub{
							GitHub: prmergetest.New(prmergetest.PullRequest("o", "r", 42, "abc123", "someone"), prwaittest.Head{
								CheckRuns: []*github.CheckRun{prwaittest.CheckRun("go-build", status, conclusion)},
							}),
							interrupt: func(ctx context.Context) error { return interrupttest.Send(ctx, sig) },
							cleanup:   phase == "branch_cleanup",
						}
						gh.MergeCommitSHA = releasewaittest.ReleaseSHA
						login := loggedIn
						if phase == "github_login" {
							login = func(ctx context.Context) (authstore.Token, error) {
								if err := interrupttest.Send(ctx, sig); err != context.Canceled {
									t.Fatalf("signal did not cancel login: %v", err)
								}
								// PersonGitHub reports a failed subprocess as auth_required,
								// without retaining its cancellation error.
								return authstore.Token{}, &authstore.AuthRequiredError{Identity: "GitHub (gh login)", Cause: "gh has no login for github.com", Hint: "gh auth login"}
							}
						}
						r := &runner{
							flag:   &flag{Timeout: time.Hour, ReleaseTimeout: time.Hour},
							stdout: os.Stdout, stderr: os.Stderr, gate: func(bool) error { return nil },
							requireGitHub: login, personGitHub: login,
							requireCircleCI: func(ctx context.Context) (authstore.Token, error) {
								if len(gh.Merges()) != 1 {
									t.Fatal("release wait reached without exactly one merge")
								}
								return authstore.Token{}, interrupttest.Send(ctx, sig)
							},
							endpoints: agentcli.DefaultEndpoints,
							clock:     func() (agentcli.Clock, error) { return agentcli.NewClock(0.001, nil), nil },
							github: func(githubclient.Config) (clients, error) {
								return clients{github: gh, policy: prmergetest.Policy(false), entries: sources.Entries, release: sources.GitHub}, nil
							},
						}
						err := r.Run(&cobra.Command{}, []string{"o/r", "42"})
						if !afterMerge && len(gh.Merges()) != 0 {
							t.Fatal("interrupted wait merged the pull request")
						}
						return err
					})
					if doc["merged"] != afterMerge || !strings.Contains(doc["reason"].(string), "interrupted") {
						t.Fatalf("interruption document: %v", doc)
					}
					if afterMerge {
						if doc["verdict"] != "release_unconfirmed" || doc["mergeCommitSha"] != releasewaittest.ReleaseSHA || !strings.Contains(doc["reason"].(string), "devctl release wait o/r --pr 42") {
							t.Fatalf("merged release is unconfirmed: %v", doc)
						}
						if phase == "release_wait" {
							release := doc["release"].(map[string]any)
							if release["verdict"] != "timeout" || release["tag"] != "v1.2.3" || !strings.Contains(release["reason"].(string), "CircleCI pipeline") {
								t.Fatalf("interrupted release lost its state: %v", doc)
							}
						} else if doc["branchDeleted"] != false || doc["release"] != nil {
							t.Fatalf("interrupted cleanup claims to have finished: %v", doc)
						}
					} else {
						if doc["mergeCommitSha"] != "" || doc["release"] != nil || doc["verdict"] != "timeout" {
							t.Fatalf("pre-merge partial state: %v", doc)
						}
						if phase == "before_merge" && (len(doc["unfinished"].([]any)) == 0 || !strings.Contains(doc["reason"].(string), "go-build")) {
							t.Fatalf("pre-merge wait lost its pending checks: %v", doc)
						}
						if phase == "github_login" && (len(doc["checks"].([]any)) != 0 || doc["repository"] != "o/r") {
							t.Fatalf("interrupted login lost its invocation: %v", doc)
						}
					}
				})
			}
		})
	}
}

type interruptedMergeGitHub struct {
	*prmergetest.GitHub
	interrupt func(context.Context) error
	reads     int
	cleanup   bool
}

func (g *interruptedMergeGitHub) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	if g.cleanup {
		return g.interrupt(ctx)
	}
	return g.GitHub.DeleteBranch(ctx, owner, repo, branch)
}

func (g *interruptedMergeGitHub) CheckRunsForRef(ctx context.Context, owner, repo, ref string) ([]*github.CheckRun, error) {
	g.reads++
	if g.reads == 2 {
		return nil, g.interrupt(ctx)
	}
	return g.GitHub.CheckRunsForRef(ctx, owner, repo, ref)
}
