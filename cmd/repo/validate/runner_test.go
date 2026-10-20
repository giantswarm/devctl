package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// teamFile is a team file of two entries the schema accepts, new-cli being
// added.
const teamFile = `- name: new-cli
  componentType: cli
  gen:
    flavours: [cli]
    language: go
- name: old-tool
  componentType: cli
  gen:
    flavours: [cli]
    language: go
`

// managerAnswer is validate_repository's answer as giantswarm-repo-manager
// gives it: the engine's result rendered as the manager writes a created
// entry, with the author it read from the token.
const managerAnswer = `{"team":"team-e2e","mode":"create","schema":"giantswarm/github@main","entries":[{"name":"new-cli","rendered":"- name: new-cli\n  componentType: tool\n  visibility: private\n  align: true\n  gen:\n    ci:\n      generate: true\n    flavours:\n      - cli\n    language: go\n","template":"giantswarm/template","nameCheck":{"verdict":"free","detail":"no repository giantswarm/new-cli"},"accepted":true}],"accepted":true,"author":"alice","authorLogin":"alice","authorTeams":["team-e2e"],"teamsSource":"github","machineApproved":true}`

// recorder is the manager: it records the one call and answers it.
type recorder struct {
	answer string
	err    error
	tool   string
	args   map[string]any
}

func (r *recorder) Call(_ context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	r.tool, r.args = tool, args
	if r.err != nil {
		return nil, r.err
	}
	return json.RawMessage(r.answer), nil
}

func writeTeamFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "team-e2e.yaml")
	require.NoError(t, os.WriteFile(path, []byte(teamFile), 0o600))
	return path
}

func newRunner(t *testing.T, f *flag, rec *recorder, open client.Opener) (*runner, *bytes.Buffer) {
	t.Helper()
	if open == nil {
		open = func(context.Context) (*client.Session, error) {
			return &client.Session{Caller: rec, Endpoint: "https://muster.example/mcp", Login: "alice@example.com"}, nil
		}
	}
	var out bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&bytes.Buffer{})
	return &runner{flag: f, logger: logger, stdout: &out, stderr: &bytes.Buffer{}, open: open}, &out
}

func document(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc), out.String())
	return doc
}

// The entries being added go to the manager's validate_repository as they
// stand in the team file, and its answer is printed as it came, under the
// engine that answered.
func TestRunManager(t *testing.T) {
	t.Setenv(authstore.EnvCI, "")
	rec := &recorder{answer: managerAnswer}
	r, out := newRunner(t, &flag{TeamFile: writeTeamFile(t), Entries: []string{"new-cli"}, Owner: reposetup.DefaultOwner}, rec, nil)

	require.NoError(t, r.run(context.Background()))

	require.Equal(t, manager.ToolValidateRepository, rec.tool)
	require.Equal(t, "team-e2e", rec.args["team"])
	require.Equal(t, []any{map[string]any{"name": "new-cli", "componentType": "cli", "gen": map[string]any{"flavours": []any{"cli"}, "language": "go"}}}, rec.args["entries"])
	doc := document(t, out)
	require.Equal(t, map[string]any{"name": "manager", "server": manager.Server, "endpoint": "https://muster.example/mcp", "login": "alice@example.com"}, doc["engine"])
	require.Equal(t, true, doc["accepted"])
	require.Equal(t, "giantswarm/github@main", doc["schema"])
	require.Equal(t, "alice", doc["authorLogin"], "the manager's own fields are kept")
	require.Equal(t, true, doc["machineApproved"])
	require.Contains(t, out.String(), "visibility: private", "the entry as the manager renders it")
}

