package promote

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
)

const refRepository = "giantswarm/substrate"
const refWorkflowRoute = "GET /repos/" + refRepository + "/contents/.github/workflows/zz_generated.auto_release.yaml"

func refWorkflow(triggers string) string {
	return triggers + "\njobs:\n  release:\n    steps:\n      - name: Resolve the candidate to promote\n        env:\n          PROMOTE_VERSION: ${{ inputs.version }}\n"
}

func refWorkflowResponse(triggers string) []sequence.Response {
	return []sequence.Response{{Body: map[string]any{
		"type": "file", "encoding": "base64", "content": base64Encode(refWorkflow(triggers)),
	}}}
}

func refRoutes(branch string) sequence.Routes {
	root := "GET /repos/" + refRepository
	releases := []map[string]any{
		{"tag_name": "v2.0.0"},
		{"tag_name": "v2.1.0-rc.1", "prerelease": true},
		// A higher stable inherited from another series must not hide this
		// branch's candidate. The workflow uses the v1.9.Z baseline too.
		{"tag_name": "v1.10.0"},
		{"tag_name": "v1.9.8"},
		{"tag_name": "v1.9.7-rc.1", "prerelease": true},
		{"tag_name": "v1.9.6-rc.1", "prerelease": true, "draft": true},
		{"tag_name": "v1.9.5-rc.9", "prerelease": true},
		{"tag_name": "v1.9.5-rc.10", "prerelease": true},
		{"tag_name": "v1.9.4"},
		{"tag_name": "v1.9.3-rc.30", "prerelease": true},
	}
	routes := sequence.Routes{
		root:                                {{Body: map[string]any{"default_branch": "main"}}},
		refWorkflowRoute + "?ref=" + branch: refWorkflowResponse("on:\n  push:\n    branches: [main, 'release-[0-9]+.[0-9]+']\n  workflow_dispatch:"),
		root + "/releases":                  {{Body: releases}},
		"POST /repos/" + refRepository + "/actions/workflows/zz_generated.auto_release.yaml/dispatches": {{Status: http.StatusNoContent}},
	}
	for _, release := range releases {
		tag := release["tag_name"].(string)
		state := "ahead"
		if tag == "v2.0.0" || tag == "v2.1.0-rc.1" || tag == "v1.9.8" || tag == "v1.9.7-rc.1" {
			state = "diverged"
		}
		routes[root+"/compare/"+tag+"..."+branch] = []sequence.Response{{Body: map[string]any{"status": state}}}
		if strings.Contains(tag, "-rc.") {
			routes[root+"/commits/"+tag+"/status"] = []sequence.Response{{Body: map[string]any{"state": "success", "total_count": 1}}}
		}
	}
	return routes
}

type refDispatchRecorder struct {
	releasepromote.GitHub
	ref    string
	inputs map[string]any
}

func (r *refDispatchRecorder) DispatchWorkflowRun(ctx context.Context, owner, repo, file, ref string, inputs map[string]any) (githubclient.DispatchedRun, error) {
	r.ref, r.inputs = ref, inputs
	return r.GitHub.DispatchWorkflowRun(ctx, owner, repo, file, ref, inputs)
}

// Parse the actual public flags, then run the real command/engine/HTTP client
// with only the version gate and credential lookup replaced by fixtures.
func runRefCommand(t *testing.T, args []string, routes sequence.Routes) (map[string]any, *githubmock.Server, *refDispatchRecorder, error) {
	t.Helper()
	f := &flag{}
	cmd := &cobra.Command{}
	f.Init(cmd)
	require.NoError(t, cmd.ParseFlags(args))
	server, open := mockSources(t, routes)
	sources, err := open()
	require.NoError(t, err)
	recorder := &refDispatchRecorder{GitHub: sources.GitHub}
	sources.GitHub = recorder
	doc, err := runCommand(t, cmd.Flags().Args(), f, func() (*releasepromote.Sources, error) { return sources, nil })
	return doc, server, recorder, err
}

func TestRunRefMaintenance(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		name := "dispatch"
		args := []string{refRepository, "--ref", "release-1.9", "--no-follow"}
		if dryRun {
			name = "dry run"
			args = append(args, "--dry-run")
		}
		t.Run(name, func(t *testing.T) {
			doc, server, recorder, err := runRefCommand(t, args, refRoutes("release-1.9"))
			require.NoError(t, err)
			require.Equal(t, float64(agentcli.ExitOK), doc["exitCode"])
			entry := doc["repositories"].([]any)[0].(map[string]any)
			require.Equal(t, "v1.9.4", entry["stable"])
			require.Equal(t, "v1.9.5-rc.10", entry["candidate"])
			require.Contains(t, entry["message"], "on release-1.9 to promote v1.9.5-rc.10")
			if dryRun {
				require.Equal(t, "would_dispatch", entry["state"])
				require.Zero(t, dispatches(server))
			} else {
				require.Equal(t, "dispatched", entry["state"])
				require.Equal(t, 1, dispatches(server))
				require.Equal(t, "release-1.9", recorder.ref)
				require.Equal(t, map[string]any{"release-type": "stable"}, recorder.inputs)
			}
			for _, request := range server.Requests() {
				if strings.HasSuffix(request.Path, "/contents/.github/workflows/zz_generated.auto_release.yaml") {
					require.Equal(t, "release-1.9", request.Query.Get("ref"))
				}
				if strings.HasSuffix(request.Path, "/contents/UPGRADE.md") {
					require.Equal(t, "v1.9.5-rc.10", request.Query.Get("ref"))
				}
				if strings.Contains(request.Path, "/compare/") {
					require.True(t, strings.HasSuffix(request.Path, "...release-1.9"), request.Path)
				}
				if strings.Contains(request.Path, "/commits/") {
					require.True(t, strings.HasSuffix(request.Path, "/v1.9.5-rc.10/status"), request.Path)
				}
			}
		})
	}
}

