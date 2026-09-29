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

// promoteRoutes script one auto-release repository with a built candidate.
func promoteRoutes(repository string) sequence.Routes {
	return sequence.Routes{
		"GET /repos/" + repository: {{Body: map[string]any{"name": repository, "default_branch": "main"}}},
		"GET /repos/" + repository + "/actions/workflows/zz_generated.auto_release.yaml":             {{Body: map[string]any{"id": 1, "state": "active"}}},
		"GET /repos/" + repository + "/releases":                                                     {{Body: []map[string]any{{"tag_name": "v1.3.0-rc.1", "prerelease": true}, {"tag_name": "v1.2.0"}}}},
		"GET /repos/" + repository + "/commits/v1.3.0-rc.1/status":                                   {{Body: map[string]any{"state": "success", "total_count": 1}}},
		"POST /repos/" + repository + "/actions/workflows/zz_generated.auto_release.yaml/dispatches": {{Status: http.StatusNoContent}},
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
	}}, doc["repositories"])
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
	delete(routes, "GET /repos/giantswarm/kserve/actions/workflows/zz_generated.auto_release.yaml")
	server, open := mockSources(t, routes)
	doc, err := runCommand(t, []string{"giantswarm/kserve"}, &flag{}, open)
	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.Equal(t, string(agentcli.VerdictRed), doc["verdict"])
	require.Equal(t, "1 of 1 repositories not dispatched: giantswarm/kserve (not_auto_release)", doc["reason"])
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

func base64Encode(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
