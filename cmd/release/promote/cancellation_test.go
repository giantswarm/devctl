package promote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/project"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
)

// The public command must carry ExecuteContext's cancellation into its
// requests, so cancelling a preflight cannot dispatch a stable release.
func TestCommandCancellationBeforeDispatch(t *testing.T) {
	for _, stage := range []string{"before execution", "reading candidate status", "uncancelled control"} {
		t.Run(stage, func(t *testing.T) {
			// The real version gate and credential resolver stay offline and
			// never consult a user's keychain or inherited token.
			t.Setenv("DEVCTL_UNSAFE_FORCE_VERSION", project.Version())
			t.Setenv(authstore.EnvCI, "true")
			for _, name := range authstore.GitHubEnvVars {
				t.Setenv(name, "")
			}
			t.Setenv("DEVCTL_GITHUB_TOKEN", "ghu_synthetic_cancellation_test")
			t.Setenv(agentcli.EnvKeyringFile, filepath.Join(t.TempDir(), "unused-keyring.json"))
			t.Setenv(agentcli.EnvGitHubAPIURL, "https://promote.invalid")
			t.Setenv(agentcli.EnvTimeScale, "0.001")

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			script, err := sequence.New(promoteRoutes("giantswarm/kserve"))
			require.NoError(t, err)
			transport := &cancellationTransport{script: script, cancel: cancel}
			switch stage {
			case "before execution":
				cancel()
			case "reading candidate status":
				transport.cancelPath = "/repos/giantswarm/kserve/commits/v1.3.0-rc.1/status"
			}
			base := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = base })

			var stdout, stderr bytes.Buffer
			cmd, err := New(Config{Stdout: &stdout, Stderr: &stderr})
			require.NoError(t, err)
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			// Match the containing devctl command's error-output policy.
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs([]string{"giantswarm/kserve"})
			err = cmd.ExecuteContext(ctx)

			var doc releasepromote.Document
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc), "stdout must contain exactly one document")
			require.Empty(t, stderr.String())
			require.Equal(t, releasepromote.Command, doc.Command)
			require.Equal(t, agentcli.Exit(err), doc.ExitCode)
			require.Len(t, doc.Repositories, 1)

			var writes []string
			for _, request := range script.Requests() {
				if request.Method != http.MethodGet && request.Method != http.MethodHead {
					writes = append(writes, request.String())
				}
			}
			if stage == "uncancelled control" {
				require.NoError(t, err)
				require.Equal(t, agentcli.VerdictGreen, doc.Verdict)
				require.Equal(t, releasepromote.StateDispatched, doc.Repositories[0].State)
				require.Equal(t, []string{"POST /repos/giantswarm/kserve/actions/workflows/zz_generated.auto_release.yaml/dispatches"}, writes)
				require.NotNil(t, doc.Repositories[0].Run)
				require.Equal(t, "success", doc.Repositories[0].Run.Conclusion)
				return
			}

			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Equal(t, agentcli.ExitRed, agentcli.Exit(err), "%v", err)
			require.Equal(t, agentcli.VerdictRed, doc.Verdict)
			require.Equal(t, releasepromote.StateFailed, doc.Repositories[0].State)
			require.Contains(t, doc.Repositories[0].Message, context.Canceled.Error())
			require.Nil(t, doc.Repositories[0].Run)
			require.Empty(t, writes, "cancellation before dispatch must prevent workflow writes")
			if stage == "before execution" {
				require.Empty(t, script.Requests())
			} else {
				require.True(t, transport.cancelledRequest, "the in-flight request must inherit the caller's cancellation")
				require.Equal(t, "v1.3.0-rc.1", doc.Repositories[0].Candidate)
			}
		})
	}
}

// cancellationTransport serves synthetic responses entirely in memory. It
// honours request cancellation like a network transport; if the command
// loses its caller's context, the otherwise valid fixture allows dispatch.
type cancellationTransport struct {
	script           *sequence.Script
	cancel           context.CancelFunc
	cancelPath       string
	cancelledRequest bool
}

func (t *cancellationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "promote.invalid" {
		return nil, fmt.Errorf("unexpected fixture host %q", req.URL.Host)
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	response, ok := t.script.Next(req)
	if !ok {
		response = githubmock.NotFound
	}
	if req.URL.Path == t.cancelPath {
		t.cancel()
		if err := req.Context().Err(); err != nil {
			t.cancelledRequest = true
			return nil, err
		}
	}
	recorder := httptest.NewRecorder()
	if err := sequence.Write(recorder, req, response); err != nil {
		return nil, err
	}
	return recorder.Result(), nil
}
