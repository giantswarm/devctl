package wait

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

func TestRunRecordsFailedPublish(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			patches := 0
			var body string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method + " " + r.URL.Path {
				case "GET /repos/giantswarm/devctl/releases/tags/v1.2.3":
					_, _ = io.WriteString(w, `{"id":42,"body":"Keep notes"}`)
				case "PATCH /repos/giantswarm/devctl/releases/42":
					patches++
					var patch map[string]string
					require.NoError(t, json.NewDecoder(r.Body).Decode(&patch))
					body = patch["body"]
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{}`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			s := sources(t, "failed")
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			c, _, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
			require.NoError(t, err)
			s.Client = c
			stdout := &bytes.Buffer{}
			r := &runner{
				gate: func(bool) error { return nil }, flag: &flag{Timeout: time.Minute}, stdout: stdout, stderr: io.Discard,
				open: func(context.Context, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
					return s, nil
				},
			}
			err = r.run(t.Context(), []string{"giantswarm/devctl", "v1.2.3"})
			require.Equal(t, agentcli.ExitRed, agentcli.Exit(err), "a notes error must preserve the failed-CI outcome")
			var doc releasewait.Document
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc))
			require.Equal(t, agentcli.VerdictCIFailed, doc.Verdict)
			require.Contains(t, doc.Reason, "push-to-registries-release")
			require.Equal(t, 1, patches)
			require.Contains(t, body, "**Not published:**")
			if status == http.StatusOK {
				require.Empty(t, doc.Warnings)
			} else {
				require.Len(t, doc.Warnings, 1)
				require.Contains(t, doc.Warnings[0], "could not record publish status")
				require.Contains(t, doc.Warnings[0], "Contents: write")
			}

			// The same sources and write-capable client handed to the
			// server remain read-only: no additional PATCH is sent.
			tool := toolDocument(t, s)
			require.Equal(t, string(agentcli.VerdictCIFailed), tool["verdict"])
			require.Equal(t, 1, patches)
		})
	}
}

func TestRunClearsRecoveredPublish(t *testing.T) {
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"id":42,"body":"<!-- devctl:publish-failed -->\nPrevious failure\n<!-- /devctl:publish-failed -->\n\nKeep notes"}`)
		} else {
			patches++
			var patch map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&patch))
			require.Equal(t, "Keep notes", patch["body"])
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(server.Close)
	s, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", &releasewaittest.Registry{}, "success")
	require.NoError(t, err)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	s.Client, _, err = githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
	require.NoError(t, err)
	doc := cliDocument(t, s)
	require.Equal(t, float64(agentcli.ExitOK), doc["exitCode"])
	require.Equal(t, string(agentcli.VerdictAvailable), doc["verdict"])
	require.Equal(t, 1, patches)
}
