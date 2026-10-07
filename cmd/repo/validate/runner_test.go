package validate

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

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
