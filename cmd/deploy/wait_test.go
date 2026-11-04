package deploy

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mergeWaitFunc func(context.Context, string, string, int, time.Duration) error

func (f mergeWaitFunc) WaitForPRMerge(ctx context.Context, owner, repo string, number int, timeout time.Duration) error {
	return f(ctx, owner, repo, number, timeout)
}

type deploymentWaitFunc func(context.Context, string, string, string, time.Duration) error

func (f deploymentWaitFunc) WaitForAppDeployment(ctx context.Context, app, namespace, cluster string, timeout time.Duration) error {
	return f(ctx, app, namespace, cluster, timeout)
}

func TestDeploymentWaitTimeouts(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want time.Duration
	}{
		{name: "default", want: 5 * time.Minute},
		{name: "seconds", args: []string{"--timeout", "30s"}, want: 30 * time.Second},
		{name: "fractional", args: []string{"--timeout", "250ms"}, want: 250 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &flag{}
			cmd := &cobra.Command{}
			f.Init(cmd)
			require.NoError(t, cmd.ParseFlags(append([]string{"--app-name", "hello-world", "--app-version", "2.8.1"}, tc.args...)))
			require.NoError(t, f.Validate())
			r := &runner{Flag: f}
			ctx := t.Context()
			var calls []string
			merge := mergeWaitFunc(func(got context.Context, owner, repo string, number int, timeout time.Duration) error {
				calls = append(calls, "merge")
				require.Equal(t, ctx, got)
				require.Equal(t, "fixture-owner", owner)
				require.Equal(t, "fixture-gitops", repo)
				require.Equal(t, 42, number)
				assert.Equal(t, tc.want, timeout, "merge must use the parsed duration")
				return nil
			})
			deployment := deploymentWaitFunc(func(got context.Context, app, namespace, cluster string, timeout time.Duration) error {
				calls = append(calls, "deployment")
				require.Equal(t, ctx, got)
				require.Equal(t, "operations-hello-world", app)
				require.Equal(t, "org-giantswarm", namespace)
				require.Equal(t, "gazelle", cluster)
				assert.Equal(t, tc.want, timeout, "deployment must use the parsed duration")
				return nil
			})
			require.NoError(t, r.waitForDeployment(ctx, "fixture-owner", "fixture-gitops", 42, merge, deployment))
			require.Equal(t, []string{"merge", "deployment"}, calls)
		})
	}
}

func TestDeploymentWaitFailures(t *testing.T) {
	for _, failAt := range []string{"merge", "deployment"} {
		t.Run(failAt, func(t *testing.T) {
			r := &runner{Flag: validFlag()}
			failure := errors.New("synthetic wait failure")
			var calls []string
			merge := mergeWaitFunc(func(context.Context, string, string, int, time.Duration) error {
				calls = append(calls, "merge")
				if failAt == "merge" {
					return failure
				}
				return nil
			})
			deployment := deploymentWaitFunc(func(context.Context, string, string, string, time.Duration) error {
				calls = append(calls, "deployment")
				return failure
			})
			require.ErrorIs(t, r.waitForDeployment(t.Context(), "fixture-owner", "fixture-gitops", 42, merge, deployment), failure)
			want := []string{"merge"}
			if failAt == "deployment" {
				want = append(want, "deployment")
			}
			require.Equal(t, want, calls)
		})
	}
}

func TestDeploymentRejectsNonpositiveTimeout(t *testing.T) {
	// No token in CI keeps a broken validation path offline: it would return
	// an authentication error instead of reaching any real GitHub operation.
	t.Setenv("CI", "1")
	for _, name := range []string{"DEVCTL_GITHUB_TOKEN", "GITHUB_TOKEN", "OPSCTL_GITHUB_TOKEN"} {
		t.Setenv(name, "")
	}
	for _, timeout := range []string{"0s", "-1s"} {
		t.Run(timeout, func(t *testing.T) {
			cmd, err := New(Config{Logger: logrus.New(), Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs([]string{"--app-name", "hello-world", "--app-version", "2.8.1", "--timeout", timeout})
			err = cmd.ExecuteContext(t.Context())
			require.ErrorContains(t, err, "--timeout must be positive")
		})
	}
}
