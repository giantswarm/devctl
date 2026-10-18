package promote

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
)

// runCommand runs the command with the sources open returns and decodes the
// one document on stdout.
func runCommand(t *testing.T, args []string, f *flag, open func() (*releasepromote.Sources, error)) (map[string]any, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	r := &runner{
		gate:   func(bool) error { return nil },
		flag:   f,
		stdout: &stdout,
		stderr: &stderr,
		open: func(context.Context, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasepromote.Sources, error) {
			return open()
		},
	}
	err := r.run(t.Context(), args)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc), "stdout is not one JSON document:\n%s", stdout.String())
	require.Empty(t, stderr.String(), "stderr stays empty without --progress")
	return doc, err
}

func refuseToOpen() (*releasepromote.Sources, error) {
	return nil, errors.New("must not be opened")
}

func TestRunUsageErrorsAreDocuments(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		flag   flag
		reason string
	}{
		{name: "neither repositories nor team", reason: "pass exactly one of repositories and --team"},
		{name: "both repositories and team", args: []string{"giantswarm/devctl"}, flag: flag{Team: "team-bumblebee"}, reason: "pass exactly one of repositories and --team"},
		{name: "no slash", args: []string{"giantswarm/devctl", "devctl"}, reason: `"devctl" is not a repository: expected owner/repo`},
		{name: "too many slashes", args: []string{"giantswarm/devctl/x"}, reason: "expected owner/repo"},
		{name: "a version that is no candidate tag", args: []string{"giantswarm/devctl"}, flag: flag{Version: "v1.2.3"}, reason: `--version "v1.2.3" is not a release candidate tag: expected vX.Y.Z-rc.N`},
		{name: "a version for two repositories", args: []string{"giantswarm/devctl", "giantswarm/klaus"}, flag: flag{Version: "v1.2.3-rc.14"}, reason: "--version names a candidate of one repository"},
		{name: "a version for a team", flag: flag{Team: "team-bumblebee", Version: "v1.2.3-rc.14"}, reason: "--version names a candidate of one repository"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.flag
			doc, err := runCommand(t, tc.args, &f, refuseToOpen)
			require.Equal(t, agentcli.ExitUsage, agentcli.Exit(err), "%v", err)
			require.Equal(t, "release promote", doc["command"])
			require.Equal(t, string(agentcli.VerdictUsage), doc["verdict"])
			require.Equal(t, float64(agentcli.ExitUsage), doc["exitCode"])
			require.Contains(t, doc["reason"], tc.reason)
			require.Equal(t, []any{}, doc["repositories"])
		})
	}
}

func TestRunAuthRequiredIsExit8(t *testing.T) {
	authErr := &authstore.AuthRequiredError{Identity: "GitHub", Cause: "no token in the keychain", Hint: "devctl auth login --github-only"}
	doc, err := runCommand(t, []string{"giantswarm/devctl"}, &flag{}, func() (*releasepromote.Sources, error) { return nil, authErr })
	require.Equal(t, agentcli.ExitAuthRequired, agentcli.Exit(err))
	require.Equal(t, string(agentcli.VerdictAuthRequired), doc["verdict"])
	require.Contains(t, doc["reason"], "devctl auth login")
}

// promoteRoutes script one auto-release repository with a built candidate
// on its default branch.
func promoteRoutes(repository string) sequence.Routes {
	workflow := "jobs:\n  release:\n    steps:\n      - name: Resolve the candidate to promote\n"
	return sequence.Routes{
		"GET /repos/" + repository: {{Body: map[string]any{"name": repository, "default_branch": "main"}}},
		"GET /repos/" + repository + "/contents/.github/workflows/zz_generated.auto_release.yaml": {{Body: map[string]any{
			"type": "file", "encoding": "base64", "path": ".github/workflows/zz_generated.auto_release.yaml", "content": base64Encode(workflow),
		}}},
		"GET /repos/" + repository + "/releases":                   {{Body: []map[string]any{{"tag_name": "v1.3.0-rc.1", "prerelease": true}, {"tag_name": "v1.2.0"}}}},
		"GET /repos/" + repository + "/compare/v1.2.0...main":      {{Body: map[string]any{"status": "ahead"}}},
		"GET /repos/" + repository + "/compare/v1.3.0-rc.1...main": {{Body: map[string]any{"status": "identical"}}},
		"GET /repos/" + repository + "/commits/v1.3.0-rc.1/status": {{Body: map[string]any{"state": "success", "total_count": 1}}},
		"POST /repos/" + repository + "/actions/workflows/zz_generated.auto_release.yaml/dispatches": {{Body: map[string]any{
			"workflow_run_id": 7, "html_url": "https://github.com/" + repository + "/actions/runs/7",
		}}},
		"GET /repos/" + repository + "/actions/runs/7": {{Body: map[string]any{
			"id": 7, "status": "completed", "conclusion": "success", "html_url": "https://github.com/" + repository + "/actions/runs/7",
		}}},
	}
}