// Without --entry every entry goes; a refused answer is printed and the
// exit status says so.
func TestRunManagerRefused(t *testing.T) {
	t.Setenv(authstore.EnvCI, "")
	rec := &recorder{answer: `{"team":"team-e2e","mode":"create","schema":"giantswarm/github@main","entries":[{"name":"new-cli","accepted":true},{"name":"old-tool","nameCheck":{"verdict":"taken","detail":"repository giantswarm/old-tool exists"},"problems":[{"field":"name","message":"taken: repository giantswarm/old-tool exists"}],"accepted":false}],"accepted":false}`}
	r, out := newRunner(t, &flag{TeamFile: writeTeamFile(t), Mode: "create"}, rec, nil)

	err := r.run(context.Background())
	require.True(t, IsRefused(err), "%v", err)
	require.Contains(t, err.Error(), "1 of 2 entries refused")
	require.Len(t, rec.args["entries"], 2)
	doc := document(t, out)
	require.Equal(t, false, doc["accepted"])
	require.Equal(t, "manager", doc["engine"].(map[string]any)["name"])
}

// Without a muster token the error names the login and --local, and the
// manager is not called; an entry the file lacks is refused before the call;
// what only the local engine does is refused without --local.
func TestRunManagerRefusals(t *testing.T) {
	t.Setenv(authstore.EnvCI, "")
	path := writeTeamFile(t)

	rec := &recorder{answer: managerAnswer}
	r, out := newRunner(t, &flag{TeamFile: path, Entries: []string{"new-cli"}}, rec, func(context.Context) (*client.Session, error) {
		return nil, &authstore.AuthRequiredError{Identity: "muster", Cause: "no token in the keychain", Hint: "devctl auth login --muster-only"}
	})
	err := r.run(context.Background())
	require.True(t, errors.Is(err, authstore.ErrAuthRequired), "%v", err)
	require.Contains(t, err.Error(), "devctl auth login --muster-only")
	require.Contains(t, err.Error(), "--local runs devctl's own engine")
	require.Empty(t, rec.tool)
	require.Empty(t, out.String(), "nothing is printed")

	r, _ = newRunner(t, &flag{TeamFile: path, Entries: []string{"nope"}}, rec, nil)
	err = r.run(context.Background())
	require.True(t, reposetup.IsEntryNotFound(err), "%v", err)
	require.Empty(t, rec.tool)

	r, _ = newRunner(t, &flag{TeamFile: path}, rec, nil)
	err = r.run(context.Background())
	require.True(t, IsInvalidFlag(err), "%v", err)
	require.Contains(t, err.Error(), "existing mode is the local engine's: pass --local")
	require.Empty(t, rec.tool)
}

// --local runs devctl's own engine, the manager untouched, and the document
// says so; under CI the keychain is not read and the engine is local too.
func TestRunLocal(t *testing.T) {
	path := writeTeamFile(t)
	for _, name := range append([]string{"MY_TOKEN"}, authstore.GitHubEnvVars...) {
		t.Setenv(name, "")
	}
	t.Setenv(agentcli.EnvKeyringFile, t.TempDir()+"/keyring.json")

	for _, tc := range []struct {
		name   string
		flag   flag
		ci     string
		reason string
	}{
		{name: "--local", flag: flag{TeamFile: path, Local: true}, reason: "--local"},
		{name: "CI", flag: flag{TeamFile: path, Mode: "existing"}, ci: "true", reason: "$CI is set, so the keychain is not read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(authstore.EnvCI, tc.ci)
			rec := &recorder{answer: managerAnswer}
			r, out := newRunner(t, &tc.flag, rec, nil)

			require.NoError(t, r.run(context.Background()))

			require.Empty(t, rec.tool, "the manager is not asked")
			doc := document(t, out)
			who := doc["engine"].(map[string]any)
			require.Equal(t, "local", who["name"])
			require.Equal(t, tc.reason, who["reason"])
			require.NotEmpty(t, who["version"])
			require.Equal(t, "team-e2e", doc["team"])
			require.Equal(t, "existing", doc["mode"])
			require.Equal(t, "embedded", doc["schema"], "no GitHub token: the embedded schema")
			require.Equal(t, true, doc["accepted"])
			require.Len(t, doc["entries"], 2)
		})
	}
}

