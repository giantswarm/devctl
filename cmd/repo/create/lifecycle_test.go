package create

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const declarationOriginal = "# Keep this header.\n- name: alpha\n  description: Keep alpha.\n\n# Keep omega's comment.\n- name: omega\n  description: Keep omega.\n"
const declarationDescription = "A description:\n- name: this-is-description-text"

// declarationAPI models the separately persisted GitHub writes. Reusing it
// across command executions reproduces reruns after a partially completed
// creation. It never delegates a request to a real transport.
type declarationAPI struct {
	t             *testing.T
	failWrite     string
	failStatus    int
	lostPullReply bool
	nameStatus    int
	lookupFails   bool
	branchExists  bool
	pullExists    bool
	lookupCount   int
	writeAttempts []string
	committed     []byte
}

func newDeclarationAPI(t *testing.T) *declarationAPI {
	t.Helper()
	t.Setenv("CI", "true")
	t.Setenv("DEVCTL_CREATE_LIFECYCLE_TEST_TOKEN", "synthetic-token")
	api := &declarationAPI{t: t, nameStatus: http.StatusNotFound}
	previous := http.DefaultTransport
	http.DefaultTransport = api
	t.Cleanup(func() { http.DefaultTransport = previous })
	return api
}

func (a *declarationAPI) reply(r *http.Request, status int, value any) (*http.Response, error) {
	body, err := json.Marshal(value)
	require.NoError(a.t, err)
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(bytes.NewReader(body)), Request: r,
	}, nil
}

func (a *declarationAPI) file(r *http.Request, content, sha string) (*http.Response, error) {
	require.Equal(a.t, "main", r.URL.Query().Get("ref"))
	return a.reply(r, http.StatusOK, map[string]string{
		"type": "file", "encoding": "base64", "sha": sha,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	})
}