func mockSources(t *testing.T, routes sequence.Routes) (*githubmock.Server, func() (*releasepromote.Sources, error)) {
	t.Helper()
	server, err := githubmock.Start(routes)
	require.NoError(t, err)
	t.Cleanup(server.Close)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	gh, _, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
	require.NoError(t, err)
	return server, func() (*releasepromote.Sources, error) {
		return &releasepromote.Sources{
			GitHub: gh,
			Team: func(ctx context.Context, team string) ([]string, error) {
				return releasepromote.TeamRepositories(ctx, gh.GitHub(), team)
			},
			Identity: releasepromote.Identity{Source: authstore.SourceKeychain, Login: "octocat"},
		}, nil
	}
}

func dispatches(server *githubmock.Server) int {
	n := 0
	for _, r := range server.Requests() {
		if r.Method == http.MethodPost {
			n++
		}
	}
	return n
}

func TestRunDispatchesAndReports(t *testing.T) {
	server, open := mockSources(t, promoteRoutes("giantswarm/kserve"))
	doc, err := runCommand(t, []string{"giantswarm/kserve"}, &flag{}, open)
	require.NoError(t, err)
	require.Equal(t, string(agentcli.VerdictGreen), doc["verdict"])
	require.Equal(t, float64(agentcli.ExitOK), doc["exitCode"])
	require.Equal(t, false, doc["dryRun"])
	require.Equal(t, []any{map[string]any{
		"repository":  "giantswarm/kserve",
		"stable":      "v1.2.0",
		"candidate":   "v1.3.0-rc.1",
		"statusState": "success",
		"state":       "dispatched",
		"message":     "dispatched zz_generated.auto_release.yaml on main to promote v1.3.0-rc.1",
		"run":         map[string]any{"id": float64(7), "url": "https://github.com/giantswarm/kserve/actions/runs/7", "status": "completed", "conclusion": "success"},
	}}, doc["repositories"])
	require.Equal(t, 1, dispatches(server))
}

// The stable gate refusing in the dispatched run is exit 1 at the run's
// conclusion, the run's URL and the gate's refusal in the message.
func TestRunGateRefusalIsExit1(t *testing.T) {
	routes := promoteRoutes("giantswarm/kserve")
	routes["GET /repos/giantswarm/kserve/actions/runs/7"] = []sequence.Response{
		{Body: map[string]any{"id": 7, "status": "in_progress", "html_url": "https://github.com/giantswarm/kserve/actions/runs/7"}},
		{Body: map[string]any{"id": 7, "status": "completed", "conclusion": "failure", "html_url": "https://github.com/giantswarm/kserve/actions/runs/7"}},
	}
	routes["GET /repos/giantswarm/kserve/actions/runs/7/jobs"] = []sequence.Response{{Body: map[string]any{"total_count": 1, "jobs": []map[string]any{
		{"id": 71, "name": "Tag", "status": "completed", "conclusion": "failure", "html_url": "https://github.com/giantswarm/kserve/actions/runs/7/job/71"},
	}}}}
	routes["GET /repos/giantswarm/kserve/check-runs/71/annotations"] = []sequence.Response{{Body: []map[string]any{
		{"annotation_level": "failure", "title": "Stable gate refused", "message": ".github/release-gate.custom.sh refused v1.3.0: no tag and no release are cut."},
		{"annotation_level": "failure", "message": "Process completed with exit code 1."},
	}}}
	_, open := mockSources(t, routes)
	t.Setenv(agentcli.EnvTimeScale, "0.001")
	doc, err := runCommand(t, []string{"giantswarm/kserve"}, &flag{}, open)
	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err), "%v", err)
	require.Equal(t, "1 of 1 repositories not promoted: giantswarm/kserve (refused)", doc["reason"])
	entry := doc["repositories"].([]any)[0].(map[string]any)
	require.Equal(t, "refused", entry["state"])
	require.Equal(t, "the run https://github.com/giantswarm/kserve/actions/runs/7 promoting v1.3.0-rc.1 concluded failure: no stable release is cut; Stable gate refused: .github/release-gate.custom.sh refused v1.3.0: no tag and no release are cut.", entry["message"])
	job := entry["failedJobs"].([]any)[0].(map[string]any)
	require.Equal(t, "Tag", job["name"])
	require.Equal(t, "https://github.com/giantswarm/kserve/actions/runs/7/job/71", job["url"])
}

