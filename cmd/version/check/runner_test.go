package check

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/giantswarm/micrologger"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// A canceled command must stop before constructing the updater, resolving a
// token, asking for releases or refreshing the cache.
func TestCheckHonorsACanceledCommandContext(t *testing.T) {
	// If cancellation is lost, fail construction before any request or token
	// lookup, even when the test binary carries a release version.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(agentcli.EnvGitHubAPIURL, "://no-scheme")
	logger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd, err := New(Config{Logger: logger, Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--no-cache"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("check: %v, want context cancellation", err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("canceled check wrote stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}
