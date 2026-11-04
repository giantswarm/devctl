package create

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// A default dry run needs the manager login, even when no GitHub token is
// available. The temporary store keeps it from reading the host's keychain.
func TestCommandDryRunRequiresManager(t *testing.T) {
	t.Setenv(authstore.EnvCI, "true")
	t.Setenv(agentcli.EnvKeyringFile, filepath.Join(t.TempDir(), "keyring.json"))
	for _, name := range authstore.GitHubEnvVars {
		t.Setenv(name, "")
	}
	var out, stderr bytes.Buffer
	cmd, err := New(Config{Logger: logrus.New(), Stdout: &out, Stderr: &stderr})
	require.NoError(t, err)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--team", "e2e", "--name", "new-cli", "--dry-run"})
	err = cmd.Execute()
	var required *authstore.AuthRequiredError
	require.True(t, errors.As(err, &required), "%v", err)
	require.Equal(t, "muster", required.Identity)
	require.Contains(t, err.Error(), "devctl auth login --muster-only")
	require.Empty(t, out.String())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Every request is handled in memory, including unexpected ones: no test
// can reach GitHub or any other service through the default transport.
func setTransport(t *testing.T, f roundTripFunc) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = previous })
}

type managerCall struct {
	call func(context.Context, string, map[string]any) (json.RawMessage, error)
}

func (c managerCall) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return c.call(ctx, tool, args)
}

// The manager's answer decides both text and JSON verdicts, even if its
// engine understands a flavour the CLI's embedded engine does not know.
func TestDryRunManagerVerdict(t *testing.T) {
	for _, tc := range []struct {
		name     string
		output   string
		accepted bool
		refusal  string
	}{
		{name: "case 0: accepted text", output: outputText, accepted: true},
		{name: "case 1: accepted JSON", output: outputJSON, accepted: true},
		{name: "case 2: refused text", output: outputText},
		{name: "case 3: refused JSON", output: outputJSON},
		{name: "case 4: creation refused text", output: outputText, accepted: true, refusal: "the team file already declares new-cli"},
		{name: "case 5: creation refused JSON", output: outputJSON, accepted: true, refusal: "the team file already declares new-cli"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTransport(t, func(req *http.Request) (*http.Response, error) {
				t.Errorf("unexpected local request: %s %s", req.Method, req.URL.Path)
				return nil, errors.New("local validation must not run")
			})
			answer := map[string]any{
				"team": "team-e2e", "mode": "create", "schema": "manager-schema",
				"entries": []any{map[string]any{
					"name": "new-cli", "accepted": tc.accepted, "rendered": "- name: new-cli\n  description: rendered by the manager\n",
					"nameCheck": map[string]any{"verdict": "free"},
					"problems":  []any{map[string]any{"field": "gen.language", "message": "the manager's verdict"}},
				}},
				"accepted": tc.accepted, "authorLogin": "alice", "machineApproved": tc.accepted,
				"creation": map[string]any{"refusal": tc.refusal},
			}
			payload, err := json.Marshal(answer)
			require.NoError(t, err)
			calls := 0
			caller := managerCall{call: func(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
				calls++
				require.Equal(t, manager.ToolValidateRepository, tool)
				require.Equal(t, map[string]any{
					"team": "team-e2e",
					"entry": map[string]any{
						"name": "new-cli", "componentType": "cli", "description": "What it does", "visibility": "private", "align": true,
						"gen": map[string]any{"language": "go", "flavours": []any{"cli", "manager-only"}, "ci": map[string]any{"generate": true}},
					},
				}, args)
				deadline, ok := ctx.Deadline()
				require.True(t, ok, "manager reads are bounded")
				require.Positive(t, time.Until(deadline))
				require.LessOrEqual(t, time.Until(deadline), validationTimeout)
				return payload, nil
			}}
			var out, stderr bytes.Buffer
			r := &runner{
				flag:   &flag{Team: "e2e", Name: "new-cli", ComponentType: "cli", Description: "What it does", Flavours: []string{"cli", "manager-only"}, Language: "go", Owner: reposetup.DefaultOwner, DryRun: true, Output: tc.output},
				logger: logrus.New(), stdout: &out, stderr: &stderr,
				open: func(context.Context) (*client.Session, error) { return &client.Session{Caller: caller}, nil },
			}
			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())
			err = r.Run(cmd, nil)
			if tc.accepted && tc.refusal == "" {
				require.NoError(t, err)
			} else {
				require.True(t, IsRefused(err), "%v", err)
			}
			require.Equal(t, 1, calls)
			if tc.output == outputJSON {
				var doc map[string]any
				require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
				require.Equal(t, map[string]any{"dryRun": answer}, doc, "manager fields survive unchanged; no pull request or next command is reported")
			} else {
				require.Contains(t, out.String(), "validation: giantswarm-repo-manager")
				require.Contains(t, out.String(), "rendered by the manager")
				require.Contains(t, out.String(), "dry run: no pull request opened")
				if !tc.accepted {
					require.Contains(t, out.String(), "verdict:    refused")
					require.Contains(t, out.String(), "gen.language: the manager's verdict")
				}
				if tc.refusal != "" {
					require.Contains(t, out.String(), "creation refused: "+tc.refusal)
				}
			}
		})
	}
}

func TestDryRunManagerFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
		err    error
	}{
		{name: "case 0: unreachable manager", err: context.DeadlineExceeded},
		{name: "case 1: bearer refused", err: &authstore.AuthRequiredError{Identity: "muster", Hint: "devctl auth login --muster-only"}},
		{name: "case 2: malformed JSON", answer: `"not a validation"`},
		{name: "case 3: missing entry", answer: `{"accepted":true,"entries":[]}`},
		{name: "case 4: wrong entry", answer: `{"accepted":true,"entries":[{"name":"another-cli"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTransport(t, func(req *http.Request) (*http.Response, error) {
				t.Errorf("unexpected fallback request: %s %s", req.Method, req.URL.Path)
				return nil, errors.New("local fallback must not run")
			})
			var out, stderr bytes.Buffer
			caller := managerCall{call: func(context.Context, string, map[string]any) (json.RawMessage, error) {
				return json.RawMessage(tc.answer), tc.err
			}}
			r := &runner{
				flag:   &flag{Team: "team-e2e", Name: "new-cli", DryRun: true, Owner: reposetup.DefaultOwner, Output: outputJSON},
				logger: logrus.New(), stdout: &out, stderr: &stderr,
				open: func(context.Context) (*client.Session, error) { return &client.Session{Caller: caller}, nil },
			}
			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())
			err := r.Run(cmd, nil)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.True(t, client.IsUnexpectedAnswer(err), "%v", err)
			}
			require.Empty(t, out.String())
		})
	}
}

func TestDryRunManagerCommandContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	caller := managerCall{call: func(ctx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
		return nil, ctx.Err()
	}}
	r := &runner{
		flag:   &flag{Team: "team-e2e", Name: "new-cli", DryRun: true, Owner: reposetup.DefaultOwner, Output: outputJSON},
		logger: logrus.New(), stdout: io.Discard, stderr: io.Discard,
		open: func(context.Context) (*client.Session, error) { return &client.Session{Caller: caller}, nil },
	}
	require.ErrorIs(t, r.Run(cmd, nil), context.Canceled)
}

// --dry-run --local still runs the local schema, creation and name checks,
// accepts a free name and refuses a taken one, and makes no write requests.
func TestCommandDryRunLocal(t *testing.T) {
	schema, err := os.ReadFile("../../../pkg/reposetup/schema/repositories.schema.json")
	require.NoError(t, err)
	for _, tc := range []struct {
		name  string
		taken bool
	}{
		{name: "case 0: free name"},
		{name: "case 1: taken name", taken: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(authstore.EnvCI, "true")
			t.Setenv(agentcli.EnvKeyringFile, filepath.Join(t.TempDir(), "keyring.json"))
			t.Setenv("DEVCTL_CREATE_TEST_TOKEN", "synthetic-offline-token") // #nosec G101 -- synthetic test token
			var reads []string
			setTransport(t, func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodGet, req.Method, "a local dry run writes nothing")
				require.Equal(t, "api.github.com", req.URL.Host)
				reads = append(reads, req.URL.Path)
				status, body := http.StatusOK, `{}`
				file := func(content []byte) string {
					encoded, err := json.Marshal(map[string]any{"type": "file", "path": strings.TrimPrefix(req.URL.Path, "/repos/giantswarm/github/contents/"), "encoding": "base64", "content": base64.StdEncoding.EncodeToString(content)})
					require.NoError(t, err)
					return string(encoded)
				}
				switch req.URL.Path {
				case "/repos/giantswarm/github/contents/repositories/team-e2e.yaml":
					body = file([]byte("- name: old-cli\n  componentType: cli\n  gen:\n    flavours: [cli]\n    language: go\n"))
				case "/repos/giantswarm/github/contents/.github/repositories.schema.json":
					body = file(schema)
				case "/user":
					body = `{"login":"alice"}`
				case "/orgs/giantswarm/teams/team-e2e", "/orgs/giantswarm/teams/team-planeteers":
				case "/orgs/giantswarm/teams/team-e2e/memberships/alice", "/orgs/giantswarm/teams/team-planeteers/memberships/alice":
					body = `{"state":"active"}`
				case "/repos/giantswarm/new-cli":
					if tc.taken {
						body = `{"full_name":"giantswarm/new-cli"}`
					} else {
						status, body = http.StatusNotFound, `{"message":"Not Found"}`
					}
				default:
					t.Errorf("unexpected request: %s", req.URL.Path)
					status = http.StatusNotFound
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			var out, stderr bytes.Buffer
			cmd, err := New(Config{Logger: logrus.New(), Stdout: &out, Stderr: &stderr})
			require.NoError(t, err)
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs([]string{"--team", "e2e", "--name", "new-cli", "--component-type", "cli", "--flavour", "cli", "--language", "go", "--description", "What it does", "--dry-run", "--local", "--output", "json", "--github-token-envvar", "DEVCTL_CREATE_TEST_TOKEN"})
			err = cmd.Execute()
			if tc.taken {
				require.True(t, IsRefused(err), "%v", err)
			} else {
				require.NoError(t, err)
			}
			var doc struct {
				DryRun      reposetup.Result `json:"dryRun"`
				PullRequest string           `json:"pullRequest"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
			require.Equal(t, !tc.taken, doc.DryRun.Accepted)
			require.Equal(t, reposetup.SchemaOriginGitHub, doc.DryRun.Schema)
			require.Empty(t, doc.PullRequest)
			require.Len(t, doc.DryRun.Entries, 1)
			require.Contains(t, doc.DryRun.Entries[0].Rendered, "align: true")
			require.Contains(t, doc.DryRun.Entries[0].Rendered, "visibility: private")
			require.Len(t, reads, 8, "the team file, schema, user, memberships and repository were all checked locally")
		})
	}
}

func TestDryRunLocalFlags(t *testing.T) {
	for _, tc := range []struct {
		name    string
		local   bool
		dryRun  bool
		owner   string
		refused string
	}{
		{name: "case 0: manager preview", dryRun: true, owner: reposetup.DefaultOwner},
		{name: "case 1: local preview", local: true, dryRun: true, owner: "example"},
		{name: "case 2: local without dry run", local: true, owner: reposetup.DefaultOwner, refused: "--local requires --dry-run"},
		{name: "case 3: manager cannot validate another owner", dryRun: true, owner: "example", refused: "--owner example takes --dry-run --local"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &flag{Team: "e2e", Name: "new-cli", Output: outputText, Local: tc.local, DryRun: tc.dryRun, Owner: tc.owner}
			err := f.Validate()
			if tc.refused == "" {
				require.NoError(t, err)
				require.Equal(t, "team-e2e", f.Team)
			} else {
				require.True(t, IsInvalidFlag(err), "%v", err)
				require.Contains(t, err.Error(), tc.refused)
			}
		})
	}
}