// --no-follow returns right after the dispatch: the run is never read.
func TestRunNoFollowReadsNoRun(t *testing.T) {
	routes := promoteRoutes("giantswarm/kserve")
	delete(routes, "GET /repos/giantswarm/kserve/actions/runs/7")
	server, open := mockSources(t, routes)
	doc, err := runCommand(t, []string{"giantswarm/kserve"}, &flag{NoFollow: true}, open)
	require.NoError(t, err)
	entry := doc["repositories"].([]any)[0].(map[string]any)
	require.Equal(t, "dispatched", entry["state"])
	require.Equal(t, map[string]any{"id": float64(7), "url": "https://github.com/giantswarm/kserve/actions/runs/7", "status": "", "conclusion": ""}, entry["run"])
	require.Equal(t, 1, dispatches(server))
}

func TestRunDryRunDispatchesNothing(t *testing.T) {
	server, open := mockSources(t, promoteRoutes("giantswarm/kserve"))
	doc, err := runCommand(t, []string{"giantswarm/kserve"}, &flag{DryRun: true}, open)
	require.NoError(t, err)
	require.Equal(t, true, doc["dryRun"])
	require.Equal(t, "would_dispatch", doc["repositories"].([]any)[0].(map[string]any)["state"])
	require.Zero(t, dispatches(server))
}

func TestRunNotAutoReleaseIsExit1(t *testing.T) {
	routes := promoteRoutes("giantswarm/kserve")
	delete(routes, "GET /repos/giantswarm/kserve/contents/.github/workflows/zz_generated.auto_release.yaml")
	server, open := mockSources(t, routes)
	doc, err := runCommand(t, []string{"giantswarm/kserve"}, &flag{}, open)
	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.Equal(t, string(agentcli.VerdictRed), doc["verdict"])
	require.Equal(t, "1 of 1 repositories not promoted: giantswarm/kserve (not_auto_release)", doc["reason"])
	require.Zero(t, dispatches(server))
}

func TestRunTeam(t *testing.T) {
	teamFile := "- name: kserve\n  gen:\n    flavours: [app]\n    language: go\n    ci:\n      generate: true\n- name: legacy\n  gen:\n    flavours: [app]\n    language: go\n"
	routes := promoteRoutes("giantswarm/kserve")
	routes["GET /repos/giantswarm/github/contents/repositories/team-bumblebee.yaml"] = []sequence.Response{{Body: map[string]any{
		"type": "file", "encoding": "base64", "path": "repositories/team-bumblebee.yaml", "name": "team-bumblebee.yaml",
		"content": base64Encode(teamFile),
	}}}
	server, open := mockSources(t, routes)
	doc, err := runCommand(t, nil, &flag{Team: "team-bumblebee"}, open)
	require.NoError(t, err)
	require.Equal(t, "team-bumblebee", doc["team"])
	repositories := doc["repositories"].([]any)
	require.Len(t, repositories, 1)
	require.Equal(t, "giantswarm/kserve", repositories[0].(map[string]any)["repository"])
	require.Equal(t, 1, dispatches(server))
}

func TestRunDispatchesWithTheLogin(t *testing.T) {
	server, open := mockSources(t, promoteRoutes("giantswarm/kserve"))
	doc, err := runCommand(t, []string{"giantswarm/kserve"}, &flag{}, open)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"source": authstore.SourceKeychain, "login": "octocat"}, doc["identity"])
	require.Equal(t, "dispatched", doc["repositories"].([]any)[0].(map[string]any)["state"])
	require.Equal(t, 1, dispatches(server))
}

func TestRunRemovesDuplicateRepositories(t *testing.T) {
	routes := promoteRoutes("giantswarm/kserve")
	for key, responses := range promoteRoutes("giantswarm/klaus") {
		routes[key] = responses
	}
	server, open := mockSources(t, routes)
	doc, err := runCommand(t, []string{"giantswarm/kserve", "giantswarm/klaus", "giantswarm/kserve"}, &flag{}, open)
	require.NoError(t, err)
	var repositories []string
	for _, r := range doc["repositories"].([]any) {
		repositories = append(repositories, r.(map[string]any)["repository"].(string))
	}
	require.Equal(t, []string{"giantswarm/kserve", "giantswarm/klaus"}, repositories)
	require.Equal(t, 2, dispatches(server))
}

