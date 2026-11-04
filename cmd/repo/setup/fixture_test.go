package setup

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

type setupAPITransport func(*http.Request) (*http.Response, error)

func (f setupAPITransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func setupAPIResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}
func setupRepositoryJSON(branch string, archived bool) string {
	return fmt.Sprintf(`{"name":"tool","full_name":"example/tool","owner":{"login":"example"},"default_branch":%q,"archived":%t,"has_issues":true,"has_wiki":false,"has_projects":false,"allow_merge_commit":false,"allow_squash_merge":true,"allow_rebase_merge":false,"allow_update_branch":true,"allow_auto_merge":true,"delete_branch_on_merge":true,"squash_merge_commit_title":"PR_TITLE"}`, branch, archived)
}
func setupTestRunner(t *testing.T, args ...string) (*runner, *bytes.Buffer) {
	t.Helper()
	f := &flag{}
	cmd := &cobra.Command{}
	f.Init(cmd)
	args = append([]string{"--renovate=false", "--output=json"}, args...)
	require.NoError(t, cmd.ParseFlags(args))
	require.NoError(t, f.Validate())
	output := &bytes.Buffer{}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return &runner{flag: f, logger: logger, stdout: output, stderr: io.Discard}, output
}
func setupTestClient(t *testing.T, dryRun bool, transport setupAPITransport) *githubclient.Client {
	t.Helper()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	client, err := githubclient.New(githubclient.Config{Logger: logger, AccessToken: "synthetic-setup-test-token", DryRun: dryRun, Transport: transport})
	require.NoError(t, err)
	return client
}