func TestRunRefWorkflowTriggers(t *testing.T) {
	cases := []struct {
		name, branch, push string
		allowed            bool
	}{
		{"generated maintenance range", "release-1.9", "branches: [main, 'release-[0-9]+.[0-9]+']", true},
		{"multi digit series", "release-10.19", "branches: ['release-[0-9]+.[0-9]+']", true},
		{"exact branch", "release-1.9", "branches: [release-1.9]", true},
		{"wildcard", "release-1.9", "branches: ['release-*']", true},
		{"all branches", "release-1.9", "branches: ['**']", true},
		{"optional preceding character", "release-1.9", "branches: ['release-v?1.9']", true},
		{"escaped literal dot", "release-1.9", `branches: ['release-1\.9']`, true},
		{"wrong literal dot", "release-1.9", "branches: [release-1x9]", false},
		{"disabled maintenance", "release-1.9", "branches: [main]", false},
		{"other branch", "release-1.9", "branches: [release-1.3]", false},
		{"exclude after include", "release-1.9", "branches: ['release-*', '!release-1.9']", false},
		{"include after exclude", "release-1.9", "branches: ['release-*', '!release-1.*', 'release-1.9']", true},
		{"ignore selected branch", "release-1.9", "branches-ignore: [release-1.9]", false},
		{"ignore other branch", "release-1.9", "branches-ignore: [main]", true},
		{"tags only", "release-1.9", "tags: ['v*']", false},
		{"both branch filters", "release-1.9", "branches: ['*']\n    branches-ignore: [main]", false},
		{"malformed range", "release-1.9", "branches: ['release-[0-9']", false},
		{"feature branch", "feature", "branches: ['**']", false},
		{"partial maintenance name", "release-1.9-fix", "branches: ['**']", false},
		{"tag", "v1.9.5-rc.10", "branches: ['**']", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routes := refRoutes(tc.branch)
			routes[refWorkflowRoute+"?ref="+tc.branch] = refWorkflowResponse("on:\n  push:\n    " + tc.push + "\n  workflow_dispatch:")
			doc, server, _, err := runRefCommand(t, []string{refRepository, "--ref", tc.branch, "--dry-run"}, routes)
			if tc.allowed {
				require.NoError(t, err)
				require.NotEmpty(t, doc["repositories"])
			} else {
				require.Equal(t, agentcli.ExitUsage, agentcli.Exit(err), "%v", err)
				require.Equal(t, float64(agentcli.ExitUsage), doc["exitCode"])
				require.Contains(t, doc["reason"], "--ref")
				require.Empty(t, doc["repositories"])
				for _, request := range server.Requests() {
					require.NotContains(t, request.Path, "/releases")
					require.NotContains(t, request.Path, "/commits/")
				}
			}
			require.Zero(t, dispatches(server))
		})
	}
}