func (a *declarationAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	require.Equal(a.t, "api.github.com", r.URL.Host)
	key := r.Method + " " + r.URL.Path
	stage := map[string]string{
		"POST /repos/giantswarm/github/git/refs":                               "branch",
		"PUT /repos/giantswarm/github/contents/repositories/team-fixture.yaml": "commit",
		"POST /repos/giantswarm/github/pulls":                                  "pull request",
	}[key]
	if stage != "" {
		a.writeAttempts = append(a.writeAttempts, stage)
		if a.failWrite == stage {
			if stage == "pull request" && a.lostPullReply {
				a.pullExists = true
			}
			status := a.failStatus
			if status == 0 {
				status = http.StatusInternalServerError
			}
			return a.reply(r, status, map[string]string{"message": "synthetic " + stage + " failure"})
		}
	}
	switch key {
	case "GET /repos/giantswarm/github/contents/repositories/team-fixture.yaml":
		return a.file(r, declarationOriginal, "team-sha")
	case "GET /repos/giantswarm/github/contents/.github/repositories.schema.json":
		return a.file(r, `{"type":"array","items":{"type":"object","properties":{"componentType":{"enum":["service"]}}}}`, "schema-sha")
	case "GET /user":
		return a.reply(r, http.StatusOK, map[string]string{"login": "fixture-user"})
	case "GET /orgs/giantswarm/teams/team-fixture", "GET /orgs/giantswarm/teams/team-planeteers":
		return a.reply(r, http.StatusOK, map[string]string{"slug": "fixture-team"})
	case "GET /orgs/giantswarm/teams/team-fixture/memberships/fixture-user":
		return a.reply(r, http.StatusOK, map[string]string{"state": "active"})
	case "GET /orgs/giantswarm/teams/team-planeteers/memberships/fixture-user":
		return a.reply(r, http.StatusNotFound, map[string]string{"message": "not a member"})
	case "GET /repos/giantswarm/fixture-repo":
		return a.reply(r, a.nameStatus, map[string]string{"full_name": "giantswarm/fixture-repo", "message": "synthetic name response"})
	case "GET /repos/giantswarm/github/git/ref/heads/main":
		return a.reply(r, http.StatusOK, map[string]any{"object": map[string]string{"sha": "main-sha"}})
	case "POST /repos/giantswarm/github/git/refs":
		var body map[string]string
		require.NoError(a.t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(a.t, "refs/heads/repo-create/fixture-repo", body["ref"])
		require.Equal(a.t, "main-sha", body["sha"])
		if a.branchExists {
			return a.reply(r, http.StatusUnprocessableEntity, map[string]string{"message": "Reference already exists"})
		}
		a.branchExists = true
		return a.reply(r, http.StatusCreated, body)
	case "PUT /repos/giantswarm/github/contents/repositories/team-fixture.yaml":
		var body struct {
			Branch, SHA, Message string
			Content              []byte
		}
		require.NoError(a.t, json.NewDecoder(r.Body).Decode(&body))
		require.True(a.t, a.branchExists)
		require.Equal(a.t, "repo-create/fixture-repo", body.Branch)
		require.Equal(a.t, "team-sha", body.SHA, "use the read blob SHA to detect concurrent edits")
		require.Equal(a.t, "feat(fixture): declare fixture-repo", body.Message)
		a.committed = body.Content
		return a.reply(r, http.StatusOK, map[string]any{})
	case "POST /repos/giantswarm/github/pulls":
		var body map[string]string
		require.NoError(a.t, json.NewDecoder(r.Body).Decode(&body))
		require.NotEmpty(a.t, a.committed)
		require.Equal(a.t, "main", body["base"])
		require.Equal(a.t, "repo-create/fixture-repo", body["head"])
		require.Equal(a.t, "feat(fixture): declare fixture-repo", body["title"])
		require.Contains(a.t, body["body"], "giantswarm/fixture-repo")
		require.Contains(a.t, body["body"], "reconciler creates")
		a.pullExists = true
		return a.reply(r, http.StatusCreated, a.pull())
	case "GET /repos/giantswarm/github/pulls":
		a.lookupCount++
		require.Equal(a.t, "open", r.URL.Query().Get("state"))
		require.Equal(a.t, "main", r.URL.Query().Get("base"))
		require.Equal(a.t, "giantswarm:repo-create/fixture-repo", r.URL.Query().Get("head"))
		if a.lookupFails {
			return a.reply(r, http.StatusInternalServerError, map[string]string{"message": "synthetic lookup failure"})
		}
		if a.pullExists {
			return a.reply(r, http.StatusOK, []any{a.pull()})
		}
		return a.reply(r, http.StatusOK, []any{})
	default:
		a.t.Errorf("unexpected offline request: %s", key)
		return nil, fmt.Errorf("unexpected offline request: %s", key)
	}
}

func (a *declarationAPI) pull() map[string]any {
	return map[string]any{"number": 6179, "html_url": "https://github.com/giantswarm/github/pull/6179"}
}

func (a *declarationAPI) execute(format string, extraArgs ...string) (string, error) {
	a.t.Helper()
	var stdout bytes.Buffer
	cmd, err := New(Config{Logger: logrus.New(), Stdout: &stdout, Stderr: io.Discard})
	require.NoError(a.t, err)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	args := []string{
		"--github-token-envvar", "DEVCTL_CREATE_LIFECYCLE_TEST_TOKEN",
		"--team", "fixture", "--name", "fixture-repo", "--component-type", "service",
		"--flavour", "app", "--flavour", "cli", "--language", "go",
		"--description", declarationDescription, "--output", format,
	}
	cmd.SetArgs(append(args, extraArgs...))
	err = cmd.Execute()
	return stdout.String(), err
}

func requireDeclarationReceipt(t *testing.T, text, format string) {
	t.Helper()
	const next = "devctl repo watch giantswarm/fixture-repo --pull-request 6179"
	if format == outputText {
		require.Contains(t, text, "pull request: https://github.com/giantswarm/github/pull/6179")
		require.Contains(t, text, "next: "+next)
		return
	}
	var got struct {
		DryRun      reposetup.Result `json:"dryRun"`
		PullRequest string           `json:"pullRequest"`
		Next        string           `json:"next"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &got))
	require.True(t, got.DryRun.Accepted)
	require.Equal(t, "https://github.com/giantswarm/github/pull/6179", got.PullRequest)
	require.Equal(t, next, got.Next)
}

func TestCommandDeclarationPersists(t *testing.T) {
	for _, visibility := range []string{"private", "public"} {
		t.Run(visibility, func(t *testing.T) {
			api := newDeclarationAPI(t)
			var args []string
			if visibility == "public" {
				args = []string{"--visibility", "public"}
			}
			text, err := api.execute(outputJSON, args...)
			require.NoError(t, err)
			requireDeclarationReceipt(t, text, outputJSON)
			require.Equal(t, []string{"branch", "commit", "pull request"}, api.writeAttempts)
			tf, err := reposetup.ParseTeamFile("team-fixture", bytes.NewReader(api.committed))
			require.NoError(t, err)
			require.Len(t, tf.Entries, 3, "description text must remain a scalar, not inject another declaration")
			require.Equal(t, []string{"alpha", "fixture-repo", "omega"}, []string{tf.Entries[0].Name, tf.Entries[1].Name, tf.Entries[2].Name})
			entry := tf.Entries[1]
			fields, err := entry.Fields()
			require.NoError(t, err)
			require.Equal(t, declarationDescription, fields.Description)
			require.Equal(t, visibility, fields.Visibility)
			require.Equal(t, "service", fields.ComponentType)
			require.True(t, fields.Align)
			require.Equal(t, []string{"app", "cli"}, fields.Gen.Flavours)
			require.Equal(t, "go", fields.Gen.Language)
			require.NotNil(t, fields.Gen.CI.Generate)
			require.True(t, *fields.Gen.CI.Generate)
			// Remove just the inserted YAML; all original text must survive.
			rendered, err := entry.YAML()
			require.NoError(t, err)
			require.Equal(t, declarationOriginal, strings.Replace(string(api.committed), rendered, "", 1))
		})
	}
}

func TestCommandDeclarationRerun(t *testing.T) {
	for _, format := range []string{outputText, outputJSON} {
		t.Run(format, func(t *testing.T) {
			api := newDeclarationAPI(t)
			text, err := api.execute(format)
			require.NoError(t, err)
			requireDeclarationReceipt(t, text, format)
			before := append([]byte(nil), api.committed...)
			text, err = api.execute(format)
			require.NoError(t, err)
			requireDeclarationReceipt(t, text, format)
			if format == outputText {
				require.Contains(t, text, "(open already)")
			}
			require.Equal(t, []string{"branch", "commit", "pull request", "branch"}, api.writeAttempts)
			require.Equal(t, before, api.committed)
			require.Equal(t, 1, api.lookupCount)
		})
	}
}

func TestCommandDeclarationPartialFailure(t *testing.T) {
	for _, tc := range []struct {
		stage  string
		status int
	}{
		{stage: "branch", status: http.StatusInternalServerError},
		{stage: "commit", status: http.StatusInternalServerError},
		{stage: "commit", status: http.StatusConflict},
		{stage: "pull request", status: http.StatusInternalServerError},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.stage, tc.status), func(t *testing.T) {
			stage := tc.stage
			api := newDeclarationAPI(t)
			api.failWrite, api.failStatus = stage, tc.status
			text, err := api.execute(outputJSON)
			require.ErrorContains(t, err, "synthetic "+stage+" failure")
			require.NotContains(t, text, `"pullRequest"`)
			require.NotContains(t, text, `"next"`)
			attempts := []string{"branch"}
			if stage != "branch" {
				attempts = append(attempts, "commit")
			}
			if stage == "pull request" {
				attempts = append(attempts, "pull request")
			}
			require.Equal(t, attempts, api.writeAttempts, "stop at the failed stage without automatic retries")
			require.Equal(t, stage != "branch", api.branchExists)
			require.Equal(t, stage == "pull request", len(api.committed) > 0)
			require.False(t, api.pullExists)
			if stage == "branch" {
				return
			}
			api.failWrite = ""
			text, err = api.execute(outputJSON)
			require.True(t, IsBranchWithoutPullRequest(err), "%v", err)
			require.Contains(t, err.Error(), "repo-create/fixture-repo")
			require.Contains(t, err.Error(), "open the pull request from it or delete the branch")
			require.NotContains(t, text, `"pullRequest"`)
			require.Equal(t, append(attempts, "branch"), api.writeAttempts, "never rewrite or delete the leftover branch")
			require.True(t, api.branchExists)
		})
	}
}

func TestCommandDeclarationLostPullReply(t *testing.T) {
	api := newDeclarationAPI(t)
	api.failWrite, api.lostPullReply = "pull request", true
	_, err := api.execute(outputJSON)
	require.ErrorContains(t, err, "synthetic pull request failure")
	require.True(t, api.pullExists, "the server completed the write before its reply was lost")
	api.failWrite = ""
	text, err := api.execute(outputJSON)
	require.NoError(t, err)
	requireDeclarationReceipt(t, text, outputJSON)
	require.Equal(t, []string{"branch", "commit", "pull request", "branch"}, api.writeAttempts)
	require.Equal(t, 1, api.lookupCount)
}

func TestCommandDeclarationLookupFailure(t *testing.T) {
	api := newDeclarationAPI(t)
	api.branchExists, api.pullExists, api.lookupFails = true, true, true
	text, err := api.execute(outputJSON)
	require.ErrorContains(t, err, "synthetic lookup failure")
	require.False(t, IsBranchWithoutPullRequest(err), "an unreadable PR is not proof there is no PR")
	require.Equal(t, []string{"branch"}, api.writeAttempts)
	require.NotContains(t, text, `"pullRequest"`)
	require.NotContains(t, text, `"next"`)
}

func TestCommandDeclarationRefusedBeforeWrites(t *testing.T) {
	for _, tc := range []struct {
		name       string
		nameStatus int
		args       []string
		refused    bool
	}{
		{name: "taken name", nameStatus: http.StatusOK, refused: true},
		{name: "schema refusal", nameStatus: http.StatusNotFound, args: []string{"--component-type", "invalid"}, refused: true},
		{name: "name check unavailable", nameStatus: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newDeclarationAPI(t)
			api.nameStatus = tc.nameStatus
			text, err := api.execute(outputJSON, tc.args...)
			require.Error(t, err)
			require.Equal(t, tc.refused, IsRefused(err), "%v", err)
			require.Empty(t, api.writeAttempts)
			require.False(t, api.branchExists)
			require.NotContains(t, text, `"pullRequest"`)
			require.NotContains(t, text, `"next"`)
		})
	}
}
