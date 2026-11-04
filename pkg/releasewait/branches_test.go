package releasewait

import (
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func TestWaitPullRequestPushBranches(t *testing.T) {
	testCases := []struct {
		name     string
		workflow string
		base     string
		wait     bool
		readBase bool
	}{
		{"case 0: fork backport excluded", "on: {push: {branches: [giantswarm]}}", "release-1.3", false, true},
		{"case 1: consumed fork branch", "on: {push: {branches: [giantswarm]}}", "giantswarm", true, true},
		{"case 2: listed backport", "on: {push: {branches: [main, release-2.x]}}", "release-2.x", true, true},
		{"case 3: glob matched backport", "on: {push: {branches: [main, 'release-[0-9]*']}}", "release-2.x", true, true},
		{"case 4: generated numeric ranges", "on: {push: {branches: ['release-v[0-9]+.[0-9]+.x']}}", "release-v12.34.x", true, true},
		{"case 5: whole branch required", "on: {push: {branches: ['release-v[0-9]+.[0-9]+.x']}}", "release-v3.7.x-backport", false, true},
		{"case 6: star stops at slash", "on: {push: {branches: ['release/*']}}", "release/2/x", false, true},
		{"case 7: double star crosses slash", "on: {push: {branches: ['release/**']}}", "release/2/x", true, true},
		{"case 8: negative pattern excludes", "on: {push: {branches: ['release-*', '!release-2.x']}}", "release-2.x", false, true},
		{"case 9: later positive includes again", "on: {push: {branches: ['release-*', '!release-2.*', 'release-2.x']}}", "release-2.x", true, true},
		{"case 10: ignored branch", "on: {push: {branches-ignore: ['release-*']}}", "release-2.x", false, true},
		{"case 11: ignored other branch", "on: {push: {branches-ignore: ['release-*']}}", "main", true, true},
		{"case 12: scalar push", "on: push", "release-2.x", true, false},
		{"case 13: sequence push", "on: [workflow_dispatch, push]", "release-2.x", true, false},
		{"case 14: null push", "on:\n  push:\n  workflow_dispatch:", "release-2.x", true, false},
		{"case 15: tag pushes only", "on: {push: {tags: ['v*']}}", "main", false, true},
		{"case 16: manual trigger only", "on: workflow_dispatch", "release-2.x", false, true},
		{"case 17: optional preceding character", "on: {push: {branches: ['release-v?2.x']}}", "release-2.x", true, true},
		{"case 18: escaped special character", `on: {push: {branches: ['release\+2']}}`, "release+2", true, true},
		{"case 19: double star directory can be absent", "on: {push: {branches: ['**/release-2.x']}}", "release-2.x", true, true},
		{"case 20: paths do not exclude a branch forever", "on: {push: {branches: [main], paths: ['src/**']}}", "main", true, true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, knownMerge := range []bool{false, true} {
				name := "read merge"
				if knownMerge {
					name = "known merge"
				}
				t.Run(name, func(t *testing.T) {
					github := branchWaitRoutes(tc.workflow, tc.base)
					if tc.wait {
						// The tag arrives on the second poll. Excluding this base
						// incorrectly would lose a release that is still being cut.
						github[repoRoute("/tags")] = append(github[repoRoute("/tags")], sequence.Response{Body: []map[string]any{
							{"name": testTag, "commit": map[string]any{"sha": testSHA}},
						}})
					}
					// The mock's rate-limit reset uses wall time. Start there
					// too, so its interval does not jump straight to the deadline.
					clock := agentcli.NewVirtualClock(time.Now())
					start := clock.Now()
					fx := fixture{
						entry: generatedEntry(t), github: github, pr: 7, timeout: time.Minute, clock: &clock,
						checkGitHub: func(requests []sequence.Request) {
							workflowReads, prReads, runReads := 0, 0, 0
							for _, request := range requests {
								switch request.Path {
								case "/repos/giantswarm/kserve/contents/.github/workflows/zz_generated.auto_release.yaml":
									workflowReads++
									if request.Query.Get("ref") != testSHA {
										t.Errorf("workflow read at %q, want merge commit", request.Query.Get("ref"))
									}
								case "/repos/giantswarm/kserve/pulls/7":
									prReads++
								case "/repos/giantswarm/kserve/actions/runs":
									runReads++
								}
							}
							wantPRReads := 1
							if knownMerge && !tc.readBase {
								wantPRReads = 0
							}
							if workflowReads != 1 || prReads != wantPRReads || (!tc.wait && runReads != 0) {
								t.Errorf("reads: workflow=%d PR=%d (want %d) runs=%d", workflowReads, prReads, wantPRReads, runReads)
							}
						},
					}
					if knownMerge {
						fx.mergeCommit = testSHA
					}
					if tc.wait {
						fx.circleci = pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}}, map[string][]map[string]any{"w1": {job("push-to-registries-release", "success")}})
						fx.registry = sequence.Routes{
							"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
							"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
						}
					}
					result, err := run(t, fx)
					if tc.wait {
						assertExit(t, err, agentcli.ExitOK, "")
						if result.Tag != testTag || !clock.Now().After(start) {
							t.Errorf("did not follow the delayed tag: %+v", result)
						}
					} else {
						assertNoRelease(t, err)
						assertExit(t, err, agentcli.ExitNotApplicable, "nothing tags a merge into "+tc.base)
						if !strings.Contains(err.Error(), "trigger on zz_generated.auto_release.yaml (") || !clock.Now().Equal(start) {
							t.Errorf("expected immediate outcome naming the trigger: %v", err)
						}
						if tc.base == "release-1.3" && !strings.Contains(err.Error(), "branches: giantswarm") {
							t.Errorf("reason must name consumed branch: %v", err)
						}
					}
					if result.SHA != testSHA || result.ReleaseModel != ReleaseModelAutoRelease {
						t.Errorf("lost merge result: %+v", result)
					}
				})
			}
		})
	}
}