// The document is the answer with the engine added, numbers as they came;
// an answer that is no object is refused.
func TestPrint(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, print(&out, engine{Name: engineLocal, Version: "1.2.3", Reason: "--local"}, json.RawMessage(`{"team":"t","n":1.50,"accepted":true}`)))
	require.Equal(t, "{\n  \"accepted\": true,\n  \"engine\": {\n    \"name\": \"local\",\n    \"version\": \"1.2.3\",\n    \"reason\": \"--local\"\n  },\n  \"n\": 1.50,\n  \"team\": \"t\"\n}\n", out.String())

	err := print(&out, engine{Name: engineManager}, json.RawMessage(`"refused"`))
	require.True(t, client.IsUnexpectedAnswer(err), "%v", err)
	require.True(t, strings.HasSuffix(strings.TrimSpace(out.String()), "}"), "nothing more was written")

	require.NoError(t, verdict(json.RawMessage(`{"accepted":true,"entries":[{"accepted":true}]}`)))
	err = verdict(json.RawMessage(`{"accepted":false,"entries":[{"accepted":false},{"accepted":true},{"accepted":false}]}`))
	require.True(t, IsRefused(err), "%v", err)
	require.Contains(t, err.Error(), "2 of 3 entries refused")
}

func TestGitHubClientToken(t *testing.T) {
	cases := []struct {
		name     string
		envVar   string // --github-token-envvar
		env      map[string]string
		keychain bool // the keychain is readable; otherwise any read fails

		wantClient bool
		wantErr    bool
		wantStderr string // substring; empty means nothing
		wantLog    string // substring of the log; empty means nothing
	}{
		{name: "CI without a token: the embedded schema, the keychain never opened",
			env:     map[string]string{authstore.EnvCI: "true"},
			wantLog: "no GitHub token ($CI is set, so the keychain is not read, and there is no token in $DEVCTL_GITHUB_TOKEN, $GITHUB_TOKEN or $OPSCTL_GITHUB_TOKEN): validating against the embedded schema"},
		{name: "no App login: the embedded schema", keychain: true,
			wantLog: "no GitHub token (no token in the keychain): validating against the embedded schema"},
		{name: "without CI the keychain is opened, and its failure returned",
			wantErr: true},
		{name: "a default variable overrides the App login with the warning",
			env:        map[string]string{"OPSCTL_GITHUB_TOKEN": "ghp_env"},
			wantClient: true, wantStderr: "warning: the GitHub token in $OPSCTL_GITHUB_TOKEN overrides the devctl GitHub App login"},
		{name: "--github-token-envvar names the one variable",
			envVar: "MY_TOKEN", env: map[string]string{"MY_TOKEN": "ghp_mine", "GITHUB_TOKEN": "ghp_other"},
			wantClient: true, wantStderr: "warning: the GitHub token in $MY_TOKEN overrides"},
		{name: "--github-token-envvar: other variables are not read",
			envVar: "MY_TOKEN", env: map[string]string{"GITHUB_TOKEN": "ghp_other", authstore.EnvCI: "true"},
			wantLog: "there is no token in $MY_TOKEN"},
		{name: "a token under CI: no warning",
			env:        map[string]string{"GITHUB_TOKEN": "ghp_ci", authstore.EnvCI: "true"},
			wantClient: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range append([]string{authstore.EnvCI, "MY_TOKEN"}, authstore.GitHubEnvVars...) {
				t.Setenv(name, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if tc.keychain {
				t.Setenv(agentcli.EnvKeyringFile, t.TempDir()+"/keyring.json")
			} else {
				t.Setenv(agentcli.EnvKeyringFile, t.TempDir())
			}
			var stderr, log bytes.Buffer
			logger := logrus.New()
			logger.SetOutput(&log)
			r := &runner{flag: &flag{GithubTokenEnvVar: tc.envVar}, logger: logger, stderr: &stderr}

			client, err := r.githubClient(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want an error: %v", err, tc.wantErr)
			}
			if (client != nil) != tc.wantClient {
				t.Errorf("client = %v, want one: %v", client, tc.wantClient)
			}
			if got := stderr.String(); (tc.wantStderr == "") != (got == "") || !strings.Contains(got, tc.wantStderr) {
				t.Errorf("stderr %q, want %q", got, tc.wantStderr)
			}
			if got := log.String(); (tc.wantLog == "") != (got == "") || !strings.Contains(got, tc.wantLog) {
				t.Errorf("log %q, want %q", got, tc.wantLog)
			}
		})
	}
}
