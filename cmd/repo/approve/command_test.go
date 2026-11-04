package approve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type approvalTransport func(*http.Request) (*http.Response, error)

func (f approvalTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the real flag bindings, session opening and manager request. The
// dry-run banner must correspond to a request that cannot select commit mode.
func TestCommandApprovalMode(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		for _, output := range []string{"text", "json"} {
			t.Run(fmt.Sprintf("dry_run_%t/%s", dryRun, output), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "keyring.json")
				t.Setenv(agentcli.EnvKeyringFile, path)
				t.Setenv(agentcli.EnvTimeScale, "1")
				store := &authstore.FileStore{Path: path}
				require.NoError(t, store.Set(authstore.UserMuster, authstore.Record{
					Token: "synthetic-muster-token", Endpoint: "https://muster.invalid/mcp", Login: "test-person",
				}))
				payload := json.RawMessage(`{"pullRequest":6179,"team":"team-example","member":true,"message":"Membership checked.","future":{"retained":true}}`)
				calls := 0
				old := http.DefaultClient
				t.Cleanup(func() { http.DefaultClient = old })
				http.DefaultClient = &http.Client{Transport: approvalTransport(func(r *http.Request) (*http.Response, error) {
					if r.URL.String() != "https://muster.invalid/mcp" || r.Method != http.MethodPost {
						return nil, fmt.Errorf("unexpected manager endpoint or method")
					}
					require.Equal(t, "Bearer synthetic-muster-token", r.Header.Get("Authorization"))
					defer func() { _ = r.Body.Close() }()
					var msg struct {
						ID     int    `json:"id"`
						Method string `json:"method"`
						Params struct {
							Name      string `json:"name"`
							Arguments struct {
								Name      string         `json:"name"`
								Arguments map[string]any `json:"arguments"`
							} `json:"arguments"`
						} `json:"params"`
					}
					require.NoError(t, json.NewDecoder(r.Body).Decode(&msg))
					var result any = map[string]any{}
					switch msg.Method {
					case "initialize":
					case "notifications/initialized":
						return &http.Response{StatusCode: http.StatusAccepted, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
					case "tools/call":
						calls++
						require.Equal(t, manager.MetaToolCall, msg.Params.Name)
						require.Equal(t, manager.ToolApproveChange, msg.Params.Arguments.Name)
						want := map[string]any{"pullRequest": float64(6179)}
						if dryRun {
							want["dryRun"] = true
						} else {
							want["mode"] = "commit"
						}
						require.Equal(t, want, msg.Params.Arguments.Arguments)
						inner, err := json.Marshal(map[string]any{"content": []any{}, "structuredContent": payload})
						require.NoError(t, err)
						result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(inner)}}}
					default:
						return nil, fmt.Errorf("unexpected MCP method %q", msg.Method)
					}
					body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
					require.NoError(t, err)
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
				})}
				var out, stderr bytes.Buffer
				cmd, err := New(Config{Logger: logrus.New(), Stdout: &out, Stderr: &stderr})
				require.NoError(t, err)
				args := []string{"6179", "--output", output}
				if dryRun {
					args = append(args, "--dry-run")
				}
				cmd.SetArgs(args)
				require.NoError(t, cmd.Execute())
				require.Equal(t, 1, calls)
				require.Empty(t, stderr.String())
				if output == "json" {
					require.JSONEq(t, string(payload), out.String())
				} else {
					require.Equal(t, dryRun, strings.HasPrefix(out.String(), "dry run: nothing written\n"))
					require.Contains(t, out.String(), "pull request #6179 of team-example: you are a member\n")
					require.Contains(t, out.String(), "Membership checked.\n")
				}
			})
		}
	}
}

// Bad input must be refused before opening even a malformed auth record,
// and must never reach the transport.
func TestCommandRejectsInvalidInputBeforeSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing PR", nil, "accepts 1 arg(s), received 0"},
		{"extra PR", []string{"6179", "6180"}, "accepts 1 arg(s), received 2"},
		{"non-number", []string{"abc"}, "expected the pull request's number"},
		{"zero", []string{"0"}, "expected the pull request's number"},
		{"negative", []string{"--", "-1"}, "expected the pull request's number"},
		{"overflow", []string{"9999999999999999999999999999"}, "expected the pull request's number"},
		{"invalid format", []string{"6179", "--output", "yaml"}, "--output must be text or json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keyring.json")
			require.NoError(t, os.WriteFile(path, []byte("malformed synthetic record"), 0600))
			t.Setenv(agentcli.EnvKeyringFile, path)
			t.Setenv(agentcli.EnvTimeScale, "1")
			old := http.DefaultClient
			t.Cleanup(func() { http.DefaultClient = old })
			requests := 0
			http.DefaultClient = &http.Client{Transport: approvalTransport(func(*http.Request) (*http.Response, error) {
				requests++
				return nil, fmt.Errorf("unexpected network request")
			})}
			var out bytes.Buffer
			cmd, err := New(Config{Logger: logrus.New(), Stdout: &out, Stderr: io.Discard})
			require.NoError(t, err)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			require.ErrorContains(t, cmd.Execute(), tc.want)
			require.Zero(t, requests)
			require.Empty(t, out.String())
		})
	}
}