func TestRunTeamWithoutAutoReleaseRepositoriesWarns(t *testing.T) {
	teamFile := "- name: legacy\n  gen:\n    flavours: [app]\n    language: go\n"
	_, open := mockSources(t, sequence.Routes{
		"GET /repos/giantswarm/github/contents/repositories/team-bumblebee.yaml": {{Body: map[string]any{
			"type": "file", "encoding": "base64", "path": "repositories/team-bumblebee.yaml", "name": "team-bumblebee.yaml",
			"content": base64Encode(teamFile),
		}}},
	})
	doc, err := runCommand(t, nil, &flag{Team: "team-bumblebee"}, open)
	require.NoError(t, err)
	require.Equal(t, []any{}, doc["repositories"])
	require.Equal(t, []any{"the team file of team-bumblebee declares no auto-release repository: nothing to promote"}, doc["warnings"])
}

// streamRoutes script a repository merging steadily: rc.15 is building,
// rc.14 is built, on a workflow that takes the version input.
func streamRoutes(repository string) sequence.Routes {
	workflow := "jobs:\n  release:\n    steps:\n      - name: Resolve the candidate to promote\n        env:\n          PROMOTE_VERSION: ${{ inputs.version }}\n"
	routes := promoteRoutes(repository)
	routes["GET /repos/"+repository+"/contents/.github/workflows/zz_generated.auto_release.yaml"] = []sequence.Response{{Body: map[string]any{
		"type": "file", "encoding": "base64", "path": ".github/workflows/zz_generated.auto_release.yaml", "content": base64Encode(workflow),
	}}}
	routes["GET /repos/"+repository+"/releases"] = []sequence.Response{{Body: []map[string]any{{"tag_name": "v1.2.3-rc.15", "prerelease": true}, {"tag_name": "v1.2.3-rc.14", "prerelease": true}, {"tag_name": "v1.2.2"}}}}
	routes["GET /repos/"+repository+"/compare/v1.2.2...main"] = []sequence.Response{{Body: map[string]any{"status": "ahead"}}}
	routes["GET /repos/"+repository+"/compare/v1.2.3-rc.15...main"] = []sequence.Response{{Body: map[string]any{"status": "identical"}}}
	routes["GET /repos/"+repository+"/compare/v1.2.3-rc.14...main"] = []sequence.Response{{Body: map[string]any{"status": "ahead"}}}
	routes["GET /repos/"+repository+"/commits/v1.2.3-rc.15/status"] = []sequence.Response{{Body: map[string]any{"state": "pending", "total_count": 2}}}
	routes["GET /repos/"+repository+"/commits/v1.2.3-rc.14/status"] = []sequence.Response{{Body: map[string]any{"state": "success", "total_count": 2}}}
	return routes
}

// With rc.15 building and rc.14 built, --version v1.2.3-rc.14 promotes rc.14
// and names rc.15 as left out.
func TestRunVersionPromotesTheNamedCandidate(t *testing.T) {
	server, open := mockSources(t, streamRoutes("giantswarm/kserve"))
	doc, err := runCommand(t, []string{"giantswarm/kserve"}, &flag{Version: "v1.2.3-rc.14"}, open)
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{
		"repository":  "giantswarm/kserve",
		"stable":      "v1.2.2",
		"candidate":   "v1.2.3-rc.14",
		"later":       []any{"v1.2.3-rc.15"},
		"statusState": "success",
		"state":       "dispatched",
		"message":     "dispatched zz_generated.auto_release.yaml on main to promote v1.2.3-rc.14; not in it, cut after it: v1.2.3-rc.15",
		"run":         map[string]any{"id": float64(7), "url": "https://github.com/giantswarm/kserve/actions/runs/7", "status": "completed", "conclusion": "success"},
	}}, doc["repositories"])
	require.Equal(t, 1, dispatches(server))
}

// Without --version, the refusal of rc.15, still building, names rc.14.
func TestRunNotBuiltNamesTheNewestBuiltCandidate(t *testing.T) {
	server, open := mockSources(t, streamRoutes("giantswarm/kserve"))
	doc, err := runCommand(t, []string{"giantswarm/kserve"}, &flag{}, open)
	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.Equal(t, "1 of 1 repositories not promoted: giantswarm/kserve (not_built)", doc["reason"])
	require.Equal(t, []any{map[string]any{
		"repository":  "giantswarm/kserve",
		"stable":      "v1.2.2",
		"candidate":   "v1.2.3-rc.15",
		"newestBuilt": "v1.2.3-rc.14",
		"statusState": "pending",
		"state":       "not_built",
		"message":     "the commit statuses of v1.2.3-rc.15 are pending: promote it once its pipelines passed, or promote the newest built candidate with --version v1.2.3-rc.14",
	}}, doc["repositories"])
	require.Zero(t, dispatches(server))
}

func base64Encode(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