func TestRunRefPreservesCandidateChecks(t *testing.T) {
	for _, scenario := range []string{"pending", "full release", "older named", "named off branch", "already stable"} {
		t.Run(scenario, func(t *testing.T) {
			routes := refRoutes("release-1.9")
			args := []string{refRepository, "--ref", "release-1.9", "--no-follow"}
			wantState, wantCandidate := "failed", "v1.9.5-rc.10"
			switch scenario {
			case "pending":
				routes["GET /repos/"+refRepository+"/commits/v1.9.5-rc.10/status"] = []sequence.Response{{Body: map[string]any{"state": "pending", "total_count": 1}}}
				wantState = "not_built"
			case "full release":
				releases := routes["GET /repos/"+refRepository+"/releases"][0].Body.([]map[string]any)
				for _, release := range releases {
					if release["tag_name"] == "v1.9.5-rc.10" {
						release["prerelease"] = false
					}
				}
			case "older named":
				args = append(args, "--version", "1.9.5-rc.9")
				wantState, wantCandidate = "dispatched", "v1.9.5-rc.9"
			case "named off branch":
				args = append(args, "--version", "v1.9.7-rc.1")
				wantCandidate = ""
			case "already stable":
				// Publish the stable of the target: neither older RC is new.
				routes["GET /repos/"+refRepository+"/releases"] = []sequence.Response{{Body: []map[string]any{
					{"tag_name": "v1.9.5"}, {"tag_name": "v1.9.5-rc.10", "prerelease": true},
				}}}
				routes["GET /repos/"+refRepository+"/compare/v1.9.5...release-1.9"] = []sequence.Response{{Body: map[string]any{"status": "ahead"}}}
				wantState, wantCandidate = "nothing_to_promote", ""
			}
			doc, server, recorder, err := runRefCommand(t, args, routes)
			entry := doc["repositories"].([]any)[0].(map[string]any)
			require.Equal(t, wantState, entry["state"], "%v", err)
			require.Equal(t, wantCandidate, entry["candidate"])
			if scenario == "named off branch" {
				require.Contains(t, entry["message"], "v1.9.7-rc.1")
				require.Contains(t, entry["message"], "not on that branch")
			}
			if scenario == "older named" {
				require.NoError(t, err)
				require.Equal(t, []any{"v1.9.5-rc.10"}, entry["later"])
				require.Equal(t, "release-1.9", recorder.ref)
				require.Equal(t, map[string]any{"release-type": "stable", "version": "v1.9.5-rc.9"}, recorder.inputs)
				require.Equal(t, 1, dispatches(server))
			} else {
				if scenario == "already stable" {
					require.NoError(t, err)
				} else {
					require.Equal(t, agentcli.ExitRed, agentcli.Exit(err), "%v", err)
				}
				require.Zero(t, dispatches(server))
			}
		})
	}
}

func TestRunRefDefaultAndExistingOptions(t *testing.T) {
	for _, option := range [][]string{nil, {"--ref", "main"}, {"--branch", "main"}, {"--ref", "giantswarm"}} {
		name := strings.Join(option, " ")
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			routes := promoteRoutes(refRepository)
			branch := "main"
			if len(option) > 0 && option[1] == "giantswarm" {
				branch = "giantswarm"
				routes["GET /repos/"+refRepository] = []sequence.Response{{Body: map[string]any{"default_branch": branch}}}
				for route, responses := range routes {
					if strings.HasSuffix(route, "...main") {
						routes[strings.TrimSuffix(route, "main")+branch] = responses
						delete(routes, route)
					}
				}
			}
			args := append([]string{refRepository, "--no-follow"}, option...)
			doc, server, recorder, err := runRefCommand(t, args, routes)
			require.NoError(t, err)
			entry := doc["repositories"].([]any)[0].(map[string]any)
			require.Equal(t, "v1.3.0-rc.1", entry["candidate"])
			require.Equal(t, branch, recorder.ref)
			require.Equal(t, 1, dispatches(server))
		})
	}
}

func TestRunRefWorkflowReadFailures(t *testing.T) {
	cases := []struct {
		name     string
		response []sequence.Response
		code     int
	}{
		{"missing workflow", []sequence.Response{{Status: http.StatusNotFound}}, agentcli.ExitUsage},
		{"forbidden read", []sequence.Response{{Status: http.StatusForbidden, Body: map[string]any{"message": "forbidden"}}}, agentcli.ExitRed},
		{"unavailable read", []sequence.Response{{Status: http.StatusServiceUnavailable, Body: map[string]any{"message": "unavailable"}}}, agentcli.ExitRed},
		{"no dispatch trigger", refWorkflowResponse("on:\n  push:\n    branches: ['release-*']"), agentcli.ExitUsage},
		{"no push trigger", refWorkflowResponse("on:\n  workflow_dispatch:"), agentcli.ExitUsage},
		{"malformed workflow", refWorkflowResponse("on: ["), agentcli.ExitUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routes := refRoutes("release-1.9")
			routes[refWorkflowRoute+"?ref=release-1.9"] = tc.response
			doc, server, _, err := runRefCommand(t, []string{refRepository, "--ref", "release-1.9", "--no-follow"}, routes)
			require.Equal(t, tc.code, agentcli.Exit(err), "%v", err)
			require.Equal(t, float64(tc.code), doc["exitCode"])
			require.Contains(t, doc["reason"], "zz_generated.auto_release.yaml")
			require.Empty(t, doc["repositories"])
			require.Zero(t, dispatches(server))
		})
	}
}

func TestRunRefUsageBeforeReads(t *testing.T) {
	for _, args := range [][]string{
		{"--team", "team-bumblebee", "--ref", "release-1.9"},
		{refRepository, "giantswarm/other", "--ref", "release-1.9"},
		{refRepository, "--branch", "release-1.9", "--ref", "release-1.9"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			doc, server, _, err := runRefCommand(t, args, sequence.Routes{})
			require.Equal(t, agentcli.ExitUsage, agentcli.Exit(err), "%v", err)
			require.Contains(t, doc["reason"], "--ref")
			require.Empty(t, doc["repositories"])
			require.Empty(t, server.Requests())
		})
	}
}
