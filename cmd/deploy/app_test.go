package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMain lets a copy of this test executable stand in for kubectl on
// every platform, without a shell, a real plugin, or a cluster connection.
func TestMain(m *testing.M) {
	if mode := os.Getenv("DEVCTL_DEPLOY_FIXTURE_MODE"); mode != "" {
		dir, err := os.Getwd()
		if err != nil {
			os.Exit(90)
		}
		b, err := json.Marshal(kubectlCall{Args: os.Args[1:], Dir: dir})
		if err != nil || os.WriteFile(os.Getenv("DEVCTL_DEPLOY_FIXTURE_CALL"), b, 0o600) != nil {
			os.Exit(91)
		}
		fmt.Fprintln(os.Stdout, "fixture kubectl stdout")
		fmt.Fprintln(os.Stderr, "fixture kubectl stderr")
		switch mode {
		case "cancel":
			// The parent cancels on stdout delivery. This watchdog makes a
			// broken cancellation path finish without leaking a helper process.
			<-time.After(2 * time.Second)
		case "failure":
			os.Exit(9)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type kubectlCall struct {
	Args []string
	Dir  string
}

func fixtureKubectl(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	source, err := os.Open(executable)
	require.NoError(t, err)
	t.Cleanup(func() { _ = source.Close() })
	bin := t.TempDir()
	name := "kubectl"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target, err := os.OpenFile(filepath.Join(bin, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700)
	require.NoError(t, err)
	_, err = io.Copy(target, source)
	require.NoError(t, target.Close())
	require.NoError(t, err)
	t.Setenv("PATH", bin)
	call := filepath.Join(t.TempDir(), "call.json")
	t.Setenv("DEVCTL_DEPLOY_FIXTURE_MODE", mode)
	t.Setenv("DEVCTL_DEPLOY_FIXTURE_CALL", call)
	return call
}

func TestAddAppArgumentsAndFailure(t *testing.T) {
	for _, mode := range []string{"success", "failure"} {
		t.Run(mode, func(t *testing.T) {
			callFile := fixtureKubectl(t, mode)
			var stdout, stderr bytes.Buffer
			r := &runner{Flag: validFlag(), Stdout: &stdout, Stderr: &stderr}
			dir := t.TempDir()
			err := r.addApp(t.Context(), dir)
			if mode == "failure" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			data, err := os.ReadFile(callFile)
			require.NoError(t, err)
			var call kubectlCall
			require.NoError(t, json.Unmarshal(data, &call))
			require.Equal(t, []string{
				"gs", "gitops", "add", "app",
				"--app", "prometheus-operator",
				"--version", "v1.2.3",
				"--catalog", "giantswarm",
				"--target-namespace", "monitoring",
				"--management-cluster", "gazelle",
				"--workload-cluster", "operations",
				"--organization", "giantswarm",
			}, call.Args)
			wantDir, err := filepath.EvalSymlinks(dir)
			require.NoError(t, err)
			gotDir, err := filepath.EvalSymlinks(call.Dir)
			require.NoError(t, err)
			require.Equal(t, wantDir, gotDir)
			require.Equal(t, "fixture kubectl stdout\n", stdout.String())
			require.Equal(t, "fixture kubectl stderr\n", stderr.String())
		})
	}
}

type cancelOnWrite struct {
	cancel context.CancelFunc
}

func (w cancelOnWrite) Write(p []byte) (int, error) {
	w.cancel()
	return len(p), nil
}

func TestAddAppCancellation(t *testing.T) {
	for _, when := range []string{"before start", "while running"} {
		t.Run(when, func(t *testing.T) {
			callFile := fixtureKubectl(t, "cancel")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := &runner{Flag: validFlag(), Stdout: cancelOnWrite{cancel: cancel}, Stderr: io.Discard}
			if when == "before start" {
				cancel()
			}
			require.ErrorIs(t, r.addApp(ctx, t.TempDir()), context.Canceled)
			_, err := os.Stat(callFile)
			if when == "before start" {
				require.ErrorIs(t, err, os.ErrNotExist, "a canceled command must not start kubectl")
			} else {
				require.NoError(t, err, "kubectl must have started before cancellation")
			}
		})
	}
}