func branchWaitRoutes(workflow, base string) sequence.Routes {
	routes := mergedWithoutTag([]sequence.Response{{Body: map[string]any{"total_count": 0, "workflow_runs": []any{}}}})
	file := workflowsDir + "/zz_generated.auto_release.yaml"
	routes[repoRoute("/contents/"+file)] = []sequence.Response{{Body: fileContent(file, workflow)}}
	routes[repoRoute("/pulls/7")] = []sequence.Response{{Body: map[string]any{
		"number": 7, "state": "closed", "merged": true, "merge_commit_sha": testSHA, "base": map[string]any{"ref": base},
	}}}
	return routes
}

func TestWaitPullRequestPushBranchReadFailures(t *testing.T) {
	testCases := []struct {
		name     string
		workflow string
		base     string
		status   int
	}{
		{"case 0: forbidden workflow", "on: push", "main", 403},
		{"case 1: missing listed workflow", "on: push", "main", 404},
		{"case 2: malformed workflow", "on: [", "main", 0},
		{"case 3: invalid pattern", "on: {push: {branches: ['release-[']}}", "release-2.x", 0},
		{"case 4: missing base", "on: {push: {branches: [main]}}", "", 0},
		{"case 5: conflicting branch filters", "on: {push: {branches: [main], branches-ignore: [main]}}", "main", 0},
		{"case 6: forbidden pull request", "on: {push: {branches: [main]}}", "main", 0},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			routes := branchWaitRoutes(tc.workflow, tc.base)
			if tc.status != 0 {
				routes[repoRoute("/contents/"+workflowsDir+"/zz_generated.auto_release.yaml")] = []sequence.Response{{Status: tc.status, Body: map[string]any{"message": "unavailable"}}}
			}
			if tc.name == "case 6: forbidden pull request" {
				routes[repoRoute("/pulls/7")] = []sequence.Response{{Status: 403, Body: map[string]any{"message": "unavailable"}}}
			}
			_, err := run(t, fixture{entry: generatedEntry(t), github: routes, pr: 7, mergeCommit: testSHA})
			if err == nil || IsNoRelease(err) || agentcli.Exit(err) == agentcli.ExitTimeout {
				t.Fatalf("an unreadable trigger must remain unconfirmed: %v", err)
			}
		})
	}
}

func TestWaitPullRequestPushBranchesChecksEveryWorkflow(t *testing.T) {
	for _, secondBase := range []string{"release-1.3", "main"} {
		t.Run(secondBase, func(t *testing.T) {
			routes := branchWaitRoutes("on: {push: {branches: [giantswarm]}}", "release-1.3")
			routes[repoRoute("/contents/"+workflowsDir)] = []sequence.Response{{Body: dirListing("zz_generated.auto_release.yaml", "custom-auto-release.yml")}}
			file := workflowsDir + "/custom-auto-release.yml"
			routes[repoRoute("/contents/"+file)] = []sequence.Response{{Body: fileContent(file, "on: {push: {branches: ["+secondBase+"]}}")}}
			_, err := run(t, fixture{entry: generatedEntry(t), github: routes, pr: 7, mergeCommit: testSHA, timeout: 30 * time.Second})
			if secondBase == "release-1.3" {
				assertExit(t, err, agentcli.ExitTimeout, "no auto-release run of it has started")
			} else {
				assertNoRelease(t, err)
				assertExit(t, err, agentcli.ExitNotApplicable, "custom-auto-release.yml (branches: main)")
			}
		})
	}
}

func TestWaitPullRequestPushBranchesPreservesExistingTag(t *testing.T) {
	routes := branchWaitRoutes("on: {push: {branches: [giantswarm]}}", "release-1.3")
	routes[repoRoute("/tags")] = []sequence.Response{{Body: []map[string]any{{"name": testTag, "commit": map[string]any{"sha": testSHA}}}}}
	result, err := run(t, fixture{entry: generatedEntry(t), github: routes, pr: 7, mergeCommit: testSHA, circleci: noPipelineYet(), timeout: 30 * time.Second,
		checkGitHub: func(requests []sequence.Request) {
			for _, request := range requests {
				if strings.HasSuffix(request.Path, "/zz_generated.auto_release.yaml") || strings.HasSuffix(request.Path, "/pulls/7") {
					t.Errorf("existing tag does not need push filters: %s", request.String())
				}
			}
		},
	})
	assertExit(t, err, agentcli.ExitTimeout, "")
	if result.Tag != testTag || IsNoRelease(err) {
		t.Fatalf("existing tag was lost: %+v, %v", result, err)
	}
}
