package failed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

func TestRunListsRecordedPublishFailures(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "/repos/o/r/releases", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if empty {
					_, _ = io.WriteString(w, `[]`)
					return
				}
				if r.URL.Query().Get("page") == "" {
					w.Header().Set("Link", fmt.Sprintf("<http://%s/repos/o/r/releases?page=2&per_page=100>; rel=\"next\"", r.Host))
					_, _ = io.WriteString(w, `[{"tag_name":"v1.2.4","body":"A release without binary assets is valid."}]`)
					return
				}
				notice := "<!-- devctl:publish-failed -->\n> **Not published:** tag pipeline failed\n<!-- /devctl:publish-failed -->\n\nKeep notes"
				_ = json.NewEncoder(w).Encode([]any{
					map[string]any{"tag_name": "v1.2.3", "html_url": "https://github.com/o/r/releases/tag/v1.2.3", "body": notice},
					map[string]any{"tag_name": "v1.2.5-rc.1", "html_url": "https://github.com/o/r/releases/tag/v1.2.5-rc.1", "prerelease": true, "body": notice},
					map[string]any{"tag_name": "v1.2.6", "draft": true, "body": notice},
				})
			}))
			t.Cleanup(server.Close)
			stdout := &bytes.Buffer{}
			r := &runner{
				stdout: stdout, gate: func(bool) error { return nil },
				open: func(_ context.Context, owner string, transport http.RoundTripper, _ func(string)) (*githubclient.Client, error) {
					require.Equal(t, "o", owner)
					logger := logrus.New()
					logger.SetOutput(io.Discard)
					c, _, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL, Transport: transport})
					return c, err
				},
			}
			c, err := New(Config{Stdout: stdout})
			require.NoError(t, err)
			c.RunE = r.Run
			c.SetArgs([]string{"o/r"})
			require.NoError(t, c.ExecuteContext(t.Context()))
			var doc document
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc))
			require.Equal(t, command, doc.Command)
			require.Equal(t, "o/r", doc.Repository)
			require.Equal(t, agentcli.VerdictListed, doc.Verdict)
			require.Equal(t, agentcli.ExitOK, doc.ExitCode)
			if empty {
				require.NotNil(t, doc.Releases)
				require.Empty(t, doc.Releases)
				require.Equal(t, 1, requests)
			} else {
				require.Equal(t, []release{{Tag: "v1.2.3", URL: "https://github.com/o/r/releases/tag/v1.2.3"}, {Tag: "v1.2.5-rc.1", URL: "https://github.com/o/r/releases/tag/v1.2.5-rc.1"}}, doc.Releases)
				require.Equal(t, 2, requests)
			}
		})
	}
}

func TestRunUsageAndAuthFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{name: "missing repository", code: agentcli.ExitUsage},
		{name: "extra argument", args: []string{"o/r", "v1.2.3"}, code: agentcli.ExitUsage},
		{name: "malformed repository", args: []string{"o/r/extra"}, code: agentcli.ExitUsage},
		{name: "no login", args: []string{"o/r"}, code: agentcli.ExitAuthRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			r := &runner{
				stdout: stdout, gate: func(bool) error { return nil },
				open: func(context.Context, string, http.RoundTripper, func(string)) (*githubclient.Client, error) {
					require.Equal(t, agentcli.ExitAuthRequired, tc.code, "usage must be checked before authentication")
					return nil, &authstore.AuthRequiredError{Identity: "GitHub", Hint: "devctl auth login"}
				},
			}
			err := r.Run(&cobra.Command{}, tc.args)
			require.Equal(t, tc.code, agentcli.Exit(err))
			var doc document
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc))
			require.Equal(t, tc.code, doc.ExitCode)
			require.Empty(t, doc.Releases)
		})
	}
	stdout := &bytes.Buffer{}
	c, err := New(Config{Stdout: stdout})
	require.NoError(t, err)
	c.SetArgs([]string{"--unknown"})
	err = c.ExecuteContext(t.Context())
	require.Equal(t, agentcli.ExitUsage, agentcli.Exit(err))
	var doc document
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc))
	require.Equal(t, agentcli.VerdictUsage, doc.Verdict)
}
