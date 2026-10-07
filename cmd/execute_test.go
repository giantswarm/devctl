package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// TestExecuteAuthRequired: a command for people that returns the resolver's
// ErrAuthRequired, masked as the commands do, prints the one sentence naming
// the GitHub login and exits 8.
func TestExecuteAuthRequired(t *testing.T) {
	for _, name := range append([]string{authstore.EnvCI}, authstore.GitHubEnvVars...) {
		t.Setenv(name, "")
	}
	t.Setenv(agentcli.EnvKeyringFile, filepath.Join(t.TempDir(), "keyring.json"))

	root := &cobra.Command{
		Use:           "devctl",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := authstore.ResolveGitHub(cmd.Context())
			return microerror.Mask(err)
		},
	}
	root.SetArgs(nil)
	var stderr bytes.Buffer
	if code := Execute(root, &stderr); code != agentcli.ExitAuthRequired {
		t.Errorf("exit %d, want %d", code, agentcli.ExitAuthRequired)
	}
	want := "Error: GitHub authentication required (no token in the keychain): run `devctl auth login --github-only`.\n"
	if stderr.String() != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", stderr.String(), want)
	}
}
